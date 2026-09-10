package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

// TestEvalFarm_WithoutAuthoringGate_RefusesEveryVerb pins FM-17
// (docs/spec-eval-farm.md §3.1): every `zcp eval farm` verb is refused,
// nonzero, with one line on stderr naming the gate, when ZCP_AUTHORING is
// not "1" — the same discipline as runtime.Info.Authoring
// (internal/runtime/runtime.go:52).
func TestEvalFarm_WithoutAuthoringGate_RefusesEveryVerb(t *testing.T) {
	t.Setenv("ZCP_AUTHORING", "")

	verbs := []string{"push", "run", "status", "pull", "report", "coverage", "gc"}
	for _, verb := range verbs {
		t.Run(verb, func(t *testing.T) {
			code := runEvalFarm([]string{verb})
			if code == 0 {
				t.Fatalf("runEvalFarm([%q]) = 0, want nonzero without ZCP_AUTHORING=1", verb)
			}
		})
	}
}

// TestEvalFarm_UnimplementedVerbs_ExitNonzeroNotImplemented pins that
// run/status/report/coverage/gc are recognized verbs whose slices land
// later (S2 write-set excludes them): with the gate open they exit nonzero
// naming "not implemented" on stderr, never silently succeed and never
// report "unknown subcommand".
func TestEvalFarm_UnimplementedVerbs_ExitNonzeroNotImplemented(t *testing.T) {
	t.Setenv("ZCP_AUTHORING", "1")

	verbs := []string{"run", "status", "coverage", "gc"}
	for _, verb := range verbs {
		t.Run(verb, func(t *testing.T) {
			var code int
			_, stderr := captureOutput(t, func() {
				code = runEvalFarm([]string{verb})
			})
			if code == 0 {
				t.Fatalf("runEvalFarm([%q]) = 0, want nonzero", verb)
			}
			if !strings.Contains(stderr, "not implemented") {
				t.Errorf("stderr = %q, want it to contain %q", stderr, "not implemented")
			}
		})
	}
}

// fakeFarmS3 is a minimal in-memory path-style S3 double for the push/pull
// tool-layer tests: PUT stores, GET reads, and a list-type=2 GET on the
// bucket root answers every stored key (no pagination — the push/pull
// fixtures here are small).
type fakeFarmS3 struct {
	mu      sync.Mutex
	bucket  string
	objects map[string][]byte
}

func newFakeFarmS3(bucket string) *fakeFarmS3 {
	return &fakeFarmS3{bucket: bucket, objects: map[string][]byte{}}
}

func (f *fakeFarmS3) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			_, _ = r.Body.Read(body)
			f.objects[key] = body
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			data, ok := f.objects[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write(data)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
}

func (f *fakeFarmS3) serveList(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	prefix := r.URL.Query().Get("prefix")
	var keys []string
	for key := range f.objects {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	f.mu.Unlock()
	sort.Strings(keys)

	var body strings.Builder
	body.WriteString("<ListBucketResult>")
	for _, key := range keys {
		fmt.Fprintf(&body, "<Contents><Key>%s</Key></Contents>", key)
	}
	body.WriteString("<IsTruncated>false</IsTruncated></ListBucketResult>")
	w.Header().Set("Content-Type", "application/xml")
	_, _ = w.Write([]byte(body.String()))
}

func setFarmEnv(t *testing.T, serverURL string) {
	t.Helper()
	t.Setenv("ZCP_AUTHORING", "1")
	t.Setenv("ZCP_FARM_S3_URL", serverURL)
	t.Setenv("ZCP_FARM_S3_BUCKET", "zcp-farm")
	t.Setenv("ZCP_FARM_S3_KEY", "AKIDEXAMPLE")
	t.Setenv("ZCP_FARM_S3_SECRET", "secret")
}

// TestFarmPush_Candidate_UploadsUnderSha256Key pins docs/spec-eval-farm.md
// §1.1: `farm push --candidate <file>` uploads to
// candidates/<sha256(file)>/zcp and prints that digest.
func TestFarmPush_Candidate_UploadsUnderSha256Key(t *testing.T) {
	fake := newFakeFarmS3("zcp-farm")
	server := fake.server()
	defer server.Close()
	setFarmEnv(t, server.URL)

	dir := t.TempDir()
	candidatePath := filepath.Join(dir, "zcp")
	body := []byte("pretend candidate binary")
	if err := os.WriteFile(candidatePath, body, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	sum := sha256.Sum256(body)
	wantDigest := hex.EncodeToString(sum[:])

	var code int
	stdout, stderr := captureOutput(t, func() {
		code = runEvalFarm([]string{"push", "--candidate", candidatePath})
	})
	if code != 0 {
		t.Fatalf("runEvalFarm(push --candidate) = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, wantDigest) {
		t.Errorf("stdout = %q, want it to contain the digest %q", stdout, wantDigest)
	}

	fake.mu.Lock()
	got, ok := fake.objects["candidates/"+wantDigest+"/zcp"]
	fake.mu.Unlock()
	if !ok {
		t.Fatalf("fake bucket has no object at candidates/%s/zcp; objects: %v", wantDigest, fake.objects)
	}
	if string(got) != string(body) {
		t.Errorf("uploaded object = %q, want %q", got, body)
	}
}

// TestFarmPull_Run_DownloadsAllParts pins docs/spec-eval-farm.md §1.1/§5:
// `farm pull <runId> --out <dir>` downloads every object under
// runs/<runId>/ to <dir>/<runId>/, and prints "bundle: complete" once
// done.json is among what it fetched.
func TestFarmPull_Run_DownloadsAllParts(t *testing.T) {
	fake := newFakeFarmS3("zcp-farm")
	server := fake.server()
	defer server.Close()
	setFarmEnv(t, server.URL)

	fake.objects["runs/r1/started.json"] = []byte(`{"runId":"r1"}`)
	fake.objects["runs/r1/results/summary.json"] = []byte(`{"ok":true}`)
	fake.objects["runs/r1/capture/mcp/zcp-1.jsonl"] = []byte(`{"seq":1}`)
	fake.objects["runs/r1/done.json"] = []byte(`{"runId":"r1","parts":{}}`)

	outDir := t.TempDir()
	var code int
	stdout, stderr := captureOutput(t, func() {
		code = runEvalFarm([]string{"pull", "r1", "--out", outDir})
	})
	if code != 0 {
		t.Fatalf("runEvalFarm(pull r1) = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "bundle: complete") {
		t.Errorf("stdout = %q, want it to contain %q", stdout, "bundle: complete")
	}

	for key, want := range fake.objects {
		rel := strings.TrimPrefix(key, "runs/r1/")
		got, err := os.ReadFile(filepath.Join(outDir, "r1", rel))
		if err != nil {
			t.Errorf("ReadFile(%s): %v", rel, err)
			continue
		}
		if string(got) != string(want) {
			t.Errorf("downloaded %s = %q, want %q", rel, got, want)
		}
	}
}
