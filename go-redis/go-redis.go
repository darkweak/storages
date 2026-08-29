package redis

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/darkweak/storages/core"
	"github.com/pierrec/lz4/v4"
	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/proto"
)

// Redis provider type.
type Redis struct {
	inClient      redis.UniversalClient
	stale         time.Duration
	ctx           context.Context
	logger        core.Logger
	configuration redis.UniversalOptions
	close         func() error
	reconnecting  bool
	hashtags      string
}

// Factory function create new Redis instance.
func Factory(redisConfiguration core.CacheProvider, logger core.Logger, stale time.Duration) (core.Storer, error) {
	var options redis.UniversalOptions

	var hashtags string

	if redisConfiguration.Configuration != nil {
		bc, err := json.Marshal(redisConfiguration.Configuration)
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal(bc, &options); err != nil {
			logger.Infof("Cannot parse your redis configuration: %+v", err)
		}

		if redisConfig, ok := redisConfiguration.Configuration.(map[string]interface{}); ok && redisConfig != nil {
			if value, ok := redisConfig["HashTag"]; ok {
				if v, ok := value.(string); ok {
					hashtags = v
				}
			}

			if value, ok := redisConfig["TLSConfig"]; ok {
				tlsConfigBytes, err := json.Marshal(value)
				if err != nil {
					return nil, err
				}

				var tlsConfig tls.Config
				if err = json.Unmarshal(tlsConfigBytes, &tlsConfig); err != nil {
					return nil, err
				}

				options.TLSConfig = &tlsConfig
			}
		}
	} else {
		options = redis.UniversalOptions{
			Addrs:    strings.Split(redisConfiguration.URL, ","),
			PoolSize: 1000,
		}
	}

	if len(options.Addrs) == 0 {
		return nil, errors.New("no redis addresses given")
	}

	if options.ClientName == "" {
		options.ClientName = "souin-redis"
	}

	cli := redis.NewUniversalClient(&options)

	return &Redis{
		inClient:      cli,
		ctx:           context.Background(),
		stale:         stale,
		configuration: options,
		logger:        logger,
		close:         cli.Close,
		hashtags:      hashtags,
	}, nil
}

// Name returns the storer name.
func (provider *Redis) Name() string {
	return "REDIS"
}

// Uuid returns an unique identifier.
func (provider *Redis) Uuid() string {
	return fmt.Sprintf(
		"%s-%s-%d-%s-%s",
		strings.Join(provider.configuration.Addrs, ","),
		provider.configuration.Username,
		provider.configuration.DB,
		provider.configuration.ClientName,
		provider.stale,
	)
}

// ListKeys method returns the list of existing keys.
func (provider *Redis) ListKeys() []string {
	if provider.reconnecting {
		provider.logger.Error("Impossible to list the redis keys while reconnecting.")

		return []string{}
	}

	keys := []string{}

	iter := provider.inClient.Scan(provider.ctx, 0, provider.hashtags+core.MappingKeyPrefix+"*", 100).Iterator()
	for iter.Next(provider.ctx) {
		now := time.Now()

		// Hash-based mappings first: calling Get on them raises WRONGTYPE,
		// which would trigger a reconnect.
		if fields, err := provider.inClient.HGetAll(provider.ctx, iter.Val()).Result(); err == nil && len(fields) > 0 {
			for _, raw := range fields {
				keyItem := &core.KeyIndex{}
				if proto.Unmarshal([]byte(raw), keyItem) != nil {
					continue
				}

				if keyItem.GetFreshTime().AsTime().Before(now) && keyItem.GetStaleTime().AsTime().Before(now) {
					continue
				}

				keys = append(keys, keyItem.GetRealKey())
			}

			continue
		}

		value, err := provider.inClient.Get(provider.ctx, iter.Val()).Bytes()
		if err != nil || len(value) > core.MaxMappingSize {
			continue
		}

		mapping, err := core.DecodeMapping(value)
		if err != nil {
			continue
		}

		for _, v := range mapping.GetMapping() {
			if v.GetFreshTime().AsTime().Before(time.Now()) && v.GetStaleTime().AsTime().Before(time.Now()) {
				continue
			}

			keys = append(keys, v.GetRealKey())
		}
	}

	if err := iter.Err(); err != nil {
		if !provider.reconnecting {
			go provider.Reconnect()
		}

		provider.logger.Error(err)

		return []string{}
	}

	return keys
}

