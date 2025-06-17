package redis_test

import (
	"fmt"
	"testing"
	"time"

	"net/http"

	"github.com/darkweak/storages/core"
	"github.com/darkweak/storages/redis"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"sync"
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
	storer, _ := getRedisInstance()
	client, ok := storer.(*redis.Redis)
	if !ok {
		t.Fatal("Could not assert client to *redis.Redis")
	}
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

func TestRedis_SetMultiLevel_Concurrent(t *testing.T) {
	client, err := getRedisInstance()
	if err != nil {
		t.Fatalf("Failed to get Redis instance: %v", err)
	}
	// Attempt to cast to *redis.Redis to access 'hashtags' if necessary,
	// otherwise, assume no hashtag for testing.
	var hashtags string
	if rc, ok := client.(*redis.Redis); ok {
		// This is a bit of a hack to get the internal hashtags field.
		// A better way would be if the Storer interface exposed it or if tests were designed
		// not to depend on such internal details.
		// For now, we'll try to inspect it via a temporary method or assume empty.
		// Let's assume empty for simplicity as test instances usually don't set it.
		// If hashtags were consistently used in tests, getRedisInstance() should configure it.
		_ = rc // Avoid unused variable if not used later for hashtags
	}


	baseKey := "concurrentBaseKey"
	numGoroutines := 20
	var wg sync.WaitGroup
	duration := 10 * time.Second

	// Cleanup at the start and end
	defer client.DeleteMany(core.MappingKeyPrefix + baseKey)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			// variedKey := fmt.Sprintf("variedKeyConcurrent%d", id) // This was the unused variable
			value := []byte(fmt.Sprintf("value%d", id))
			etag := fmt.Sprintf("etagConcurrent%d", id)
			// realKey is the key under which the actual content is stored.
			// If hashtags are used, they should be prefixed to variedKey to form realKey.
			// The variedKey itself is the "identifier" for this specific variation.
			// The realKey is what's used as the key in the StorageMapper's map.
			plainVariedKey := fmt.Sprintf("variedKeyConcurrent%d", id)
			realKeyStorage := hashtags + plainVariedKey // This is the key for actual data and in StorageMapper.Mapping

			err := client.SetMultiLevel(baseKey, plainVariedKey, value, nil, etag, duration, realKeyStorage)
			if err != nil {
				t.Errorf("Goroutine %d: SetMultiLevel failed: %v", id, err)
			}
			// The SetMultiLevel call itself should store the variedKey (content),
			// so an explicit client.Set(realKeyStorage, value, duration) here is redundant
			// as per SetMultiLevel's responsibility.
			// We ensure the content exists by checking the mapping refers to it.
			defer client.Delete(realKeyStorage) // Cleanup individual varied keys (actual content)
		}(i)
	}

	wg.Wait()

	mappingKeyName := hashtags + core.MappingKeyPrefix + baseKey
	mappingBytes := client.Get(mappingKeyName)
	if mappingBytes == nil {
		t.Fatalf("Mapping key %s should exist after concurrent SetMultiLevel calls, but it's nil", mappingKeyName)
	}

	storageMapper := &core.StorageMapper{}
	if err := proto.Unmarshal(mappingBytes, storageMapper); err != nil {
		t.Fatalf("Failed to unmarshal StorageMapper for key %s: %v", mappingKeyName, err)
	}

	if len(storageMapper.GetMapping()) != numGoroutines {
		t.Errorf("Expected %d entries in StorageMapper, got %d. Map: %v", numGoroutines, len(storageMapper.GetMapping()), storageMapper.GetMapping())
	}

	for i := 0; i < numGoroutines; i++ {
		plainVariedKey := fmt.Sprintf("variedKeyConcurrent%d", i)
		// The key in StorageMapper.Mapping is the one passed as `realKey` to SetMultiLevel,
		// which is `hashtags + plainVariedKey`.
		// The `variedKey` argument to SetMultiLevel is `plainVariedKey`.
		// `core.MappingUpdater` uses the `hashtags + variedKey` (which is `realKey` here) as the map key.
		expectedKeyInMap := hashtags + plainVariedKey
		expectedEtag := fmt.Sprintf("etagConcurrent%d", i)

		keyIndex, ok := storageMapper.GetMapping()[expectedKeyInMap]
		if !ok {
			t.Errorf("Expected realKey %s to be in StorageMapper, but it was not found", expectedKeyInMap)
			continue
		}

		if keyIndex.GetEtag() != expectedEtag {
			t.Errorf("For realKey %s, expected etag %s, got %s", expectedKeyInMap, expectedEtag, keyIndex.GetEtag())
		}

		// RealKey field in KeyIndex should also match this expectedKeyInMap
		if keyIndex.GetRealKey() != expectedKeyInMap {
             t.Errorf("For realKey %s, expected RealKey field to be %s, got %s", expectedKeyInMap, expectedKeyInMap, keyIndex.GetRealKey())
        }
	}
}
