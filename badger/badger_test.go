package badger

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/darkweak/storages/core"
	"go.uber.org/zap"
)

// newTestBadger is a helper to create a new Badger instance for testing.
// It returns the Badger provider and a cleanup function to be called with defer.
func newTestBadger(tb testing.TB) (*Badger, func()) {
	tb.Helper()
	// Using a Nop logger to keep test output clean.
	// Replace with a test-specific logger if log output needs to be asserted.
	logger := zap.NewNop().Sugar()

	// Factory is expected to create an in-memory Badger instance if Path is empty.
	provider, err := Factory(core.CacheProvider{Path: ""}, logger, 5*time.Minute)
	if err != nil {
		tb.Fatalf("Failed to create Badger instance for test: %v", err)
	}

	badgerProvider, ok := provider.(*Badger)
	if !ok {
		tb.Fatalf("Factory did not return a *Badger instance")
	}

	cleanup := func() {
		// Attempt to reset the database to clear all data.
		if err := badgerProvider.Reset(); err != nil {
			// Log failure to reset, as it might affect subsequent tests if not handled.
			// This should clear all data for the next test.
			tb.Logf("Warning: failed to reset Badger DB: %v", err)
		}
		// Do NOT close badgerProvider.DB.Close() here if Factory caches instances.
		// Let the underlying shared instance remain open. Reset() should handle data clearing.
	}

	return badgerProvider, cleanup
}

func TestBadger_DeleteRelated_Basic(t *testing.T) {
	provider, cleanup := newTestBadger(t)
	defer cleanup()

	baseKey := "baseDeleteTest"
	relatedKeys := map[string]string{
		baseKey:                                   "baseValue",
		core.MappingKeyPrefix + baseKey + "_var1": "idxValue1",
		core.MappingKeyPrefix + baseKey + "_var2": "idxValue2",
		core.SurrogateKeyPrefix + baseKey + "_surA": "surrogateValueA",
		core.SurrogateKeyPrefix + baseKey + "_surB": "surrogateValueB",
		"GET_" + baseKey + "_res1":                "getValue1",
		"GET_" + baseKey + "_res2":                "getValue2",
	}
	unrelatedKeys := map[string]string{
		"otherBase":                                   "otherBaseValue",
		core.MappingKeyPrefix + "otherBase" + "_varX": "otherIdxValue",
		core.SurrogateKeyPrefix + "another" + "_surY": "anotherSurrogate",
		"GET_somethingElse_resZ":                      "anotherGetValue",
	}

	// Populate data
	for k, v := range relatedKeys {
		if err := provider.Set(k, []byte(v), 1*time.Hour); err != nil {
			t.Fatalf("Failed to set related key %s: %v", k, err)
		}
	}
	for k, v := range unrelatedKeys {
		if err := provider.Set(k, []byte(v), 1*time.Hour); err != nil {
			t.Fatalf("Failed to set unrelated key %s: %v", k, err)
		}
	}

	// Call DeleteRelated
	if err := provider.DeleteRelated(baseKey); err != nil {
		t.Fatalf("DeleteRelated(%s) failed: %v", baseKey, err)
	}

	// Verify related keys are deleted
	for k := range relatedKeys {
		if val := provider.Get(k); val != nil {
			t.Errorf("Key %s should have been deleted, but got value: %s", k, string(val))
		}
	}

	// Verify unrelated keys are preserved
	for k, expectedV := range unrelatedKeys {
		if val := provider.Get(k); string(val) != expectedV {
			t.Errorf("Unrelated key %s should have been preserved with value '%s', but got: '%s'", k, expectedV, string(val))
		} else if val == nil {
			t.Errorf("Unrelated key %s should have been preserved with value '%s', but it was deleted.", k, expectedV)
		}
	}
}

