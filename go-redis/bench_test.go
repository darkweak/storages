package redis_test

import (
	"context"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darkweak/storages/core"
)

// benchPayloadSizes are the response body sizes exercised by the benchmarks:
// a small API response and a regular HTML page.
var benchPayloadSizes = []struct {
	name string
	size int
}{
	{name: "1KB", size: 1 << 10},
	{name: "64KB", size: 64 << 10},
}

const benchAlphabet = "abcdefghijklmnopqrstuvwxyz"

// benchTTL bounds the lifetime of anything the benchmarks leave behind.
const benchTTL = 10 * time.Minute

var benchKeyspace atomic.Int64

// benchPrefix returns a key prefix unique to the caller, so the benchmarks
// never step on each other's keys when they share a storage.
func benchPrefix(name string) string {
	return fmt.Sprintf("bench_%s_%d_%d_", name, time.Now().UnixNano(), benchKeyspace.Add(1))
}

// benchResponse returns a serialized HTTP response whose body mixes
// repetitive and random bytes, so lz4 shrinks it like a real page.
func benchResponse(tb testing.TB, size int) []byte {
	tb.Helper()

	random := make([]byte, size)
	_, _ = rand.NewChaCha8([32]byte{}).Read(random)

	body := make([]byte, size)

	for i := range body {
		if i%4 == 0 {
			body[i] = random[i]
		} else {
			body[i] = benchAlphabet[i%len(benchAlphabet)]
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

// skipUnsupported skips the benchmarks listed in benchUnsupported, the
// operations the storer doesn't implement in a measurable way.
func skipUnsupported(tb testing.TB, operation string) {
	tb.Helper()

	if reason, found := benchUnsupported[operation]; found {
		tb.Skip(reason)
	}
}

func BenchmarkGoRedis_Set(b *testing.B) {
	skipUnsupported(b, "Set")

	for _, payload := range benchPayloadSizes {
		b.Run(payload.name, func(b *testing.B) {
			client, prefix := newBenchStorer(b)
			value := benchResponse(b, payload.size)

			b.SetBytes(int64(len(value)))
			b.ReportAllocs()
			b.ResetTimer()

			for i := range b.N {
				if err := client.Set(prefix+strconv.Itoa(i%1000), value, benchTTL); err != nil {
					b.Fatalf("impossible to store the key: %v", err)
				}
			}
		})
	}
}

func BenchmarkGoRedis_Get(b *testing.B) {
	skipUnsupported(b, "Get")

	for _, payload := range benchPayloadSizes {
		b.Run(payload.name, func(b *testing.B) {
			client, prefix := newBenchStorer(b)
			key := prefix + "get"
			value := benchResponse(b, payload.size)

			if err := client.Set(key, value, benchTTL); err != nil {
				b.Fatalf("impossible to store the key: %v", err)
			}

			b.SetBytes(int64(len(value)))
			b.ReportAllocs()
			b.ResetTimer()

			for range b.N {
				if len(client.Get(key)) != len(value) {
					b.Fatalf("the key %s should return %d bytes", key, len(value))
				}
			}
		})
	}
}

func BenchmarkGoRedis_SetMultiLevel(b *testing.B) {
	skipUnsupported(b, "SetMultiLevel")

	for _, payload := range benchPayloadSizes {
		b.Run(payload.name, func(b *testing.B) {
			client, prefix := newBenchStorer(b)
			value := benchResponse(b, payload.size)

			b.SetBytes(int64(len(value)))
			b.ReportAllocs()
			b.ResetTimer()

			for i := range b.N {
				key := prefix + strconv.Itoa(i%1000)
				if err := client.SetMultiLevel(key, key, value, http.Header{}, "", benchTTL, key); err != nil {
					b.Fatalf("impossible to store the key %s: %v", key, err)
				}
			}
		})
	}
}

// BenchmarkGoRedis_SetMultiLevel_Variants measures the mapping update cost
// when a base key already references several variants.
func BenchmarkGoRedis_SetMultiLevel_Variants(b *testing.B) {
	skipUnsupported(b, "SetMultiLevel_Variants")

	client, prefix := newBenchStorer(b)
	value := benchResponse(b, 1<<10)
	baseKey := prefix + "variants"

	b.SetBytes(int64(len(value)))
	b.ReportAllocs()
	b.ResetTimer()

	for i := range b.N {
		variant := strconv.Itoa(i % 10)
		variedKey := baseKey + "-" + variant
		headers := http.Header{acceptEncoding: {variant}}

		if err := client.SetMultiLevel(baseKey, variedKey, value, headers, "", benchTTL, variedKey); err != nil {
			b.Fatalf("impossible to store the key %s: %v", variedKey, err)
		}
	}
}

func BenchmarkGoRedis_GetMultiLevel(b *testing.B) {
	skipUnsupported(b, "GetMultiLevel")

	for _, payload := range benchPayloadSizes {
		b.Run(payload.name, func(b *testing.B) {
			client, prefix := newBenchStorer(b)
			value := benchResponse(b, payload.size)
			key := prefix + "gml"

			if err := client.SetMultiLevel(key, key, value, http.Header{}, "", benchTTL, key); err != nil {
				b.Fatalf("impossible to store the key %s: %v", key, err)
			}

			request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)

			// Ensure the benchmark measures a cache hit and not a lookup miss.
			if fresh, _ := client.GetMultiLevel(key, request, new(core.Revalidator)); fresh == nil {
				b.Fatalf("the key %s should return a fresh response", key)
			}

			b.SetBytes(int64(len(value)))
			b.ReportAllocs()
			b.ResetTimer()

			for range b.N {
				fresh, _ := client.GetMultiLevel(key, request, new(core.Revalidator))
				if fresh == nil {
					b.Fatalf("the key %s should return a fresh response", key)
				}

				// Serve the body the way a client would, to the end.
				_, _ = io.Copy(io.Discard, fresh.Body)
			}
		})
	}
}

// BenchmarkGoRedis_GetMultiLevel_Parallel measures concurrent reads of a 64KB
// response, each served to the end.
func BenchmarkGoRedis_GetMultiLevel_Parallel(b *testing.B) {
	skipUnsupported(b, "GetMultiLevel")

	client, prefix := newBenchStorer(b)
	value := benchResponse(b, 64<<10)
	key := prefix + "parallel"

	if err := client.SetMultiLevel(key, key, value, http.Header{}, "", benchTTL, key); err != nil {
		b.Fatalf("impossible to store the key %s: %v", key, err)
	}

	b.SetBytes(int64(len(value)))
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)

		for pb.Next() {
			fresh, _ := client.GetMultiLevel(key, request, new(core.Revalidator))
			if fresh == nil {
				b.Errorf("the key %s should return a fresh response", key)

				return
			}

			_, _ = io.Copy(io.Discard, fresh.Body)
		}
	})
}

