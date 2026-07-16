package object

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// ---- New: config validation + no network I/O ----

func TestNew_RejectsMissingEndpointOrBucket(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cfg  Config
	}{
		{"missing both", Config{}},
		{"missing endpoint", Config{Bucket: "b"}},
		{"missing bucket", Config{Endpoint: "127.0.0.1:9000"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if _, err := New(c.cfg); !errors.Is(err, provider.ErrInvalid) {
				t.Fatalf("New(%+v) = %v, want ErrInvalid", c.cfg, err)
			}
		})
	}
}

// New must not dial the network: minio.New only validates the endpoint string
// and wires credentials into a client — it makes no round-trip. A fake,
// unroutable endpoint has to succeed here, hermetically.
func TestNew_NoNetworkIO(t *testing.T) {
	t.Parallel()
	p, err := New(Config{Endpoint: "127.0.0.1:1", Bucket: "b", AccessKey: "ak", SecretKey: "sk"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if p.Kind() != "object-storage" {
		t.Errorf("Kind() = %q, want object-storage", p.Kind())
	}
}

func TestNew_CapsReflectConfig(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		readOnly      bool
		maxInline     int64
		wantMaxInline int64
	}{
		{"read-write, default max inline", false, 0, 16 << 20},
		{"read-only, default max inline", true, 0, 16 << 20},
		{"read-write, custom max inline", false, 4096, 4096},
		{"negative max inline falls back to default", false, -1, 16 << 20},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			p, err := New(Config{Endpoint: "127.0.0.1:1", Bucket: "b", ReadOnly: c.readOnly, MaxInlineBytes: c.maxInline})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			caps := p.Caps()
			if caps.Family != provider.FamilyObject {
				t.Errorf("Family = %q, want %q", caps.Family, provider.FamilyObject)
			}
			if caps.Support != provider.SupportFull {
				t.Errorf("Support = %q, want SupportFull", caps.Support)
			}
			if caps.EditBlob != !c.readOnly {
				t.Errorf("EditBlob = %v, want %v", caps.EditBlob, !c.readOnly)
			}
			if caps.Upload != !c.readOnly {
				t.Errorf("Upload = %v, want %v", caps.Upload, !c.readOnly)
			}
			if caps.ReadOnly != c.readOnly {
				t.Errorf("ReadOnly = %v, want %v", caps.ReadOnly, c.readOnly)
			}
			if caps.MaxInlineBytes != c.wantMaxInline {
				t.Errorf("MaxInlineBytes = %d, want %d", caps.MaxInlineBytes, c.wantMaxInline)
			}
		})
	}
}

// ---- path/key handling ----

func TestPrefix(t *testing.T) {
	t.Parallel()
	p := newTestProvider(t)
	cases := []struct {
		segs []string
		want string
	}{
		{nil, ""},
		{[]string{"a"}, "a"},
		{[]string{"a", "b"}, "a/b"},
		{[]string{"a", "b", "c.txt"}, "a/b/c.txt"},
	}
	for _, c := range cases {
		if got := p.prefix(provider.Path{Segments: c.segs}); got != c.want {
			t.Errorf("prefix(%v) = %q, want %q", c.segs, got, c.want)
		}
	}
}

func TestChildPath(t *testing.T) {
	t.Parallel()
	parent := provider.Path{Service: "s3", Segments: []string{"a"}}
	c := childPath(parent, "b")
	if c.Service != "s3" || len(c.Segments) != 2 || c.Segments[0] != "a" || c.Segments[1] != "b" {
		t.Fatalf("childPath = %+v", c)
	}
	// must not alias the parent slice
	if len(parent.Segments) != 1 {
		t.Fatalf("parent mutated: %+v", parent)
	}
}

func TestLastSegment(t *testing.T) {
	t.Parallel()
	if got := lastSegment(provider.Path{}); got != "" {
		t.Errorf("lastSegment(empty) = %q, want empty", got)
	}
	if got := lastSegment(provider.Path{Segments: []string{"a", "b"}}); got != "b" {
		t.Errorf("lastSegment = %q, want b", got)
	}
}

