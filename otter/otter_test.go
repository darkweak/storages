package otter_test

import (
	"testing"
	"time"

	"github.com/darkweak/storages/core"
	"github.com/darkweak/storages/otter"
	"go.uber.org/zap"
)

const (
	byteKey        = "MyByteKey"
	nonExistentKey = "NonExistentKey"
	baseValue      = "My first data"
)

func getOtterInstance() (core.Storer, error) {
	return otter.Factory(core.CacheProvider{}, zap.NewNop().Sugar(), 0)
}

// This test ensure that Otter options are override by the Souin configuration.
func TestCustomOtterConnectionFactory(t *testing.T) {
	instance, err := getOtterInstance()

	if nil != err {
		t.Error("Shouldn't have panic")
	}

	if nil == instance {
		t.Error("Otter should be instanciated")
	}
}

func TestOtterConnectionFactory(t *testing.T) {
	instance, err := getOtterInstance()

	if nil != err {
		t.Error("Shouldn't have panic")
	}

	if nil == instance {
		t.Error("Otter should be instanciated")
	}
}

func TestIShouldBeAbleToReadAndWriteDataInOtter(t *testing.T) {
	client, _ := getOtterInstance()

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

func TestOtter_GetRequestInCache(t *testing.T) {
	client, _ := getOtterInstance()
	res := client.Get(nonExistentKey)

	if 0 < len(res) {
		t.Errorf("Key %s should not exist", nonExistentKey)
	}
}

func TestOtter_GetSetRequestInCache_OneByte(t *testing.T) {
	client, _ := getOtterInstance()
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

func TestOtter_SetRequestInCache_TTL(t *testing.T) {
	key := "MyEmptyKey"
	client, _ := getOtterInstance()
	value := []byte("Hello world")
	_ = client.Set(key, value, time.Duration(20)*time.Second)
	time.Sleep(1 * time.Second)

	newValue := client.Get(key)

	if len(newValue) != len(value) {
		t.Errorf("Key %s should be equals to %s, %s provided", key, value, newValue)
	}
}

func TestOtter_SetRequestInCache_Negative_TTL(t *testing.T) {
	client, _ := getOtterInstance()
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

func TestOtter_DeleteRequestInCache(t *testing.T) {
	client, _ := getOtterInstance()
	client.Delete(byteKey)
	time.Sleep(1 * time.Second)

	if 0 < len(client.Get(byteKey)) {
		t.Errorf("Key %s should not exist", byteKey)
	}
}

func TestOtter_Init(t *testing.T) {
	client, _ := getOtterInstance()
	err := client.Init()

	if nil != err {
		t.Error("Impossible to init Otter provider")
	}
}
