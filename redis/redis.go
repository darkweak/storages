package redis

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/darkweak/storages/core"
	lz4 "github.com/pierrec/lz4/v4"
	redis "github.com/redis/rueidis"
	"google.golang.org/protobuf/proto"
)

const (
	// luaCasScript performs an atomic Compare-And-Swap (CAS) operation on a Redis key.
	// It first gets the current value of KEYS[1]. If this value matches ARGV[1],
	// it sets KEYS[1] to ARGV[2]. Otherwise, it does nothing.
	//
	// KEYS[1]: The Redis key to operate on (e.g., a mappingKey).
	// ARGV[1]: The expected current value of KEYS[1].
	//          If the key is expected to not exist, this should be the string "false",
	//          as Redis Lua scripts convert nil replies from GET to Lua's false boolean.
	// ARGV[2]: The new value to set for KEYS[1] if the CAS succeeds.
	//
	// Returns:
	//   1 if the operation was successful (key was set).
	//   0 if the CAS operation failed (i.e., the current value of KEYS[1] did not match ARGV[1]).
	luaCasScript = `
local current_value = redis.call('GET', KEYS[1])
if current_value == ARGV[1] or (current_value == false and ARGV[1] == 'false') then
    redis.call('SET', KEYS[1], ARGV[2])
    return 1
else
    return 0
end
`
	maxCasRetries = 15
)

// Redis provider type.
type Redis struct {
	inClient      redis.Client
	stale         time.Duration
	ctx           context.Context
	logger        core.Logger
	configuration redis.ClientOption
	close         func()
	hashtags      string
}

// Factory function create new Redis instance.
func Factory(redisConfiguration core.CacheProvider, logger core.Logger, stale time.Duration) (core.Storer, error) {
	logger.Debugf(
		"Redis Factory called. URL: '%s', Stale: %s, Custom Configuration Provided: %t, Custom Configuration Type: %T",
		redisConfiguration.URL,
		stale,
		redisConfiguration.Configuration != nil,
		redisConfiguration.Configuration,
	)
	var options redis.ClientOption

	var hashtags string

	redisConfig, err := json.Marshal(redisConfiguration.Configuration)
	if err != nil {
		return nil, err
	}

	if redisConfiguration.Configuration != nil {
		if err := json.Unmarshal(redisConfig, &options); err != nil {
			logger.Infof("Cannot parse your redis configuration: %+v", err)
		}

		if redisConfig, ok := redisConfiguration.Configuration.(map[string]interface{}); ok && redisConfig != nil {
			if value, ok := redisConfig["HashTag"]; ok {
				if v, ok := value.(string); ok {
					hashtags = v
				}
			}
		}
	} else {
		options = redis.ClientOption{
			InitAddress: strings.Split(redisConfiguration.URL, ","),
			SelectDB:    0,
			ClientName:  "souin-redis",
		}
	}

	if options.Dialer.Timeout == 0 {
		options.Dialer.Timeout = time.Second
	}

	if len(options.InitAddress) == 0 {
		return nil, errors.New("no redis addresses given.")
	}

	cli, err := redis.NewClient(options)
	if err != nil {
		return nil, err
	}

	return &Redis{
		inClient:      cli,
		ctx:           context.Background(),
		stale:         stale,
		configuration: options,
		logger:        logger,
		close:         cli.Close,
		hashtags:      hashtags,
	}, err
}

// Name returns the storer name.
func (provider *Redis) Name() string {
	return "REDIS"
}

// Uuid returns an unique identifier.
func (provider *Redis) Uuid() string {
	return fmt.Sprintf(
		"%s-%s-%d-%s-%s",
		strings.Join(provider.configuration.InitAddress, ","),
		provider.configuration.Username,
		provider.configuration.SelectDB,
		provider.configuration.ClientName,
		provider.stale,
	)
}

// ListKeys method returns the list of existing keys.
func (provider *Redis) ListKeys() []string {
	var scan redis.ScanEntry

	var err error

	elements := []string{}

	provider.logger.Debugf("Call the ListKeys function in redis")

	for more := true; more; more = scan.Cursor != 0 {
		if scan, err = provider.inClient.Do(context.Background(), provider.inClient.B().Scan().Cursor(scan.Cursor).Match(provider.hashtags+core.MappingKeyPrefix+"*").Build()).AsScanEntry(); err != nil {
			provider.logger.Errorf("Cannot scan: %v", err)
		}

		for _, element := range scan.Elements {
			value := provider.Get(element)

			mapping, err := core.DecodeMapping(value)
			if err != nil {
				continue
			}

			for _, v := range mapping.GetMapping() {
				if v.GetFreshTime().AsTime().Before(time.Now()) && v.GetStaleTime().AsTime().Before(time.Now()) {
					continue
				}

				elements = append(elements, v.GetRealKey())
			}
		}
	}

	return elements
}

