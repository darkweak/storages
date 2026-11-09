//go:build !wasm && !wasi

package core

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/pierrec/lz4/v4"
)

var benchPayloadSizes = []struct {
	name string
	size int
}{
	{name: "1KB", size: 1 << 10},
	{name: "64KB", size: 64 << 10},
	{name: "1MB", size: 1 << 20},
}

const alphabet = "abcdefghijklmnopqrstuvwxyz"

// legacyCompress is the compression every storer used to inline: a fresh
// writer with the default 4 MB block size per call.
func legacyCompress(value []byte) ([]byte, error) {
	compressed := new(bytes.Buffer)
	writer := lz4.NewWriter(compressed)

	if _, err := writer.Write(value); err != nil {
		_ = writer.Close()

		return nil, err
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	return compressed.Bytes(), nil
}

// dumpedResponse returns a serialized HTTP response whose body is a mix of
// repetitive and random bytes, so lz4 shrinks it like a real page.
func dumpedResponse(tb testing.TB, size int) []byte {
	tb.Helper()

	random := make([]byte, size)
	_, _ = rand.NewChaCha8([32]byte{}).Read(random)

	body := make([]byte, size)

	for i := range body {
		if i%4 == 0 {
			body[i] = random[i]
		} else {
			body[i] = alphabet[i%len(alphabet)]
		}
	}

	recorder := httptest.NewRecorder()
	recorder.Header().Set("Content-Type", "text/html")
	_, _ = recorder.Write(body)

	dumped, err := httputil.DumpResponse(recorder.Result(), true)
	if err != nil {
		tb.Fatalf("impossible to dump the response: %v", err)
	}

	return dumped
}

func TestCompressValue_RoundTrip(t *testing.T) {
	for _, size := range []int{0, 1, 1 << 10, 64<<10 - 1, 64 << 10, 64<<10 + 1, 1 << 20, 5 << 20} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			value := bytes.Repeat([]byte("souin"), size/5+1)[:size]

			compressed, err := CompressValue(value)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			decompressed, err := io.ReadAll(lz4.NewReader(bytes.NewReader(compressed)))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !bytes.Equal(decompressed, value) {
				t.Fatalf("the round trip altered the value of %d bytes", size)
			}
		})
	}
}

// TestCompressValue_Interoperability ensures the frames written before and
// after the switch to the smaller block size can all be read back.
func TestCompressValue_Interoperability(t *testing.T) {
	dumped := dumpedResponse(t, 300<<10)
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)

	legacy, err := legacyCompress(dumped)
	if err != nil {
		t.Fatal(err)
	}

	current, err := CompressValue(dumped)
	if err != nil {
		t.Fatal(err)
	}

	for name, frame := range map[string][]byte{"legacy": legacy, "current": current} {
		response, err := readResponse(frame, request)
		if err != nil {
			t.Fatalf("%s: impossible to read the response: %v", name, err)
		}

		body, _ := io.ReadAll(response.Body)
		if len(body) != 300<<10 {
			t.Fatalf("%s: unexpected body size %d", name, len(body))
		}
	}
}

func TestCompressValue_OwnedResult(t *testing.T) {
	first, _ := CompressValue([]byte("first value"))
	snapshot := bytes.Clone(first)

	var group sync.WaitGroup

	for i := range 32 {
		group.Go(func() {
			_, _ = CompressValue(bytes.Repeat([]byte(strconv.Itoa(i)), 4096))
		})
	}

	group.Wait()

	if !bytes.Equal(first, snapshot) {
		t.Fatal("a pooled buffer was shared with a returned value")
	}

	// Allow the allocator size class rounding, but no pinned scratch buffer.
	if cap(first) > 2*len(first) {
		t.Fatalf("the returned value should not pin a scratch buffer, len %d cap %d", len(first), cap(first))
	}
}

// nopLogger discards everything, core has no logger implementation.
type nopLogger struct {
	Logger
}

