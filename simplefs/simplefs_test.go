package simplefs_test

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/darkweak/storages/core"
	"github.com/darkweak/storages/simplefs"
	"github.com/pierrec/lz4/v4"
	"go.uber.org/zap"
)

const (
	byteKey        = "MyByteKey"
	nonExistentKey = "NonExistentKey"
	baseValue      = "My first data"
)

// getSimplefsInstance builds a storage in a directory dedicated to the calling
// test. Without a configured path simplefs falls back to the working directory,
// which would scatter the stored responses in the source tree and let Init
// garbage collect them there.
func getSimplefsInstance(tb testing.TB) (core.Storer, error) {
	tb.Helper()

	return simplefs.Factory(core.CacheProvider{Path: tb.TempDir()}, zap.NewNop().Sugar(), 0)
}

// This test ensure that Simplefs options are override by the Souin configuration.
func TestCustomSimplefsConnectionFactory(t *testing.T) {
	instance, err := getSimplefsInstance(t)

	if nil != err {
		t.Error("Shouldn't have panic")
	}

	if nil == instance {
		t.Error("Simplefs should be instanciated")
	}
}

func TestSimplefsConnectionFactory(t *testing.T) {
	instance, err := getSimplefsInstance(t)

	if nil != err {
		t.Error("Shouldn't have panic")
	}

	if nil == instance {
		t.Error("Simplefs should be instanciated")
	}
}

func TestIShouldBeAbleToReadAndWriteDataInSimplefs(t *testing.T) {
	client, _ := getSimplefsInstance(t)

	_ = client.Set("Test", []byte(baseValue), time.Duration(20)*time.Second)
	time.Sleep(1 * time.Second)

	res := client.Get("Test")
	if len(res) == 0 {
		t.Errorf("Key %s should exist", baseValue)
	}

	if baseValue != string(res) {
		t.Errorf("%s not corresponding to %s", string(res), baseValue)
	}
}

func TestSimplefs_GetRequestInCache(t *testing.T) {
	client, _ := getSimplefsInstance(t)
	res := client.Get(nonExistentKey)

	if 0 < len(res) {
		t.Errorf("Key %s should not exist", nonExistentKey)
	}
}

func TestSimplefs_GetSetRequestInCache_OneByte(t *testing.T) {
	client, _ := getSimplefsInstance(t)
	_ = client.Set(byteKey, []byte("A"), time.Duration(20)*time.Second)
	time.Sleep(1 * time.Second)

	res := client.Get(byteKey)
	if len(res) == 0 {
		t.Errorf("Key %s should exist", byteKey)
	}

	if string(res) != "A" {
		t.Errorf("%s not corresponding to %v", res, 65)
	}
}

func TestSimplefs_SetRequestInCache_TTL(t *testing.T) {
	key := "MyEmptyKey"
	client, _ := getSimplefsInstance(t)
	value := []byte("Hello world")
	_ = client.Set(key, value, time.Duration(20)*time.Second)
	time.Sleep(1 * time.Second)

	newValue := client.Get(key)

	if len(newValue) != len(value) {
		t.Errorf("Key %s should be equals to %s, %s provided", key, value, newValue)
	}
}

func TestSimplefs_SetRequestInCache_Negative_TTL(t *testing.T) {
	client, _ := getSimplefsInstance(t)
	value := []byte("New value")
	_ = client.Set(byteKey, value, -1)

	time.Sleep(1 * time.Second)

	_ = client.Set(byteKey, value, time.Duration(20)*time.Second)
	time.Sleep(1 * time.Second)

	newValue := client.Get(byteKey)

	if len(newValue) != len(value) {
		t.Errorf("Key %s should be equals to %s, %s provided", byteKey, value, newValue)
	}
}

func TestSimplefs_DeleteRequestInCache(t *testing.T) {
	client, _ := getSimplefsInstance(t)
	client.Delete(byteKey)
	time.Sleep(1 * time.Second)

	if 0 < len(client.Get(byteKey)) {
		t.Errorf("Key %s should not exist", byteKey)
	}
}

