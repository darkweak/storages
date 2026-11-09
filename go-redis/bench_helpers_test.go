package redis_test

import (
	"os"
	"testing"

	"github.com/darkweak/storages/core"
	redis "github.com/darkweak/storages/go-redis"
	"go.uber.org/zap"
)

const (
	benchListedEntries  = 1000
	benchDeletedEntries = 100
)

var benchUnsupported = map[string]string{}

// newBenchStorer connects to the redis instance shipped by compose.test.yml,
// or to REDIS_ADDRESS, on the database 15 to stay away from the application
// data. The keys the benchmark stored are removed afterwards.
func newBenchStorer(tb testing.TB) (core.Storer, string) {
	tb.Helper()

	address := os.Getenv("REDIS_ADDRESS")
	if address == "" {
		address = "localhost:6379"
	}

	client, err := redis.Factory(core.CacheProvider{
		Configuration: map[string]interface{}{
			"Addrs":      []string{address},
			"DB":         15,
			"ClientName": "storages-bench",
		},
	}, zap.NewNop().Sugar(), 0)
	if err != nil {
		tb.Skipf("no redis reachable on %s, skipping: %v", address, err)
	}

	prefix := benchPrefix("goredis")

	tb.Cleanup(func() {
		client.DeleteMany("^(" + core.MappingKeyPrefix + ")?" + prefix)
		_ = client.Reset()
	})

	return client, prefix
}
