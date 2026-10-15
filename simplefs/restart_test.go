package simplefs_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/darkweak/storages/core"
)

// TestSimplefs_Init_RestoresTheStoredResponses ensures the responses stored by
// a previous process are served again after a restart, thanks to the index
// persisted on the disk.
// See https://github.com/darkweak/storages/issues/60.
func TestSimplefs_Init_RestoresTheStoredResponses(t *testing.T) {
	first, dir := newStorer(t, 0, "10MB")
	value := responsePayload(t, 4096, "restart")

	keys := []string{"first", "second", "third"}
	for _, key := range keys {
		if err := first.SetMultiLevel(key, key, value, http.Header{}, "", time.Hour, key); err != nil {
			t.Fatalf("impossible to store the key %s: %v", key, err)
		}
	}

	stored := directorySizeOf(t, dir)
	if stored == 0 {
		t.Fatal("the responses should have been written on the disk")
	}

	// Restart on the same directory, the way a new process would.
	second := restartedStorer(t, dir, "10MB")

	for _, key := range keys {
		fresh, _ := second.GetMultiLevel(key, newRequest(t), new(core.Revalidator))
		if fresh == nil {
			t.Errorf("the key %s should be served again after a restart", key)
		}

		if len(second.Get(key)) == 0 {
			t.Errorf("the response of the key %s should still be readable after a restart", key)
		}
	}

	if restored := directorySizeOf(t, dir); restored != stored {
		t.Errorf("the restart changed the stored bytes, from %d to %d", stored, restored)
	}
}

// TestSimplefs_Init_RestoresTheVariedHeaders ensures the metadata that drives
// the election survives the restart, not only the response body.
func TestSimplefs_Init_RestoresTheVariedHeaders(t *testing.T) {
	first, dir := newStorer(t, 0, "10MB")
	value := responsePayload(t, 1024, "varied")

	variedHeaders := http.Header{"Accept-Encoding": []string{"gzip"}}
	if err := first.SetMultiLevel("varied", "varied", value, variedHeaders, "", time.Hour, "varied"); err != nil {
		t.Fatalf("impossible to store the key varied: %v", err)
	}

	second := restartedStorer(t, dir, "10MB")

	matching := newRequest(t)
	matching.Header.Set("Accept-Encoding", "gzip")

	if fresh, _ := second.GetMultiLevel("varied", matching, new(core.Revalidator)); fresh == nil {
		t.Error("a request matching the varied headers should be served after a restart")
	}

	if fresh, _ := second.GetMultiLevel("varied", newRequest(t), new(core.Revalidator)); fresh != nil {
		t.Error("a request not matching the varied headers should not be served after a restart")
	}
}

// TestSimplefs_Init_PurgesUnreachableFiles ensures a response the index does
// not reference is removed: nothing can serve it and the eviction would never
// free it, which would leak the directory.
func TestSimplefs_Init_PurgesUnreachableFiles(t *testing.T) {
	_, dir := newStorer(t, 0, "10MB")

	// A response written by a process that never persisted its index. It has to
	// look like a stored response, an lz4 frame, for the cleanup to own it.
	orphan := filepath.Join(dir, "orphan")
	if err := os.WriteFile(orphan, compressedPayload(t, 2048, "orphan"), 0o600); err != nil {
		t.Fatalf("impossible to write the orphan file: %v", err)
	}

	restartedStorer(t, dir, "10MB")

	if _, err := os.Stat(orphan); err == nil {
		t.Error("the unreachable response should have been purged")
	}
}

// TestSimplefs_Init_DropsTheExpiredResponses ensures an entry whose freshness
// elapsed while the process was down is not restored, and that its response is
// removed from the disk.
func TestSimplefs_Init_DropsTheExpiredResponses(t *testing.T) {
	first, dir := newStorer(t, 0, "10MB")
	value := responsePayload(t, 1024, "expired")

	if err := first.SetMultiLevel("expired", "expired", value, http.Header{}, "", 300*time.Millisecond, "expired"); err != nil {
		t.Fatalf("impossible to store the key expired: %v", err)
	}

	if err := first.SetMultiLevel("kept", "kept", value, http.Header{}, "", time.Hour, "kept"); err != nil {
		t.Fatalf("impossible to store the key kept: %v", err)
	}

	time.Sleep(600 * time.Millisecond)

	second := restartedStorer(t, dir, "10MB")

	if fresh, _ := second.GetMultiLevel("expired", newRequest(t), new(core.Revalidator)); fresh != nil {
		t.Error("an entry whose freshness elapsed should not be restored")
	}

	if _, err := os.Stat(filepath.Join(dir, "expired")); err == nil {
		t.Error("the response of an expired entry should have been removed from the disk")
	}

	if fresh, _ := second.GetMultiLevel("kept", newRequest(t), new(core.Revalidator)); fresh == nil {
		t.Error("the entry that is still fresh should have been restored")
	}
}

