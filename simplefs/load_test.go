package simplefs_test

import (
	"fmt"
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
//	STORAGES_LOAD_TEST=1 go test -run Load -v ./simplefs
//
// The workload can be tuned with STORAGES_LOAD_WORKERS, STORAGES_LOAD_DURATION
// and STORAGES_LOAD_PAYLOAD.

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

// TestSimplefs_Load_MixedWorkload keeps the storage under a sustained
// read/write/invalidate workload and checks it neither deadlocks, errors nor
// corrupts the stored responses.
func TestSimplefs_Load_MixedWorkload(t *testing.T) {
	config := loadConfigFromEnv(t)

	client, _ := newStorer(t, 0, "")
	value := responsePayload(t, config.payload, "load")

	// Preload a working set so the readers hit an existing entry.
	const workingSet = 64

	for i := range workingSet {
		key := fmt.Sprintf("load_%d", i)
		if err := client.SetMultiLevel(key, key, value, http.Header{}, "", time.Hour, key); err != nil {
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
			key := fmt.Sprintf("load_w%d_%d", worker, iteration)
			err := client.SetMultiLevel(key, key, value, http.Header{}, "", time.Hour, key)
			report.record(time.Since(start), err)
		case 2:
			client.Delete(fmt.Sprintf("load_w%d_%d", worker, iteration-2))
			report.record(time.Since(start), nil)
		default:
			key := fmt.Sprintf("load_%d", iteration%workingSet)
			fresh, stale := client.GetMultiLevel(key, request, &core.Revalidator{})
			report.recordRead(time.Since(start), fresh != nil || stale != nil)
		}
	})

	report.summarize(t, "simplefs mixed", time.Since(started))

	if report.errors > 0 {
		t.Errorf("the mixed workload reported %d errors, it should report none", report.errors)
	}

	// The preloaded working set never expires, so every read had to be a hit.
	if report.misses > 0 {
		t.Errorf("the mixed workload reported %d misses on a preloaded working set", report.misses)
	}
}

// TestSimplefs_Load_SaturatedDirectory hammers a storage whose directory size
// limit is permanently exceeded, which is the scenario that used to deadlock.
// See https://github.com/darkweak/storages/issues/57.
func TestSimplefs_Load_SaturatedDirectory(t *testing.T) {
	config := loadConfigFromEnv(t)

	const directorySize = 4 * 1024 * 1024

	client, dir := newStorer(t, 0, strconv.Itoa(directorySize))
	value := responsePayload(t, config.payload, "saturated")

	report := &loadReport{}
	counter := &atomicCounter{}
	started := time.Now()

	runLoadWorkers(t, config, func(_, _ int) {
		key := fmt.Sprintf("saturated_%d", counter.next())

		start := time.Now()
		err := client.SetMultiLevel(key, key, value, http.Header{}, "", time.Hour, key)
		report.record(time.Since(start), err)
	})

	report.summarize(t, "simplefs saturated", time.Since(started))

	if report.errors > 0 {
		t.Errorf("the saturated workload reported %d errors, it should report none", report.errors)
	}

	// Let the asynchronous eviction callbacks drain before measuring the disk.
	time.Sleep(time.Second)

	// The eviction is planned before the write, so the directory can exceed the
	// limit by the size of the entries written concurrently.
	tolerance := int64(directorySize + config.workers*config.payload*2)
	if actual := directorySizeOf(t, dir); actual > tolerance {
		t.Errorf("the directory grew to %d bytes, it should stay close to the %d bytes limit", actual, directorySize)
	}
}
