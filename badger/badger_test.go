//go:build !wasm && !wasi

package badger_test

import (
	"testing"
	"time"

	"github.com/darkweak/storages/badger"
	"github.com/darkweak/storages/core"
	"go.uber.org/zap"
)

const (
	byteKey        = "MyByteKey"
	nonExistentKey = "NonExistentKey"
	baseValue      = "My first data"
)

func getBadgerInstance() (core.Storer, error) {
	return badger.Factory(core.CacheProvider{}, zap.NewNop().Sugar(), 0)
}

// This test ensure that Badger options are override by the Souin configuration.
func TestCustomBadgerConnectionFactory(t *testing.T) {
	instance, err := getBadgerInstance()

	if nil != err {
		t.Error("Shouldn't have panic")
	}

	if nil == instance {
		t.Error("Badger should be instanciated")
	}
}

func TestBadgerConnectionFactory(t *testing.T) {
	instance, err := getBadgerInstance()

	if nil != err {
		t.Error("Shouldn't have panic")
	}

	if nil == instance {
		t.Error("Badger should be instanciated")
	}
}

func TestIShouldBeAbleToReadAndWriteDataInBadger(t *testing.T) {
	client, _ := getBadgerInstance()

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

func TestBadger_GetRequestInCache(t *testing.T) {
	client, _ := getBadgerInstance()
	res := client.Get(nonExistentKey)

	if 0 < len(res) {
		t.Errorf("Key %s should not exist", nonExistentKey)
	}
}

func TestBadger_GetSetRequestInCache_OneByte(t *testing.T) {
	client, _ := getBadgerInstance()
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

func TestBadger_SetRequestInCache_TTL(t *testing.T) {
	key := "MyEmptyKey"
	client, _ := getBadgerInstance()
	value := []byte("Hello world")
	_ = client.Set(key, value, time.Duration(20)*time.Second)
	time.Sleep(1 * time.Second)

	newValue := client.Get(key)

	if len(newValue) != len(value) {
		t.Errorf("Key %s should be equals to %s, %s provided", key, value, newValue)
	}
}

func TestBadger_SetRequestInCache_Negative_TTL(t *testing.T) {
	client, _ := getBadgerInstance()
	value := []byte("New value")
	_ = client.Set(byteKey, value, -1)

	time.Sleep(1 * time.Second)

	newValue := client.Get(byteKey)

	if len(newValue) != len([]byte{}) {
		t.Errorf("Key %s should be equals to %s, %s provided", byteKey, []byte{}, newValue)
	}
}

func TestBadger_DeleteRequestInCache(t *testing.T) {
	client, _ := getBadgerInstance()
	client.Delete(byteKey)
	time.Sleep(1 * time.Second)

	if 0 < len(client.Get(byteKey)) {
		t.Errorf("Key %s should not exist", byteKey)
	}
}

func TestBadger_Init(t *testing.T) {
	client, _ := getBadgerInstance()
	err := client.Init()

	if nil != err {
		t.Error("Impossible to init Badger provider")
	}
}
