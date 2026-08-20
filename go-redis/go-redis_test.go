package redis_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/darkweak/storages/core"
	redis "github.com/darkweak/storages/go-redis"
	baseRedis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	byteKey        = "MyByteKey"
	nonExistentKey = "NonExistentKey"
	baseValue      = "My first data"
	redisAddr      = "localhost:6379"
)

func getRedisInstance() (core.Storer, error) {
	return redis.Factory(core.CacheProvider{URL: redisAddr}, zap.NewNop().Sugar(), 0)
}

func getRedisConfigurationInstance() (core.Storer, error) {
	return redis.Factory(core.CacheProvider{Configuration: map[string]interface{}{
		"Addrs": []string{redisAddr},
	}}, zap.NewNop().Sugar(), 0)
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

func TestRedisConnectionFactoryConfiguration(t *testing.T) {
	instance, err := getRedisConfigurationInstance()
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
	client, _ := getRedisConfigurationInstance()
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
	client, _ := getRedisConfigurationInstance()
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

const maxCount = 10

func TestRedis_MapKeys(t *testing.T) {
	client, _ := getRedisInstance()
	prefix := "MAP_KEYS_PREFIX_"

	keys := client.MapKeys(prefix)
	if len(keys) != 0 {
		t.Error("The map should be empty")
	}

	for i := range maxCount {
		_ = client.Set(fmt.Sprintf("%s%d", prefix, i), []byte(fmt.Sprintf("Hello from %d", i)), time.Second)
	}

	keys = client.MapKeys(prefix)
	if len(keys) != maxCount {
		t.Errorf("The map should contain %d elements, %d given", maxCount, len(keys))
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

	client.DeleteMany("MAP_KEYS_PREFIX_*")

	if len(client.MapKeys("")) != 2 {
		t.Errorf("The map should contain 2 element, %d given", len(client.MapKeys("")))
	}

	client.DeleteMany(".*")

	if len(client.MapKeys("")) != 0 {
		t.Errorf("The map should be empty, %d given", len(client.MapKeys("")))
	}
}

func TestRedis_WalkMappings(t *testing.T) {
	client, _ := getRedisInstance()
	client.DeleteMany(".*")

	walker, ok := client.(core.MappingWalker)
	if !ok {
		t.Fatal("The go-redis storer should implement core.MappingWalker")
	}

	prefix := "WALK_MAPPINGS_PREFIX_"
	// Use more keys than one batch to cover the batch boundary.
	count := 250

	for i := range count {
		_ = client.Set(fmt.Sprintf("%s%d", prefix, i), fmt.Appendf(nil, "Hello from %d", i), time.Minute)
	}

	values := map[string]string{}

	if err := walker.WalkMappings(prefix, func(key string, value []byte) bool {
		values[key] = string(value)

		return true
	}); err != nil {
		t.Errorf("The walk shouldn't error, %v given", err)
	}

	if len(values) != count {
		t.Errorf("The walk should visit %d entries, %d given", count, len(values))
	}

	for k, v := range values {
		if v != "Hello from "+k {
			t.Errorf("Expected Hello from %s, %s given", k, v)
		}
	}

	visited := 0

	if err := walker.WalkMappings(prefix, func(key string, value []byte) bool {
		visited++

		return false
	}); err != nil {
		t.Errorf("The walk shouldn't error, %v given", err)
	}

	if visited != 1 {
		t.Errorf("The walk should stop after the first entry, %d visited", visited)
	}

	client.DeleteMany(".*")
}

func TestRedis_SetMultiLevel_MappingTTL(t *testing.T) {
	client, _ := getRedisInstance()
	client.DeleteMany(".*")

	inspector := baseRedis.NewClient(&baseRedis.Options{Addr: redisAddr})

	defer func() {
		_ = inspector.Close()
	}()

	ctx := context.Background()
	mappingKey := core.MappingKeyPrefix + "base"

	if err := client.SetMultiLevel("base", "varied-short", []byte("value"), http.Header{}, "", 10*time.Second, "varied-short"); err != nil {
		t.Errorf("Impossible to store the value, %v given", err)
	}

	ttl := inspector.TTL(ctx, mappingKey).Val()
	if ttl <= 0 || ttl > 10*time.Second {
		t.Errorf("The mapping key should expire within the entry lifetime, %v given", ttl)
	}

	if err := client.SetMultiLevel("base", "varied-long", []byte("value"), http.Header{}, "", time.Hour, "varied-long"); err != nil {
		t.Errorf("Impossible to store the value, %v given", err)
	}

	ttl = inspector.TTL(ctx, mappingKey).Val()
	if ttl <= 10*time.Second || ttl > time.Hour {
		t.Errorf("The mapping key expiration should be extended by the longer-lived entry, %v given", ttl)
	}

	// A shorter-lived entry must not shorten the mapping key lifetime owned
	// by the longer-lived one.
	if err := client.SetMultiLevel("base", "varied-shorter", []byte("value"), http.Header{}, "", 5*time.Second, "varied-shorter"); err != nil {
		t.Errorf("Impossible to store the value, %v given", err)
	}

	ttl = inspector.TTL(ctx, mappingKey).Val()
	if ttl <= 10*time.Second || ttl > time.Hour {
		t.Errorf("The mapping key expiration shouldn't be shortened, %v given", ttl)
	}

	// Legacy mapping keys stored without expiration must become bounded on
	// their next update.
	if err := inspector.Set(ctx, mappingKey, inspector.Get(ctx, mappingKey).Val(), 0).Err(); err != nil {
		t.Errorf("Impossible to remove the mapping key expiration, %v given", err)
	}

	if err := client.SetMultiLevel("base", "varied-migrated", []byte("value"), http.Header{}, "", 30*time.Second, "varied-migrated"); err != nil {
		t.Errorf("Impossible to store the value, %v given", err)
	}

	ttl = inspector.TTL(ctx, mappingKey).Val()
	if ttl <= 0 || ttl > 30*time.Second {
		t.Errorf("The unbounded mapping key should become bounded, %v given", ttl)
	}

	client.DeleteMany(".*")
}

func TestRedis_Sets(t *testing.T) {
	client, _ := getRedisInstance()
	client.DeleteMany(".*")

	setStorer, ok := client.(core.SetStorer)
	if !ok {
		t.Fatal("The go-redis storer should implement core.SetStorer")
	}

	key := "SURROGATE_test"

	if err := setStorer.AddToSet(key, []string{"key1", "key2"}, time.Minute); err != nil {
		t.Errorf("The set addition shouldn't error, %v given", err)
	}

	// Duplicated members must be stored once.
	if err := setStorer.AddToSet(key, []string{"key2", "key3"}, time.Minute); err != nil {
		t.Errorf("The set addition shouldn't error, %v given", err)
	}

	members := setStorer.GetSet(key)
	if len(members) != 3 {
		t.Errorf("The set should contain 3 members, %d given", len(members))
	}

	inspector := baseRedis.NewClient(&baseRedis.Options{Addr: redisAddr})

	defer func() {
		_ = inspector.Close()
	}()

	ctx := context.Background()

	ttl := inspector.TTL(ctx, key).Val()
	if ttl <= 0 || ttl > time.Minute {
		t.Errorf("The set should expire within the given duration, %v given", ttl)
	}

	// A shorter lifetime must not shorten the remaining one.
	if err := setStorer.AddToSet(key, []string{"key4"}, time.Second); err != nil {
		t.Errorf("The set addition shouldn't error, %v given", err)
	}

	ttl = inspector.TTL(ctx, key).Val()
	if ttl <= time.Second {
		t.Errorf("The set expiration shouldn't be shortened, %v given", ttl)
	}

	client.DeleteMany(".*")
}

func TestRedis_Sets_LegacyStringMigration(t *testing.T) {
	client, _ := getRedisInstance()
	client.DeleteMany(".*")

	setStorer, _ := client.(core.SetStorer)
	key := "SURROGATE_legacy"

	// Legacy format: comma-joined string without expiration.
	if err := client.Set(key, []byte("old1,old2"), -1); err != nil {
		t.Errorf("Impossible to store the legacy value, %v given", err)
	}

	members := setStorer.GetSet(key)
	if len(members) != 2 {
		t.Errorf("The legacy value should expose 2 members, %d given", len(members))
	}

	if err := setStorer.AddToSet(key, []string{"new1"}, time.Minute); err != nil {
		t.Errorf("The set addition shouldn't error, %v given", err)
	}

	members = setStorer.GetSet(key)
	if len(members) != 3 {
		t.Errorf("The migrated set should contain 3 members, %d given", len(members))
	}

	inspector := baseRedis.NewClient(&baseRedis.Options{Addr: redisAddr})

	defer func() {
		_ = inspector.Close()
	}()

	ctx := context.Background()

	if keyType := inspector.Type(ctx, key).Val(); keyType != "set" {
		t.Errorf("The legacy value should be migrated to a native set, %s given", keyType)
	}

	ttl := inspector.TTL(ctx, key).Val()
	if ttl <= 0 || ttl > time.Minute {
		t.Errorf("The migrated set should become bounded, %v given", ttl)
	}

	client.DeleteMany(".*")
}

func TestRedis_WalkSets(t *testing.T) {
	client, _ := getRedisInstance()
	client.DeleteMany(".*")

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

	if err := setStorer.WalkSets(prefix, func(key string, members []string) bool {
		visited++

		return false
	}); err != nil {
		t.Errorf("The walk shouldn't error, %v given", err)
	}

	if visited != 1 {
		t.Errorf("The walk should stop after the first set, %d visited", visited)
	}

	client.DeleteMany(".*")
}
