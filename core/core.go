//go:build !wasi && !wasm

package core

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/pierrec/lz4/v4"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	lz4ReaderPool   = sync.Pool{New: func() any { return lz4.NewReader(nil) }}
	bufReaderPool   = sync.Pool{New: func() any { return bufio.NewReader(nil) }}
	bytesReaderPool = sync.Pool{New: func() any { return bytes.NewReader(nil) }}
	bufferPool      = sync.Pool{New: func() any { return new(bytes.Buffer) }}
	Lz4WriterPool   = sync.Pool{New: func() any { return lz4.NewWriter(nil) }}
)

// maxPooledBuffer bounds the capacity of buffers we keep in the pool to avoid
// pinning oversize allocations after a single large response.
const maxPooledBuffer = 1 << 20

// GetBuffer returns a recycled bytes.Buffer reset to empty.
func GetBuffer() *bytes.Buffer {
	b := bufferPool.Get().(*bytes.Buffer)
	b.Reset()

	return b
}

// PutBuffer returns a bytes.Buffer to the pool unless its capacity exceeds the retention cap.
func PutBuffer(b *bytes.Buffer) {
	if b == nil || b.Cap() > maxPooledBuffer {
		return
	}

	bufferPool.Put(b)
}

type Storer interface {
	MapKeys(prefix string) map[string]string
	ListKeys() []string
	Get(key string) []byte
	Set(key string, value []byte, duration time.Duration) error
	Delete(key string)
	DeleteMany(key string)
	Init() error
	Name() string
	Uuid() string
	Reset() error

	// Multi level storer to handle fresh/stale at once
	GetMultiLevel(key string, req *http.Request, validator *Revalidator) (fresh *http.Response, stale *http.Response)
	SetMultiLevel(baseKey, variedKey string, value []byte, variedHeaders http.Header, etag string, duration time.Duration, realKey string) error
}

// CacheProvider config.
type CacheProvider struct {
	// URL to connect to the storage system.
	URL string `json:"url" yaml:"url"`
	// Path to the configuration file.
	Path string `json:"path" yaml:"path"`
	// Declare the cache provider directly in the Souin configuration.
	Configuration any `json:"configuration" yaml:"configuration"`
}

const (
	DISABLE_VARY_CTX   = "storages_bypass_vary"
	MappingKeyPrefix   = "IDX_"
	SurrogateKeyPrefix = "SURROGATE_"
)

func DecodeMapping(item []byte) (*StorageMapper, error) {
	mapping := &StorageMapper{}
	e := proto.Unmarshal(item, mapping)

	return mapping, e
}

// pooledLZ4Body wraps an LZ4-decompressed HTTP response body and returns its
// underlying readers to their pools on Close. Read/Close are idempotent against
// Close-after-Close.
type pooledLZ4Body struct {
	body   io.ReadCloser
	br     *bufio.Reader
	lz4r   *lz4.Reader
	bytr   *bytes.Reader
	closed bool
}

func (p *pooledLZ4Body) Read(b []byte) (int, error) {
	return p.body.Read(b)
}

func (p *pooledLZ4Body) Close() error {
	if p.closed {
		return nil
	}

	p.closed = true

	err := p.body.Close()

	if p.br != nil {
		bufReaderPool.Put(p.br)
		p.br = nil
	}

	if p.lz4r != nil {
		lz4ReaderPool.Put(p.lz4r)
		p.lz4r = nil
	}

	if p.bytr != nil {
		p.bytr.Reset(nil)
		bytesReaderPool.Put(p.bytr)
		p.bytr = nil
	}

	return err
}

