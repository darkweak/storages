package nuts_test

import (
	"bytes"
	"net/http"
	"testing"
	"time"

	"github.com/darkweak/storages/core"
	"github.com/darkweak/storages/nuts"
	"github.com/pierrec/lz4/v4"
	"go.uber.org/zap"
)

const (
	byteKey        = "MyByteKey"
	nonExistentKey = "NonExistentKey"
	baseValue      = "My first data"
)

func getNutsInstance() (core.Storer, error) {
	return nuts.Factory(core.CacheProvider{}, zap.NewNop().Sugar(), 0)
}

func TestNutsConnectionFactory(t *testing.T) {
	instance, err := getNutsInstance()

	if nil != err {
		t.Error("Shouldn't have panic")
	}

	if nil == instance {
		t.Error("Nuts should be instanciated")
	}

	if nil == instance.(*nuts.Nuts).DB {
		t.Error("Nuts database should be accesible")
	}
}

func TestIShouldBeAbleToReadAndWriteDataInNuts(t *testing.T) {
	client, _ := getNutsInstance()

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

func TestNuts_GetRequestInCache(t *testing.T) {
	client, _ := getNutsInstance()
	res := client.Get(nonExistentKey)

	if 0 < len(res) {
		t.Errorf("Key %s should not exist", nonExistentKey)
	}
}

func TestNuts_GetSetRequestInCache_OneByte(t *testing.T) {
	client, _ := getNutsInstance()
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

func TestNuts_SetRequestInCache_TTL(t *testing.T) {
	key := "MyEmptyKey"
	client, _ := getNutsInstance()
	value := []byte("Hello world")
	_ = client.Set(key, value, time.Duration(20)*time.Second)
	time.Sleep(1 * time.Second)

	newValue := client.Get(key)

	if len(newValue) != len(value) {
		t.Errorf("Key %s should be equals to %s, %s provided", key, value, newValue)
	}
}

func TestNuts_DeleteRequestInCache(t *testing.T) {
	client, _ := getNutsInstance()
	client.Delete(byteKey)
	time.Sleep(1 * time.Second)

	if 0 < len(client.Get(byteKey)) {
		t.Errorf("Key %s should not exist", byteKey)
	}
}

func TestNuts_Init(t *testing.T) {
	client, _ := getNutsInstance()
	err := client.Init()

	if nil != err {
		t.Error("Impossible to init Nuts provider")
	}
}

// TestNuts_SetMultiLevel_LargeValue tests that large values are not truncated
// when stored and retrieved via SetMultiLevel. This reproduces issue #41.
// See: https://github.com/darkweak/storages/issues/41
func TestNuts_SetMultiLevel_LargeValue(t *testing.T) {
	client, err := getNutsInstance()
	if err != nil {
		t.Fatalf("Failed to create nuts instance: %v", err)
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
