package etcd_test

import (
	"os"
	"testing"

	"github.com/darkweak/storages/core"
	"github.com/darkweak/storages/etcd"
	"go.uber.org/zap"
)

const (
	benchListedEntries  = 200
	benchDeletedEntries = 50
)

var benchUnsupported = map[string]string{}

// newBenchStorer connects to the etcd instance shipped by compose.test.yml,
// or to ETCD_ADDRESS. The keys the benchmark stored are removed afterwards.
func newBenchStorer(tb testing.TB) (core.Storer, string) {
	tb.Helper()

	address := os.Getenv("ETCD_ADDRESS")
	if address == "" {
		address = "http://etcd:2379"
	}

	client, err := etcd.Factory(core.CacheProvider{
		Configuration: map[string]interface{}{"Endpoints": []string{address}},
	}, zap.NewNop().Sugar(), 0)
	if err != nil {
		tb.Skipf("no etcd reachable on %s, skipping: %v", address, err)
	}

	prefix := benchPrefix("etcd")

	tb.Cleanup(func() {
		client.DeleteMany("^(" + core.MappingKeyPrefix + ")?" + prefix)
		_ = client.Reset()
	})

	return client, prefix
}
