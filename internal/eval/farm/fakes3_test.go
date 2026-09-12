package farm

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
)

// fakeS3 is a minimal in-memory path-style S3 double: PUT stores, GET/HEAD
// read, and list-type=2 GETs page through the store two keys at a time so
// TestSinkClient_PutGetList_RoundTripAgainstFake exercises continuation
// tokens without a large fixture. Shared by sink_test.go (via SinkClient)
// and wrapper_test.go (via curl --aws-sigv4, docs/spec-eval-farm.md §2.3).
type fakeS3 struct {
	mu       sync.Mutex
	bucket   string
	objects  map[string][]byte
	putOrder []string
}

// fakeS3Bucket is the one bucket every farm test talks to.
const fakeS3Bucket = "zcp-farm"

func newFakeS3() *fakeS3 {
	return &fakeS3{bucket: fakeS3Bucket, objects: map[string][]byte{}}
}

// get reads one stored object (wrapper_test.go: inspecting an uploaded
// bundle without a second signed round trip).
func (f *fakeS3) get(key string) ([]byte, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.objects[key]
	return v, ok
}

// puts returns every key PUT so far, in PUT order (wrapper_test.go:
// TestWrapper_Success_UploadsPartsThenDoneLast asserts started.json/results/
// capture land before done.json, FM-3).
func (f *fakeS3) puts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.putOrder))
	copy(out, f.putOrder)
	return out
}

func (f *fakeS3) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential=") ||
			!strings.Contains(auth, ", SignedHeaders=") ||
			!strings.Contains(auth, ", Signature=") {
			t.Errorf("Authorization header %q does not have the AWS4-HMAC-SHA256 shape", auth)
			w.WriteHeader(http.StatusForbidden)
			return
		}

		prefix := "/" + f.bucket
		if r.URL.Path == prefix && r.URL.Query().Get("list-type") == "2" {
			f.serveList(w, r)
			return
		}
		key := strings.TrimPrefix(r.URL.Path, prefix+"/")

		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.Method {
		case http.MethodPut:
			if r.Header.Get("If-None-Match") == "*" {
				if _, exists := f.objects[key]; exists {
					w.WriteHeader(http.StatusPreconditionFailed)
					return
				}
			}
			body := make([]byte, r.ContentLength)
			_, _ = io.ReadFull(r.Body, body)
			f.objects[key] = body
			f.putOrder = append(f.putOrder, key)
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			data, ok := f.objects[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write(data)
		case http.MethodHead:
			data, ok := f.objects[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
}

func (f *fakeS3) serveList(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	prefix := r.URL.Query().Get("prefix")
	var keys []string
	for key := range f.objects {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	f.mu.Unlock()

	const pageSize = 2
	start := 0
	if token := r.URL.Query().Get("continuation-token"); token != "" {
		// Malformed input from SinkClient itself would be a bug in this
		// package's own List(), not something this fake needs to report —
		// it would simply page from the start again next iteration.
		_, _ = fmt.Sscanf(token, "%d", &start)
	}
	end := start + pageSize
	truncated := end < len(keys)
	if end > len(keys) {
		end = len(keys)
	}
	page := keys[start:end]

	var body strings.Builder
	body.WriteString("<ListBucketResult>")
	for _, key := range page {
		fmt.Fprintf(&body, "<Contents><Key>%s</Key></Contents>", key)
	}
	if truncated {
		fmt.Fprintf(&body, "<IsTruncated>true</IsTruncated><NextContinuationToken>%d</NextContinuationToken>", end)
	} else {
		body.WriteString("<IsTruncated>false</IsTruncated>")
	}
	body.WriteString("</ListBucketResult>")

	w.Header().Set("Content-Type", "application/xml")
	_, _ = w.Write([]byte(body.String()))
}
