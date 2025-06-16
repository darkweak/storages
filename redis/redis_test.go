package redis_test

import (
	"fmt"
	"testing"
	"time"

	"net/http"

	"github.com/darkweak/storages/core"
	"github.com/darkweak/storages/redis"
	"go.uber.org/zap"
)

const (
	byteKey        = "MyByteKey"
	nonExistentKey = "NonExistentKey"
	baseValue      = "My first data"
)

func getRedisInstance() (core.Storer, error) {
	return redis.Factory(core.CacheProvider{URL: "localhost:6379"}, zap.NewNop().Sugar(), 0)
}

func TestRedisConnectionFactory(t *testing.T) {
	instance, err := getRedisInstance()
	if nil != err {
		t.Error("Shouldn't have panic", err)
	}

	if nil == instance {
		t.Error("Redis should be instanciated")
	}
}

func TestIShouldBeAbleToReadAndWriteDataInRedis(t *testing.T) {
	client, _ := getRedisInstance()

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

func TestRedis_GetRequestInCache(t *testing.T) {
	client, _ := getRedisInstance()
	res := client.Get(nonExistentKey)

	if 0 < len(res) {
		t.Errorf("Key %s should not exist", nonExistentKey)
	}
}

func TestRedis_GetSetRequestInCache_OneByte(t *testing.T) {
	client, _ := getRedisInstance()
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

func TestRedis_SetRequestInCache_TTL(t *testing.T) {
	key := "MyEmptyKey"
	client, _ := getRedisInstance()
	val := []byte("Hello world")
	_ = client.Set(key, val, time.Duration(20)*time.Second)
	time.Sleep(1 * time.Second)

	newValue := client.Get(key)

	if len(newValue) != len(val) {
		t.Errorf("Key %s should be equals to %s, %s provided", key, val, newValue)
	}
}

func TestRedis_DeleteRequestInCache(t *testing.T) {
	client, _ := getRedisInstance()
	client.Delete(byteKey)
	time.Sleep(1 * time.Second)

	if 0 < len(client.Get(byteKey)) {
		t.Errorf("Key %s should not exist", byteKey)
	}
}

func TestRedis_Init(t *testing.T) {
	client, _ := getRedisInstance()
	err := client.Init()

	if nil != err {
		t.Error("Impossible to init Redis provider")
	}
}

const maxCounter = 10

func TestRedis_MapKeys(t *testing.T) {
	client, _ := getRedisInstance()
	prefix := "MAP_KEYS_PREFIX_"

	keys := client.MapKeys(prefix)
	if len(keys) != 0 {
		t.Error("The map should be empty")
	}

	for i := range maxCounter {
		_ = client.Set(fmt.Sprintf("%s%d", prefix, i), []byte(fmt.Sprintf("Hello from %d", i)), time.Second)
	}

	keys = client.MapKeys(prefix)
	if len(keys) != maxCounter {
		t.Errorf("The map should contain %d elements, %d given", maxCounter, len(keys))
	}

	for k, v := range keys {
		if v != "Hello from "+k {
			t.Errorf("Expected Hello from %s, %s given", k, v)
		}
	}
}

func TestRedis_DeleteMany(t *testing.T) {
	client, _ := getRedisInstance()

	if len(client.MapKeys("")) != 12 {
		t.Error("The map should contain 12 elements")
	}

	client.DeleteMany("MAP_KEYS_PREFIX_*")

	if len(client.MapKeys("")) != 2 {
		t.Error("The map should contain 2 element")
	}

	client.DeleteMany("*")

	if len(client.MapKeys("")) != 0 {
		t.Error("The map should be empty")
	}
}

func TestRedis_DeleteRelated(t *testing.T) {
	client, _ := getRedisInstance()
	baseKey := "baseKeyDeleteRelated"
	variedKey1 := "variedKeyDeleteRelated1"
	variedKey2 := "variedKeyDeleteRelated2"
	data := []byte("some data")
	duration := 1 * time.Hour

	// Scenario 1: Basic case with one real_key
	_ = client.SetMultiLevel(baseKey, variedKey1, data, http.Header{}, "etag1", duration, variedKey1)
	time.Sleep(1 * time.Second) // Give time for async operations

	// Verify setup
	if len(client.Get(core.MappingKeyPrefix+baseKey)) == 0 {
		t.Errorf("Mapping key %s should exist before DeleteRelated", core.MappingKeyPrefix+baseKey)
	}
	if len(client.Get(variedKey1)) == 0 {
		t.Errorf("Real key %s should exist before DeleteRelated", variedKey1)
	}

	err := client.DeleteRelated(baseKey)
	if err != nil {
		t.Errorf("DeleteRelated failed for baseKey %s: %v", baseKey, err)
	}
	time.Sleep(1 * time.Second) // Give time for async operations

	if len(client.Get(core.MappingKeyPrefix+baseKey)) != 0 {
		t.Errorf("Mapping key %s should be deleted", core.MappingKeyPrefix+baseKey)
	}
	if len(client.Get(variedKey1)) != 0 {
		t.Errorf("Real key %s should be deleted", variedKey1)
	}

	// Scenario 2: Multiple real_keys
	_ = client.SetMultiLevel(baseKey, variedKey1, data, http.Header{}, "etag1", duration, variedKey1)
	_ = client.SetMultiLevel(baseKey, variedKey2, data, http.Header{}, "etag2", duration, variedKey2)
	time.Sleep(1 * time.Second)

	if len(client.Get(core.MappingKeyPrefix+baseKey)) == 0 {
		t.Errorf("Mapping key %s should exist before DeleteRelated (multiple)", core.MappingKeyPrefix+baseKey)
	}
	if len(client.Get(variedKey1)) == 0 {
		t.Errorf("Real key %s should exist before DeleteRelated (multiple)", variedKey1)
	}
	if len(client.Get(variedKey2)) == 0 {
		t.Errorf("Real key %s should exist before DeleteRelated (multiple)", variedKey2)
	}

	err = client.DeleteRelated(baseKey)
	if err != nil {
		t.Errorf("DeleteRelated failed for baseKey %s (multiple): %v", baseKey, err)
	}
	time.Sleep(1 * time.Second)

	if len(client.Get(core.MappingKeyPrefix+baseKey)) != 0 {
		t.Errorf("Mapping key %s should be deleted (multiple)", core.MappingKeyPrefix+baseKey)
	}
	if len(client.Get(variedKey1)) != 0 {
		t.Errorf("Real key %s should be deleted (multiple)", variedKey1)
	}
	if len(client.Get(variedKey2)) != 0 {
		t.Errorf("Real key %s should be deleted (multiple)", variedKey2)
	}

	// Scenario 3: Non-existent baseKey
	nonExistentBaseKey := "nonExistentBaseKeyForDelete"
	err = client.DeleteRelated(nonExistentBaseKey)
	if err != nil {
		t.Errorf("DeleteRelated for non-existent key %s failed: %v", nonExistentBaseKey, err)
	}
	// No specific verification needed other than no error and no panic

	// Scenario 4: baseKey with empty mapping (StorageMapper exists but no real_keys)
	// emptyMappingBaseKey := "emptyMappingBaseKey"
	// Directly set a dummy mapping key to simulate this scenario, as SetMultiLevel always adds a real_key
	// This requires knowledge of the internal mapping key structure.
	// A proper way would be to have a SetMapping function if this was a common test setup.
	// mappingKeyOnly := core.MappingKeyPrefix + emptyMappingBaseKey
	// Create an empty StorageMapper protobuf message
	// sm := &core.StorageMapper{Mapping: make(map[string]*core.KeyIndex)}
	// smBytes, _ := core.EncodeMapping(sm) // Assuming EncodeMapping exists and works like DecodeMapping's counterpart
	// _ = client.Set(mappingKeyOnly, smBytes, duration)
	// time.Sleep(1 * time.Second)

	// if len(client.Get(mappingKeyOnly)) == 0 {
	// 	t.Errorf("Mapping key %s for empty map test should exist before DeleteRelated", mappingKeyOnly)
	// }

	// err = client.DeleteRelated(emptyMappingBaseKey)
	// if err != nil {
	// 	t.Errorf("DeleteRelated failed for baseKey %s with empty mapping: %v", emptyMappingBaseKey, err)
	// }
	// time.Sleep(1 * time.Second)

	// if len(client.Get(mappingKeyOnly)) != 0 {
	// 	t.Errorf("Mapping key %s for empty map test should be deleted", mappingKeyOnly)
	// }

	// Clean up any other keys that might interfere if tests run in parallel or affect other tests
	client.DeleteMany(baseKey + "*")
	client.DeleteMany(variedKey1)
	client.DeleteMany(variedKey2)
	client.DeleteMany(nonExistentBaseKey + "*")
	// client.DeleteMany(emptyMappingBaseKey + "*")
	// client.DeleteMany(mappingKeyOnly)
}