// MapKeys method returns the list of existing keys.
func (provider *Redis) MapKeys(prefix string) map[string]string {
	mapKeys := map[string]string{}

	_ = provider.WalkMappings(prefix, func(key string, value []byte) bool {
		mapKeys[key] = string(value)

		return true
	})

	return mapKeys
}

const mappingBatchSize = 100

// WalkMappings streams the keys matching the prefix and their values in
// bounded batches so the whole mapping index is never loaded in memory at
// once. The walk stops early when walkFn returns false.
func (provider *Redis) WalkMappings(prefix string, walkFn func(key string, value []byte) bool) error {
	if provider.reconnecting {
		provider.logger.Error("Impossible to walk the redis mappings while reconnecting.")

		return errors.New("reconnecting error")
	}

	batch := make([]string, 0, mappingBatchSize)

	flush := func() (bool, error) {
		if len(batch) == 0 {
			return true, nil
		}

		vals, err := provider.inClient.MGet(provider.ctx, batch...).Result()
		if err != nil {
			return false, err
		}

		for idx, item := range batch {
			if idx >= len(vals) || vals[idx] == nil {
				continue
			}

			value, ok := vals[idx].(string)
			if !ok {
				continue
			}

			k, _ := strings.CutPrefix(item, prefix)
			if !walkFn(k, []byte(value)) {
				return false, nil
			}
		}

		batch = batch[:0]

		return true, nil
	}

	iter := provider.inClient.Scan(provider.ctx, 0, prefix+"*", mappingBatchSize).Iterator()
	for iter.Next(provider.ctx) {
		batch = append(batch, iter.Val())

		if len(batch) >= mappingBatchSize {
			if cont, err := flush(); err != nil || !cont {
				return err
			}
		}
	}

	if err := iter.Err(); err != nil {
		return err
	}

	_, err := flush()

	return err
}

// AddToSet stores members in the native set at key, migrating any legacy
// string value first. A positive duration extends the set lifetime without
// ever shortening a longer remaining one, and bounds legacy unbounded keys.
func (provider *Redis) AddToSet(key string, members []string, duration time.Duration) error {
	if provider.reconnecting {
		provider.logger.Error("Impossible to add to the redis set while reconnecting.")

		return errors.New("reconnecting error")
	}

	legacy := provider.legacySetMembers(key)

	values := make([]interface{}, 0, len(members)+len(legacy))
	for _, member := range members {
		values = append(values, member)
	}

	for _, member := range legacy {
		values = append(values, member)
	}

	expire := time.Duration(0)

	if duration > 0 {
		if remaining := provider.inClient.TTL(provider.ctx, key).Val(); remaining < duration {
			expire = duration
		}
	}

	_, err := provider.inClient.TxPipelined(provider.ctx, func(pipe redis.Pipeliner) error {
		if len(legacy) > 0 {
			pipe.Del(provider.ctx, key)
		}

		pipe.SAdd(provider.ctx, key, values...)

		if expire > 0 {
			pipe.Expire(provider.ctx, key, expire)
		}

		return nil
	})
	if err != nil {
		provider.logger.Errorf("Impossible to add members to the set %s into Redis, %v", key, err)
	}

	return err
}

// GetSet returns all members of the set stored at key, supporting sets that
// are still stored in the legacy string format.
func (provider *Redis) GetSet(key string) []string {
	if provider.reconnecting {
		provider.logger.Error("Impossible to get the redis set while reconnecting.")

		return nil
	}

	if legacy := provider.legacySetMembers(key); len(legacy) > 0 {
		return legacy
	}

	members, err := provider.inClient.SMembers(provider.ctx, key).Result()
	if err != nil {
		return nil
	}

	return members
}

// WalkSets visits every set whose key matches the prefix. The walk stops
// early when walkFn returns false.
func (provider *Redis) WalkSets(prefix string, walkFn func(key string, members []string) bool) error {
	if provider.reconnecting {
		provider.logger.Error("Impossible to walk the redis sets while reconnecting.")

		return errors.New("reconnecting error")
	}

	iter := provider.inClient.Scan(provider.ctx, 0, prefix+"*", mappingBatchSize).Iterator()
	for iter.Next(provider.ctx) {
		members := provider.GetSet(iter.Val())
		if len(members) == 0 {
			continue
		}

		key, _ := strings.CutPrefix(iter.Val(), prefix)
		if !walkFn(key, members) {
			return nil
		}
	}

	return iter.Err()
}

