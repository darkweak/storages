package simplefs_test

import (
	"testing"

	"github.com/darkweak/storages/core"
)

var contractUnsupported = map[string]string{}

func skipUnsupported(tb testing.TB, operation string) {
	tb.Helper()

	if reason, found := contractUnsupported[operation]; found {
		tb.Skip(reason)
	}
}

// newBenchStorer adapts newStorer to the contract tests shared by the storers.
func newBenchStorer(tb testing.TB) (core.Storer, string) {
	tb.Helper()

	client, _ := newStorer(tb, 0, "")

	return client, ""
}
