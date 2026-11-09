package nuts_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/darkweak/storages/core"
)

// acceptEncoding is the header the varied responses depend on.
const (
	acceptEncoding = "Accept-Encoding"
	gzipEncoding   = "gzip"
)

// The contract tests pin the observable behavior of the storer operations, so
// they pass the same way before and after any internal change.

func TestContract_SetGet(t *testing.T) {
	client, prefix := newBenchStorer(t)
	value := []byte("contract value")

	if err := client.Set(prefix+"key", value, time.Minute); err != nil {
		t.Fatalf("impossible to store the key: %v", err)
	}

	if got := client.Get(prefix + "key"); !bytes.Equal(got, value) {
		t.Fatalf("got %q, want %q", got, value)
	}

	if got := client.Get(prefix + "missing"); len(got) != 0 {
		t.Fatalf("a missing key should return nothing, got %q", got)
	}
}

func TestContract_MultiLevel(t *testing.T) {
	skipUnsupported(t, "GetMultiLevel")

	client, prefix := newBenchStorer(t)
	baseKey := prefix + "base"

	variants := []struct {
		name    string
		headers http.Header
		request string
	}{
		{name: gzipEncoding, headers: http.Header{acceptEncoding: {gzipEncoding}}, request: gzipEncoding},
		{name: "br", headers: http.Header{acceptEncoding: {"br"}}, request: "br"},
		{name: "both", headers: http.Header{acceptEncoding: {gzipEncoding, "br"}}, request: gzipEncoding + ", br"},
	}

	for _, variant := range variants {
		variedKey := baseKey + "-" + variant.name
		body := "body of " + variant.name
		response := []byte("HTTP/1.1 200 OK\r\nContent-Length: " + strconv.Itoa(len(body)) + "\r\n\r\n" + body)

		if err := client.SetMultiLevel(baseKey, variedKey, response, variant.headers, "", time.Minute, variedKey); err != nil {
			t.Fatalf("impossible to store the variant %s: %v", variant.name, err)
		}
	}

	for _, variant := range variants {
		request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
		request.Header.Set(acceptEncoding, variant.request)

		fresh, _ := client.GetMultiLevel(baseKey, request, new(core.Revalidator))
		if fresh == nil {
			t.Fatalf("the variant %s should be fresh", variant.name)
		}

		body, _ := io.ReadAll(fresh.Body)
		if string(body) != "body of "+variant.name {
			t.Fatalf("the variant %s returned the body %q", variant.name, body)
		}
	}

	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	request.Header.Set(acceptEncoding, "zstd")

	if fresh, stale := client.GetMultiLevel(baseKey, request, new(core.Revalidator)); fresh != nil || stale != nil {
		t.Fatal("no variant should match an unknown header value")
	}

	if fresh, stale := client.GetMultiLevel(prefix+"unknown", request, new(core.Revalidator)); fresh != nil || stale != nil {
		t.Fatal("an unknown base key should match nothing")
	}
}

func TestContract_ListKeys(t *testing.T) {
	skipUnsupported(t, "ListKeys")

	client, prefix := newBenchStorer(t)
	response := []byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")

	expected := make([]string, 0, 5)

	for i := range 5 {
		key := prefix + "list_" + strconv.Itoa(i)
		expected = append(expected, key)

		if err := client.SetMultiLevel(key, key, response, http.Header{}, "", time.Minute, key); err != nil {
			t.Fatalf("impossible to store the key %s: %v", key, err)
		}
	}

	listed := client.ListKeys()

	for _, key := range expected {
		if !slices.Contains(listed, key) {
			t.Fatalf("the key %s should be listed among %d keys", key, len(listed))
		}
	}
}

func TestContract_MapKeys(t *testing.T) {
	client, prefix := newBenchStorer(t)

	// The dot must be matched literally: the sibling prefix must not leak.
	mapped := prefix + "x.y_"
	sibling := prefix + "xzy_"

	expected := map[string]string{}

	for i := range 150 {
		value := "value " + strconv.Itoa(i)
		expected[strconv.Itoa(i)] = value

		if err := client.Set(mapped+strconv.Itoa(i), []byte(value), time.Minute); err != nil {
			t.Fatalf("impossible to store the key: %v", err)
		}
	}

	if err := client.Set(sibling+"0", []byte("sibling"), time.Minute); err != nil {
		t.Fatalf("impossible to store the key: %v", err)
	}

	got := client.MapKeys(mapped)
	if len(got) != len(expected) {
		t.Fatalf("got %d keys, want %d", len(got), len(expected))
	}

	for key, value := range expected {
		if got[key] != value {
			t.Fatalf("the key %s maps to %q, want %q", key, got[key], value)
		}
	}
}

func TestContract_DeleteMany(t *testing.T) {
	skipUnsupported(t, "DeleteMany")

	client, prefix := newBenchStorer(t)

	// More keys than a single deletion batch.
	for i := range 150 {
		if err := client.Set(prefix+"drop_"+strconv.Itoa(i), []byte("drop"), time.Minute); err != nil {
			t.Fatalf("impossible to store the key: %v", err)
		}
	}

	for i := range 10 {
		if err := client.Set(prefix+"keep_"+strconv.Itoa(i), []byte("keep"), time.Minute); err != nil {
			t.Fatalf("impossible to store the key: %v", err)
		}
	}

	client.DeleteMany("^" + prefix + "drop_")

	for i := range 150 {
		if got := client.Get(prefix + "drop_" + strconv.Itoa(i)); len(got) != 0 {
			t.Fatalf("the key drop_%d should have been deleted", i)
		}
	}

	for i := range 10 {
		if got := client.Get(prefix + "keep_" + strconv.Itoa(i)); string(got) != "keep" {
			t.Fatalf("the key keep_%d should have been kept, got %q", i, got)
		}
	}
}