// MapKeys method returns the list of existing keys.
func (provider *Redis) MapKeys(prefix string) map[string]string {
	var scan redis.ScanEntry
	var err error
	kvStore := map[string]string{}
	keys := []string{}

	// 1. Получаем ВСЕ ключи, как и раньше
	for more := true; more; more = scan.Cursor != 0 {
		if scan, err = provider.inClient.Do(context.Background(), provider.inClient.B().Scan().Cursor(scan.Cursor).Match(prefix+"*").Build()).AsScanEntry(); err != nil {
			provider.logger.Errorf("Cannot scan: %v", err)
			return kvStore
		}
		keys = append(keys, scan.Elements...)
	}

	if len(keys) == 0 {
		return kvStore
	}

	// 2. Делаем ОДИН MGET запрос для всех ключей
	vals, err := provider.inClient.Do(provider.ctx, provider.inClient.B().Mget().Key(keys...).Build()).AsStrSlice()
	if err != nil {
		provider.logger.Errorf("Cannot MGET: %v", err)
		return kvStore
	}

	// 3. Собираем карту
	for i, key := range keys {
		k, _ := strings.CutPrefix(key, prefix)
		if i < len(vals) { // Проверяем на случай, если ключ был удален между SCAN и MGET
			kvStore[k] = vals[i]
		}
	}

	return kvStore
}

// GetMultiLevel tries to load the key and check if one of linked keys is a fresh/stale candidate.
func (provider *Redis) GetMultiLevel(key string, req *http.Request, validator *core.Revalidator) (fresh *http.Response, stale *http.Response) {
	b, e := provider.inClient.Do(provider.ctx, provider.inClient.B().Get().Key(provider.hashtags+core.MappingKeyPrefix+key).Build()).AsBytes()
	if e != nil {
		return
	}

	fresh, stale, _ = core.MappingElection(provider, b, req, validator, provider.logger)

	return
}

// SetMultiLevel tries to store the key with the given value and update the mapping key to store metadata.
func (provider *Redis) SetMultiLevel(baseKey, variedKey string, value []byte, variedHeaders http.Header, etag string, duration time.Duration, realKey string) error {
	now := time.Now()

	compressed := new(bytes.Buffer)
	if _, err := lz4.NewWriter(compressed).ReadFrom(bytes.NewReader(value)); err != nil {
		provider.logger.Errorf("Impossible to compress the key %s into Redis, %v", variedKey, err)
		return err
	}

	mappingKey := provider.hashtags + core.MappingKeyPrefix + baseKey
	namespacedVariedKey := provider.hashtags + variedKey

	// Use a dedicated client connection for the transaction.
	// The context `provider.ctx` will be used inside the callback function for redis commands.
	return provider.inClient.Dedicated(func(c redis.DedicatedClient) error {
		// 1. Watch for any changes on the mapping key. If it changes, the transaction will fail.
		if err := c.Do(provider.ctx, c.B().Watch().Key(mappingKey).Build()).Error(); err != nil {
			provider.logger.Errorf("Redis WATCH command failed for key %s: %v", mappingKey, err)
			return err
		}

		// 2. Get the current mapping value.
		v, err := c.Do(provider.ctx, c.B().Get().Key(mappingKey).Build()).AsBytes()
		// redis.Nil is an expected error if the key doesn't exist, so we don't treat it as a failure.
		if err != nil && !redis.IsRedisNil(err) {
			provider.logger.Errorf("Redis GET command failed for key %s in transaction: %v", mappingKey, err)
			// It's a good practice to unwatch if we fail before MULTI/EXEC
			_ = c.Do(provider.ctx, c.B().Unwatch().Build()).Error()
			return err
		}

		// 3. Update the mapping with the new varied key information.
		val, e := core.MappingUpdater(namespacedVariedKey, v, provider.logger, now, now.Add(duration), now.Add(duration+provider.stale), variedHeaders, etag, realKey)
		if e != nil {
			_ = c.Do(provider.ctx, c.B().Unwatch().Build()).Error()
			return e
		}

		// 4. Execute the transaction to set both the data and the new mapping.
		// These commands will be sent to Redis in a single atomic pipeline.
		cmds := make(redis.Commands, 0, 4)
		cmds = append(
			cmds,
			c.B().Multi().Build(),
			c.B().Set().Key(namespacedVariedKey).Value(compressed.String()).Ex(duration+provider.stale).Build(),
			c.B().Set().Key(mappingKey).Value(string(val)).Build(),
			c.B().Exec().Build(),
		)

		resps := c.DoMulti(provider.ctx, cmds...)

		// 5. Check the result of the EXEC command.
		// The last response in the slice corresponds to EXEC.
		if len(resps) > 0 {
			execResp := resps[len(resps)-1]
			// If EXEC response is redis.Nil, it means the transaction was aborted because
			// the WATCHed key was modified by another client.
			if redis.IsRedisNil(execResp.Error()) {
				provider.logger.Warnf("Transaction for key %s aborted due to a data race (WATCH trigger). Retrying might be necessary.", mappingKey)
				// Here you could implement a retry mechanism if needed, e.g., by returning a specific error.
				return errors.New("concurrent modification detected, transaction aborted")
			}

			// Check for other errors during the transaction.
			for _, resp := range resps {
				if err := resp.Error(); err != nil {
					provider.logger.Errorf("An error occurred in Redis transaction: %v", err)
					return err
				}
			}
		}

		provider.logger.Debugf("Successfully stored new varied key %s and updated mapping for %s", variedKey, baseKey)
		return nil
	})
}