// GetMultiLevel tries to load the key and check if one of linked keys is a fresh/stale candidate.
func (provider *Redis) GetMultiLevel(key string, req *http.Request, validator *core.Revalidator) (fresh *http.Response, stale *http.Response) {
	mappingKey := provider.hashtags + core.MappingKeyPrefix + key

	fields, e := provider.inClient.HGetAll(provider.ctx, mappingKey).Result()
	if e == nil {
		if len(fields) == 0 {
			return fresh, stale
		}

		entries := make(map[string][]byte, len(fields))
		for name, raw := range fields {
			entries[name] = []byte(raw)
		}

		fresh, stale, _ = core.MappingElectionEntries(provider, entries, req, validator, provider.logger)

		return fresh, stale
	}

	// WRONGTYPE: the mapping still uses the legacy blob format.
	b, e := provider.inClient.Get(provider.ctx, mappingKey).Bytes()
	if e != nil {
		return fresh, stale
	}

	if len(b) > core.MaxMappingSize {
		provider.logger.Errorf("Dropping the oversized mapping key %s (%d bytes)", mappingKey, len(b))
		provider.inClient.Unlink(provider.ctx, mappingKey)

		return fresh, stale
	}

	fresh, stale, _ = core.MappingElection(provider, b, req, validator, provider.logger)

	return fresh, stale
}

// SetMultiLevel tries to store the key with the given value and update the mapping key to store metadata.
func (provider *Redis) SetMultiLevel(baseKey, variedKey string, value []byte, variedHeaders http.Header, etag string, duration time.Duration, realKey string) error {
	now := time.Now()

	compressed := new(bytes.Buffer)
	writer := lz4.NewWriter(compressed)

	// The lz4 default block size is 4 MB, which makes every compression and
	// later decompression of the value churn 4 MB pooled blocks even for tiny
	// payloads. Cached bodies are usually far smaller, so use the smallest
	// block size. Readers pick the block size up from the frame header.
	if err := writer.Apply(lz4.BlockSizeOption(lz4.Block64Kb)); err != nil {
		provider.logger.Errorf("Impossible to configure the compressor for key %s into Redis, %v", variedKey, err)

		return err
	}

	if _, err := writer.Write(value); err != nil {
		_ = writer.Close()

		provider.logger.Errorf("Impossible to compress the key %s into Redis, %v", variedKey, err)

		return err
	}

	if err := writer.Close(); err != nil {
		provider.logger.Errorf("Impossible to close the compressor for key %s into Redis, %v", variedKey, err)

		return err
	}

	if err := provider.Set(provider.hashtags+variedKey, compressed.Bytes(), duration); err != nil {
		provider.logger.Errorf("Impossible to set value into Redis, %v", err)

		return err
	}

	mappingKey := provider.hashtags + core.MappingKeyPrefix + baseKey

	entry, err := core.MappingEntry(now, now.Add(duration), now.Add(duration+provider.stale), variedHeaders, etag, realKey)
	if err != nil {
		provider.logger.Errorf("Impossible to encode the mapping value for the key %s, %v", variedKey, err)

		return err
	}

	legacyFields, dropLegacy := provider.legacyMappingFields(mappingKey)

	// Bound the mapping key lifetime instead of storing it forever: it only
	// needs to outlive the longest-lived entry it references. Never shorten
	// an expiration owned by a longer-lived entry; TTL returns a negative
	// value for missing keys or keys without expiration, so legacy unbounded
	// mapping keys become bounded on their next update.
	mappingTTL := duration + provider.stale
	if remaining := provider.inClient.TTL(provider.ctx, mappingKey).Val(); remaining > mappingTTL {
		mappingTTL = remaining
	}

	_, err = provider.inClient.TxPipelined(provider.ctx, func(pipe redis.Pipeliner) error {
		if dropLegacy || len(legacyFields) > 0 {
			pipe.Del(provider.ctx, mappingKey)
		}

		if len(legacyFields) > 0 {
			pipe.HSet(provider.ctx, mappingKey, legacyFields)
		}

		pipe.HSet(provider.ctx, mappingKey, provider.hashtags+variedKey, entry)
		pipe.Expire(provider.ctx, mappingKey, mappingTTL)

		return nil
	})
	if err != nil {
		provider.logger.Errorf("Impossible to update the mapping for the key %s into Redis, %v", variedKey, err)
	}

	return err
}

// Get method returns the populated response if exists, empty response then.
func (provider *Redis) Get(key string) (item []byte) {
	if provider.reconnecting {
		provider.logger.Error("Impossible to get the redis key while reconnecting.")

		return
	}

	result, err := provider.inClient.Get(provider.ctx, key).Result()
	if err != nil {
		if !errors.Is(err, redis.Nil) && !provider.reconnecting {
			go provider.Reconnect()
		}

		return
	}

	item = []byte(result)

	return
}

