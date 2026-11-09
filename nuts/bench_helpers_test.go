package nuts_test

import (
	"testing"

	"github.com/darkweak/storages/core"
	"github.com/darkweak/storages/nuts"
	"go.uber.org/zap"
)

const (
	// ListKeys returns at most 100 keys.
	benchListedEntries  = 100
	benchDeletedEntries = 100
)

var benchUnsupported = map[string]string{}

// newBenchStorer opens a nuts database with the default storer options in a
// dedicated directory, so each benchmark gets an instance of its own.
func newBenchStorer(tb testing.TB) (core.Storer, string) {
	tb.Helper()

	client, err := nuts.Factory(core.CacheProvider{Path: tb.TempDir()}, zap.NewNop().Sugar(), 0)
	if err != nil {
		tb.Fatalf("impossible to create the nuts instance: %v", err)
	}

	tb.Cleanup(func() { _ = client.Reset() })

	return client, benchPrefix("nuts")
}
