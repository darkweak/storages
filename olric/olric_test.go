package olric_test

import (
	"testing"
	"time"

	"github.com/darkweak/storages/core"
	"github.com/darkweak/storages/olric"
	"go.uber.org/zap"
)

const (
	byteKey        = "MyByteKey"
	nonExistentKey = "NonExistentKey"
	baseValue      = "My first data"
)

func getOlricInstance() (core.Storer, error) {
	return olric.Factory(core.CacheProvider{}, zap.NewNop().Sugar(), 0)
}

func TestIShouldBeAbleToReadAndWriteDataInOlric(t *testing.T) {
	client, _ := getOlricInstance()
	defer func() {
		_ = client.Reset()
	}()

	_ = client.Set("Test", []byte(baseValue), time.Duration(10)*time.Second)

	time.Sleep(3 * time.Second)

	res := client.Get("Test")

	if baseValue != string(res) {
		t.Errorf("%s not corresponding to %s", res, baseValue)
	}
}

func TestOlric_GetRequestInCache(t *testing.T) {
	client, _ := getOlricInstance()
	defer func() {
		_ = client.Reset()
	}()

	res := client.Get(nonExistentKey)

	if string(res) != "" {
		t.Errorf("Key %s should not exist", nonExistentKey)
	}
}

func TestOlric_SetRequestInCache_OneByte(t *testing.T) {
	client, _ := getOlricInstance()
	defer func() {
		_ = client.Reset()
	}()

	_ = client.Set(byteKey, []byte{65}, time.Duration(20)*time.Second)
}

func TestOlric_SetRequestInCache_TTL(t *testing.T) {
	key := "MyEmptyKey"

	client, _ := getOlricInstance()
	defer func() {
		_ = client.Reset()
	}()

	value := []byte("Hello world")
	_ = client.Set(key, value, time.Duration(20)*time.Second)
	time.Sleep(1 * time.Second)

	newValue := client.Get(key)

	if len(newValue) != len(value) {
		t.Errorf("Key %s should be equals to %s, %s provided", key, value, newValue)
	}
}

func TestOlric_SetRequestInCache_NoTTL(t *testing.T) {
	client, _ := getOlricInstance()
	defer func() {
		_ = client.Reset()
	}()

	value := []byte("New value")
	_ = client.Set(byteKey, value, time.Duration(20)*time.Second)
	time.Sleep(1 * time.Second)

	newValue := client.Get(byteKey)

	if len(newValue) != len(value) {
		t.Errorf("Key %s should be equals to %s, %s provided", byteKey, value, newValue)
	}
}

func TestOlric_DeleteRequestInCache(t *testing.T) {
	client, _ := getOlricInstance()
	defer func() {
		_ = client.Reset()
	}()

	client.Delete(byteKey)
	time.Sleep(1 * time.Second)

	if 0 < len(client.Get(byteKey)) {
		t.Errorf("Key %s should not exist", byteKey)
	}
}

func TestOlric_Init(t *testing.T) {
	client, _ := getOlricInstance()
	err := client.Init()

	defer func() {
		_ = client.Reset()
	}()

	if nil != err {
		t.Error("Impossible to init Olric provider")
	}
}
