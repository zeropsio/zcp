package farm

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSinkClient_DefaultTransportAndReadsAreBounded(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.FormatInt((64<<20)+1, 10))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewSinkClient(Config{URL: srv.URL, Bucket: "zcp-farm", Key: "key", Secret: "secret"})
	if client.client == http.DefaultClient || client.client.Timeout <= 0 {
		t.Fatalf("sink HTTP client has no independent finite timeout: client=%p timeout=%s", client.client, client.client.Timeout)
	}
	_, err := client.Get(context.Background(), "runs/r1/results/oversize.txt")
	if err == nil || !strings.Contains(err.Error(), "exceeds 67108864 bytes") {
		t.Fatalf("oversize GET error = %v, want 64 MiB limit", err)
	}
}

func TestSink_ConditionalCreate_ConflictPreservesBytes(t *testing.T) {
	t.Parallel()
	fake := newFakeS3()
	server := httptest.NewServer(fake.handler(t))
	defer server.Close()
	client := NewSinkClient(Config{URL: server.URL, Bucket: fakeS3Bucket, Key: "key", Secret: "secret"})
	ctx := context.Background()
	if err := client.PutIfAbsent(ctx, "batches/b1/manifest.json", []byte("first")); err != nil {
		t.Fatalf("first conditional create: %v", err)
	}
	if err := client.PutIfAbsent(ctx, "batches/b1/manifest.json", []byte("second")); err == nil {
		t.Fatal("second conditional create: want conflict")
	}
	got, err := client.Get(ctx, "batches/b1/manifest.json")
	if err != nil {
		t.Fatalf("read preserved manifest: %v", err)
	}
	if string(got) != "first" {
		t.Fatalf("manifest bytes = %q, want %q", got, "first")
	}
}

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

// TestSinkClient_PutGetList_RoundTripAgainstFake round-trips Put/Get/Head
// against fakeS3, then checks List follows continuation tokens across a
// listing wider than one page (docs/spec-eval-farm.md §1: "LIST
// (?list-type=2&prefix=, follow continuation tokens)").
func TestSinkClient_PutGetList_RoundTripAgainstFake(t *testing.T) {
	t.Parallel()

	fake := newFakeS3()
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

// TestSinkList_NonOKStatus_NamesBody pins R10d: before the fix, List read
// the response body (for the success-path XML parse) before ever checking
// the status code, so by the time statusError ran its own read on a non-2xx
// response the body was already drained — the error always reported an
// empty message. The fix checks status first and names the body.
func TestSinkList_NonOKStatus_NamesBody(t *testing.T) {
	t.Parallel()

	const wantBody = "simulated bucket failure"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, wantBody)
	}))
	defer srv.Close()

	client := NewSinkClient(Config{URL: srv.URL, Bucket: "zcp-farm", Key: "AKIDEXAMPLE", Secret: "secret"})
	_, err := client.List(context.Background(), "runs/")
	if err == nil {
		t.Fatal("List: want an error for a non-2xx response, got nil")
	}
	if !strings.Contains(err.Error(), wantBody) {
		t.Errorf("List error = %q, want it to contain the response body %q", err.Error(), wantBody)
	}
}

// TestErrObjectNotFound_SatisfiesFsErrNotExist pins the fix for the
// observer's optional-file loaders (internal/eval/farm/observer/bundle.go):
// they check errors.Is(err, os.ErrNotExist) to treat an absent optional file
// (self-review.md, platform-snapshot.json, verification.json) as "not
// recorded" rather than a hard failure. SinkClient.Get wraps
// ErrObjectNotFound on a 404, so ErrObjectNotFound itself must satisfy
// errors.Is(err, fs.ErrNotExist) or every bucket-backed (as opposed to
// local-directory) optional-file read fails the whole observation instead
// of rendering "(not recorded)".
func TestErrObjectNotFound_SatisfiesFsErrNotExist(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewSinkClient(Config{URL: srv.URL, Bucket: "zcp-farm", Key: "AKIDEXAMPLE", Secret: "secret"})
	_, err := client.Get(context.Background(), "runs/r1/missing.json")
	if err == nil {
		t.Fatal("Get: want an error for a 404 response, got nil")
	}
	if !errors.Is(err, ErrObjectNotFound) {
		t.Errorf("Get error = %v, want it to wrap ErrObjectNotFound", err)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Get error = %v, want errors.Is(err, fs.ErrNotExist) to hold (ErrObjectNotFound must satisfy fs.ErrNotExist)", err)
	}
}

// TestSinkClient_RetriesTransportFailureAndServerError pins the sink's one
// reliability rule: a request whose connection breaks (a keep-alive the
// server closed as the request went out — seen live as "transport
// connection broken" while writing a batch summary) or that answers 5xx is
// tried again, so one dropped connection never fails a whole batch. A
// 4xx answer is final.
func TestSinkClient_RetriesTransportFailureAndServerError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		firstFails  func(w http.ResponseWriter, r *http.Request)
		wantCalls   int
		wantSuccess bool
	}{
		{"broken connection", func(w http.ResponseWriter, _ *http.Request) {
			hj, ok := w.(http.Hijacker)
			if !ok {
				return
			}
			conn, _, err := hj.Hijack()
			if err == nil {
				_ = conn.Close()
			}
		}, 2, true},
		{"server error", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}, 2, true},
		{"client error is final", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}, 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var mu sync.Mutex
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				calls++
				n := calls
				mu.Unlock()
				if n == 1 {
					tc.firstFails(w, r)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			c := NewSinkClient(Config{URL: srv.URL, Bucket: "zcp-farm", Key: "k", Secret: "s"})
			err := c.Put(context.Background(), "runs/r1/done.json", []byte("{}"))
			mu.Lock()
			got := calls
			mu.Unlock()
			if (err == nil) != tc.wantSuccess {
				t.Errorf("Put error = %v, want success: %v", err, tc.wantSuccess)
			}
			if got != tc.wantCalls {
				t.Errorf("server saw %d requests, want %d", got, tc.wantCalls)
			}
		})
	}
}