func TestSimplefs_Init(t *testing.T) {
	client, _ := getSimplefsInstance(t)
	err := client.Init()

	if nil != err {
		t.Error("Impossible to init Simplefs provider")
	}
}

func TestSimplefs_EvictAfterXSeconds(t *testing.T) {
	client, _ := getSimplefsInstance(t)

	_ = client.Init()

	for i := range 10 {
		key := fmt.Sprintf("Test_%d", i)
		_ = client.SetMultiLevel(key, key, []byte(baseValue), http.Header{}, "", time.Second, key)

		time.Sleep(100 * time.Millisecond)
	}

	res := client.Get("Test_0")
	if len(res) != 0 {
		t.Errorf("Key %s should be evicted", "Test_0")
	}

	res = client.Get("Test_9")
	if len(res) == 0 {
		t.Errorf("Key %s should exist", "Test_9")
	}

	time.Sleep(3 * time.Second)
}

// TestSimplefs_SetMultiLevel_LargeValue tests that large values are not truncated
// when stored and retrieved via SetMultiLevel. This reproduces issue #41.
// See: https://github.com/darkweak/storages/issues/41
func TestSimplefs_SetMultiLevel_LargeValue(t *testing.T) {
	client, err := getSimplefsInstance(t)
	if err != nil {
		t.Fatalf("Failed to create simplefs instance: %v", err)
	}

	_ = client.Init()

	// Create a large value (5MB) to simulate the issue where large cached
	// responses were being truncated
	largeSize := 5 * 1024 * 1024

	largeValue := make([]byte, largeSize)
	for i := range largeValue {
		largeValue[i] = byte(i % 256)
	}

	key := "large_value_test"

	err = client.SetMultiLevel(key, key, largeValue, http.Header{}, "", time.Minute, key)
	if err != nil {
		t.Fatalf("Failed to set large value: %v", err)
	}

	// Retrieve and decompress
	compressed := client.Get(key)
	if compressed == nil {
		t.Fatal("Retrieved value is nil")
	}

	reader := lz4.NewReader(bytes.NewBuffer(compressed))

	decompressed := new(bytes.Buffer)
	if _, err := reader.WriteTo(decompressed); err != nil {
		t.Fatalf("Failed to decompress: %v", err)
	}

	retrieved := decompressed.Bytes()
	if len(retrieved) != len(largeValue) {
		t.Errorf("Data truncation: expected %d bytes, got %d bytes (%.2f%%)",
			len(largeValue), len(retrieved), float64(len(retrieved))/float64(len(largeValue))*100)
	}
}

// TestSimplefs_DeleteMany_StoredResponses ensures DeleteMany removes every
// matching response, its file included, and keeps the others. It used to stop
// after the first deletion.
func TestSimplefs_DeleteMany_StoredResponses(t *testing.T) {
	client, dir := newStorer(t, 0, "")
	response := []byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")

	for _, key := range []string{"drop_0", "drop_1", "drop_2", "drop_3", "drop_4", "keep_0"} {
		if err := client.SetMultiLevel(key, key, response, http.Header{}, "", time.Minute, key); err != nil {
			t.Fatalf("impossible to store the key %s: %v", key, err)
		}
	}

	client.DeleteMany("^drop_")

	for i := range 5 {
		key := fmt.Sprintf("drop_%d", i)
		if keys := client.ListKeys(); slices.Contains(keys, key) {
			t.Fatalf("the key %s should have been deleted", key)
		}

		// The eviction callback removing the file runs asynchronously.
		deadline := time.Now().Add(2 * time.Second)

		for {
			if _, err := os.Stat(filepath.Join(dir, key)); errors.Is(err, os.ErrNotExist) {
				break
			}

			if time.Now().After(deadline) {
				t.Fatalf("the file of the key %s should have been removed", key)
			}

			time.Sleep(10 * time.Millisecond)
		}
	}

	if !slices.Contains(client.ListKeys(), "keep_0") {
		t.Fatal("the key keep_0 should have been kept")
	}

	if _, err := os.Stat(filepath.Join(dir, "keep_0")); err != nil {
		t.Fatalf("the file of the key keep_0 should have been kept: %v", err)
	}
}
