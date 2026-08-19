package simplefs_test

import (
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/darkweak/storages/core"
)

// payloadSizes are the response body sizes exercised by the benchmarks. They
// cover a small API response, a regular HTML page and a large asset.
var payloadSizes = []struct {
	name string
	size int
}{
	{name: "1KB", size: 1024},
	{name: "64KB", size: 64 * 1024},
	{name: "1MB", size: 1024 * 1024},
}

// BenchmarkSimplefs_Set benchmarks the in memory key/value API. Simplefs only
// writes a file through SetMultiLevel, and Get returns the raw stored bytes for
// the surrogate keys only, so the surrogate prefix is the meaningful path here.
func BenchmarkSimplefs_Set(b *testing.B) {
	for _, payload := range payloadSizes {
		b.Run(payload.name, func(b *testing.B) {
			// A bounded capacity keeps the memory usage flat while measuring
			// the insertion and the capacity eviction steady state.
			client, _ := newStorer(b, 1000, "")
			value := incompressiblePayload(payload.size, payload.name)

			// No SetBytes here: the value is kept by reference in memory, so a
			// throughput figure would be meaningless.
			b.ReportAllocs()
			b.ResetTimer()

			for i := range b.N {
				_ = client.Set(fmt.Sprintf("%sset_%d", core.SurrogateKeyPrefix, i), value, time.Minute)
			}
		})
	}
}

func BenchmarkSimplefs_Get(b *testing.B) {
	for _, payload := range payloadSizes {
		b.Run(payload.name, func(b *testing.B) {
			client, _ := newStorer(b, 1000, "")
			key := core.SurrogateKeyPrefix + "get"
			value := incompressiblePayload(payload.size, payload.name)
			_ = client.Set(key, value, time.Minute)

			b.ReportAllocs()
			b.ResetTimer()

			for range b.N {
				if len(client.Get(key)) != payload.size {
					b.Fatalf("the key %s should return %d bytes", key, payload.size)
				}
			}
		})
	}
}

func BenchmarkSimplefs_SetMultiLevel(b *testing.B) {
	for _, payload := range payloadSizes {
		b.Run(payload.name, func(b *testing.B) {
			// The capacity bounds the disk usage: an entry falling out of the
			// LRU gets its file removed by the eviction callback.
			client, _ := newStorer(b, 50, "")
			value := responsePayload(b, payload.size, payload.name)

			b.SetBytes(int64(len(value)))
			b.ReportAllocs()
			b.ResetTimer()

			for i := range b.N {
				key := fmt.Sprintf("sml_%d", i)
				if err := client.SetMultiLevel(key, key, value, http.Header{}, "", time.Minute, key); err != nil {
					b.Fatalf("impossible to store the key %s: %v", key, err)
				}
			}
		})
	}
}

func BenchmarkSimplefs_GetMultiLevel(b *testing.B) {
	for _, payload := range payloadSizes {
		b.Run(payload.name, func(b *testing.B) {
			client, _ := newStorer(b, 50, "")
			value := responsePayload(b, payload.size, payload.name)

			if err := client.SetMultiLevel("gml", "gml", value, http.Header{}, "", time.Minute, "gml"); err != nil {
				b.Fatalf("impossible to store the key gml: %v", err)
			}

			request := newRequest(b)

			// Ensure the benchmark measures a cache hit and not a lookup miss.
			if fresh, _ := client.GetMultiLevel("gml", request, &core.Revalidator{}); fresh == nil {
				b.Fatal("the key gml should return a fresh response")
			}

			b.SetBytes(int64(len(value)))
			b.ReportAllocs()
			b.ResetTimer()

			for range b.N {
				_, _ = client.GetMultiLevel("gml", request, &core.Revalidator{})
			}
		})
	}
}

// BenchmarkSimplefs_SetMultiLevel_Eviction measures the write path when the
// directory size limit forces recoverEnoughSpaceIfNeeded to evict on every
// call. See https://github.com/darkweak/storages/issues/57.
func BenchmarkSimplefs_SetMultiLevel_Eviction(b *testing.B) {
	for _, payload := range payloadSizes {
		b.Run(payload.name, func(b *testing.B) {
			// A limit of about 10 entries keeps the eviction loop hot, while the
			// capacity bounds the number of mapping entries the eviction scan
			// has to walk over.
			client, _ := newStorer(b, 1000, strconv.Itoa(10*payload.size))
			value := responsePayload(b, payload.size, payload.name)

			b.SetBytes(int64(len(value)))
			b.ReportAllocs()
			b.ResetTimer()

			for i := range b.N {
				key := fmt.Sprintf("evict_%d", i)
				if err := client.SetMultiLevel(key, key, value, http.Header{}, "", time.Minute, key); err != nil {
					b.Fatalf("impossible to store the key %s: %v", key, err)
				}
			}
		})
	}
}

// BenchmarkSimplefs_GetMultiLevel_Parallel measures the read path contention on
// the provider lock.
func BenchmarkSimplefs_GetMultiLevel_Parallel(b *testing.B) {
	client, _ := newStorer(b, 50, "")
	value := responsePayload(b, 64*1024, "parallel")

	if err := client.SetMultiLevel("parallel", "parallel", value, http.Header{}, "", time.Minute, "parallel"); err != nil {
		b.Fatalf("impossible to store the key parallel: %v", err)
	}

	b.SetBytes(int64(len(value)))
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		request := newRequest(b)

		for pb.Next() {
			_, _ = client.GetMultiLevel("parallel", request, &core.Revalidator{})
		}
	})
}

// BenchmarkSimplefs_SetMultiLevel_Parallel measures the write path contention,
// including the eviction planning, from concurrent goroutines.
func BenchmarkSimplefs_SetMultiLevel_Parallel(b *testing.B) {
	client, _ := newStorer(b, 1000, "16MB")
	value := responsePayload(b, 64*1024, "parallel")

	b.SetBytes(int64(len(value)))
	b.ReportAllocs()
	b.ResetTimer()

	var counter atomicCounter

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			key := fmt.Sprintf("parallel_%d", counter.next())
			_ = client.SetMultiLevel(key, key, value, http.Header{}, "", time.Minute, key)
		}
	})
}

func BenchmarkSimplefs_ListKeys(b *testing.B) {
	for _, entries := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("%d_entries", entries), func(b *testing.B) {
			client, _ := newStorer(b, 0, "")
			value := incompressiblePayload(256, "listkeys")

			for i := range entries {
				_ = client.Set(fmt.Sprintf("%slistkeys_%d", core.SurrogateKeyPrefix, i), value, time.Minute)
			}

			b.ReportAllocs()
			b.ResetTimer()

			for range b.N {
				if len(client.ListKeys()) == 0 {
					b.Fatal("the keys should be listed")
				}
			}
		})
	}
}

func BenchmarkSimplefs_MapKeys(b *testing.B) {
	for _, entries := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("%d_entries", entries), func(b *testing.B) {
			client, _ := newStorer(b, 0, "")
			value := incompressiblePayload(256, "mapkeys")

			for i := range entries {
				_ = client.Set(fmt.Sprintf("%smapkeys_%d", core.SurrogateKeyPrefix, i), value, time.Minute)
			}

			b.ReportAllocs()
			b.ResetTimer()

			for range b.N {
				if len(client.MapKeys(core.SurrogateKeyPrefix+"mapkeys_")) == 0 {
					b.Fatal("the keys should be mapped")
				}
			}
		})
	}
}
