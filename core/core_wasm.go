//go:build wasi || wasm

package core

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	lz4 "github.com/pierrec/lz4/v4"
)

type keyIndex struct {
	StoredAt      time.Time   `json:"stored"`
	FreshTime     time.Time   `json:"fresh"`
	StaleTime     time.Time   `json:"stale"`
	VariedHeaders http.Header `json:"varied"`
	Etag          string      `json:"etag"`
	RealKey       string      `json:"realKey"`
}

type StorageMapper struct {
	Mapping map[string]keyIndex `json:"mapping"`
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
	Configuration interface{} `json:"configuration" yaml:"configuration"`
}

const MappingKeyPrefix = "IDX_"

func DecodeMapping(item []byte) (mapping StorageMapper, e error) {
	e = json.NewDecoder(bytes.NewBuffer(item)).Decode(&mapping)

	return
}

func MappingElection(provider Storer, item []byte, req *http.Request, validator *Revalidator, logger Logger) (resultFresh *http.Response, resultStale *http.Response, e error) {
	var mapping StorageMapper

	if len(item) != 0 {
		mapping, e = DecodeMapping(item)
		if e != nil {
			return resultFresh, resultStale, e
		}
	}

	for keyName, keyItem := range mapping.Mapping {
		valid := true

		for hname, hval := range keyItem.VariedHeaders {
			if req.Header.Get(hname) != strings.Join(hval, ", ") {
				valid = false

				break
			}
		}

		if !valid {
			continue
		}

		ValidateETagFromHeader(keyItem.Etag, validator)

		if validator.Matched {
			// If the key is fresh enough.
			if time.Since(keyItem.FreshTime) < 0 {
				response := provider.Get(keyName)
				if response != nil {
					bufW := new(bytes.Buffer)
					reader := lz4.NewReader(bytes.NewBuffer(response))
					_, _ = reader.WriteTo(bufW)

					if resultFresh, e = http.ReadResponse(bufio.NewReader(bufW), req); e != nil {
						logger.Error("An error occurred while reading response for the key %s: %v", keyName, e)

						return resultFresh, resultStale, e
					}

					logger.Debug("The stored key %s matched the current iteration key ETag %+v", keyName, validator)

					return resultFresh, resultStale, e
				}
			}

			// If the key is still stale.
			if time.Since(keyItem.StaleTime) < 0 {
				response := provider.Get(keyName)
				if response != nil {
					bufW := new(bytes.Buffer)
					reader := lz4.NewReader(bytes.NewBuffer(response))
					_, _ = reader.WriteTo(bufW)

					if resultStale, e = http.ReadResponse(bufio.NewReader(bufW), req); e != nil {
						logger.Error("An error occurred while reading response for the key %s: %v", keyName, e)

						return resultFresh, resultStale, e
					}

					logger.Debug("The stored key %s matched the current iteration key ETag %+v as stale", keyName, validator)
				}
			}
		} else {
			logger.Debug("The stored key %s didn't match the current iteration key ETag %+v", keyName, validator)
		}
	}

	return resultFresh, resultStale, e
}

func MappingUpdater(key string, item []byte, logger Logger, now, freshTime, staleTime time.Time, variedHeaders http.Header, etag, realKey string) (val []byte, e error) {
	var mapping StorageMapper
	if len(item) == 0 {
		mapping = StorageMapper{}
	} else {
		e = json.NewDecoder(bytes.NewBuffer(item)).Decode(&mapping)
		if e != nil {
			logger.Errorf("Impossible to decode the key %s, %v", key, e)

			return nil, e
		}
	}

	if mapping.Mapping == nil {
		mapping.Mapping = make(map[string]keyIndex)
	}

	mapping.Mapping[key] = keyIndex{
		StoredAt:      now,
		FreshTime:     freshTime,
		StaleTime:     staleTime,
		VariedHeaders: variedHeaders,
		Etag:          etag,
		RealKey:       realKey,
	}

	buf := new(bytes.Buffer)

	e = json.NewEncoder(buf).Encode(mapping)
	if e != nil {
		logger.Errorf("Impossible to encode the mapping value for the key %s, %v", key, e)

		return nil, e
	}

	val = buf.Bytes()

	return val, e
}