func newTestProvider(t *testing.T) *Provider {
	t.Helper()
	p, err := New(Config{Endpoint: "127.0.0.1:1", Bucket: "b", ReadOnly: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p
}

// ---- mapErr: the security-critical error sanitizer ----
//
// mapErr's own doc comment promises it "never leaks the endpoint/key/
// credentials embedded in the driver's error text." Every case below feeds in
// error text carrying fake-but-recognizable secrets and asserts none of them
// survive into the returned error's message.

const (
	leakSecret   = "AKIALEAKEDSECRETKEY"
	leakEndpoint = "internal-minio.example.zerops:9000"
	leakKey      = "tenants/customer-42/very/secret/path.csv"
	leakMessage  = "leak-me-message"
)

func assertNoLeak(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	msg := err.Error()
	for _, secret := range []string{leakSecret, leakEndpoint, leakKey, leakMessage} {
		if strings.Contains(msg, secret) {
			t.Fatalf("mapErr leaked sensitive text %q into error message: %q", secret, msg)
		}
	}
}

func leakyErrorResponse(code string) minio.ErrorResponse {
	return minio.ErrorResponse{
		Code: code, BucketName: leakEndpoint, Key: leakKey,
		Message: leakMessage, Resource: leakKey, HostID: leakSecret, Server: leakSecret,
	}
}

func TestMapErr_Nil(t *testing.T) {
	t.Parallel()
	if err := mapErr(nil); err != nil {
		t.Fatalf("mapErr(nil) = %v, want nil", err)
	}
}

func TestMapErr_NotFoundCodes(t *testing.T) {
	t.Parallel()
	for _, code := range []string{"NoSuchKey", "NoSuchBucket"} {
		t.Run(code, func(t *testing.T) {
			t.Parallel()
			got := mapErr(leakyErrorResponse(code))
			if !errors.Is(got, provider.ErrNotFound) {
				t.Fatalf("mapErr(%s) = %v, want ErrNotFound", code, got)
			}
			assertNoLeak(t, got)
		})
	}
}

func TestMapErr_AccessDeniedCodes(t *testing.T) {
	t.Parallel()
	for _, code := range []string{"AccessDenied", "SignatureDoesNotMatch", "InvalidAccessKeyId"} {
		t.Run(code, func(t *testing.T) {
			t.Parallel()
			got := mapErr(leakyErrorResponse(code))
			if !errors.Is(got, provider.ErrUpstream) {
				t.Fatalf("mapErr(%s) = %v, want ErrUpstream", code, got)
			}
			assertNoLeak(t, got)
		})
	}
}

func TestMapErr_UnknownCode_FallsBackToUpstream(t *testing.T) {
	t.Parallel()
	got := mapErr(leakyErrorResponse("SomeUnmappedFutureCode"))
	if !errors.Is(got, provider.ErrUpstream) {
		t.Fatalf("mapErr(unknown code) = %v, want ErrUpstream", got)
	}
	assertNoLeak(t, got)
}

func TestMapErr_GenericDriverError_DropsRawText(t *testing.T) {
	t.Parallel()
	// A plain error never matches minio.ToErrorResponse's type switch (it isn't
	// a minio.ErrorResponse), so resp.Code is "" and this falls to the default
	// ErrUpstream branch, which wraps only the sentinel — never err's own text.
	in := errors.New("dial tcp " + leakEndpoint + ": connect refused, key=" + leakKey)
	got := mapErr(in)
	if !errors.Is(got, provider.ErrUpstream) {
		t.Fatalf("mapErr(generic) = %v, want ErrUpstream", got)
	}
	assertNoLeak(t, got)
}

func TestMapErr_ContextCanceled_PreservesIdentityAndSanitizes(t *testing.T) {
	t.Parallel()
	// Simulates what net/http actually returns for a canceled in-flight
	// request: a *url.Error whose Error() text embeds the full request URL
	// (endpoint + bucket + key). mapErr must still classify this as
	// context.Canceled (errors.Is) for callers that branch on it, but must not
	// let that URL text through.
	wrapped := &url.Error{Op: "Get", URL: "https://" + leakEndpoint + "/b/" + leakKey, Err: context.Canceled}
	if !errors.Is(wrapped, context.Canceled) {
		t.Fatal("test bug: *url.Error must wrap context.Canceled")
	}
	got := mapErr(wrapped)
	if !errors.Is(got, context.Canceled) {
		t.Fatalf("mapErr(canceled) = %v, want errors.Is(_, context.Canceled)", got)
	}
	assertNoLeak(t, got)
}

func TestMapErr_BareContextCanceled(t *testing.T) {
	t.Parallel()
	got := mapErr(context.Canceled)
	if !errors.Is(got, context.Canceled) {
		t.Fatalf("mapErr(context.Canceled) = %v, want errors.Is(_, context.Canceled)", got)
	}
}

// ---- read-only gate: mutating methods refuse before touching the network ----

func TestReadOnlyGate(t *testing.T) {
	t.Parallel()
	p, err := New(Config{Endpoint: "127.0.0.1:1", Bucket: "b", ReadOnly: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	path := provider.Path{Segments: []string{"obj"}}

	// Every mutating method checks caps.ReadOnly before issuing any SDK call,
	// so these must return instantly without needing a live MinIO endpoint —
	// if the gate were bypassed, these calls would instead hang/dial out to
	// 127.0.0.1:1 (nothing listens there) and this test would time out.
	if err := p.WriteBlob(ctx, path, []byte("x")); !errors.Is(err, provider.ErrReadOnly) {
		t.Errorf("WriteBlob = %v, want ErrReadOnly", err)
	}
	if err := p.Delete(ctx, path); !errors.Is(err, provider.ErrReadOnly) {
		t.Errorf("Delete = %v, want ErrReadOnly", err)
	}
	if err := p.Rename(ctx, path, provider.Path{Segments: []string{"obj2"}}); !errors.Is(err, provider.ErrReadOnly) {
		t.Errorf("Rename = %v, want ErrReadOnly", err)
	}
}

// The success paths of Health/List/Stat/ReadBlob/WriteBlob/Rename need a live
// MinIO/object-storage endpoint — no general in-memory S3 fake is vendored
// (unlike miniredis for kv). They live in
// internal/dataconsole/console/provider/conformance (TestObject_Conversions,
// e2e-tagged), not as t.Skip stubs here. Delete is the one exception: its
// HEAD-then-DELETE existence contract (OBJ-AUD-02) is narrow enough that
// object_delete_test.go drives it hermetically over real HTTP against a
// minimal purpose-built S3 double (newFakeS3Server), rather than waiting for
// e2e coverage.
