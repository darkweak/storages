package olric_test

import (
	"os"
	"testing"

	"github.com/darkweak/storages/core"
	"github.com/darkweak/storages/olric"
	"go.uber.org/zap"
)

const (
	benchListedEntries  = 200
	benchDeletedEntries = 50
)

var benchUnsupported = map[string]string{}

// newBenchStorer connects to the olricd instance listening on OLRIC_ADDRESS,
// localhost:3320 by default. The keys the benchmark stored are removed
// afterwards.
func newBenchStorer(tb testing.TB) (core.Storer, string) {
	tb.Helper()

	address := os.Getenv("OLRIC_ADDRESS")
	if address == "" {
		address = "localhost:3320"
	}

	client, err := olric.Factory(core.CacheProvider{URL: address}, zap.NewNop().Sugar(), 0)
	if err != nil {
		tb.Skipf("no olric reachable on %s, skipping: %v", address, err)
	}

	if err := client.Init(); err != nil {
		tb.Skipf("no olric reachable on %s, skipping: %v", address, err)
	}

	prefix := benchPrefix("olric")

	tb.Cleanup(func() {
		client.DeleteMany("^(" + core.MappingKeyPrefix + ")?" + prefix)
		_ = client.Reset()
	})

	return client, prefix
}
