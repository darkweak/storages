package redis

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/darkweak/storages/core"
	"github.com/pierrec/lz4/v4"
	redis "github.com/redis/rueidis"
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

// defaultClientName is the client name souin expects when computing the storer
// uuid, so it must be applied whatever the way the storer got configured.
// See https://github.com/darkweak/storages/issues/64.
const defaultClientName = "souin-redis"

// The redis.ClientOption fields sanitizeProperties coerces, by type. They are
// kept in sync with rueidis by TestSanitizedFieldsCoverClientOption.
var (
	listFields = []string{"InitAddress", "ClientSetInfo", "ClientTrackingOptions"}
	intFields  = []string{
		"SelectDB", "CacheSizeEachConn", "RingScaleEachConn", "ReadBufferEachConn", "WriteBufferEachConn",
		"BlockingPoolMinSize", "BlockingPoolSize", "BlockingPipeline", "PipelineMultiplex",
	}
	durationFields = []string{"BlockingPoolCleanup", "ConnWriteTimeout", "ConnLifetime", "MaxFlushDelay"}
	boolFields     = []string{
		"DisableTCPNoDelay", "ShuffleInit", "ClientNoTouch", "DisableRetry", "DisableCache",
		"DisableAutoPipelining", "AlwaysPipelining", "AlwaysRESP2", "ForceSingleClient",
		"ReplicaOnly", "ClientNoEvict", "EnableReplicaAZInfo", "AZFromInfo",
	}
)

// sanitizeProperties coerces string scalars to the redis.ClientOption fields
// they address. Caddyfile dispensers tokenize every scalar as a string, so the
// list, numeric, duration and boolean options would otherwise fail to unmarshal
// and be dropped with only a log line (an InitAddress given as a single string
// even made the storer fail with "no redis addresses given"). Values that don't
// parse are left unchanged so the subsequent unmarshal reports them as before.
func sanitizeProperties(configMap map[string]interface{}) map[string]interface{} {
	sanitized := make(map[string]interface{}, len(configMap))
	for key, value := range configMap {
		sanitized[key] = value
	}

	for _, field := range listFields {
		// Arrays are kept untouched: souin joins them as is to compute the uuid
		// it looks the storer up with.
		if value, ok := sanitized[field].(string); ok {
			sanitized[field] = splitList([]string{value})
		}
	}

	for _, field := range intFields {
		if s, ok := sanitized[field].(string); ok {
			if v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil {
				sanitized[field] = v
			}
		}
	}

	sanitizeDurations(sanitized, durationFields)

	for _, field := range boolFields {
		if s, ok := sanitized[field].(string); ok {
			if v, err := strconv.ParseBool(strings.TrimSpace(s)); err == nil {
				sanitized[field] = v
			}
		}
	}

	if dialer, ok := sanitized["Dialer"].(map[string]interface{}); ok {
		sanitized["Dialer"] = sanitizeDialer(dialer)
	}

	if sentinel, ok := sanitized["Sentinel"].(map[string]interface{}); ok {
		copied := make(map[string]interface{}, len(sentinel))
		for key, value := range sentinel {
			copied[key] = value
		}

		if dialer, ok := copied["Dialer"].(map[string]interface{}); ok {
			copied["Dialer"] = sanitizeDialer(dialer)
		}

		sanitized["Sentinel"] = copied
	}

	return sanitized
}

// sanitizeDialer coerces the net.Dialer durations the same way.
func sanitizeDialer(dialer map[string]interface{}) map[string]interface{} {
	copied := make(map[string]interface{}, len(dialer))
	for key, value := range dialer {
		copied[key] = value
	}

	sanitizeDurations(copied, []string{"Timeout", "KeepAlive", "FallbackDelay"})

	return copied
}

// sanitizeDurations converts the string durations to nanoseconds, the way
// encoding/json expects a time.Duration. Both the Go notation ("5s") and a
// bare number of nanoseconds are accepted.
func sanitizeDurations(configMap map[string]interface{}, fields []string) {
	for _, field := range fields {
		raw, ok := configMap[field].(string)
		if !ok {
			continue
		}

		raw = strings.TrimSpace(raw)

		if v, err := time.ParseDuration(raw); err == nil {
			configMap[field] = int64(v)
		} else if v, err := strconv.ParseInt(raw, 10, 64); err == nil {
			configMap[field] = v
		}
	}
}

// splitList flattens comma separated values and drops the empty ones.
func splitList(values []string) []string {
	items := make([]string, 0, len(values))

	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			if item = strings.TrimSpace(item); item != "" {
				items = append(items, item)
			}
		}
	}

	return items
}

