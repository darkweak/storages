package simplefs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/darkweak/storages/core"
	"github.com/jellydator/ttlcache/v3"
	lz4 "github.com/pierrec/lz4/v4"
)

// Simplefs provider type.
type Simplefs struct {
	cache         *ttlcache.Cache[string, []byte]
	stale         time.Duration
	size          int
	path          string
	logger        core.Logger
	actualSize    int64
	directorySize int64
	mu            sync.Mutex
}

func onEvict(path string) error {
	return os.Remove(path)
}

// Factory function create new Simplefs instance.
func Factory(simplefsCfg core.CacheProvider, logger core.Logger, stale time.Duration) (core.Storer, error) {
	var directorySize int64

	storagePath := simplefsCfg.Path
	size := 0
	directorySize = -1

	simplefsConfiguration := simplefsCfg.Configuration
	if simplefsConfiguration != nil {
		if sfsconfig, ok := simplefsConfiguration.(map[string]interface{}); ok {
			if v, found := sfsconfig["size"]; found && v != nil {
				if val, ok := v.(int); ok && val > 0 {
					size = val
				} else if val, ok := v.(float64); ok && val > 0 {
					size = int(val)
				}
			}

			if v, found := sfsconfig["path"]; found && v != nil {
				if val, ok := v.(string); ok {
					storagePath = val
				}
			}

			if v, found := sfsconfig["directory_size"]; found && v != nil {
				if val, ok := v.(int64); ok && val > 0 {
					directorySize = val
				} else if val, ok := v.(float64); ok && val > 0 {
					directorySize = int64(val)
				}
			}
		}
	}

	var err error

	if storagePath == "" {
		logger.Info("No configuration path given, fallback to the current working directory.")

		storagePath, err = os.Getwd()
		if err != nil {
			logger.Errorf("Impossible to init the storage path in this working: %#v", err)

			return nil, err
		}
	}

	cache := ttlcache.New(
		//nolint:gosec
		ttlcache.WithCapacity[string, []byte](uint64(size)),
	)

	if cache == nil {
		err = errors.New("Impossible to initialize the simplefs storage.")
		logger.Error(err)

		return nil, err
	}

	if err := os.MkdirAll(storagePath, 0o777); err != nil {
		logger.Errorf("Impossible to create the storage directory: %#v", err)

		return nil, err
	}

	logger.Infof("Created the storage directory %s if needed", storagePath)

	go cache.Start()

	return &Simplefs{cache: cache, directorySize: directorySize, logger: logger, mu: sync.Mutex{}, path: storagePath, size: size, stale: stale}, nil
}

// Name returns the storer name.
func (provider *Simplefs) Name() string {
	return "SIMPLEFS"
}

// Uuid returns an unique identifier.
func (provider *Simplefs) Uuid() string {
	return fmt.Sprintf("%s-%d", provider.path, provider.size)
}

// MapKeys method returns a map with the key and value.
func (provider *Simplefs) MapKeys(prefix string) map[string]string {
	keys := map[string]string{}

	provider.cache.Range(func(item *ttlcache.Item[string, []byte]) bool {
		if strings.HasPrefix(item.Key(), prefix) {
			k, _ := strings.CutPrefix(item.Key(), prefix)
			keys[k] = string(item.Value())
		}

		return true
	})

	return keys
}

// ListKeys method returns the list of existing keys.
func (provider *Simplefs) ListKeys() []string {
	return provider.cache.Keys()
}

// Get method returns the populated response if exists, empty response then.
func (provider *Simplefs) Get(key string) []byte {
	result := provider.cache.Get(key)
	if result == nil {
		provider.logger.Warnf("Impossible to get the key %s in Simplefs", key)

		return nil
	}

	byteValue, err := os.ReadFile(string(result.Value()))
	if err != nil {
		provider.logger.Errorf("Impossible to read the file %s from Simplefs: %#v", result.Value(), err)

		return result.Value()
	}

	return byteValue
}

// GetMultiLevel tries to load the key and check if one of linked keys is a fresh/stale candidate.
func (provider *Simplefs) GetMultiLevel(key string, req *http.Request, validator *core.Revalidator) (fresh *http.Response, stale *http.Response) {
	val := provider.cache.Get(core.MappingKeyPrefix + key)
	if val == nil {
		provider.logger.Errorf("Impossible to get the mapping key %s in Simplefs", core.MappingKeyPrefix+key)

		return fresh, stale
	}

	fresh, stale, _ = core.MappingElection(provider, val.Value(), req, validator, provider.logger)

	return fresh, stale
}

