package redis_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/darkweak/storages/core"
	redis "github.com/darkweak/storages/go-redis"
	"github.com/pierrec/lz4/v4"
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
	if err := inspector.Persist(ctx, mappingKey).Err(); err != nil {
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

const dumpedResponse = "HTTP/1.1 200 OK\r\nContent-Length: 5\r\n\r\nhello"

func compressedResponse(t *testing.T) []byte {
	t.Helper()

	buf := new(bytes.Buffer)
	writer := lz4.NewWriter(buf)

	if _, err := writer.Write([]byte(dumpedResponse)); err != nil {
		t.Fatalf("Impossible to compress the response, %v given", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("Impossible to close the compressor, %v given", err)
	}

	return buf.Bytes()
}

func TestRedis_MultiLevel_HashMapping(t *testing.T) {
	client, _ := getRedisInstance()
	client.DeleteMany(".*")

	inspector := baseRedis.NewClient(&baseRedis.Options{Addr: redisAddr})

	defer func() {
		_ = inspector.Close()
	}()

	ctx := context.Background()
	mappingKey := core.MappingKeyPrefix + "base"

	for _, varied := range []string{"varied-1", "varied-2"} {
		if err := client.SetMultiLevel("base", varied, []byte(dumpedResponse), http.Header{}, "", time.Minute, varied); err != nil {
			t.Errorf("Impossible to store the value, %v given", err)
		}
	}

	if keyType := inspector.Type(ctx, mappingKey).Val(); keyType != "hash" {
		t.Errorf("The mapping should be stored as a hash, %s given", keyType)
	}

	if fields := inspector.HLen(ctx, mappingKey).Val(); fields != 2 {
		t.Errorf("The mapping should contain 2 fields, %d given", fields)
	}

	req := httptest.NewRequest(http.MethodGet, "http://domain.com/", nil)

	fresh, _ := client.GetMultiLevel("base", req, &core.Revalidator{})
	if fresh == nil {
		t.Error("The fresh response should be elected from the hash mapping")
	}

	client.DeleteMany(".*")
}

func TestRedis_MultiLevel_LegacyBlobMigration(t *testing.T) {
	client, _ := getRedisInstance()
	client.DeleteMany(".*")

	inspector := baseRedis.NewClient(&baseRedis.Options{Addr: redisAddr})

	defer func() {
		_ = inspector.Close()
	}()

	ctx := context.Background()
	mappingKey := core.MappingKeyPrefix + "base"
	now := time.Now()

	// Legacy format: one protobuf blob for all variants.
	blob, err := core.MappingUpdater("varied-legacy", nil, zap.NewNop().Sugar(), now, now.Add(time.Minute), now.Add(2*time.Minute), http.Header{}, "", "varied-legacy")
	if err != nil {
		t.Fatalf("Impossible to encode the legacy mapping, %v given", err)
	}

	if err := inspector.Set(ctx, mappingKey, blob, time.Minute).Err(); err != nil {
		t.Fatalf("Impossible to store the legacy mapping, %v given", err)
	}

	if err := inspector.Set(ctx, "varied-legacy", compressedResponse(t), time.Minute).Err(); err != nil {
		t.Fatalf("Impossible to store the response, %v given", err)
	}

	req := httptest.NewRequest(http.MethodGet, "http://domain.com/", nil)

	if fresh, _ := client.GetMultiLevel("base", req, &core.Revalidator{}); fresh == nil {
		t.Error("The fresh response should be elected from the legacy blob")
	}

	// Any update must migrate the legacy blob to a hash and keep its entries.
	if err := client.SetMultiLevel("base", "varied-new", []byte(dumpedResponse), http.Header{}, "", time.Minute, "varied-new"); err != nil {
		t.Errorf("Impossible to store the value, %v given", err)
	}

	if keyType := inspector.Type(ctx, mappingKey).Val(); keyType != "hash" {
		t.Errorf("The legacy mapping should be migrated to a hash, %s given", keyType)
	}

	if fields := inspector.HLen(ctx, mappingKey).Val(); fields != 2 {
		t.Errorf("The migrated mapping should contain 2 fields, %d given", fields)
	}

	client.DeleteMany(".*")
}

func TestRedis_MultiLevel_OversizedLegacyBlob(t *testing.T) {
	client, _ := getRedisInstance()
	client.DeleteMany(".*")

	inspector := baseRedis.NewClient(&baseRedis.Options{Addr: redisAddr})

	defer func() {
		_ = inspector.Close()
	}()

	ctx := context.Background()
	mappingKey := core.MappingKeyPrefix + "base"
	oversized := strings.Repeat("a", core.MaxMappingSize+1)

	if err := inspector.Set(ctx, mappingKey, oversized, 0).Err(); err != nil {
		t.Fatalf("Impossible to store the oversized mapping, %v given", err)
	}

	req := httptest.NewRequest(http.MethodGet, "http://domain.com/", nil)

	if fresh, stale := client.GetMultiLevel("base", req, &core.Revalidator{}); fresh != nil || stale != nil {
		t.Error("Nothing should be elected from an oversized mapping")
	}

	if exists := inspector.Exists(ctx, mappingKey).Val(); exists != 0 {
		t.Error("The oversized mapping should be dropped without being decoded")
	}

	// Writes must replace an oversized blob instead of decoding it.
	if err := inspector.Set(ctx, mappingKey, oversized, 0).Err(); err != nil {
		t.Fatalf("Impossible to store the oversized mapping, %v given", err)
	}

	if err := client.SetMultiLevel("base", "varied-new", []byte(dumpedResponse), http.Header{}, "", time.Minute, "varied-new"); err != nil {
		t.Errorf("Impossible to store the value, %v given", err)
	}

	if keyType := inspector.Type(ctx, mappingKey).Val(); keyType != "hash" {
		t.Errorf("The oversized mapping should be replaced by a hash, %s given", keyType)
	}

	if fields := inspector.HLen(ctx, mappingKey).Val(); fields != 1 {
		t.Errorf("The replacement mapping should contain 1 field, %d given", fields)
	}

	client.DeleteMany(".*")
}
