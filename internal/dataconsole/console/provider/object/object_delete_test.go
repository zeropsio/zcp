package object

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// newFakeS3Server is a minimal S3-protocol double sufficient to drive
// object.Provider's Delete over real HTTP via the genuine minio-go client —
// there is no in-memory S3 fake vendored (see object_test.go's note; the
// success paths otherwise depend on a live MinIO, covered by the e2e
// conformance suite), and OBJ-AUD-02's fix depends on a real HEAD-then-DELETE
// round trip that the existing ReadOnlyGate-style tests can't exercise (those
// only prove the mutating methods short-circuit BEFORE any network call).
//
// It answers exactly the three requests minio-go issues for this path:
//   - the one-time bucket-location probe (GET <bucket>/?location) every
//     fresh client issues before its first real S3 call, answered with a
//     fixed empty-region XML so it never tries to reach real AWS;
//   - HEAD <bucket>/<key> (existence), 200 for a key in `existing`, 404 for
//     any other key;
//   - DELETE <bucket>/<key>, always 204 — this is S3's own real, spec-
//     mandated idempotent-delete behavior (removing a key that was never
//     there is not an error), deliberately reproduced here because it is
//     the exact condition that makes OBJ-AUD-02 possible. The fake does NOT
//     prove anything about honesty on its own; the provider's own
//     Stat-before-delete guard is what these tests exercise.
func newFakeS3Server(t *testing.T, bucket string, existing map[string]bool) *httptest.Server {
	t.Helper()
	prefix := "/" + bucket + "/"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("location") {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/"></LocationConstraint>`))
			return
		}
		key := strings.TrimPrefix(r.URL.Path, prefix)
		switch r.Method {
		case http.MethodHead:
			if !existing[key] {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("ETag", `"fake-etag"`)
			w.Header().Set("Content-Length", "5")
			w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
			w.WriteHeader(http.StatusOK)
		case http.MethodDelete:
			delete(existing, key)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotImplemented)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newFakeS3Provider(t *testing.T, srv *httptest.Server, bucket string) *Provider {
	t.Helper()
	p, err := New(Config{
		Endpoint:  strings.TrimPrefix(srv.URL, "http://"),
		Bucket:    bucket,
		AccessKey: "ak", SecretKey: "sk",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p
}

// TestDelete_NonexistentKey_ReturnsNotFound pins OBJ-AUD-02: S3's DELETE is
// spec-idempotent (204 whether or not the key ever existed), so the provider
// used to report {"ok":true} for a key that was never there — indistinguishable
// from a real deletion, and inconsistent with Rename's existing 404-on-missing
// behavior on the identical condition (plans/dataconsole-audit/object-stream.md
// OBJ-AUD-02). The fix Stat-checks first and must report ErrNotFound instead.
func TestDelete_NonexistentKey_ReturnsNotFound(t *testing.T) {
	t.Parallel()
	srv := newFakeS3Server(t, "obj-test-bucket", map[string]bool{})
	p := newFakeS3Provider(t, srv, "obj-test-bucket")

	err := p.Delete(context.Background(), provider.Path{Segments: []string{"never-existed.txt"}})
	if !errors.Is(err, provider.ErrNotFound) {
		t.Fatalf("Delete(nonexistent) = %v, want ErrNotFound", err)
	}
}

// TestDelete_ExistingKey_Succeeds is the companion GREEN case: deleting a key
// that genuinely exists must still succeed (nil error) after the Stat-first
// guard is added — the fix must not turn every delete into a false negative.
func TestDelete_ExistingKey_Succeeds(t *testing.T) {
	t.Parallel()
	srv := newFakeS3Server(t, "obj-test-bucket", map[string]bool{"real.txt": true})
	p := newFakeS3Provider(t, srv, "obj-test-bucket")

	if err := p.Delete(context.Background(), provider.Path{Segments: []string{"real.txt"}}); err != nil {
		t.Fatalf("Delete(existing) = %v, want nil", err)
	}
}

// TestDelete_AlreadyDeletedKey_ReturnsNotFound covers the audit's other live
// reproduction: deleting the same key twice (e.g. a double-click before the
// tree refreshes) must report the second delete honestly, not another
// {"ok":true}.
func TestDelete_AlreadyDeletedKey_ReturnsNotFound(t *testing.T) {
	t.Parallel()
	srv := newFakeS3Server(t, "obj-test-bucket", map[string]bool{"once.txt": true})
	p := newFakeS3Provider(t, srv, "obj-test-bucket")
	path := provider.Path{Segments: []string{"once.txt"}}

	if err := p.Delete(context.Background(), path); err != nil {
		t.Fatalf("first Delete(existing) = %v, want nil", err)
	}
	if err := p.Delete(context.Background(), path); !errors.Is(err, provider.ErrNotFound) {
		t.Fatalf("second Delete(same key) = %v, want ErrNotFound", err)
	}
}

// TestDelete_ReadOnly_StillRejectsBeforeNetwork keeps the existing read-only
// short-circuit intact: the Stat-first check must sit AFTER the read-only
// gate, not replace it — a read-only provider must still refuse without ever
// touching the network (it never even reaches this fake server).
func TestDelete_ReadOnly_StillRejectsBeforeNetwork(t *testing.T) {
	t.Parallel()
	p, err := New(Config{Endpoint: "127.0.0.1:1", Bucket: "b", ReadOnly: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := p.Delete(context.Background(), provider.Path{Segments: []string{"x"}}); !errors.Is(err, provider.ErrReadOnly) {
		t.Fatalf("Delete(read-only) = %v, want ErrReadOnly", err)
	}
}