func (nopLogger) Debugf(string, ...interface{}) {}
func (nopLogger) Errorf(string, ...interface{}) {}

type benchStorer struct {
	Storer

	values map[string][]byte
}

func (b *benchStorer) Get(key string) []byte { return b.values[key] }

func BenchmarkCompress(b *testing.B) {
	for _, payload := range benchPayloadSizes {
		value := dumpedResponse(b, payload.size)

		b.Run("legacy/"+payload.name, func(b *testing.B) {
			b.SetBytes(int64(len(value)))
			b.ReportAllocs()

			for range b.N {
				_, _ = legacyCompress(value)
			}
		})

		b.Run("pooled/"+payload.name, func(b *testing.B) {
			b.SetBytes(int64(len(value)))
			b.ReportAllocs()

			for range b.N {
				_, _ = CompressValue(value)
			}
		})
	}
}

// BenchmarkReadResponse measures the read path of a stored response, reading
// the whole body the way a client would.
func BenchmarkReadResponse(b *testing.B) {
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)

	for _, payload := range benchPayloadSizes {
		value := dumpedResponse(b, payload.size)
		legacy, _ := legacyCompress(value)
		current, _ := CompressValue(value)

		for name, frame := range map[string][]byte{"legacy": legacy, "pooled": current} {
			b.Run(name+"/"+payload.name, func(b *testing.B) {
				b.SetBytes(int64(len(value)))
				b.ReportAllocs()

				for range b.N {
					response, err := readResponse(frame, request)
					if err != nil {
						b.Fatal(err)
					}

					_, _ = io.Copy(io.Discard, response.Body)
					_ = response.Body.Close()
				}
			})
		}
	}
}

// BenchmarkMappingElection measures the election among several variants, the
// matching one being a single small response.
func BenchmarkMappingElection(b *testing.B) {
	for _, variants := range []int{1, 10, 100} {
		b.Run(fmt.Sprintf("%d_variants", variants), func(b *testing.B) {
			now := time.Now()
			provider := &benchStorer{values: map[string][]byte{}}

			var mapping []byte

			for i := range variants {
				key := fmt.Sprintf("variant-%d", i)
				headers := http.Header{"Accept-Encoding": {fmt.Sprintf("enc-%d", i)}, "Accept-Language": {"en"}}

				var err error

				mapping, err = MappingUpdater(key, mapping, nopLogger{}, now, now.Add(time.Hour), now.Add(2*time.Hour), headers, "", key)
				if err != nil {
					b.Fatal(err)
				}
			}

			provider.values[fmt.Sprintf("variant-%d", variants-1)], _ = CompressValue(dumpedResponse(b, 1<<10))

			request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
			request.Header.Set("Accept-Encoding", fmt.Sprintf("enc-%d", variants-1))
			request.Header.Set("Accept-Language", "en")

			b.ReportAllocs()

			for range b.N {
				fresh, _, err := MappingElection(provider, mapping, request, &Revalidator{}, nopLogger{})
				if err != nil || fresh == nil {
					b.Fatalf("a fresh response was expected, got %v", err)
				}
			}
		})
	}
}

// TestMappingTTL ensures MappingTTL matches the formatted computation the
// storers used to run, for every nanosecond boundary that matters.
func TestMappingTTL(t *testing.T) {
	for _, nanosecond := range []int{0, 1, 2, 500_000_000, 999_999_999} {
		legacy, err := time.ParseDuration(fmt.Sprintf("-%ds", nanosecond*2))
		if err != nil {
			t.Fatal(err)
		}

		if current := -time.Duration(nanosecond*2) * time.Second; current != legacy {
			t.Errorf("nanosecond %d: got %v, want %v", nanosecond, current, legacy)
		}
	}

	if ttl := MappingTTL(); ttl > 0 || ttl < -2*time.Duration(time.Second.Nanoseconds())*time.Second {
		t.Errorf("unexpected mapping TTL %v", ttl)
	}
}
