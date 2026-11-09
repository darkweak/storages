package redis_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/darkweak/storages/core"
	"github.com/darkweak/storages/redis"
	baseRedis "github.com/redis/rueidis"
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
		t.Errorf("The map should contain 12 elements, %d given", len(client.MapKeys("")))
	}

	client.DeleteMany("MAP_KEYS_PREFIX_")

	if len(client.MapKeys("")) != 2 {
		t.Errorf("The map should contain 2 elements, %d given", len(client.MapKeys("")))
	}

	client.DeleteMany(".+")

	if len(client.MapKeys("")) != 0 {
		t.Errorf("The map should be empty, %d given", len(client.MapKeys("")))
	}
}

func getInspector(t *testing.T) baseRedis.Client {
	t.Helper()

	inspector, err := baseRedis.NewClient(baseRedis.ClientOption{InitAddress: []string{"localhost:6379"}})
	if err != nil {
		t.Fatalf("Impossible to create the inspector, %v given", err)
	}

	t.Cleanup(inspector.Close)

	return inspector
}

func TestRedis_Sets(t *testing.T) {
	client, _ := getRedisInstance()
	client.DeleteMany(".+")
	t.Cleanup(func() { client.DeleteMany(".+") })

	setStorer, ok := client.(core.SetStorer)
	if !ok {
		t.Fatal("The redis storer should implement core.SetStorer")
	}

	key := "SURROGATE_test"

	if err := setStorer.AddToSet(key, []string{"key1", "key2"}, time.Minute); err != nil {
		t.Errorf("The set addition shouldn't error, %v given", err)
	}

	// Duplicated members must be stored once.
	if err := setStorer.AddToSet(key, []string{"key2", "key3"}, time.Minute); err != nil {
		t.Errorf("The set addition shouldn't error, %v given", err)
	}

	if members := setStorer.GetSet(key); len(members) != 3 {
		t.Errorf("The set should contain 3 members, %d given", len(members))
	}

	inspector := getInspector(t)
	ctx := context.Background()

	ttl, _ := inspector.Do(ctx, inspector.B().Pttl().Key(key).Build()).AsInt64()
	if ttl <= 0 || ttl > time.Minute.Milliseconds() {
		t.Errorf("The set should expire within the given duration, %dms given", ttl)
	}

	// A shorter lifetime must not shorten the remaining one.
	if err := setStorer.AddToSet(key, []string{"key4"}, time.Second); err != nil {
		t.Errorf("The set addition shouldn't error, %v given", err)
	}

	ttl, _ = inspector.Do(ctx, inspector.B().Pttl().Key(key).Build()).AsInt64()
	if ttl <= time.Second.Milliseconds() {
		t.Errorf("The set expiration shouldn't be shortened, %dms given", ttl)
	}

	// Souin passes -1 for an infinite lifetime.
	if err := setStorer.AddToSet("SURROGATE_infinite", []string{"key1"}, -1); err != nil {
		t.Errorf("The set addition shouldn't error, %v given", err)
	}

	ttl, _ = inspector.Do(ctx, inspector.B().Pttl().Key("SURROGATE_infinite").Build()).AsInt64()
	if ttl != -1 {
		t.Errorf("The set shouldn't expire, %dms given", ttl)
	}
}

func TestRedis_Sets_LegacyStringMigration(t *testing.T) {
	client, _ := getRedisInstance()
	client.DeleteMany(".+")
	t.Cleanup(func() { client.DeleteMany(".+") })

	setStorer, _ := client.(core.SetStorer)
	key := "SURROGATE_legacy"

	// Legacy format: comma-joined string without expiration.
	if err := client.Set(key, []byte("old1,old2"), -1); err != nil {
		t.Errorf("Impossible to store the legacy value, %v given", err)
	}

	if members := setStorer.GetSet(key); len(members) != 2 {
		t.Errorf("The legacy value should expose 2 members, %d given", len(members))
	}

	if err := setStorer.AddToSet(key, []string{"new1"}, time.Minute); err != nil {
		t.Errorf("The set addition shouldn't error, %v given", err)
	}

	if members := setStorer.GetSet(key); len(members) != 3 {
		t.Errorf("The migrated set should contain 3 members, %d given", len(members))
	}

	inspector := getInspector(t)

	keyType, _ := inspector.Do(context.Background(), inspector.B().Type().Key(key).Build()).ToString()
	if keyType != "set" {
		t.Errorf("The legacy value should be migrated to a native set, %s given", keyType)
	}
}

func TestRedis_WalkSets(t *testing.T) {
	client, _ := getRedisInstance()
	client.DeleteMany(".+")
	t.Cleanup(func() { client.DeleteMany(".+") })

	setStorer, _ := client.(core.SetStorer)
	prefix := "SURROGATE_"

	for i := range 5 {
		if err := setStorer.AddToSet(fmt.Sprintf("%s%d", prefix, i), []string{fmt.Sprintf("key%d", i)}, time.Minute); err != nil {
			t.Errorf("The set addition shouldn't error, %v given", err)
		}
	}

	// An unrelated string key must not be visited.
	_ = client.Set("unrelated", []byte("value"), time.Minute)

	sets := map[string][]string{}

	if err := setStorer.WalkSets(prefix, func(key string, members []string) bool {
		sets[key] = members

		return true
	}); err != nil {
		t.Errorf("The walk shouldn't error, %v given", err)
	}

	if len(sets) != 5 {
		t.Errorf("The walk should visit 5 sets, %d given", len(sets))
	}

	for k, members := range sets {
		if len(members) != 1 || members[0] != "key"+k {
			t.Errorf("Expected [key%s], %v given", k, members)
		}
	}

	visited := 0

	if err := setStorer.WalkSets(prefix, func(string, []string) bool {
		visited++

		return false
	}); err != nil {
		t.Errorf("The walk shouldn't error, %v given", err)
	}

	if visited != 1 {
		t.Errorf("The walk should stop after the first set, %d visited", visited)
	}
}

// Several Souin instances share one Redis and add members to the same
// surrogate set at the same time, including while that set is still stored in
// the legacy comma-joined format. No member may be lost.
func TestRedis_Sets_ConcurrentInstances(t *testing.T) {
	first, _ := getRedisInstance()
	second, _ := getRedisInstance()

	first.DeleteMany(".+")
	t.Cleanup(func() { first.DeleteMany(".+") })

	instances := []core.SetStorer{first.(core.SetStorer), second.(core.SetStorer)}

	const (
		rounds    = 20
		perRound  = 40
		legacyLen = 2
	)

	for round := range rounds {
		key := fmt.Sprintf("SURROGATE_concurrent_%d", round)

		// Legacy format, as written by the previous string-based versions.
		if err := first.Set(key, []byte("legacy1,legacy2"), -1); err != nil {
			t.Fatalf("Impossible to store the legacy value, %v given", err)
		}

		start := make(chan struct{})

		var waitGroup sync.WaitGroup

		for index := range perRound {
			waitGroup.Add(1)

			go func(index int) {
				defer waitGroup.Done()

				<-start

				_ = instances[index%len(instances)].AddToSet(key, []string{fmt.Sprintf("member%d", index)}, -1)
			}(index)
		}

		close(start)
		waitGroup.Wait()

		if members := instances[0].GetSet(key); len(members) != perRound+legacyLen {
			t.Fatalf("Round %d: the set should contain %d members, %d given", round, perRound+legacyLen, len(members))
		}
	}
}