// parseOptions builds the rueidis options from the storer configuration.
func parseOptions(redisConfiguration core.CacheProvider, logger core.Logger) (redis.ClientOption, string, error) {
	var options redis.ClientOption

	var hashtags string

	if redisConfiguration.Configuration != nil {
		configuration := redisConfiguration.Configuration

		if configMap, ok := configuration.(map[string]interface{}); ok && configMap != nil {
			configuration = sanitizeProperties(configMap)

			if value, ok := configMap["HashTag"]; ok {
				if v, ok := value.(string); ok {
					hashtags = v
				}
			}
		}

		redisConfig, err := json.Marshal(configuration)
		if err != nil {
			return options, hashtags, err
		}

		// encoding/json skips the fields it cannot decode and keeps going, so
		// the remaining options are still applied.
		if err := json.Unmarshal(redisConfig, &options); err != nil {
			logger.Errorf("Cannot parse your redis configuration, the invalid fields are ignored: %+v", err)
		}
	} else {
		options = redis.ClientOption{
			InitAddress: strings.Split(redisConfiguration.URL, ","),
			SelectDB:    0,
		}
	}

	if options.ClientName == "" {
		options.ClientName = defaultClientName
	}

	if options.Dialer.Timeout == 0 {
		options.Dialer.Timeout = time.Second
	}

	if len(options.InitAddress) == 0 {
		return options, hashtags, errors.New("no redis addresses given")
	}

	return options, hashtags, nil
}

// Factory function create new Redis instance.
func Factory(redisConfiguration core.CacheProvider, logger core.Logger, stale time.Duration) (core.Storer, error) {
	options, hashtags, err := parseOptions(redisConfiguration, logger)
	if err != nil {
		return nil, err
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
		if scan, err = provider.inClient.Do(provider.ctx, provider.inClient.B().Scan().Cursor(scan.Cursor).Match(provider.hashtags+core.MappingKeyPrefix+"*").Count(100).Build()).AsScanEntry(); err != nil {
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
		if scan, err = provider.inClient.Do(provider.ctx, provider.inClient.B().Scan().Cursor(scan.Cursor).Match(prefix+"*").Count(100).Build()).AsScanEntry(); err != nil {
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
	writer := lz4.NewWriter(compressed)

	if _, err := writer.Write(value); err != nil {
		_ = writer.Close()

		provider.logger.Errorf("Impossible to compress the key %s into Redis, %v", variedKey, err)

		return err
	}

	if err := writer.Close(); err != nil {
		provider.logger.Errorf("Impossible to close the compressor for key %s into Redis, %v", variedKey, err)

		return err
	}

	if err := provider.inClient.Do(provider.ctx, provider.inClient.B().Set().Key(provider.hashtags+variedKey).Value(compressed.String()).Ex(duration+provider.stale).Build()).Error(); err != nil {
		provider.logger.Errorf("Impossible to set value into Redis, %v", err)

		return err
	}

	mappingKey := provider.hashtags + core.MappingKeyPrefix + baseKey

	v, err := provider.inClient.Do(provider.ctx, provider.inClient.B().Get().Key(mappingKey).Build()).AsBytes()
	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}

	val, err := core.MappingUpdater(provider.hashtags+variedKey, v, provider.logger, now, now.Add(duration), now.Add(duration+provider.stale), variedHeaders, etag, realKey)
	if err != nil {
		return err
	}

	if err = provider.inClient.Do(provider.ctx, provider.inClient.B().Set().Key(mappingKey).Value(string(val)).Build()).Error(); err != nil {
		provider.logger.Errorf("Impossible to set value into Redis, %v", err)
	}

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

// Set method will store the response in Redis provider.
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

// Delete method will delete the response in Redis provider if exists corresponding to key param.
func (provider *Redis) Delete(key string) {
	_ = provider.inClient.Do(provider.ctx, provider.inClient.B().Del().Key(key).Build())
}

// DeleteMany method will delete the responses in Redis provider if exists corresponding to the regex key param.
func (provider *Redis) DeleteMany(key string) {
	provider.logger.Debugf("Call the DeleteMany function in redis")

	var err error

	var scan redis.ScanEntry

	rgKey, err := regexp.Compile(key)
	if err != nil {
		return
	}

	for more := true; more; more = scan.Cursor != 0 {
		if scan, err = provider.inClient.Do(provider.ctx, provider.inClient.B().Scan().Cursor(scan.Cursor).Match("*").Count(100).Build()).AsScanEntry(); err != nil {
			provider.logger.Errorf("Cannot scan: %v", err)
		}

		elements := []string{}

		for _, element := range scan.Elements {
			if rgKey.MatchString(element) {
				elements = append(elements, element)
			}
		}

		// only unlink item if elements are found in the current iteration
		if len(elements) > 0 {
			if err = provider.inClient.Do(provider.ctx, provider.inClient.B().Unlink().Key(elements...).Build()).Error(); err != nil {
				provider.logger.Errorf("Cannot unlink: %v", err)
			}
		}
	}
}

// Init method will.
func (provider *Redis) Init() error {
	return nil
}

// Reset method will reset or close provider.
func (provider *Redis) Reset() error {
	if provider.close != nil {
		provider.close()
	}

	return nil
}

func (provider *Redis) Reconnect() {
	provider.logger.Debug("Doing nothing on reconnect because rueidis handles it!")
}