func (provider *Simplefs) recoverEnoughSpaceIfNeeded(size int64) {
	if provider.directorySize > -1 && provider.actualSize+size > provider.directorySize {
		provider.cache.RangeBackwards(func(item *ttlcache.Item[string, []byte]) bool {
			// Remove the oldest item if there is not enough space.
			//nolint:godox
			// TODO: open a PR to expose a range that iterate on LRU items.
			provider.cache.Delete(string(item.Value()))

			return false
		})

		provider.recoverEnoughSpaceIfNeeded(size)
	}
}

// SetMultiLevel tries to store the key with the given value and update the mapping key to store metadata.
func (provider *Simplefs) SetMultiLevel(baseKey, variedKey string, value []byte, variedHeaders http.Header, etag string, duration time.Duration, realKey string) error {
	now := time.Now()

	compressed := new(bytes.Buffer)
	if _, err := lz4.NewWriter(compressed).ReadFrom(bytes.NewReader(value)); err != nil {
		provider.logger.Errorf("Impossible to compress the key %s into Simplefs, %v", variedKey, err)

		return err
	}

	provider.recoverEnoughSpaceIfNeeded(int64(compressed.Len()))

	joinedFP := filepath.Join(provider.path, url.PathEscape(variedKey))
	//nolint:gosec
	if err := os.WriteFile(joinedFP, compressed.Bytes(), 0o644); err != nil {
		provider.logger.Errorf("Impossible to write the file %s from Simplefs: %#v", variedKey, err)

		return nil
	}

	_ = provider.cache.Set(variedKey, []byte(joinedFP), duration)

	mappingKey := core.MappingKeyPrefix + baseKey
	item := provider.cache.Get(mappingKey)

	if item == nil {
		provider.logger.Warnf("Impossible to get the mapping key %s in Simplefs", mappingKey)

		item = &ttlcache.Item[string, []byte]{}
	}

	val, e := core.MappingUpdater(variedKey, item.Value(), provider.logger, now, now.Add(duration), now.Add(duration+provider.stale), variedHeaders, etag, realKey)
	if e != nil {
		return e
	}

	provider.logger.Debugf("Store the new mapping for the key %s in Simplefs", variedKey)
	// Used to calculate -(now * 2)
	negativeNow, err := time.ParseDuration(fmt.Sprintf("-%ds", time.Now().Nanosecond()*2))
	if err != nil {
		return fmt.Errorf("Impossible to generate the duration: %w", err)
	}

	_ = provider.cache.Set(mappingKey, val, negativeNow)

	return nil
}

// Set method will store the response in Simplefs provider.
func (provider *Simplefs) Set(key string, value []byte, duration time.Duration) error {
	_ = provider.cache.Set(key, value, duration)

	return nil
}

// Delete method will delete the response in Simplefs provider if exists corresponding to key param.
func (provider *Simplefs) Delete(key string) {
	provider.cache.Delete(key)
}

// DeleteMany method will delete the responses in Simplefs provider if exists corresponding to the regex key param.
func (provider *Simplefs) DeleteMany(key string) {
	rgKey, e := regexp.Compile(key)
	if e != nil {
		return
	}

	provider.cache.Range(func(item *ttlcache.Item[string, []byte]) bool {
		if rgKey.MatchString(item.Key()) {
			provider.Delete(item.Key())
		}

		return true
	})
}

// Init method will.
func (provider *Simplefs) Init() error {
	provider.cache.OnInsertion(func(_ context.Context, item *ttlcache.Item[string, []byte]) {
		if strings.Contains(item.Key(), core.MappingKeyPrefix) {
			return
		}

		info, err := os.Stat(string(item.Value()))
		if err != nil {
			provider.logger.Errorf("impossible to get the file size %s: %#v", item.Key(), err)

			return
		}

		provider.mu.Lock()
		provider.actualSize += info.Size()
		provider.logger.Debugf("Actual size add: %d", provider.actualSize, info.Size())
		provider.mu.Unlock()
	})

	provider.cache.OnEviction(func(_ context.Context, _ ttlcache.EvictionReason, item *ttlcache.Item[string, []byte]) {
		if strings.Contains(string(item.Value()), core.MappingKeyPrefix) {
			return
		}

		info, err := os.Stat(string(item.Value()))
		if err != nil {
			provider.logger.Errorf("impossible to get the file size %s: %#v", item.Key(), err)

			return
		}

		provider.mu.Lock()
		provider.actualSize -= info.Size()
		provider.logger.Debugf("Actual size remove: %d", provider.actualSize, info.Size())
		provider.mu.Unlock()

		if err := onEvict(string(item.Value())); err != nil {
			provider.logger.Errorf("impossible to remove the file %s: %#v", item.Key(), err)
		}
	})

	return nil
}

// Reset method will reset or close provider.
func (provider *Simplefs) Reset() error {
	provider.cache.DeleteAll()

	return nil
}