// Prefix method returns the keys that match the prefix key.
func (provider *Redis) Prefix(key string) []string {
	// keys, _ := provider.inClient.Do(provider.ctx, provider.inClient.B().Keys().Pattern(key+"*").Build()).AsStrSlice()
	return []string{}
}

// Set method will store the response in Etcd provider.
func (provider *Redis) Set(key string, value []byte, duration time.Duration) error {
	if provider.reconnecting {
		provider.logger.Error("Impossible to set the redis value while reconnecting.")

		return errors.New("reconnecting error")
	}

	if duration == -1 {
		duration = 0
	} else {
		duration += provider.stale
	}

	err := provider.inClient.Set(provider.ctx, key, value, duration).Err()
	if err != nil {
		if !provider.reconnecting {
			go provider.Reconnect()
		}

		provider.logger.Errorf("Impossible to set value into Redis, %v", err)
	}

	return err
}

// Delete method will delete the response in Etcd provider if exists corresponding to key param.
func (provider *Redis) Delete(key string) {
	if provider.reconnecting {
		provider.logger.Error("Impossible to delete the redis key while reconnecting.")

		return
	}

	_ = provider.inClient.Del(provider.ctx, key)
}

// DeleteMany method will delete the responses in Redis provider if exists corresponding to the regex key param.
func (provider *Redis) DeleteMany(key string) {
	if provider.reconnecting {
		provider.logger.Error("Impossible to delete the redis keys while reconnecting.")

		return
	}

	rgKey, err := regexp.Compile(key)
	if err != nil {
		return
	}

	keys := []string{}
	iter := provider.inClient.Scan(provider.ctx, 0, "*", 100).Iterator()

	for iter.Next(provider.ctx) {
		if rgKey.MatchString(iter.Val()) {
			keys = append(keys, iter.Val())
		}

		if len(keys) >= 100 {
			provider.inClient.Unlink(provider.ctx, keys...)
			keys = keys[:0]
		}
	}

	if iter.Err() != nil && !provider.reconnecting {
		go provider.Reconnect()

		return
	}

	// unlink the rest
	if len(keys) > 0 {
		provider.inClient.Unlink(provider.ctx, keys...)
	}
}

// Init method will.
func (provider *Redis) Init() error {
	return nil
}

// Reset method will reset or close provider.
func (provider *Redis) Reset() error {
	if provider.reconnecting {
		provider.logger.Error("Impossible to reset the redis instance while reconnecting.")

		return nil
	}

	return provider.inClient.Close()
}

func (provider *Redis) Reconnect() {
	provider.reconnecting = true

	if provider.inClient = redis.NewUniversalClient(&provider.configuration); provider.inClient != nil {
		provider.reconnecting = false
	} else {
		time.Sleep(10 * time.Second)
		provider.Reconnect()
	}
}

// legacySetMembers returns the members of a set that is still stored in the
// legacy format: a single comma-joined string value.
func (provider *Redis) legacySetMembers(key string) []string {
	if keyType, _ := provider.inClient.Type(provider.ctx, key).Result(); keyType != "string" {
		return nil
	}

	value, err := provider.inClient.Get(provider.ctx, key).Result()
	if err != nil || value == "" {
		return nil
	}

	return strings.Split(value, ",")
}

// legacyMappingFields converts a legacy mapping blob into per-variant hash
// fields. Oversized or undecodable blobs are reported for dropping instead
// of being decoded.
func (provider *Redis) legacyMappingFields(key string) (map[string]interface{}, bool) {
	if keyType, _ := provider.inClient.Type(provider.ctx, key).Result(); keyType != "string" {
		return nil, false
	}

	value, err := provider.inClient.Get(provider.ctx, key).Bytes()
	if err != nil {
		return nil, false
	}

	if len(value) == 0 {
		return nil, true
	}

	if len(value) > core.MaxMappingSize {
		provider.logger.Errorf("Dropping the oversized mapping key %s (%d bytes)", key, len(value))

		return nil, true
	}

	mapping, err := core.DecodeMapping(value)
	if err != nil {
		return nil, true
	}

	fields := make(map[string]interface{}, len(mapping.GetMapping()))

	for name, item := range mapping.GetMapping() {
		raw, err := proto.Marshal(item)
		if err != nil {
			continue
		}

		fields[name] = raw
	}

	return fields, false
}
