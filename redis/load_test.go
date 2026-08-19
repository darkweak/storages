package redis_test

import (
	"net/http"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/darkweak/storages/core"
)

// The load tests are opt-in: they run for several seconds under a sustained
// concurrent workload, so they are skipped unless STORAGES_LOAD_TEST is set.
//
//	STORAGES_LOAD_TEST=1 go test -run Load -v ./redis
//
// The workload can be tuned with STORAGES_LOAD_WORKERS, STORAGES_LOAD_DURATION
// and STORAGES_LOAD_PAYLOAD, and the target with REDIS_ADDRESS and REDIS_DB.

// loadReport aggregates the outcome of a load test.
type loadReport struct {
	mu        sync.Mutex
	latencies []time.Duration
	errors    int64
	hits      int64
	misses    int64
}

// record accounts for a write or a delete. The latency has to be computed
// before the call, never inline as an argument next to the measured operation:
// Go evaluates the arguments left to right, which would measure nothing.
//
// The lock is only held for an append, which is negligible compared to the
// measured operations.
func (r *loadReport) record(latency time.Duration, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.latencies = append(r.latencies, latency)

	if err != nil {
		r.errors++
	}
}

// recordRead accounts for a read and tracks whether it was a cache hit.
func (r *loadReport) recordRead(latency time.Duration, hit bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.latencies = append(r.latencies, latency)

	if hit {
		r.hits++
	} else {
		r.misses++
	}
}

func (r *loadReport) percentile(ratio float64) time.Duration {
	if len(r.latencies) == 0 {
		return 0
	}

	index := int(float64(len(r.latencies)-1) * ratio)

	return r.latencies[index]
}

// summarize sorts the collected latencies and logs the throughput along with
// the latency distribution.
func (r *loadReport) summarize(tb testing.TB, name string, elapsed time.Duration) {
	tb.Helper()

	r.mu.Lock()
	defer r.mu.Unlock()

	sort.Slice(r.latencies, func(i, j int) bool { return r.latencies[i] < r.latencies[j] })

	operations := len(r.latencies)
	if operations == 0 {
		tb.Fatalf("%s: no operation has been performed", name)
	}

	tb.Logf(
		"%s: %d ops in %s (%.0f ops/s), %d hits, %d misses, %d errors",
		name, operations, elapsed.Round(time.Millisecond), float64(operations)/elapsed.Seconds(), r.hits, r.misses, r.errors,
	)
	tb.Logf(
		"%s: latency p50=%s p95=%s p99=%s max=%s",
		name,
		r.percentile(0.50).Round(time.Microsecond),
		r.percentile(0.95).Round(time.Microsecond),
		r.percentile(0.99).Round(time.Microsecond),
		r.latencies[operations-1].Round(time.Microsecond),
	)
}

// TestRedis_Load_MixedWorkload keeps the storage under a sustained
// read/write/invalidate workload and checks it neither errors nor stalls.
func TestRedis_Load_MixedWorkload(t *testing.T) {
	config := loadConfigFromEnv(t)

	client, prefix := newStorer(t, "load")
	value := responsePayload(t, config.payload, "load")

	// Preload a working set so the readers hit an existing entry.
	const workingSet = 64

	for i := range workingSet {
		key := prefix + strconv.Itoa(i)
		if err := client.SetMultiLevel(key, key, value, http.Header{}, "", keyTTL, key); err != nil {
			t.Fatalf("impossible to preload the key %s: %v", key, err)
		}
	}

	request := newRequest(t)
	report := &loadReport{}
	started := time.Now()

	runLoadWorkers(t, config, func(worker, iteration int) {
		start := time.Now()

		switch iteration % 10 {
		case 0, 1:
			key := prefix + "w" + strconv.Itoa(worker) + "_" + strconv.Itoa(iteration)
			err := client.SetMultiLevel(key, key, value, http.Header{}, "", keyTTL, key)
			report.record(time.Since(start), err)
		case 2:
			client.Delete(prefix + "w" + strconv.Itoa(worker) + "_" + strconv.Itoa(iteration-2))
			report.record(time.Since(start), nil)
		default:
			key := prefix + strconv.Itoa(iteration%workingSet)
			fresh, stale := client.GetMultiLevel(key, request, &core.Revalidator{})
			report.recordRead(time.Since(start), fresh != nil || stale != nil)
		}
	})

	report.summarize(t, "redis mixed", time.Since(started))

	if report.errors > 0 {
		t.Errorf("the mixed workload reported %d errors, it should report none", report.errors)
	}

	// The preloaded working set outlives the run, so every read had to be a hit.
	if report.misses > 0 {
		t.Errorf("the mixed workload reported %d misses on a preloaded working set", report.misses)
	}
}

// TestRedis_Load_ReadHeavy mirrors a cache in front of a busy origin: almost
// every request is a hit on a small hot working set.
func TestRedis_Load_ReadHeavy(t *testing.T) {
	config := loadConfigFromEnv(t)

	client, prefix := newStorer(t, "readheavy")
	value := responsePayload(t, config.payload, "readheavy")

	const workingSet = 8

	for i := range workingSet {
		key := prefix + strconv.Itoa(i)
		if err := client.SetMultiLevel(key, key, value, http.Header{}, "", keyTTL, key); err != nil {
			t.Fatalf("impossible to preload the key %s: %v", key, err)
		}
	}

	request := newRequest(t)
	report := &loadReport{}
	started := time.Now()

	runLoadWorkers(t, config, func(_, iteration int) {
		key := prefix + strconv.Itoa(iteration%workingSet)

		start := time.Now()
		fresh, stale := client.GetMultiLevel(key, request, &core.Revalidator{})
		report.recordRead(time.Since(start), fresh != nil || stale != nil)
	})

	report.summarize(t, "redis read heavy", time.Since(started))

	if report.misses > 0 {
		t.Errorf("the read heavy workload reported %d misses on a preloaded working set", report.misses)
	}
}