func TestBadger_DeleteRelated_Partial(t *testing.T) {
	provider, cleanup := newTestBadger(t)
	defer cleanup()

	baseKey := "partialDeleteTest"
	// Only set the baseKey and one IDX_ key
	keysToSet := map[string]string{
		baseKey:                                   "baseValue",
		core.MappingKeyPrefix + baseKey + "_var1": "idxValue1",
	}
	// This key should not be deleted as it's for a different base
	otherIdxKey := core.MappingKeyPrefix + "otherPartialBase" + "_varX"
	keysToSet[otherIdxKey] = "otherIdxValue"

	for k, v := range keysToSet {
		if err := provider.Set(k, []byte(v), 1*time.Hour); err != nil {
			t.Fatalf("Failed to set key %s: %v", k, err)
		}
	}

	// Call DeleteRelated
	if err := provider.DeleteRelated(baseKey); err != nil {
		t.Fatalf("DeleteRelated(%s) failed for partial set: %v", baseKey, err)
	}

	// Verify targeted keys are deleted
	if val := provider.Get(baseKey); val != nil {
		t.Errorf("Key %s should have been deleted, but got value: %s", baseKey, string(val))
	}
	if val := provider.Get(core.MappingKeyPrefix + baseKey + "_var1"); val != nil {
		t.Errorf("Key %s should have been deleted, but got value: %s", core.MappingKeyPrefix+baseKey+"_var1", string(val))
	}

	// Verify other key is preserved
	if val := provider.Get(otherIdxKey); string(val) != "otherIdxValue" {
		t.Errorf("Key %s should have been preserved, but got: %s", otherIdxKey, string(val))
	}
}

func TestBadger_DeleteRelated_NonExistentBase(t *testing.T) {
	provider, cleanup := newTestBadger(t)
	defer cleanup()

	nonExistentBaseKey := "thisBaseDoesNotExist"
	keysToKeep := map[string]string{
		"someKey": "someValue",
		core.MappingKeyPrefix + "anotherBase" + "_var1": "anotherIdx",
	}

	for k, v := range keysToKeep {
		if err := provider.Set(k, []byte(v), 1*time.Hour); err != nil {
			t.Fatalf("Failed to set key %s: %v", k, err)
		}
	}

	// Call DeleteRelated for a base key that doesn't exist
	if err := provider.DeleteRelated(nonExistentBaseKey); err != nil {
		t.Fatalf("DeleteRelated(%s) for non-existent base key failed: %v", nonExistentBaseKey, err)
	}

	// Verify no keys were accidentally deleted
	for k, expectedV := range keysToKeep {
		if val := provider.Get(k); string(val) != expectedV {
			t.Errorf("Key %s should have been preserved with value '%s', but got: '%s'", k, expectedV, string(val))
		}
	}
}

