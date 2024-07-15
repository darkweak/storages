package nats_test

import (
	"testing"
	"time"

	"github.com/darkweak/storages/core"
	"github.com/darkweak/storages/nats"
	"go.uber.org/zap"
)

const (
	byteKey        = "MyByteKey"
	nonExistentKey = "NonExistentKey"
	baseValue      = "My first data"
)

func getNatsInstance() (core.Storer, error) {
	z, _ := zap.NewDevelopment()
	return nats.Factory(core.CacheProvider{}, z, 0)
}

func TestNatsConnectionFactory(t *testing.T) {
	instance, err := getNatsInstance()

	if nil != err {
		t.Error("Shouldn't have panic", err)
	}

	if nil == instance {
		t.Error("Nats should be instanciated")
	}
}

func TestIShouldBeAbleToReadAndWriteDataInNats(t *testing.T) {
	client, _ := getNatsInstance()

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

func TestNats_GetRequestInCache(t *testing.T) {
	client, _ := getNatsInstance()
	res := client.Get(nonExistentKey)

	if 0 < len(res) {
		t.Errorf("Key %s should not exist", nonExistentKey)
	}
}

func TestNats_GetSetRequestInCache_OneByte(t *testing.T) {
	client, _ := getNatsInstance()
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

func TestNats_SetRequestInCache_TTL(t *testing.T) {
	key := "MyEmptyKey"
	client, _ := getNatsInstance()
	value := []byte("Hello world")
	_ = client.Set(key, value, time.Duration(20)*time.Second)
	time.Sleep(1 * time.Second)

	newValue := client.Get(key)

	if len(newValue) != len(value) {
		t.Errorf("Key %s should be equals to %s, %s provided", key, value, newValue)
	}
}

func TestNats_DeleteRequestInCache(t *testing.T) {
	client, _ := getNatsInstance()
	client.Delete(byteKey)
	time.Sleep(1 * time.Second)

	if 0 < len(client.Get(byteKey)) {
		t.Errorf("Key %s should not exist", byteKey)
	}
}

func TestNats_Init(t *testing.T) {
	client, _ := getNatsInstance()
	err := client.Init()

	if nil != err {
		t.Error("Impossible to init Nats provider")
	}
}
