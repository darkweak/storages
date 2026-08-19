package redis_test

import (
	"fmt"
	"net/http"
	"strconv"
	"testing"

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

func BenchmarkRedis_Set(b *testing.B) {
	for _, payload := range payloadSizes {
		b.Run(payload.name, func(b *testing.B) {
			client, prefix := newStorer(b, "set")
			value := incompressiblePayload(payload.size, payload.name)

			b.SetBytes(int64(payload.size))
			b.ReportAllocs()
			b.ResetTimer()

			for i := range b.N {
				if err := client.Set(prefix+strconv.Itoa(i), value, keyTTL); err != nil {
					b.Fatalf("impossible to store the key: %v", err)
				}
			}
		})
	}
}

func BenchmarkRedis_Get(b *testing.B) {
	for _, payload := range payloadSizes {
		b.Run(payload.name, func(b *testing.B) {
			client, prefix := newStorer(b, "get")
			key := prefix + "key"
			value := incompressiblePayload(payload.size, payload.name)

			if err := client.Set(key, value, keyTTL); err != nil {
				b.Fatalf("impossible to store the key: %v", err)
			}

			b.SetBytes(int64(payload.size))
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

func BenchmarkRedis_SetMultiLevel(b *testing.B) {
	for _, payload := range payloadSizes {
		b.Run(payload.name, func(b *testing.B) {
			client, prefix := newStorer(b, "sml")
			value := responsePayload(b, payload.size, payload.name)

			b.SetBytes(int64(len(value)))
			b.ReportAllocs()
			b.ResetTimer()

			for i := range b.N {
				key := prefix + strconv.Itoa(i)
				if err := client.SetMultiLevel(key, key, value, http.Header{}, "", keyTTL, key); err != nil {
					b.Fatalf("impossible to store the key %s: %v", key, err)
				}
			}
		})
	}
}

func BenchmarkRedis_GetMultiLevel(b *testing.B) {
	for _, payload := range payloadSizes {
		b.Run(payload.name, func(b *testing.B) {
			client, prefix := newStorer(b, "gml")
			key := prefix + "key"
			value := responsePayload(b, payload.size, payload.name)

			if err := client.SetMultiLevel(key, key, value, http.Header{}, "", keyTTL, key); err != nil {
				b.Fatalf("impossible to store the key %s: %v", key, err)
			}

			request := newRequest(b)

			// Ensure the benchmark measures a cache hit and not a lookup miss.
			if fresh, _ := client.GetMultiLevel(key, request, &core.Revalidator{}); fresh == nil {
				b.Fatalf("the key %s should return a fresh response", key)
			}

			b.SetBytes(int64(len(value)))
			b.ReportAllocs()
			b.ResetTimer()

			for range b.N {
				_, _ = client.GetMultiLevel(key, request, &core.Revalidator{})
			}
		})
	}
}

// BenchmarkRedis_GetMultiLevel_Parallel measures the read path when several
// goroutines share the connection, which is where the rueidis pipelining pays
// off compared to the sequential benchmark.
func BenchmarkRedis_GetMultiLevel_Parallel(b *testing.B) {
	client, prefix := newStorer(b, "gml_parallel")
	key := prefix + "key"
	value := responsePayload(b, 64*1024, "parallel")

	if err := client.SetMultiLevel(key, key, value, http.Header{}, "", keyTTL, key); err != nil {
		b.Fatalf("impossible to store the key %s: %v", key, err)
	}

	b.SetBytes(int64(len(value)))
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		request := newRequest(b)

		for pb.Next() {
			_, _ = client.GetMultiLevel(key, request, &core.Revalidator{})
		}
	})
}

func BenchmarkRedis_SetMultiLevel_Parallel(b *testing.B) {
	client, prefix := newStorer(b, "sml_parallel")
	value := responsePayload(b, 64*1024, "parallel")

	var counter atomicCounter

	b.SetBytes(int64(len(value)))
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			key := prefix + strconv.FormatInt(counter.next(), 10)
			_ = client.SetMultiLevel(key, key, value, http.Header{}, "", keyTTL, key)
		}
	})
}

// BenchmarkRedis_ListKeys measures the SCAN based enumeration of the mapping
// keys, which walks the whole keyspace and decodes every mapping.
func BenchmarkRedis_ListKeys(b *testing.B) {
	for _, entries := range []int{100, 1000} {
		b.Run(fmt.Sprintf("%d_entries", entries), func(b *testing.B) {
			client, prefix := newStorer(b, "listkeys")
			value := responsePayload(b, 256, "listkeys")

			for i := range entries {
				key := prefix + strconv.Itoa(i)
				if err := client.SetMultiLevel(key, key, value, http.Header{}, "", keyTTL, key); err != nil {
					b.Fatalf("impossible to store the key %s: %v", key, err)
				}
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

func BenchmarkRedis_MapKeys(b *testing.B) {
	for _, entries := range []int{100, 1000} {
		b.Run(fmt.Sprintf("%d_entries", entries), func(b *testing.B) {
			client, prefix := newStorer(b, "mapkeys")
			value := incompressiblePayload(256, "mapkeys")

			for i := range entries {
				if err := client.Set(prefix+strconv.Itoa(i), value, keyTTL); err != nil {
					b.Fatalf("impossible to store the key: %v", err)
				}
			}

			b.ReportAllocs()
			b.ResetTimer()

			for range b.N {
				if len(client.MapKeys(prefix)) == 0 {
					b.Fatal("the keys should be mapped")
				}
			}
		})
	}
}

// BenchmarkRedis_DeleteMany measures the regex based invalidation. It scans the
// whole keyspace on every call, so the timer is stopped while the dataset is
// seeded again between the iterations.
func BenchmarkRedis_DeleteMany(b *testing.B) {
	for _, entries := range []int{100, 1000} {
		b.Run(fmt.Sprintf("%d_entries", entries), func(b *testing.B) {
			client, prefix := newStorer(b, "deletemany")
			value := incompressiblePayload(256, "deletemany")

			seed := func() {
				for i := range entries {
					if err := client.Set(prefix+strconv.Itoa(i), value, keyTTL); err != nil {
						b.Fatalf("impossible to store the key: %v", err)
					}
				}
			}

			b.ReportAllocs()

			for range b.N {
				b.StopTimer()
				seed()
				b.StartTimer()

				client.DeleteMany("^" + prefix)
			}
		})
	}
}