func readResponse(data []byte, req *http.Request) (*http.Response, error) {
	bytr := bytesReaderPool.Get().(*bytes.Reader)
	bytr.Reset(data)

	lz4r := lz4ReaderPool.Get().(*lz4.Reader)
	lz4r.Reset(bytr)

	brp := bufReaderPool.Get().(*bufio.Reader)
	brp.Reset(lz4r)

	resp, err := http.ReadResponse(brp, req)
	if err != nil {
		bufReaderPool.Put(brp)
		lz4ReaderPool.Put(lz4r)
		bytr.Reset(nil)
		bytesReaderPool.Put(bytr)

		return resp, err
	}

	if resp.Body == nil {
		bufReaderPool.Put(brp)
		lz4ReaderPool.Put(lz4r)
		bytr.Reset(nil)
		bytesReaderPool.Put(bytr)

		return resp, nil
	}

	resp.Body = &pooledLZ4Body{
		body: resp.Body,
		br:   brp,
		lz4r: lz4r,
		bytr: bytr,
	}

	return resp, nil
}

func MappingElection(provider Storer, item []byte, req *http.Request, validator *Revalidator, logger Logger) (resultFresh *http.Response, resultStale *http.Response, e error) {
	mapping := &StorageMapper{}

	if len(item) != 0 {
		mapping, e = DecodeMapping(item)
		if e != nil {
			return resultFresh, resultStale, e
		}
	}

	ctx := req.Context()
	useVary := true

	if v, ok := ctx.Value(DISABLE_VARY_CTX).(bool); ok && v {
		useVary = false
	}

	header := req.Header
	now := time.Now()

	for keyName, keyItem := range mapping.GetMapping() {
		if useVary {
			valid := true

			for hname, hval := range keyItem.GetVariedHeaders() {
				vals := hval.GetHeaderValue()

				var want string
				switch len(vals) {
				case 0:
				case 1:
					want = vals[0]
				default:
					want = strings.Join(vals, ", ")
				}

				if header.Get(hname) != want {
					valid = false

					break
				}
			}

			if !valid {
				continue
			}
		}

		ValidateETagFromHeader(keyItem.GetEtag(), validator)

		if !validator.Matched {
			logger.Debugf("The stored key %s didn't match the current iteration key ETag %+v", keyName, validator)

			continue
		}

		freshAt := keyItem.GetFreshTime().AsTime()
		staleAt := keyItem.GetStaleTime().AsTime()
		isFresh := now.Before(freshAt)
		isStale := now.Before(staleAt)

		if !isFresh && !isStale {
			continue
		}

		response := provider.Get(keyName)
		if response == nil {
			continue
		}

		resp, err := readResponse(response, req)
		if err != nil {
			logger.Errorf("An error occurred while reading response for the key %s: %v", keyName, err)

			return resultFresh, resultStale, err
		}

		if isFresh {
			logger.Debugf("The stored key %s matched the current iteration key ETag %+v", keyName, validator)

			resultFresh = resp

			return resultFresh, resultStale, nil
		}

		logger.Debugf("The stored key %s matched the current iteration key ETag %+v as stale", keyName, validator)
		resultStale = resp
	}

	return resultFresh, resultStale, e
}

func MappingUpdater(key string, item []byte, logger Logger, now, freshTime, staleTime time.Time, variedHeaders http.Header, etag, realKey string) (val []byte, e error) {
	mapping := &StorageMapper{}
	if len(item) != 0 {
		e = proto.Unmarshal(item, mapping)
		if e != nil {
			logger.Errorf("Impossible to decode the key %s, %v", key, e)

			return nil, e
		}
	}

	if mapping.GetMapping() == nil {
		mapping.Mapping = make(map[string]*KeyIndex, 4)
	}

	var pbvariedeheader map[string]*KeyIndexStringList
	if len(variedHeaders) > 0 {
		pbvariedeheader = make(map[string]*KeyIndexStringList, len(variedHeaders))

		for k, v := range variedHeaders {
			pbvariedeheader[k] = &KeyIndexStringList{HeaderValue: v}
		}
	}

	mapping.Mapping[key] = &KeyIndex{
		StoredAt:      timestamppb.New(now),
		FreshTime:     timestamppb.New(freshTime),
		StaleTime:     timestamppb.New(staleTime),
		VariedHeaders: pbvariedeheader,
		Etag:          etag,
		RealKey:       realKey,
	}

	val, e = proto.Marshal(mapping)
	if e != nil {
		logger.Errorf("Impossible to encode the mapping value for the key %s, %v", key, e)

		return nil, e
	}

	return val, e
}
