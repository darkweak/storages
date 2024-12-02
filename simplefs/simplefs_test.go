package simplefs_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/darkweak/storages/core"
	"github.com/darkweak/storages/simplefs"
	"go.uber.org/zap"
)

const (
	byteKey        = "MyByteKey"
	nonExistentKey = "NonExistentKey"
	baseValue      = "My first data"
)

func getSimplefsInstance() (core.Storer, error) {
	return simplefs.Factory(core.CacheProvider{}, zap.NewNop().Sugar(), 0)
}

// This test ensure that Simplefs options are override by the Souin configuration.
func TestCustomSimplefsConnectionFactory(t *testing.T) {
	instance, err := getSimplefsInstance()

	if nil != err {
		t.Error("Shouldn't have panic")
	}

	if nil == instance {
		t.Error("Simplefs should be instanciated")
	}
}

func TestSimplefsConnectionFactory(t *testing.T) {
	instance, err := getSimplefsInstance()

	if nil != err {
		t.Error("Shouldn't have panic")
	}

	if nil == instance {
		t.Error("Simplefs should be instanciated")
	}
}

func TestIShouldBeAbleToReadAndWriteDataInSimplefs(t *testing.T) {
	client, _ := getSimplefsInstance()

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
	client, _ := getSimplefsInstance()
	res := client.Get(nonExistentKey)

	if 0 < len(res) {
		t.Errorf("Key %s should not exist", nonExistentKey)
	}
}

func TestSimplefs_GetSetRequestInCache_OneByte(t *testing.T) {
	client, _ := getSimplefsInstance()
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
	client, _ := getSimplefsInstance()
	value := []byte("Hello world")
	_ = client.Set(key, value, time.Duration(20)*time.Second)
	time.Sleep(1 * time.Second)

	newValue := client.Get(key)

	if len(newValue) != len(value) {
		t.Errorf("Key %s should be equals to %s, %s provided", key, value, newValue)
	}
}

func TestSimplefs_SetRequestInCache_Negative_TTL(t *testing.T) {
	client, _ := getSimplefsInstance()
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
	client, _ := getSimplefsInstance()
	client.Delete(byteKey)
	time.Sleep(1 * time.Second)

	if 0 < len(client.Get(byteKey)) {
		t.Errorf("Key %s should not exist", byteKey)
	}
}

func TestSimplefs_Init(t *testing.T) {
	client, _ := getSimplefsInstance()
	err := client.Init()

	if nil != err {
		t.Error("Impossible to init Simplefs provider")
	}
}

func TestSimplefs_EvictAfterXSeconds(t *testing.T) {
	client, _ := getSimplefsInstance()
	client.Init()

	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("Test_%d", i)
		_ = client.SetMultiLevel(key, key, []byte(baseValue), http.Header{}, "", 1*time.Second, key)
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
