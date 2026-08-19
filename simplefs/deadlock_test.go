package simplefs_test

import (
	"fmt"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestSimplefs_RecoverEnoughSpaceIfNeeded_NoDeadlock ensures that saturating the
// cache directory does not freeze SetMultiLevel.
// See: https://github.com/darkweak/storages/issues/57
func TestSimplefs_RecoverEnoughSpaceIfNeeded_NoDeadlock(t *testing.T) {
	client, _ := getBoundedInstance(t, "64KB")

	done := make(chan struct{})

	go func() {
		defer close(done)

		for i := range 40 {
			key := fmt.Sprintf("deadlock_%d", i)
			_ = client.SetMultiLevel(key, key, incompressiblePayload(8*1024, key), http.Header{}, "", time.Minute, key)
		}
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("deadlock detected: SetMultiLevel did not return within 10s once the cache directory was saturated")
	}
}

// TestSimplefs_RecoverEnoughSpaceIfNeeded_EvictsEnough ensures the eviction
// actually frees space so the directory stays bounded by directory_size.
func TestSimplefs_RecoverEnoughSpaceIfNeeded_EvictsEnough(t *testing.T) {
	const directorySize = 64 * 1024

	client, dir := getBoundedInstance(t, "64KB")

	done := make(chan struct{})

	go func() {
		defer close(done)

		for i := range 40 {
			key := fmt.Sprintf("evict_%d", i)
			_ = client.SetMultiLevel(key, key, incompressiblePayload(8*1024, key), http.Header{}, "", time.Minute, key)
		}
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("deadlock detected: SetMultiLevel did not return within 10s once the cache directory was saturated")
	}

	// Let the asynchronous ttlcache eviction callbacks remove the files.
	time.Sleep(500 * time.Millisecond)

	if actual := directorySizeOf(t, dir); actual > directorySize {
		t.Errorf("the directory %s grew to %d bytes, it should stay under the %d bytes limit", filepath.Base(dir), actual, directorySize)
	}

	// The eviction must free just enough space, not drop the whole cache: the
	// most recent entries have to survive.
	kept := 0

	for i := range 40 {
		if len(client.Get(fmt.Sprintf("evict_%d", i))) > 0 {
			kept++
		}
	}

	if kept == 0 {
		t.Error("the cache has been fully evicted, it should only evict the least recently used entries")
	}
}

// TestSimplefs_RecoverEnoughSpaceIfNeeded_Concurrent ensures a saturated cache
// written by concurrent goroutines neither deadlocks nor races.
func TestSimplefs_RecoverEnoughSpaceIfNeeded_Concurrent(t *testing.T) {
	client, _ := getBoundedInstance(t, "64KB")

	var waitGroup sync.WaitGroup

	done := make(chan struct{})

	for worker := range 8 {
		waitGroup.Add(1)

		go func() {
			defer waitGroup.Done()

			for i := range 20 {
				key := fmt.Sprintf("concurrent_%d_%d", worker, i)
				_ = client.SetMultiLevel(key, key, incompressiblePayload(8*1024, key), http.Header{}, "", time.Minute, key)
			}
		}()
	}

	go func() {
		waitGroup.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("deadlock detected: concurrent SetMultiLevel calls did not return within 30s")
	}
}