// Get method returns the populated response if exists, empty response then.
func (provider *Redis) Get(key string) []byte {
	r, e := provider.inClient.Do(provider.ctx, provider.inClient.B().Get().Key(key).Build()).AsBytes()
	if e != nil && !errors.Is(e, redis.Nil) {
		return nil
	}

	return r
}

// Set method will store the response in Etcd provider.
func (provider *Redis) Set(key string, value []byte, duration time.Duration) error {
	var cmd redis.Completed
	if duration == -1 {
		cmd = provider.inClient.B().Set().Key(key).Value(string(value)).Build()
	} else {
		cmd = provider.inClient.B().Set().Key(key).Value(string(value)).Ex(duration + provider.stale).Build()
	}

	err := provider.inClient.Do(provider.ctx, cmd).Error()
	if err != nil {
		provider.logger.Errorf("Impossible to set value into Redis, %v", err)
	}

	return err
}

// Delete method will delete the response in Etcd provider if exists corresponding to key param.
func (provider *Redis) Delete(key string) {
	_ = provider.inClient.Do(provider.ctx, provider.inClient.B().Del().Key(key).Build())
}

var deleteByPatternScript = redis.NewLuaScript(`
  local cursor = "0"
  local count = 0
  repeat
    local result = redis.call("SCAN", cursor, "MATCH", ARGV[1], "COUNT", "1000")
    cursor = result[1]
    local keys = result[2]
    if #keys > 0 then
      redis.call("DEL", unpack(keys))
      count = count + #keys
    end
  until cursor == "0"
  return count
`)

func (provider *Redis) DeleteMany(key string) {
	// Выполняем скрипт, передавая паттерн как аргумент.
	// Это будет одна сетевая операция со стороны клиента.
	err := deleteByPatternScript.Exec(provider.ctx, provider.inClient, nil, []string{key}).Error()
	if err != nil {
		provider.logger.Errorf("Failed to delete keys by pattern with Lua script: %v", err)
	}
}

// Init method will.
func (provider *Redis) Init() error {
	return nil
}

// Reset method will reset or close provider.
func (provider *Redis) Reset() error {
	_ = provider.inClient.Do(provider.ctx, provider.inClient.B().Flushdb().Build())

	return nil
}

// DeleteRelated deletes the StorageMapper identified by baseKey and all the real_keys (content keys)
// referenced within it. The deletion of all keys (mapper key + content keys) is performed
// atomically using a Redis Lua script.
//
// Parameters:
//   - baseKey: The base key used to identify the StorageMapper.
//
// Returns:
//   - An error if the operation fails, nil otherwise.
func (provider *Redis) DeleteRelated(baseKey string) error {
	mappingKey := provider.hashtags + core.MappingKeyPrefix + baseKey
	byteValue := provider.Get(mappingKey)

	if byteValue == nil {
		provider.logger.Debugf("Mapping key %s not found", mappingKey)
		return nil
	}

	storageMapper := core.StorageMapper{}
	if err := proto.Unmarshal(byteValue, &storageMapper); err != nil {
		provider.logger.Errorf("Cannot unmarshal proto %s, %v", string(byteValue), err)
		return err
	}

	keysToDelete := []string{mappingKey}
	for _, keyIndex := range storageMapper.GetMapping() {
		keysToDelete = append(keysToDelete, keyIndex.GetRealKey())
	}

	if len(keysToDelete) > 1 {
		luaScript := "return redis.call('DEL', unpack(ARGV))"
		err := provider.inClient.Do(provider.ctx, provider.inClient.B().Eval().Script(luaScript).Numkeys(0).Arg(keysToDelete...).Build()).Error()
		if err != nil {
			provider.logger.Errorf("Error executing Lua script for key %s: %v", baseKey, err)
			return err
		}
	} else if len(keysToDelete) == 1 && keysToDelete[0] == mappingKey {
		// Only the mapping key exists, delete it directly
		provider.Delete(mappingKey)
	}

	return nil
}

func (provider *Redis) Reconnect() {
	provider.logger.Debug("Doing nothing on reconnect because rueidis handles it!")
}