// TestSimplefs_Init_SurvivesACorruptIndex ensures a truncated or garbage index
// file is dropped instead of breaking the startup.
func TestSimplefs_Init_SurvivesACorruptIndex(t *testing.T) {
	first, dir := newStorer(t, 0, "10MB")
	value := responsePayload(t, 1024, "corrupt")

	for _, key := range []string{"corrupt", "healthy"} {
		if err := first.SetMultiLevel(key, key, value, http.Header{}, "", time.Hour, key); err != nil {
			t.Fatalf("impossible to store the key %s: %v", key, err)
		}
	}

	index := filepath.Join(dir, ".simplefs-index", "corrupt")
	if err := os.WriteFile(index, []byte("this is not a protobuf mapping"), 0o600); err != nil {
		t.Fatalf("impossible to corrupt the index file: %v", err)
	}

	second := restartedStorer(t, dir, "10MB")

	if _, err := os.Stat(index); err == nil {
		t.Error("the corrupt index file should have been dropped")
	}

	if fresh, _ := second.GetMultiLevel("healthy", newRequest(t), new(core.Revalidator)); fresh == nil {
		t.Error("a corrupt index file should not prevent the other entries from being restored")
	}
}

// TestSimplefs_Init_KeepsTheForeignFiles ensures the cleanup never removes a
// file simplefs did not write. The storage path is not necessarily a directory
// simplefs owns, and it falls back to the working directory when no path is
// configured.
func TestSimplefs_Init_KeepsTheForeignFiles(t *testing.T) {
	_, dir := newStorer(t, 0, "10MB")

	foreigners := map[string][]byte{
		"notes.txt":    []byte("some notes that simplefs must not touch"),
		"main.go":      []byte("package main\n\nfunc main() {}\n"),
		"empty.bin":    {},
		"truncated.hd": {0x04, 0x22},
	}

	for name, content := range foreigners {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
			t.Fatalf("impossible to write the file %s: %v", name, err)
		}
	}

	restartedStorer(t, dir, "10MB")

	for name, content := range foreigners {
		//nolint:gosec
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Errorf("the file %s has been removed, only the files simplefs wrote may be purged: %v", name, err)

			continue
		}

		if string(got) != string(content) {
			t.Errorf("the file %s has been altered", name)
		}
	}
}

// TestSimplefs_Init_KeepsTheIndexedFiles ensures calling Init on a warm storage
// is harmless.
func TestSimplefs_Init_KeepsTheIndexedFiles(t *testing.T) {
	client, dir := newStorer(t, 0, "10MB")
	value := responsePayload(t, 4096, "warm")

	if err := client.SetMultiLevel("warm", "warm", value, http.Header{}, "", time.Hour, "warm"); err != nil {
		t.Fatalf("impossible to store the key warm: %v", err)
	}

	before := directorySizeOf(t, dir)

	if err := client.Init(); err != nil {
		t.Fatalf("impossible to init the storage a second time: %v", err)
	}

	if after := directorySizeOf(t, dir); after != before {
		t.Errorf("an indexed response has been purged, the directory went from %d to %d bytes", before, after)
	}

	if fresh, _ := client.GetMultiLevel("warm", newRequest(t), new(core.Revalidator)); fresh == nil {
		t.Error("the key warm should still return a fresh response after a second Init")
	}
}

// TestSimplefs_Init_KeepsTheDirectorySizeLimit ensures the limit still holds
// once the previously stored responses have been restored.
func TestSimplefs_Init_KeepsTheDirectorySizeLimit(t *testing.T) {
	const (
		directorySize = 64 * 1024
		payloadSize   = 8 * 1024
	)

	first, dir := newStorer(t, 0, "64KB")
	value := responsePayload(t, payloadSize, "limit")

	for i := range 8 {
		key := "before_" + strconv.Itoa(i)
		if err := first.SetMultiLevel(key, key, value, http.Header{}, "", time.Hour, key); err != nil {
			t.Fatalf("impossible to store the key %s: %v", key, err)
		}
	}

	second := restartedStorer(t, dir, "64KB")

	for i := range 16 {
		key := "after_" + strconv.Itoa(i)
		if err := second.SetMultiLevel(key, key, value, http.Header{}, "", time.Hour, key); err != nil {
			t.Fatalf("impossible to store the key %s: %v", key, err)
		}
	}

	// Let the asynchronous eviction callbacks remove the files.
	time.Sleep(500 * time.Millisecond)

	// The insertion callback updates the actual size in its own goroutine, so a
	// write can be planned against a slightly stale size and the directory can
	// exceed the limit by one entry.
	tolerance := int64(directorySize + len(value))
	if actual := directorySizeOf(t, dir); actual > tolerance {
		t.Errorf("the directory grew to %d bytes after a restart, it should stay within %d bytes", actual, tolerance)
	}
}
