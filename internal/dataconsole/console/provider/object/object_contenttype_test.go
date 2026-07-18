package object

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// newFakeS3PutServer is a minimal S3-protocol double that records the
// Content-Type header minio-go's PutObject sends on the wire — sufficient to
// pin OBJ-AUD-01's fix without a live MinIO. Verified against the pinned
// github.com/minio/minio-go/v7 v7.2.1 source: PutObjectOptions.Header()
// defaults an EMPTY ContentType to "application/octet-stream" before ever
// reaching this handler — that default is exactly the degraded value the
// audit found on every console-originated write, so this double only needs
// to prove WriteBlob's contentType argument reaches the real Content-Type
// header, not reimplement S3.
func newFakeS3PutServer(t *testing.T, bucket string) (*httptest.Server, map[string]string) {
	t.Helper()
	contentTypes := map[string]string{}
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
		case http.MethodPut:
			contentTypes[key] = r.Header.Get("Content-Type")
			w.Header().Set("ETag", `"fake-etag"`)
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotImplemented)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, contentTypes
}

// TestWriteBlob_CarriesContentTypeToS3 pins OBJ-AUD-01: neither write path
// (PUT /api/blob nor multipart /api/upload) used to set an S3 Content-Type
// at all, so minio-go/S3 defaulted every console-originated write to
// "application/octet-stream" — breaking the next read's isTextual/isImage
// classification (dc-format.js), which is why an uploaded image lost its
// preview and an edited text file became un-editable on its next open
// (object-stream.md OBJ-AUD-01). WriteBlob must carry a real content-type
// through as S3 object metadata.
func TestWriteBlob_CarriesContentTypeToS3(t *testing.T) {
	t.Parallel()
	srv, contentTypes := newFakeS3PutServer(t, "obj-ct-bucket")
	p, err := New(Config{
		Endpoint: strings.TrimPrefix(srv.URL, "http://"),
		Bucket:   "obj-ct-bucket", AccessKey: "ak", SecretKey: "sk",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := p.WriteBlob(context.Background(), provider.Path{Segments: []string{"pic.png"}}, []byte("fakepngbytes"), "image/png"); err != nil {
		t.Fatalf("WriteBlob: %v", err)
	}
	if got := contentTypes["pic.png"]; got != "image/png" {
		t.Fatalf("S3 PUT Content-Type = %q, want image/png", got)
	}
}

// TestWriteBlob_EmptyContentType_DegradesToOctetStream is the documented
// baseline this fix moves away from: minio-go's own default for an EMPTY
// ContentType. Pinning it here makes the OBJ-AUD-01 fix's value explicit —
// server.go must never hand WriteBlob an empty content-type when a real one
// is knowable (see server package: resolveContentType).
func TestWriteBlob_EmptyContentType_DegradesToOctetStream(t *testing.T) {
	t.Parallel()
	srv, contentTypes := newFakeS3PutServer(t, "obj-ct-bucket2")
	p, err := New(Config{
		Endpoint: strings.TrimPrefix(srv.URL, "http://"),
		Bucket:   "obj-ct-bucket2", AccessKey: "ak", SecretKey: "sk",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := p.WriteBlob(context.Background(), provider.Path{Segments: []string{"x.bin"}}, []byte("data"), ""); err != nil {
		t.Fatalf("WriteBlob: %v", err)
	}
	if got := contentTypes["x.bin"]; got != "application/octet-stream" {
		t.Fatalf("S3 PUT Content-Type for empty contentType = %q, want application/octet-stream (minio-go's own default)", got)
	}
}