func BenchmarkGoRedis_ListKeys(b *testing.B) {
	skipUnsupported(b, "ListKeys")

	client, prefix := newBenchStorer(b)
	value := benchResponse(b, 256)

	for i := range benchListedEntries {
		key := prefix + strconv.Itoa(i)
		if err := client.SetMultiLevel(key, key, value, http.Header{}, "", benchTTL, key); err != nil {
			b.Fatalf("impossible to store the key %s: %v", key, err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		if len(client.ListKeys()) < benchListedEntries {
			b.Fatalf("at least %d keys should be listed", benchListedEntries)
		}
	}
}

func BenchmarkGoRedis_MapKeys(b *testing.B) {
	skipUnsupported(b, "MapKeys")

	client, prefix := newBenchStorer(b)
	value := benchResponse(b, 256)

	for i := range benchListedEntries {
		if err := client.Set(prefix+strconv.Itoa(i), value, benchTTL); err != nil {
			b.Fatalf("impossible to store the key: %v", err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		if len(client.MapKeys(prefix)) != benchListedEntries {
			b.Fatalf("%d keys should be mapped", benchListedEntries)
		}
	}
}

// BenchmarkGoRedis_DeleteMany measures a regexp purge removing
// benchDeletedEntries keys among benchListedEntries.
func BenchmarkGoRedis_DeleteMany(b *testing.B) {
	skipUnsupported(b, "DeleteMany")

	client, prefix := newBenchStorer(b)
	value := benchResponse(b, 256)

	for i := range benchListedEntries {
		if err := client.Set(prefix+"keep_"+strconv.Itoa(i), value, benchTTL); err != nil {
			b.Fatalf("impossible to store the key: %v", err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		b.StopTimer()

		for i := range benchDeletedEntries {
			if err := client.Set(prefix+"drop_"+strconv.Itoa(i), value, benchTTL); err != nil {
				b.Fatalf("impossible to store the key: %v", err)
			}
		}

		b.StartTimer()

		client.DeleteMany("^" + prefix + "drop_")
	}

	b.StopTimer()

	if got := client.Get(prefix + "drop_0"); len(got) != 0 {
		b.Fatal("the matching keys should have been deleted")
	}

	if got := client.Get(prefix + "keep_0"); len(got) == 0 {
		b.Fatal("the other keys should have been kept")
	}
}
