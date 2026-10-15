package redis_test

import (
	"context"
	"fmt"
	"hash/fnv"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darkweak/storages/core"
	"github.com/darkweak/storages/redis"
	"go.uber.org/zap"
)

// The benchmarks and the load tests need a reachable redis. It defaults to the
// instance shipped by compose.test.yml and can be pointed elsewhere with
// REDIS_ADDRESS. They run against REDIS_DB (15 by default) to stay away from
// the application data, they namespace every key with a unique prefix and they
// always store with a TTL, so a leftover key expires on its own.
const (
	defaultRedisAddress = "localhost:6379"
	defaultRedisDB      = 15
	// keyTTL bounds the lifetime of anything the benchmarks leave behind.
	keyTTL = 10 * time.Minute
)

var keyspace atomicCounter

func redisAddress() string {
	if address := os.Getenv("REDIS_ADDRESS"); address != "" {
		return address
	}

	return defaultRedisAddress
}

func redisDB(tb testing.TB) int {
	tb.Helper()

	value := os.Getenv("REDIS_DB")
	if value == "" {
		return defaultRedisDB
	}

	database, err := strconv.Atoi(value)
	if err != nil || database < 0 {
		tb.Fatalf("REDIS_DB must be a positive integer, got %q", value)
	}

	return database
}

// newStorer connects to redis and skips the caller when no instance is
// reachable, so the suite stays runnable without a redis around. It returns a
// key prefix unique to the caller, and takes care of removing the keys and of
// closing the connection once the caller is done.
func newStorer(tb testing.TB, name string) (core.Storer, string) {
	tb.Helper()

	client, err := redis.Factory(core.CacheProvider{
		Configuration: map[string]interface{}{
			"InitAddress": []string{redisAddress()},
			"SelectDB":    redisDB(tb),
			"ClientName":  "storages-bench",
		},
	}, zap.NewNop().Sugar(), 0)
	if err != nil {
		tb.Skipf("no redis reachable on %s, skipping: %v", redisAddress(), err)
	}

	prefix := uniqueKeyPrefix(name)

	tb.Cleanup(func() {
		// The pattern is deliberately not anchored: it has to match the varied
		// keys, prefixed with the prefix, as well as their mapping keys, which
		// SetMultiLevel stores as IDX_ + the base key and without any TTL.
		client.DeleteMany(prefix)

		_ = client.Reset()
	})

	return client, prefix
}

// uniqueKeyPrefix namespaces the keys of a single benchmark or test so the
// concurrent runs and the leftovers of a previous run cannot interfere.
func uniqueKeyPrefix(name string) string {
	return fmt.Sprintf("storages_%s_%d_", name, keyspace.next())
}

// incompressiblePayload returns a payload that lz4 cannot shrink, so the stored
// size is predictable. The content is derived from the given seed to keep the
// test deterministic.
func incompressiblePayload(size int, seed string) []byte {
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(seed))

	payload := make([]byte, size)
	state := hasher.Sum32()

	for i := range payload {
		state = state*1664525 + 1013904223
		payload[i] = byte(state >> 24)
	}

	return payload
}

// responsePayload builds a raw HTTP response of about the given body size. The
// multi level API stores the wire format of a response, so the payload has to
// be parsable by http.ReadResponse to exercise the read path realistically.
func responsePayload(tb testing.TB, size int, seed string) []byte {
	tb.Helper()

	recorder := httptest.NewRecorder()
	recorder.Header().Set("Content-Type", "application/octet-stream")
	recorder.WriteHeader(http.StatusOK)
	_, _ = recorder.Write(incompressiblePayload(size, seed))

	dumped, err := httputil.DumpResponse(recorder.Result(), true)
	if err != nil {
		tb.Fatalf("impossible to dump the response: %v", err)
	}

	return dumped
}

// newRequest builds the request handed to GetMultiLevel. It is read only, so a
// single instance can be shared by concurrent readers.
func newRequest(tb testing.TB) *http.Request {
	tb.Helper()

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com/", nil)
	if err != nil {
		tb.Fatalf("impossible to create the request: %v", err)
	}

	return request
}

// atomicCounter hands out unique suffixes to the concurrent workers so every
// goroutine writes its own keys.
type atomicCounter struct {
	value atomic.Int64
}

func (c *atomicCounter) next() int64 {
	return c.value.Add(1)
}

// loadConfig drives the opt-in load tests.
type loadConfig struct {
	workers  int
	duration time.Duration
	payload  int
}

// loadConfigFromEnv skips the calling test unless STORAGES_LOAD_TEST is set,
// then reads the workload shape from the environment.
func loadConfigFromEnv(tb testing.TB) loadConfig {
	tb.Helper()

	if os.Getenv("STORAGES_LOAD_TEST") == "" {
		tb.Skip("set STORAGES_LOAD_TEST=1 to run the load tests")
	}

	config := loadConfig{workers: 16, duration: 10 * time.Second, payload: 32 * 1024}

	if value := os.Getenv("STORAGES_LOAD_WORKERS"); value != "" {
		workers, err := strconv.Atoi(value)
		if err != nil || workers < 1 {
			tb.Fatalf("STORAGES_LOAD_WORKERS must be a positive integer, got %q", value)
		}

		config.workers = workers
	}

	if value := os.Getenv("STORAGES_LOAD_DURATION"); value != "" {
		duration, err := time.ParseDuration(value)
		if err != nil || duration <= 0 {
			tb.Fatalf("STORAGES_LOAD_DURATION must be a positive duration, got %q", value)
		}

		config.duration = duration
	}

	if value := os.Getenv("STORAGES_LOAD_PAYLOAD"); value != "" {
		payload, err := strconv.Atoi(value)
		if err != nil || payload < 1 {
			tb.Fatalf("STORAGES_LOAD_PAYLOAD must be a positive integer, got %q", value)
		}

		config.payload = payload
	}

	tb.Logf("load configuration: %d workers, %s, %d bytes payload", config.workers, config.duration, config.payload)

	return config
}

// runLoadWorkers runs work on config.workers goroutines until the configured
// duration elapses, and fails the test instead of hanging if the storage
// stops making progress.
func runLoadWorkers(tb testing.TB, config loadConfig, work func(worker, iteration int)) {
	tb.Helper()

	// Grace period on top of the workload duration: a storage that stopped
	// making progress is a deadlock, not a slow run.
	const grace = 30 * time.Second

	var waitGroup sync.WaitGroup

	deadline := time.Now().Add(config.duration)

	for worker := range config.workers {
		waitGroup.Add(1)

		go func() {
			defer waitGroup.Done()

			for iteration := 0; time.Now().Before(deadline); iteration++ {
				work(worker, iteration)
			}
		}()
	}

	done := make(chan struct{})

	go func() {
		waitGroup.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(config.duration + grace):
		tb.Fatalf("deadlock detected: the workers did not finish within %s after the %s workload", grace, config.duration)
	}
}