// Test to ensure DeleteRelated doesn't delete related keys of a *different* base key.
func TestBadger_DeleteRelated_Specificity(t *testing.T) {
	provider, cleanup := newTestBadger(t)
	defer cleanup()

	baseKeyToDelete := "baseToDelete"
	baseKeyToKeep := "baseToKeep"

	// Keys related to baseKeyToDelete
	keysToDelete := map[string]string{
		baseKeyToDelete:                                         "valueToDelete",
		core.MappingKeyPrefix + baseKeyToDelete + "_variant":    "idxToDelete",
		core.SurrogateKeyPrefix + baseKeyToDelete + "_surrogate": "surrogateToDelete",
		"GET_" + baseKeyToDelete + "_resource":                  "getToDelete",
	}

	// Keys related to baseKeyToKeep (these should NOT be deleted)
	keysToKeep := map[string]string{
		baseKeyToKeep:                                       "valueToKeep",
		core.MappingKeyPrefix + baseKeyToKeep + "_variant":  "idxToKeep",
		core.SurrogateKeyPrefix + baseKeyToKeep + "_surrogate": "surrogateToKeep",
		"GET_" + baseKeyToKeep + "_resource":                "getToKeep",
		"unrelatedKey":                                      "unrelatedValue",
	}

	for k, v := range keysToDelete {
		if err := provider.Set(k, []byte(v), 1*time.Hour); err != nil {
			t.Fatalf("Failed to set key %s for deletion: %v", k, err)
		}
	}
	for k, v := range keysToKeep {
		if err := provider.Set(k, []byte(v), 1*time.Hour); err != nil {
			t.Fatalf("Failed to set key %s for preservation: %v", k, err)
		}
	}

	// Call DeleteRelated on baseKeyToDelete
	if err := provider.DeleteRelated(baseKeyToDelete); err != nil {
		t.Fatalf("DeleteRelated(%s) failed: %v", baseKeyToDelete, err)
	}

	// Verify keys related to baseKeyToDelete are deleted
	for k := range keysToDelete {
		if val := provider.Get(k); val != nil {
			t.Errorf("Key %s (related to %s) should have been deleted, but got value: %s", k, baseKeyToDelete, string(val))
		}
	}

	// Verify keys related to baseKeyToKeep (and the unrelated one) are preserved
	for k, expectedV := range keysToKeep {
		val := provider.Get(k)
		if val == nil {
			t.Errorf("Key %s (related to %s or unrelated) should have been preserved, but it was deleted.", k, baseKeyToKeep)
		} else if string(val) != expectedV {
			t.Errorf("Key %s (related to %s or unrelated) should have value '%s', but got: '%s'", k, baseKeyToKeep, expectedV, string(val))
		}
	}
}

// Test for empty baseKey string if that's a possible input,
// and how the function should behave (e.g. error out, or do nothing).
// Based on current implementation, it would try to delete "", "IDX_", "SURROGATE_", "GET_"
// which is probably not intended but won't error.
func TestBadger_DeleteRelated_EmptyBaseKey(t *testing.T) {
	provider, cleanup := newTestBadger(t)
	defer cleanup()

	emptyBaseKey := ""
	// These keys might conflict if "" is used as baseKey in prefixes.
	// e.g. core.MappingKeyPrefix + "" -> "IDX_"
	// BadgerDB does not allow empty keys for Set, so we don't set "" itself.
	// We test if DeleteRelated("") correctly deletes keys that are just the prefixes.
	keysToSetUp := map[string]string{
		core.MappingKeyPrefix:               "idxRoot",    // "IDX_" + ""
		core.SurrogateKeyPrefix:             "surrogateRoot",// "SURROGATE_" + ""
		"GET_":                              "getRoot",    // "GET_" + ""
		core.MappingKeyPrefix + "something": "idxSomething", // Should not be deleted
		"randomKey":                         "randomValue",  // Should not be deleted
	}

	// keysToDeleteWithEmptyBase and keysToPreserve are implicitly covered by checking keysToSetUp after expected error.
	// No need to declare them separately if the error occurs before their specific logic is tested.

	for k, v := range keysToSetUp {
		if err := provider.Set(k, []byte(v), 1*time.Hour); err != nil {
			t.Fatalf("Failed to set key %s: %v", k, err)
		}
	}

	// Call DeleteRelated with an empty baseKey
	err := provider.DeleteRelated(emptyBaseKey)
	if err == nil {
		t.Fatalf("DeleteRelated(\"\") was expected to fail, but it succeeded.")
	}

	// Optionally, check for the specific error message if it's consistent.
	// BadgerDB's error for empty keys in Delete is "Key cannot be empty".
	expectedErrorMsg := "Key cannot be empty"
	if !strings.Contains(err.Error(), expectedErrorMsg) {
		t.Errorf("DeleteRelated(\"\") failed with an unexpected error. Got: %v, Expected to contain: %s", err, expectedErrorMsg)
	}

	// Since DeleteRelated("") errors out early, no keys should have been deleted.
	// Verify all originally set keys are still present.
	for k, v := range keysToSetUp {
		if val := provider.Get(k); string(val) != v {
			t.Errorf("Key %s should have been preserved due to early error, but got: %s (expected %s)", k, string(val), v)
		}
	}
}

