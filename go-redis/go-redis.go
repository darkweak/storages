package redis

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/darkweak/storages/core"
	lz4 "github.com/pierrec/lz4/v4"
	redis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// Redis provider type
type Redis struct {
	inClient      redis.UniversalClient
	stale         time.Duration
	ctx           context.Context
	logger        *zap.Logger
	configuration redis.UniversalOptions
	close         func() error
	reconnecting  bool
	hashtags      string
}

// Factory function create new Redis instance
func Factory(redisConfiguration core.CacheProvider, logger *zap.Logger, stale time.Duration) (core.Storer, error) {
	var options redis.UniversalOptions

	var hashtags string

	if redisConfiguration.Configuration != nil {
		bc, _ := json.Marshal(redisConfiguration.Configuration)
		if err := json.Unmarshal(bc, &options); err != nil {
			logger.Sugar().Infof("Cannot parse your redis configuration: %+v", err)
		}
		if redisConfig, ok := redisConfiguration.Configuration.(map[string]interface{}); ok && redisConfig != nil {
			if value, ok := redisConfig["HashTag"]; ok {
				if v, ok := value.(string); ok {
					hashtags = v
				}
			}
		}
	} else {
		options = redis.UniversalOptions{
			Addrs:      strings.Split(redisConfiguration.URL, ","),
			PoolSize:   1000,
			ClientName: "souin-redis",
		}
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

// Name returns the storer name
func (provider *Redis) Name() string {
	return "REDIS"
}

// Uuid returns an unique identifier.
func (provider *Redis) Uuid() string {
	return fmt.Sprintf(
		"%s-%s-%s-%s",
		strings.Join(provider.configuration.Addrs, ","),
		provider.configuration.Username,
		provider.configuration.ClientName,
		provider.stale,
	)
}

// ListKeys method returns the list of existing keys
func (provider *Redis) ListKeys() []string {
	if provider.reconnecting {
		provider.logger.Sugar().Error("Impossible to list the redis keys while reconnecting.")
		return []string{}
	}
	keys := []string{}

	iter := provider.inClient.Scan(provider.ctx, 0, "*", 0).Iterator()
	for iter.Next(provider.ctx) {
		keys = append(keys, string(iter.Val()))
	}
	if err := iter.Err(); err != nil {
		if !provider.reconnecting {
			go provider.Reconnect()
		}
		provider.logger.Sugar().Error(err)
		return []string{}
	}

	return keys
}

// MapKeys method returns the list of existing keys
func (provider *Redis) MapKeys(prefix string) map[string]string {
	m := map[string]string{}
	keys := []string{}

	iter := provider.inClient.Scan(provider.ctx, 0, prefix+"*", 0).Iterator()
	for iter.Next(provider.ctx) {
		keys = append(keys, string(iter.Val()))
	}
	if err := iter.Err(); err != nil {
		return m
	}

	vals, err := provider.inClient.MGet(provider.ctx, keys...).Result()
	if err != nil {
		return m
	}
	for idx, item := range keys {
		k, _ := strings.CutPrefix(item, prefix)
		m[k] = vals[idx].(string)
	}

	return m
}

// GetMultiLevel tries to load the key and check if one of linked keys is a fresh/stale candidate.
func (provider *Redis) GetMultiLevel(key string, req *http.Request, validator *core.Revalidator) (fresh *http.Response, stale *http.Response) {
	b, e := provider.inClient.Get(provider.ctx, provider.hashtags+core.MappingKeyPrefix+key).Bytes()
	if e != nil {
		return fresh, stale
	}

	fresh, stale, _ = core.MappingElection(provider, b, req, validator, provider.logger)

	return fresh, stale
}

// SetMultiLevel tries to store the key with the given value and update the mapping key to store metadata.
func (provider *Redis) SetMultiLevel(baseKey, variedKey string, value []byte, variedHeaders http.Header, etag string, duration time.Duration, realKey string) error {
	now := time.Now()

	compressed := new(bytes.Buffer)
	if _, err := lz4.NewWriter(compressed).ReadFrom(bytes.NewReader(value)); err != nil {
		provider.logger.Sugar().Errorf("Impossible to compress the key %s into Redis, %v", variedKey, err)
		return err
	}

	if err := provider.Set(provider.hashtags+variedKey, compressed.Bytes(), duration); err != nil {
		provider.logger.Sugar().Errorf("Impossible to set value into Redis, %v", err)
		return err
	}

	mappingKey := provider.hashtags + core.MappingKeyPrefix + baseKey
	v, e := provider.inClient.Get(provider.ctx, mappingKey).Bytes()
	if e != nil && !errors.Is(e, redis.Nil) {
		return e
	}

	val, e := core.MappingUpdater(provider.hashtags+variedKey, v, provider.logger, now, now.Add(duration), now.Add(duration+provider.stale), variedHeaders, etag, realKey)
	if e != nil {
		return e
	}

	if e = provider.Set(mappingKey, val, -1); e != nil {
		provider.logger.Sugar().Errorf("Impossible to set value into Redis, %v", e)
	}

	return e
}

// Get method returns the populated response if exists, empty response then
func (provider *Redis) Get(key string) (item []byte) {
	if provider.reconnecting {
		provider.logger.Sugar().Error("Impossible to get the redis key while reconnecting.")
		return
	}
	r, e := provider.inClient.Get(provider.ctx, key).Result()
	if e != nil {
		if e != redis.Nil && !provider.reconnecting {
			go provider.Reconnect()
		}
		return
	}

	item = []byte(r)

	return
}

// Prefix method returns the keys that match the prefix key
func (provider *Redis) Prefix(key string) []string {
	// keys, _ := provider.inClient.Do(provider.ctx, provider.inClient.B().Keys().Pattern(key+"*").Build()).AsStrSlice()
	return []string{}
}

// Set method will store the response in Etcd provider
func (provider *Redis) Set(key string, value []byte, duration time.Duration) error {
	if provider.reconnecting {
		provider.logger.Sugar().Error("Impossible to set the redis value while reconnecting.")
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
		provider.logger.Sugar().Errorf("Impossible to set value into Redis, %v", err)
	}

	return err
}

// Delete method will delete the response in Etcd provider if exists corresponding to key param
func (provider *Redis) Delete(key string) {
	if provider.reconnecting {
		provider.logger.Sugar().Error("Impossible to delete the redis key while reconnecting.")
		return
	}
	_ = provider.inClient.Del(provider.ctx, key)
}

// DeleteMany method will delete the responses in Redis provider if exists corresponding to the regex key param
func (provider *Redis) DeleteMany(key string) {
	if provider.reconnecting {
		provider.logger.Sugar().Error("Impossible to delete the redis keys while reconnecting.")
		return
	}

	re, e := regexp.Compile(key)

	if e != nil {
		return
	}

	keys := []string{}
	iter := provider.inClient.Scan(provider.ctx, 0, "*", 0).Iterator()
	for iter.Next(provider.ctx) {
		if re.MatchString(iter.Val()) {
			keys = append(keys, iter.Val())
		}
	}

	if iter.Err() != nil && !provider.reconnecting {
		go provider.Reconnect()
		return
	}

	provider.inClient.Del(provider.ctx, keys...)
}

// Init method will
func (provider *Redis) Init() error {
	return nil
}

// Reset method will reset or close provider
func (provider *Redis) Reset() error {
	if provider.reconnecting {
		provider.logger.Sugar().Error("Impossible to reset the redis instance while reconnecting.")
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
