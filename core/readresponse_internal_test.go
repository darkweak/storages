//go:build !wasm && !wasi

package core

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sync"
	"testing"

	"github.com/pierrec/lz4/v4"
)

// TestReadResponse_Concurrency tests the concurrent safety of the real readResponse function in core.go.
func TestReadResponse_Concurrency(t *testing.T) {
	const (
		iterations  = 20
		parallelism = 10
		bodySize    = 1024 * 10
	)

	body := bytes.Repeat([]byte("test-body-data"), bodySize/14)

	// Create HTTP response with Content-Length
	resp := &http.Response{
		StatusCode:    http.StatusOK,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        http.Header{},
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
	}
	resp.Header.Set("Content-Type", "text/plain")
	resp.Header.Set("X-Custom", "test-header")

	// Write response to buffer
	var buf bytes.Buffer

	_ = resp.Write(&buf)

	respBytes := buf.Bytes()

	compressed, err := testCompressResponse(respBytes)
	if err != nil {
		t.Fatalf("failed to compress response: %v", err)
	}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com/test", nil)

	var waitGroup sync.WaitGroup

	errChan := make(chan error, 100)

	for range parallelism {
		waitGroup.Add(1)

		go func() {
			defer waitGroup.Done()

			for range iterations {
				// Each goroutine uses independent data copy
				dataCopy := make([]byte, len(compressed))
				copy(dataCopy, compressed)

				// Use real readResponse from core.go
				resp, err := readResponse(dataCopy, req)
				if err != nil {
					select {
					case errChan <- err:
					default:
					}

					continue
				}

				// Read Body synchronously
				totalRead := 0

				for {
					buf := make([]byte, bodySize)
					n, readErr := resp.Body.Read(buf)
					totalRead += n

					if readErr == io.EOF {
						break
					}

					if readErr != nil {
						select {
						case errChan <- readErr:
						default:
						}

						break
					}
				}

				// Verify read length
				if int64(totalRead) != resp.ContentLength {
					select {
					case errChan <- &testError{
						msg:  "read length mismatch",
						have: int64(totalRead),
						want: resp.ContentLength,
					}:
					default:
					}
				}
			}
		}()
	}

	waitGroup.Wait()
	close(errChan)

	errCount := 0
	for err := range errChan {
		errCount++
		t.Logf("error %d: %v", errCount, err)
	}

	if errCount > 0 {
		t.Errorf("total errors: %d", errCount)
	}
}

type testError struct {
	msg  string
	have int64
	want int64
}

func (e *testError) Error() string {
	return e.msg
}

// testCompressResponse creates LZ4 compressed data from HTTP response bytes.
func testCompressResponse(respBytes []byte) ([]byte, error) {
	var buf bytes.Buffer

	writer := lz4.NewWriter(&buf)

	if _, err := writer.Write(respBytes); err != nil {
		return nil, err
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