func ExampleBadger_DeleteRelated() {
	// This is an example function, it won't be run by `go test`
	// but can be run by `go test -run=ExampleBadger_DeleteRelated`
	// and serves as documentation.

	logger := zap.NewNop().Sugar()
	provider, err := Factory(core.CacheProvider{Path: ""}, logger, 5*time.Minute)
	if err != nil {
		fmt.Printf("Failed to create Badger instance: %v\n", err)
		return
	}
	badgerProvider := provider.(*Badger)
	// Defer Reset and Close to attempt to clean up the instance used in the example.
	// Reset clears data. Close would free resources if this was the only user.
	// Given Factory caching, Close might impact other identical Factory users.
	// For an example, showing cleanup is good practice.
	defer func() {
		if rErr := badgerProvider.Reset(); rErr != nil {
			fmt.Printf("Example: Error resetting DB: %v\n", rErr)
		}
		if cErr := badgerProvider.DB.Close(); cErr != nil {
			fmt.Printf("Example: Error closing DB: %v\n", cErr)
		}
	}()

	baseKey := "myDocument"
	idxKey := core.MappingKeyPrefix + baseKey + "/version/1"
	surrogateKey := core.SurrogateKeyPrefix + baseKey + "/user/A"
	getKey := "GET_" + baseKey + "/data.json"
	otherKey := "unrelated"

	_ = badgerProvider.Set(baseKey, []byte("base data"), time.Minute)
	_ = badgerProvider.Set(idxKey, []byte("index data"), time.Minute)
	_ = badgerProvider.Set(surrogateKey, []byte("surrogate data"), time.Minute)
	_ = badgerProvider.Set(getKey, []byte("get data"), time.Minute)
	_ = badgerProvider.Set(otherKey, []byte("other data"), time.Minute)

	fmt.Printf("Before DeleteRelated:\n")
	fmt.Printf("Get(%s): %s\n", baseKey, badgerProvider.Get(baseKey))
	fmt.Printf("Get(%s): %s\n", idxKey, badgerProvider.Get(idxKey))
	fmt.Printf("Get(%s): %s\n", surrogateKey, badgerProvider.Get(surrogateKey))
	fmt.Printf("Get(%s): %s\n", getKey, badgerProvider.Get(getKey))
	fmt.Printf("Get(%s): %s\n", otherKey, badgerProvider.Get(otherKey))

	if err := badgerProvider.DeleteRelated(baseKey); err != nil {
		fmt.Printf("DeleteRelated failed: %v\n", err)
		return
	}

	fmt.Printf("\nAfter DeleteRelated:\n")
	fmt.Printf("Get(%s): %v\n", baseKey, badgerProvider.Get(baseKey))        // Expect nil
	fmt.Printf("Get(%s): %v\n", idxKey, badgerProvider.Get(idxKey))          // Expect nil
	fmt.Printf("Get(%s): %v\n", surrogateKey, badgerProvider.Get(surrogateKey)) // Expect nil
	fmt.Printf("Get(%s): %v\n", getKey, badgerProvider.Get(getKey))          // Expect nil
	fmt.Printf("Get(%s): %s\n", otherKey, badgerProvider.Get(otherKey))      // Expect "other data"

	// Output:
	// Before DeleteRelated:
	// Get(myDocument): base data
	// Get(IDX_myDocument/version/1): index data
	// Get(SURROGATE_myDocument/user/A): surrogate data
	// Get(GET_myDocument/data.json): get data
	// Get(unrelated): other data
	//
	// After DeleteRelated:
	// Get(myDocument): []
	// Get(IDX_myDocument/version/1): []
	// Get(SURROGATE_myDocument/user/A): []
	// Get(GET_myDocument/data.json): []
	// Get(unrelated): other data
}
