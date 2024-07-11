package etcd_test

import (
	"testing"
	"time"

	"github.com/darkweak/storages/core"
	"github.com/darkweak/storages/etcd"
	"go.uber.org/zap"
)

const (
	byteKey        = "MyByteKey"
	nonExistentKey = "NonExistentKey"
	baseValue      = "My first data"
)

func getEtcdInstance() (core.Storer, error) {
	return etcd.Factory(core.CacheProvider{
		Configuration: map[string]interface{}{
			"Endpoints": []string{"http://localhost:2379"},
		},
	}, zap.NewNop(), 0)
}

func TestEtcdConnectionFactory(t *testing.T) {
	checker := make(chan bool)
	asyncCheck := func() {
		select {
		case <-time.After(3 * time.Second):
			panic("It should not take more than 3 seconds to connect to the etcd instance")
		case <-checker:
		}
	}

	go asyncCheck()

	instance, err := getEtcdInstance()
	checker <- true

	if nil != err {
		t.Error("Shouldn't have panic")
	}

	if nil == instance {
		t.Error("Etcd should be instanciated")
	}
}

func TestIShouldBeAbleToReadAndWriteDataInEtcd(t *testing.T) {
	client, _ := getEtcdInstance()

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

func TestEtcd_GetRequestInCache(t *testing.T) {
	client, _ := getEtcdInstance()
	res := client.Get(nonExistentKey)

	if 0 < len(res) {
		t.Errorf("Key %s should not exist", nonExistentKey)
	}
}

func TestEtcd_GetSetRequestInCache_OneByte(t *testing.T) {
	client, _ := getEtcdInstance()
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

func TestEtcd_SetRequestInCache_TTL(t *testing.T) {
	key := "MyEmptyKey"
	client, _ := getEtcdInstance()
	value := []byte("Hello world")
	_ = client.Set(key, value, time.Duration(20)*time.Second)
	time.Sleep(1 * time.Second)

	newValue := client.Get(key)

	if len(newValue) != len(value) {
		t.Errorf("Key %s should be equals to %s, %s provided", key, value, newValue)
	}
}

func TestEtcd_DeleteRequestInCache(t *testing.T) {
	client, _ := getEtcdInstance()
	client.Delete(byteKey)
	time.Sleep(1 * time.Second)

	if 0 < len(client.Get(byteKey)) {
		t.Errorf("Key %s should not exist", byteKey)
	}
}

func TestEtcd_Init(t *testing.T) {
	client, _ := getEtcdInstance()
	err := client.Init()

	if nil != err {
		t.Error("Impossible to init Etcd provider")
	}
}
