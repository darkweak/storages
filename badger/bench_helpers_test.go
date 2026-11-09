//go:build !wasm && !wasi

package badger_test

import (
	"testing"

	"github.com/darkweak/storages/badger"
	"github.com/darkweak/storages/core"
	"go.uber.org/zap"
)

const (
	benchListedEntries  = 1000
	benchDeletedEntries = 100
)

var benchUnsupported = map[string]string{}

// newBenchStorer opens a badger database with the default storer options in
// a dedicated directory, so each benchmark gets an instance of its own.
func newBenchStorer(tb testing.TB) (core.Storer, string) {
	tb.Helper()

	client, err := badger.Factory(core.CacheProvider{Path: tb.TempDir()}, zap.NewNop().Sugar(), 0)
	if err != nil {
		tb.Fatalf("impossible to create the badger instance: %v", err)
	}

	tb.Cleanup(func() { _ = client.Reset() })

	return client, benchPrefix("badger")
}
