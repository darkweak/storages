package otter_test

import (
	"testing"

	"github.com/darkweak/storages/core"
	"github.com/darkweak/storages/otter"
	"go.uber.org/zap"
)

const (
	benchListedEntries  = 1000
	benchDeletedEntries = 100
	// benchCacheSize is distinct from the default size so the benchmarks get
	// an instance of their own, otter shares the instances by size.
	benchCacheSize = 100_000
)

var benchUnsupported = map[string]string{}

func newBenchStorer(tb testing.TB) (core.Storer, string) {
	tb.Helper()

	client, err := otter.Factory(core.CacheProvider{
		Configuration: map[string]interface{}{"size": benchCacheSize},
	}, zap.NewNop().Sugar(), 0)
	if err != nil {
		tb.Fatalf("impossible to create the otter instance: %v", err)
	}

	tb.Cleanup(func() { _ = client.Reset() })

	return client, benchPrefix("otter")
}
