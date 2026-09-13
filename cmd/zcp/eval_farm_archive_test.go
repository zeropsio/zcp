package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/zeropsio/zcp/internal/eval/farm"
)

// archiveFakeS3 is a minimal in-memory path-style S3 double covering
// PUT/GET/HEAD/list-type=2, honoring If-None-Match on PUT (S3's
// conditional-create precondition) so PutArchive's conditional-create
// semantics are actually exercised — statusFakeS3 (eval_farm_run_test.go)
// deliberately does not honor that header.
type archiveFakeS3 struct {
	mu      sync.Mutex
	bucket  string
	objects map[string][]byte
	puts    []string
}

func newArchiveFakeS3Server(t *testing.T) (*httptest.Server, *archiveFakeS3) {
	t.Helper()
	f := &archiveFakeS3{bucket: "zcp-farm", objects: map[string][]byte{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			f.puts = append(f.puts, key)
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
	}))
	t.Cleanup(srv.Close)
	return srv, f
}

func (f *archiveFakeS3) serveList(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	prefix := r.URL.Query().Get("prefix")
	var keys []string
	for key := range f.objects {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	f.mu.Unlock()

	var body strings.Builder
	body.WriteString("<ListBucketResult>")
	for _, key := range keys {
		fmt.Fprintf(&body, "<Contents><Key>%s</Key></Contents>", key)
	}
	body.WriteString("<IsTruncated>false</IsTruncated></ListBucketResult>")
	w.Header().Set("Content-Type", "application/xml")
	_, _ = w.Write([]byte(body.String()))
}

func (f *archiveFakeS3) putCount(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, k := range f.puts {
		if k == key {
			n++
		}
	}
	return n
}

func (f *archiveFakeS3) hasObject(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.objects[key]
	return ok
}

// TestFarmArchive_UnknownBatch_Refused pins §3.7: `farm archive` refuses a
// batch id with no manifest.json, and writes nothing — a partial archive
// (some batches marked, one refused) would leave the console unable to
// explain why a run left the default view.
func TestFarmArchive_UnknownBatch_Refused(t *testing.T) {
	s3Srv, fake := newArchiveFakeS3Server(t)
	t.Setenv("ZCP_FARM_S3_URL", s3Srv.URL)
	t.Setenv("ZCP_FARM_S3_BUCKET", "zcp-farm")
	t.Setenv("ZCP_FARM_S3_KEY", "sink-key")
	t.Setenv("ZCP_FARM_S3_SECRET", "sink-secret")

	var exitCode int
	_, stderr := captureOutput(t, func() {
		exitCode = runFarmArchive([]string{"ghost-batch"}, envrForTest())
	})
	if exitCode == 0 {
		t.Fatalf("exit code = 0, want non-zero (stderr: %s)", stderr)
	}
	if fake.hasObject("batches/ghost-batch/archived.json") {
		t.Errorf("archived.json was written for a batch with no manifest")
	}
}

// TestFarmArchive_Idempotent_SecondCallNoOp pins §3.7's re-archive
// discipline: a second `farm archive` on an already-archived batch exits
// 0 (not an error) but performs no second PUT — the conditional-create
// marker is never overwritten with a new archivedAt/note.
func TestFarmArchive_Idempotent_SecondCallNoOp(t *testing.T) {
	s3Srv, fake := newArchiveFakeS3Server(t)
	t.Setenv("ZCP_FARM_S3_URL", s3Srv.URL)
	t.Setenv("ZCP_FARM_S3_BUCKET", "zcp-farm")
	t.Setenv("ZCP_FARM_S3_KEY", "sink-key")
	t.Setenv("ZCP_FARM_S3_SECRET", "sink-secret")

	cfg, err := farm.ConfigFromEnv()
	if err != nil {
		t.Fatalf("ConfigFromEnv: %v", err)
	}
	sink := farm.NewSinkClient(cfg)
	ctx := context.Background()
	batch := "batch-idempotent-1"
	if err := farm.PutManifest(ctx, sink, batch, farm.BatchManifest{Batch: batch, Set: "gate"}); err != nil {
		t.Fatalf("PutManifest: %v", err)
	}

	var exitCode1 int
	_, stderr1 := captureOutput(t, func() {
		exitCode1 = runFarmArchive([]string{batch}, envrForTest())
	})
	if exitCode1 != 0 {
		t.Fatalf("first call exit code = %d, want 0 (stderr: %s)", exitCode1, stderr1)
	}
	if n := fake.putCount("batches/" + batch + "/archived.json"); n != 1 {
		t.Fatalf("archived.json PUT count after first call = %d, want 1", n)
	}

	var exitCode2 int
	stdout2, stderr2 := captureOutput(t, func() {
		exitCode2 = runFarmArchive([]string{batch}, envrForTest())
	})
	if exitCode2 != 0 {
		t.Fatalf("second call exit code = %d, want 0 (stderr: %s)", exitCode2, stderr2)
	}
	if n := fake.putCount("batches/" + batch + "/archived.json"); n != 1 {
		t.Errorf("archived.json PUT count after second call = %d, want still 1 (no overwrite)", n)
	}
	if !strings.Contains(stdout2, "no-op") {
		t.Errorf("second call stdout = %q, want it to say the re-archive was a no-op", stdout2)
	}
}
