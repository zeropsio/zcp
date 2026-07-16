package object

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// newFakeS3Server is a minimal S3-protocol double sufficient to drive
// object.Provider's Delete/Rename over real HTTP via the genuine minio-go
// client — there is no in-memory S3 fake vendored (see object_test.go's note;
// the success paths otherwise depend on a live MinIO, covered by the e2e
// conformance suite). OBJ-AUD-02's honest-delete and D-07's folder-honesty
// (S20) both depend on real round trips the ReadOnlyGate-style tests can't
// exercise (those only prove the mutating methods short-circuit BEFORE any
// network call).
//
// It answers the requests minio-go issues for these paths, all driven off the
// one `existing` set of literal object keys:
//   - the one-time bucket-location probe (GET <bucket>/?location) every fresh
//     client issues before its first real S3 call, answered with a fixed
//     empty-region XML so it never reaches real AWS;
//   - LIST v2 (GET <bucket>?list-type=2&prefix=…), returning every key in
//     `existing` under the requested prefix as Contents (IsTruncated=false) —
//     this is what makes a prefix "have children", the signal Delete/Rename
//     use to tell a folder from a genuinely-missing key (D-07);
//   - HEAD <bucket>/<key> (existence), 200 for a key in `existing`, 404 else;
//   - PUT <bucket>/<dst> with X-Amz-Copy-Source (CopyObject, Rename's copy
//     half), 200 + a CopyObjectResult, adding <dst> to `existing`;
//   - DELETE <bucket>/<key>, always 204 — S3's own spec-mandated idempotent
//     delete (removing a key that was never there is not an error), the exact
//     condition that made OBJ-AUD-02 possible. The fake proves nothing about
//     honesty on its own; the provider's Stat-then-classify guard does.
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
		if r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2" {
			var keys []string
			for k := range existing {
				if strings.HasPrefix(k, r.URL.Query().Get("prefix")) {
					keys = append(keys, k)
				}
			}
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, listBucketResultXML(bucket, r.URL.Query().Get("prefix"), keys))
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
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
			w.WriteHeader(http.StatusOK)
		case http.MethodPut:
			if r.Header.Get("X-Amz-Copy-Source") == "" {
				w.WriteHeader(http.StatusNotImplemented)
				return
			}
			existing[key] = true
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, copyObjectResultXML())
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

// listBucketResultXML renders a ListObjectsV2 response carrying keys as
// Contents (never CommonPrefixes — the provider's hasChildren probe is
// recursive, so a single child under the prefix is enough), IsTruncated=false
// so minio-go stops after one page.
func listBucketResultXML(bucket, prefix string, keys []string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	b.WriteString(`<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`)
	b.WriteString("<Name>" + bucket + "</Name>")
	b.WriteString("<Prefix>" + prefix + "</Prefix>")
	b.WriteString("<KeyCount>" + strconv.Itoa(len(keys)) + "</KeyCount>")
	b.WriteString("<MaxKeys>1000</MaxKeys>")
	b.WriteString("<IsTruncated>false</IsTruncated>")
	for _, k := range keys {
		b.WriteString("<Contents>")
		b.WriteString("<Key>" + k + "</Key>")
		b.WriteString("<LastModified>2026-07-17T00:00:00.000Z</LastModified>")
		b.WriteString(`<ETag>"fake-etag"</ETag>`)
		b.WriteString("<Size>5</Size>")
		b.WriteString("<StorageClass>STANDARD</StorageClass>")
		b.WriteString("</Contents>")
	}
	b.WriteString("</ListBucketResult>")
	return b.String()
}

// copyObjectResultXML is the minimal body minio-go's CopyObject decodes (ETag +
// LastModified) so a Rename's copy half succeeds against the fake.
func copyObjectResultXML() string {
	return `<?xml version="1.0" encoding="UTF-8"?>` +
		`<CopyObjectResult><ETag>"copy-etag"</ETag>` +
		`<LastModified>2026-07-17T00:00:00.000Z</LastModified></CopyObjectResult>`
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

// TestStat_ReportsTypedNodeMeta drives Stat end-to-end over the real minio-go
// client against the fake HEAD responder above — unlike
// objectListMeta/objectStatMeta's pure-function unit tests (object_test.go),
// this proves Stat itself (not just the mapping helper) reaches the typed
// NodeMeta on the actual HTTP round trip, the same call chain
// TestObject_Conversions (conformance, e2e) later re-proves live.
func TestStat_ReportsTypedNodeMeta(t *testing.T) {
	t.Parallel()
	srv := newFakeS3Server(t, "obj-test-bucket", map[string]bool{"seen.txt": true})
	p := newFakeS3Provider(t, srv, "obj-test-bucket")

	node, err := p.Stat(context.Background(), provider.Path{Segments: []string{"seen.txt"}})
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if node.Meta == nil {
		t.Fatal("node.Meta = nil, want populated")
	}
	if node.Meta.Size == nil || *node.Meta.Size != 5 {
		t.Errorf("Meta.Size = %v, want 5 (Content-Length)", node.Meta.Size)
	}
	if node.Meta.ContentType != "text/plain" {
		t.Errorf("Meta.ContentType = %q, want text/plain", node.Meta.ContentType)
	}
	if node.Meta.ETag != "fake-etag" {
		t.Errorf("Meta.ETag = %q, want fake-etag", node.Meta.ETag)
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
