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
	elements := []string{}

	provider.logger.Debugf("Call the MapKeys in redis with the prefix %s", prefix)

	for more := true; more; more = scan.Cursor != 0 {
		if scan, err = provider.inClient.Do(context.Background(), provider.inClient.B().Scan().Cursor(scan.Cursor).Match(prefix+"*").Build()).AsScanEntry(); err != nil {
			provider.logger.Errorf("Cannot scan: %v", err)
		}

		elements = append(elements, scan.Elements...)
	}

	for _, key := range elements {
		k, _ := strings.CutPrefix(key, prefix)
		kvStore[k] = string(provider.Get(key))
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

	if err := provider.inClient.Do(provider.ctx, provider.inClient.B().Set().Key(provider.hashtags+variedKey).Value(compressed.String()).Ex(duration+provider.stale).Build()).Error(); err != nil {
		provider.logger.Errorf("Impossible to set value into Redis, %v", err)

		return err
	}

	mappingKey := provider.hashtags + core.MappingKeyPrefix + baseKey

	// The following loop implements an optimistic locking strategy using a Compare-And-Swap (CAS)
	// operation performed by a Lua script. This is crucial for preventing lost updates to the
	// mappingKey when multiple concurrent requests try to modify it simultaneously.
	// Each iteration attempts to update the mappingKey, retrying up to maxCasRetries times
	// if conflicts are detected (i.e., if another process modified the key in between
	// our GET and attempted SET).
	for i := 0; i < maxCasRetries; i++ {
		currentMappingBytes, err := provider.inClient.Do(provider.ctx, provider.inClient.B().Get().Key(mappingKey).Build()).AsBytes()
		expectedOldValue := string(currentMappingBytes)
		isNil := false

		if err != nil {
			if errors.Is(err, redis.Nil) {
				expectedOldValue = "false" // Lua script expects 'false' string for nil
				isNil = true
			} else {
				provider.logger.Errorf("Impossible to get mapping key %s from Redis: %v", mappingKey, err)
				return err
			}
		}

		var newMappingProtoBytes []byte
		newMappingProtoBytes, err = core.MappingUpdater(
			provider.hashtags+variedKey,
			currentMappingBytes, // Pass original bytes, even if nil (MappingUpdater handles nil)
			provider.logger,
			now,
			now.Add(duration),
			now.Add(duration+provider.stale),
			variedHeaders,
			etag,
			realKey,
		)
		if err != nil {
			provider.logger.Errorf("Impossible to update mapping for key %s: %v", mappingKey, err)
			return err
		}

		// Execute Lua script for CAS
		// KEYS[1] = mappingKey
		// ARGV[1] = expectedOldValue (string representation of old value, or "false" if nil)
		// ARGV[2] = newMappingProtoBytes (string representation of new value)
		scriptResult, err := provider.inClient.Do(
			provider.ctx,
			provider.inClient.B().Eval().
				Script(luaCasScript).
				Numkeys(1).Key(mappingKey).
				Arg(expectedOldValue, string(newMappingProtoBytes)).
				Build(),
		).AsInt64()

		if err != nil {
			provider.logger.Errorf("Error executing Lua CAS script for key %s: %v", mappingKey, err)
			return err
		}

		if scriptResult == 1 {
			// CAS successful
			return nil
		}
		// CAS failed (value changed by another process), retry if attempts left
		provider.logger.Debugf("CAS conflict for key %s, current value was nil: %v. Retrying (%d/%d)...", mappingKey, isNil, i+1, maxCasRetries)
		// Optional: time.Sleep(time.Duration(rand.Intn(100)) * time.Millisecond)
	}

	err := fmt.Errorf("failed to set mapping key %s after %d CAS retries due to conflicts", mappingKey, maxCasRetries)
	provider.logger.Error(err.Error())
	return err
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

// DeleteMany method will delete the responses in Redis provider if exists corresponding to the regex key param.
func (provider *Redis) DeleteMany(key string) {
	var scan redis.ScanEntry

	var err error

	elements := []string{}

	provider.logger.Debugf("Call the DeleteMany function in redis")

	for more := true; more; more = scan.Cursor != 0 {
		if scan, err = provider.inClient.Do(context.Background(), provider.inClient.B().Scan().Cursor(scan.Cursor).Match(key).Build()).AsScanEntry(); err != nil {
			provider.logger.Errorf("Cannot scan: %v", err)
		}

		elements = append(elements, scan.Elements...)
	}

	_ = provider.inClient.Do(provider.ctx, provider.inClient.B().Del().Key(elements...).Build())
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
