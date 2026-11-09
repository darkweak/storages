package nats_test

import (
	"os"
	"testing"

	"github.com/darkweak/storages/core"
	"github.com/darkweak/storages/nats"
	"go.uber.org/zap"
)

const (
	benchListedEntries  = 200
	benchDeletedEntries = 50
)

var benchUnsupported = map[string]string{}

// newBenchStorer connects to the nats instance shipped by compose.test.yml,
// or to NATS_ADDRESS. The keys the benchmark stored are removed afterwards.
func newBenchStorer(tb testing.TB) (core.Storer, string) {
	tb.Helper()

	address := os.Getenv("NATS_ADDRESS")
	if address == "" {
		address = "nats://127.0.0.1:4222"
	}

	client, err := nats.Factory(core.CacheProvider{URL: address}, zap.NewNop().Sugar(), 0)
	if err != nil {
		tb.Skipf("no nats reachable on %s, skipping: %v", address, err)
	}

	prefix := benchPrefix("nats")

	tb.Cleanup(func() {
		client.DeleteMany("^(" + core.MappingKeyPrefix + ")?" + prefix)
		_ = client.Reset()
	})

	return client, prefix
}
