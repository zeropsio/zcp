package farm

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestSinkClient_SigV4_MatchesKnownVector checks SignV4 — the package's
// public signing seam — against AWS's own published Signature Version 4
// test suite ("get-vanilla" case: a plain GET / with only Host and
// X-Amz-Date signed), never a value computed by this package's own
// implementation.
//
// Independent oracle: AWS Signature Version 4 Test Suite, "basic/get-vanilla"
// case (config: accessKeyId=AKIDEXAMPLE, secretAccessKey=
// wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY, region=us-east-1,
// service="service"). AWS originally published and hosted this suite
// alongside the SigV4 docs; it is mirrored verbatim (as parsed
// request/creq/sts/authz fields) at
// github.com/saibotsivad/aws-sig-v4-test-suite. Before being pinned here,
// the expected Authorization value below was independently re-derived from
// that vector's creq/sts fields using Python's stdlib hmac/hashlib (not this
// package's code, not this test) — see the S2 build report for the
// verification transcript.
func TestSinkClient_SigV4_MatchesKnownVector(t *testing.T) {
	t.Parallel()

	const (
		accessKey = "AKIDEXAMPLE"
		secretKey = "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY"
		region    = "us-east-1"
		service   = "service"
	)
	when, err := time.Parse("20060102T150405Z", "20150830T123600Z")
	if err != nil {
		t.Fatalf("time.Parse: %v", err)
	}

	req := SigV4Request{
		Method: "GET",
		Path:   "/",
		Query:  "",
		Headers: map[string]string{
			"Host":       "example.amazonaws.com",
			"X-Amz-Date": "20150830T123600Z",
		},
		PayloadSHA256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
	}

	const wantAuthorization = "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20150830/us-east-1/service/aws4_request, " +
		"SignedHeaders=host;x-amz-date, " +
		"Signature=5fa00fa31553b73ebf1942676e86291e8372ff2a2260956d9b8aae1d763fbf31"
	if got := SignV4(req, accessKey, secretKey, region, service, when); got != wantAuthorization {
		t.Errorf("SignV4 =\n%q\nwant\n%q", got, wantAuthorization)
	}
}

// TestSinkClient_PathStyleURL_NeverVirtualHost pins FM-8
// (docs/spec-eval-farm.md §1.3): every request SinkClient sends addresses
// the bucket as a literal path segment (<url>/<bucket>/<key>) — never the
// virtual-host form (<bucket>.<host>/<key>), which does not resolve on
// Zerops object storage.
func TestSinkClient_PathStyleURL_NeverVirtualHost(t *testing.T) {
	t.Parallel()

	var gotHost, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewSinkClient(Config{
		URL:    server.URL,
		Bucket: "zcp-farm",
		Key:    "AKIDEXAMPLE",
		Secret: "secret",
	})
	if err := client.Put(context.Background(), "candidates/deadbeef/zcp", []byte("binary")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	serverHost := server.Listener.Addr().String()
	if gotHost != serverHost {
		t.Errorf("request Host = %q, want the bare server host %q (a virtual-host URL would prefix the bucket)", gotHost, serverHost)
	}
	const wantPath = "/zcp-farm/candidates/deadbeef/zcp"
	if gotPath != wantPath {
		t.Errorf("request Path = %q, want %q", gotPath, wantPath)
	}
}

// fakeS3 is a minimal in-memory path-style S3 double: PUT stores, GET/HEAD
// read, and list-type=2 GETs page through the store two keys at a time so
// TestSinkClient_PutGetList_RoundTripAgainstFake exercises continuation
// tokens without a large fixture.
type fakeS3 struct {
	mu      sync.Mutex
	bucket  string
	objects map[string][]byte
}

func newFakeS3(bucket string) *fakeS3 {
	return &fakeS3{bucket: bucket, objects: map[string][]byte{}}
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
			body := make([]byte, r.ContentLength)
			_, _ = io.ReadFull(r.Body, body)
			f.objects[key] = body
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

// TestSinkClient_PutGetList_RoundTripAgainstFake round-trips Put/Get/Head
// against fakeS3, then checks List follows continuation tokens across a
// listing wider than one page (docs/spec-eval-farm.md §1: "LIST
// (?list-type=2&prefix=, follow continuation tokens)").
func TestSinkClient_PutGetList_RoundTripAgainstFake(t *testing.T) {
	t.Parallel()

	fake := newFakeS3("zcp-farm")
	server := httptest.NewServer(fake.handler(t))
	defer server.Close()

	client := NewSinkClient(Config{URL: server.URL, Bucket: "zcp-farm", Key: "AKIDEXAMPLE", Secret: "secret"})
	ctx := context.Background()

	if err := client.Put(ctx, "runs/r1/a.txt", []byte("alpha")); err != nil {
		t.Fatalf("Put a.txt: %v", err)
	}
	got, err := client.Get(ctx, "runs/r1/a.txt")
	if err != nil {
		t.Fatalf("Get a.txt: %v", err)
	}
	if string(got) != "alpha" {
		t.Errorf("Get a.txt = %q, want %q", got, "alpha")
	}

	exists, size, err := client.Head(ctx, "runs/r1/a.txt")
	if err != nil {
		t.Fatalf("Head a.txt: %v", err)
	}
	if !exists || size != int64(len("alpha")) {
		t.Errorf("Head a.txt = (exists=%v, size=%d), want (true, %d)", exists, size, len("alpha"))
	}

	if _, err := client.Get(ctx, "runs/r1/missing.txt"); err == nil {
		t.Error("Get missing.txt: want an error, got nil")
	}
	if exists, _, err := client.Head(ctx, "runs/r1/missing.txt"); err != nil || exists {
		t.Errorf("Head missing.txt = (exists=%v, err=%v), want (false, nil)", exists, err)
	}

	for _, name := range []string{"b.txt", "c.txt", "d.txt"} {
		if err := client.Put(ctx, "runs/r1/"+name, []byte(name)); err != nil {
			t.Fatalf("Put %s: %v", name, err)
		}
	}

	keys, err := client.List(ctx, "runs/r1/")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	sort.Strings(keys)
	want := []string{"runs/r1/a.txt", "runs/r1/b.txt", "runs/r1/c.txt", "runs/r1/d.txt"}
	if len(keys) != len(want) {
		t.Fatalf("List returned %d keys (%v), want %d (%v)", len(keys), keys, len(want), want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Errorf("List()[%d] = %q, want %q", i, keys[i], want[i])
		}
	}
}
