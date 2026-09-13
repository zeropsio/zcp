package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/zeropsio/zcp/internal/eval/farm"
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
// report is a recognized verb whose slice lands later (S4 implements
// run/status/gc; report remains for a later slice): with the gate open it
// exits nonzero naming "not implemented" on stderr, never silently succeeds
// and never reports "unknown subcommand".
// fakeFarmS3 is a minimal in-memory path-style S3 double for the push/pull
// tool-layer tests: PUT stores, GET reads, and a list-type=2 GET on the
// bucket root answers every stored key (no pagination — the push/pull
// fixtures here are small).
type fakeFarmS3 struct {
	mu      sync.Mutex
	bucket  string
	objects map[string][]byte
}

func newFakeFarmS3() *fakeFarmS3 {
	return &fakeFarmS3{bucket: "zcp-farm", objects: map[string][]byte{}}
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
			body, err := io.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
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
	fake := newFakeFarmS3()
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
		code = runEvalFarm([]string{"push", "--candidate", candidatePath, "--allow-empty-corpus"})
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

// buildVCSFixtureBinary builds internal/eval/farm/testdata/vcsbuildfixture
// into t.TempDir() and returns its path. It runs the real go toolchain
// from inside this repo's git working tree so the resulting binary
// carries genuine vcs.revision/vcs.time/vcs.modified debug/buildinfo
// settings — a fabricated file body cannot exercise the real
// debug/buildinfo.ReadFile path at all. Skipped under -short: it shells
// out to `go build`.
func buildVCSFixtureBinary(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("builds a real Go binary via the go toolchain; skipped under -short")
	}
	out := filepath.Join(t.TempDir(), "candidate-with-vcs")
	cmd := exec.CommandContext(t.Context(), "go", "build", "-o", out, "github.com/zeropsio/zcp/internal/eval/farm/testdata/vcsbuildfixture")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build fixture: %v\n%s", err, output)
	}
	return out
}

// TestFarmPush_CandidateInfo_UploadsWhenVCSPresent pins docs/spec-eval-farm.md
// §3.3: a candidate binary whose embedded Go build info carries a
// vcs.revision uploads candidates/<sha256>.info.json (sibling to
// candidates/<sha256>/zcp) and prints "candidate-info: <revision[:12]>".
func TestFarmPush_CandidateInfo_UploadsWhenVCSPresent(t *testing.T) {
	candidatePath := buildVCSFixtureBinary(t)

	fake := newFakeFarmS3()
	server := fake.server()
	defer server.Close()
	setFarmEnv(t, server.URL)

	body, err := os.ReadFile(candidatePath)
	if err != nil {
		t.Fatalf("ReadFile fixture: %v", err)
	}
	sum := sha256.Sum256(body)
	wantDigest := hex.EncodeToString(sum[:])

	var code int
	stdout, stderr := captureOutput(t, func() {
		code = runEvalFarm([]string{"push", "--candidate", candidatePath, "--allow-empty-corpus"})
	})
	if code != 0 {
		t.Fatalf("runEvalFarm(push --candidate) = %d, stderr = %q", code, stderr)
	}

	fake.mu.Lock()
	infoBody, ok := fake.objects["candidates/"+wantDigest+".info.json"]
	fake.mu.Unlock()
	if !ok {
		t.Fatalf("fake bucket has no object at candidates/%s.info.json; objects: %v", wantDigest, fake.objects)
	}
	var info farm.CandidateInfo
	if err := json.Unmarshal(infoBody, &info); err != nil {
		t.Fatalf("unmarshal candidate info: %v\nbody: %s", err, infoBody)
	}
	if info.Revision == "" || info.GoVersion == "" {
		t.Fatalf("candidate info = %+v, want a non-empty Revision and GoVersion", info)
	}

	wantLine := "candidate-info: " + info.Revision[:12]
	if !strings.Contains(stdout, wantLine) {
		t.Errorf("stdout = %q, want it to contain %q", stdout, wantLine)
	}
}

// TestFarmPush_CandidateInfo_NoneWithoutVCSStamping pins docs/spec-eval-farm.md
// §3.3: a candidate file debug/buildinfo.ReadFile finds no VCS revision in
// (here, one that is not even a Go binary) uploads no info object and
// prints "candidate-info: none (built without VCS stamping)" — the push of
// the candidate binary itself still succeeds.
func TestFarmPush_CandidateInfo_NoneWithoutVCSStamping(t *testing.T) {
	fake := newFakeFarmS3()
	server := fake.server()
	defer server.Close()
	setFarmEnv(t, server.URL)

	dir := t.TempDir()
	candidatePath := filepath.Join(dir, "zcp")
	body := []byte("pretend candidate binary, not a real Go binary")
	if err := os.WriteFile(candidatePath, body, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	sum := sha256.Sum256(body)
	wantDigest := hex.EncodeToString(sum[:])

	var code int
	stdout, stderr := captureOutput(t, func() {
		code = runEvalFarm([]string{"push", "--candidate", candidatePath, "--allow-empty-corpus"})
	})
	if code != 0 {
		t.Fatalf("runEvalFarm(push --candidate) = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "candidate-info: none (built without VCS stamping)") {
		t.Errorf("stdout = %q, want it to contain the none-VCS line", stdout)
	}

	fake.mu.Lock()
	_, ok := fake.objects["candidates/"+wantDigest+".info.json"]
	fake.mu.Unlock()
	if ok {
		t.Errorf("fake bucket has candidates/%s.info.json, want none uploaded without VCS info", wantDigest)
	}
}

// TestFarmPush_CandidateWithoutCorpus_Refused pins docs/spec-eval-farm.md
// FM-66: `farm push --candidate <file>` refuses, nonzero, a candidate
// binary carrying fewer than farm.MinCorpusMarkers `guiSlug: "` markers —
// the corpus is embedded from disk at build time
// (internal/knowledge/documents.go), and a worktree/fresh clone has none
// (CLAUDE.md "Knowledge sync"). --allow-empty-corpus opts out; a candidate
// carrying the full local-build marker count (47) proceeds either way.
func TestFarmPush_CandidateWithoutCorpus_Refused(t *testing.T) {
	writeCandidate := func(t *testing.T, markers int) string {
		t.Helper()
		dir := t.TempDir()
		path := filepath.Join(dir, "zcp")
		body := append([]byte("pretend candidate binary\n"), bytes.Repeat([]byte(`guiSlug: "`), markers)...)
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		return path
	}

	t.Run("zero markers refused", func(t *testing.T) {
		fake := newFakeFarmS3()
		server := fake.server()
		defer server.Close()
		setFarmEnv(t, server.URL)

		candidatePath := writeCandidate(t, 0)
		var code int
		stdout, stderr := captureOutput(t, func() {
			code = runEvalFarm([]string{"push", "--candidate", candidatePath})
		})
		if code == 0 {
			t.Fatalf("runEvalFarm(push --candidate, 0 markers) = 0, want nonzero; stdout = %q", stdout)
		}
		if !strings.Contains(stderr, "embeds no recipe corpus") {
			t.Errorf("stderr = %q, want it to mention the missing corpus", stderr)
		}
		if len(fake.objects) != 0 {
			t.Errorf("fake bucket objects = %v, want none uploaded on refusal", fake.objects)
		}
	})

	t.Run("allow-empty-corpus proceeds", func(t *testing.T) {
		fake := newFakeFarmS3()
		server := fake.server()
		defer server.Close()
		setFarmEnv(t, server.URL)

		candidatePath := writeCandidate(t, 0)
		var code int
		_, stderr := captureOutput(t, func() {
			code = runEvalFarm([]string{"push", "--candidate", candidatePath, "--allow-empty-corpus"})
		})
		if code != 0 {
			t.Fatalf("runEvalFarm(push --candidate --allow-empty-corpus) = %d, stderr = %q", code, stderr)
		}
	})

	t.Run("47 markers proceeds", func(t *testing.T) {
		fake := newFakeFarmS3()
		server := fake.server()
		defer server.Close()
		setFarmEnv(t, server.URL)

		candidatePath := writeCandidate(t, 47)
		var code int
		_, stderr := captureOutput(t, func() {
			code = runEvalFarm([]string{"push", "--candidate", candidatePath})
		})
		if code != 0 {
			t.Fatalf("runEvalFarm(push --candidate, 47 markers) = %d, stderr = %q", code, stderr)
		}
	})
}

// TestFarmPush_WrapperContentAddressed pins R5 (LAND review):
// `farm push --wrapper <file>` uploads content-addressed to
// farm/wrapper/<sha256>.sh (never the old unpinned farm/wrapper.sh key),
// writes the plain-text pointer farm/wrapper/current (the same pattern as
// evaluators/current), and prints both.
func TestFarmPush_WrapperContentAddressed(t *testing.T) {
	fake := newFakeFarmS3()
	server := fake.server()
	defer server.Close()
	setFarmEnv(t, server.URL)

	dir := t.TempDir()
	wrapperPath := filepath.Join(dir, "wrapper.sh")
	body := []byte("#!/bin/sh\necho pretend wrapper\n")
	if err := os.WriteFile(wrapperPath, body, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	sum := sha256.Sum256(body)
	wantDigest := hex.EncodeToString(sum[:])

	var code int
	stdout, stderr := captureOutput(t, func() {
		code = runEvalFarm([]string{"push", "--wrapper", wrapperPath})
	})
	if code != 0 {
		t.Fatalf("runEvalFarm(push --wrapper) = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, wantDigest) {
		t.Errorf("stdout = %q, want it to contain the digest %q", stdout, wantDigest)
	}
	if !strings.Contains(stdout, "farm/wrapper/current") {
		t.Errorf("stdout = %q, want it to contain the pointer key %q", stdout, "farm/wrapper/current")
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()

	gotBody, ok := fake.objects["farm/wrapper/"+wantDigest+".sh"]
	if !ok {
		t.Fatalf("fake bucket has no object at farm/wrapper/%s.sh; objects: %v", wantDigest, fake.objects)
	}
	if string(gotBody) != string(body) {
		t.Errorf("uploaded object = %q, want %q", gotBody, body)
	}

	pointer, ok := fake.objects["farm/wrapper/current"]
	if !ok {
		t.Fatalf("fake bucket has no object at farm/wrapper/current; objects: %v", fake.objects)
	}
	if string(pointer) != wantDigest {
		t.Errorf("farm/wrapper/current body = %q, want %q", pointer, wantDigest)
	}

	if _, ok := fake.objects["farm/wrapper.sh"]; ok {
		t.Errorf("fake bucket has an unpinned farm/wrapper.sh object — push must never write it")
	}
}

// TestFarmPull_Run_DownloadsAllParts pins docs/spec-eval-farm.md §1.1/§5:
// `farm pull <runId> --out <dir>` downloads every object under
// runs/<runId>/ to <dir>/<runId>/, and prints "bundle: complete" once
// done.json is among what it fetched.
func TestFarmPull_Run_DownloadsAllParts(t *testing.T) {
	fake := newFakeFarmS3()
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

// TestFarmPull_RejectsPathEscape pins R4a (LAND review): a bucket key is
// an opaque string, not a filesystem path — a compromised run's own
// write-capable bucket key (FM-8) can plant an object at
// "runs/<id>/../../.ssh/authorized_keys". `farm pull` must never join that
// key's relative part onto --out unchecked (which would write outside it
// on the operator's machine): it refuses the escaping key, names it on
// stderr, still downloads every other object in the run, and exits
// nonzero rather than silently reporting a clean bundle.
func TestFarmPull_RejectsPathEscape(t *testing.T) {
	fake := newFakeFarmS3()
	server := fake.server()
	defer server.Close()
	setFarmEnv(t, server.URL)

	fake.objects["runs/r1/started.json"] = []byte(`{"runId":"r1"}`)
	fake.objects["runs/r1/results/summary.json"] = []byte(`{"ok":true}`)
	fake.objects["runs/r1/../../.ssh/authorized_keys"] = []byte("ssh-ed25519 AAAA... attacker\n")
	fake.objects["runs/r1/done.json"] = []byte(`{"runId":"r1","parts":{}}`)

	outDir := t.TempDir()
	var code int
	_, stderr := captureOutput(t, func() {
		code = runEvalFarm([]string{"pull", "r1", "--out", outDir})
	})
	if code == 0 {
		t.Fatalf("runEvalFarm(pull r1) = 0, want nonzero when a key escapes --out")
	}
	if !strings.Contains(stderr, "runs/r1/../../.ssh/authorized_keys") {
		t.Errorf("stderr = %q, want it to name the escaping key", stderr)
	}

	// The escaping key must never have been written anywhere under or
	// above outDir.
	escapeTarget := filepath.Join(filepath.Dir(filepath.Dir(outDir)), ".ssh", "authorized_keys")
	if _, err := os.Stat(escapeTarget); err == nil {
		t.Fatalf("path escape succeeded: %s was written", escapeTarget)
	}
	found := false
	_ = filepath.WalkDir(outDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr == nil && !d.IsDir() && d.Name() == "authorized_keys" {
			found = true
		}
		return nil
	})
	if found {
		t.Fatalf("an authorized_keys file was written somewhere under %s", outDir)
	}

	// Every safe object in the same run must still have been downloaded.
	for _, rel := range []string{"started.json", "results/summary.json", "done.json"} {
		if _, err := os.Stat(filepath.Join(outDir, "r1", rel)); err != nil {
			t.Errorf("safe object %s was not downloaded despite the sibling escaping key: %v", rel, err)
		}
	}
}

// TestFarmPull_Batch_ReadsManifestRuns pins D14: `farm pull --batch <batch>`
// decodes batches/<batch>/manifest.json in its real shape
// (farm.BatchManifest, docs/spec-eval-farm.md §1.4 FM-9 — Runs is a list of
// objects carrying runId, not a bare list of strings) and downloads every
// run the manifest names.
func TestFarmPull_Batch_ReadsManifestRuns(t *testing.T) {
	fake := newFakeFarmS3()
	server := fake.server()
	defer server.Close()
	setFarmEnv(t, server.URL)

	fake.objects["batches/b1/manifest.json"] = []byte(`{
		"batch": "b1", "createdAt": "2026-09-10T00:00:00Z", "startedAt": "2026-09-10T00:00:00Z",
		"set": "gate", "candidateSha256": "cand", "evaluatorSha256": "eval", "scenariosDigest": "scen",
		"runs": [
			{"runId": "b1-recipe-a", "scenario": "recipe-a", "projectName": "zcp-farm-b1-recipe-a"},
			{"runId": "b1-recipe-b", "scenario": "recipe-b", "projectName": "zcp-farm-b1-recipe-b"}
		]
	}`)
	fake.objects["runs/b1-recipe-a/done.json"] = []byte(`{"runId":"b1-recipe-a","parts":{}}`)
	fake.objects["runs/b1-recipe-b/done.json"] = []byte(`{"runId":"b1-recipe-b","parts":{}}`)

	outDir := t.TempDir()
	var code int
	stdout, stderr := captureOutput(t, func() {
		code = runEvalFarm([]string{"pull", "--batch", "b1", "--out", outDir})
	})
	if code != 0 {
		t.Fatalf("runEvalFarm(pull --batch b1) = %d, stderr = %q", code, stderr)
	}
	for _, runID := range []string{"b1-recipe-a", "b1-recipe-b"} {
		if !strings.Contains(stdout, runID+": bundle: complete") {
			t.Errorf("stdout = %q, want it to report %s: bundle: complete", stdout, runID)
		}
		if _, err := os.Stat(filepath.Join(outDir, runID, "done.json")); err != nil {
			t.Errorf("downloaded bundle for %s: %v", runID, err)
		}
	}
}

// TestFarmPush_UploadsGateSetAndCurrentPointer pins outcome 1 of the S15
// brief: `farm push --scenarios <dir>` also uploads the local gate scenario
// list to sets/<scenariosDigest>/gate.txt, and `farm push --evaluator
// <file>` also writes the plain-text pointer evaluators/current whose body
// is the evaluator's own digest. Both keys are printed. --gate-set is left
// to its default, which must resolve the real checkout's eval/farm/gate-set.txt
// from eval/behavioral/scenarios.
func TestFarmPush_UploadsGateSetAndCurrentPointer(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	t.Chdir(repoRoot)

	fake := newFakeFarmS3()
	server := fake.server()
	defer server.Close()
	setFarmEnv(t, server.URL)

	scenariosDir := filepath.Join(repoRoot, "eval", "behavioral", "scenarios")

	evalDir := t.TempDir()
	evaluatorPath := filepath.Join(evalDir, "zcp")
	evaluatorBody := []byte("pretend evaluator binary")
	if err := os.WriteFile(evaluatorPath, evaluatorBody, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	evalSum := sha256.Sum256(evaluatorBody)
	wantEvalDigest := hex.EncodeToString(evalSum[:])

	var code int
	stdout, stderr := captureOutput(t, func() {
		code = runEvalFarm([]string{
			"push", "--evaluator", evaluatorPath, "--scenarios", scenariosDir,
		})
	})
	if code != 0 {
		t.Fatalf("runEvalFarm(push) = %d, stderr = %q", code, stderr)
	}

	wantGateSetBody, err := os.ReadFile(filepath.Join(repoRoot, "eval", "farm", "gate-set.txt"))
	if err != nil {
		t.Fatalf("read local gate-set.txt: %v", err)
	}

	fake.mu.Lock()
	evalPointer, hasEvalPointer := fake.objects["evaluators/current"]
	fake.mu.Unlock()
	if !hasEvalPointer {
		t.Fatalf("fake bucket has no object at evaluators/current; objects: %v", fake.objects)
	}
	if string(evalPointer) != wantEvalDigest {
		t.Errorf("evaluators/current body = %q, want %q", evalPointer, wantEvalDigest)
	}
	if !strings.Contains(stdout, "evaluators/current") {
		t.Errorf("stdout = %q, want it to contain the pointer key %q", stdout, "evaluators/current")
	}

	var gateKey string
	fake.mu.Lock()
	for key := range fake.objects {
		if strings.HasPrefix(key, "sets/") && strings.HasSuffix(key, "/gate.txt") {
			gateKey = key
		}
	}
	got, hasGate := fake.objects[gateKey]
	fake.mu.Unlock()
	if !hasGate {
		t.Fatalf("fake bucket has no sets/<digest>/gate.txt object; objects: %v", fake.objects)
	}
	if string(got) != string(wantGateSetBody) {
		t.Errorf("uploaded gate set = %q, want %q", got, wantGateSetBody)
	}
	if !strings.Contains(stdout, gateKey) {
		t.Errorf("stdout = %q, want it to contain the gate-set key %q", stdout, gateKey)
	}

	digest := strings.TrimSuffix(strings.TrimPrefix(gateKey, "sets/"), "/gate.txt")
	boundGateKey := "scenarios/" + digest + "/.farm-gate-set.txt"
	fake.mu.Lock()
	boundGate, hasBoundGate := fake.objects[boundGateKey]
	fake.mu.Unlock()
	if !hasBoundGate {
		t.Fatalf("fake bucket has no digest-bound gate object %s", boundGateKey)
	}
	if string(boundGate) != string(wantGateSetBody) {
		t.Errorf("digest-bound gate set = %q, want %q", boundGate, wantGateSetBody)
	}

	downloaded := t.TempDir()
	fake.mu.Lock()
	for key, body := range fake.objects {
		prefix := "scenarios/" + digest + "/"
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		rel := strings.TrimPrefix(key, prefix)
		path := filepath.Join(downloaded, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			fake.mu.Unlock()
			t.Fatalf("MkdirAll downloaded scenario tree: %v", err)
		}
		if err := os.WriteFile(path, body, 0o600); err != nil {
			fake.mu.Unlock()
			t.Fatalf("write downloaded scenario tree: %v", err)
		}
	}
	fake.mu.Unlock()
	gotDigest, err := farm.TreeDigest(downloaded)
	if err != nil {
		t.Fatalf("TreeDigest(downloaded scenario tree): %v", err)
	}
	if gotDigest != digest {
		t.Errorf("uploaded scenario tree digest = %s, printed/keyed digest = %s", gotDigest, digest)
	}
}

func TestFarmPush_ScenarioTreeSkipsSymlinkOutsideRoot(t *testing.T) {
	fake := newFakeFarmS3()
	server := fake.server()
	defer server.Close()
	setFarmEnv(t, server.URL)

	scenariosDir := t.TempDir()
	scenarioBody := []byte("---\nid: scenario-a\n---\n")
	if err := os.WriteFile(filepath.Join(scenariosDir, "scenario-a.md"), scenarioBody, 0o600); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
	canaryBody := []byte("outside-tree-canary-must-never-be-uploaded")
	canaryPath := filepath.Join(t.TempDir(), "canary.txt")
	if err := os.WriteFile(canaryPath, canaryBody, 0o600); err != nil {
		t.Fatalf("write canary: %v", err)
	}
	if err := os.Symlink(canaryPath, filepath.Join(scenariosDir, "linked-canary.md")); err != nil {
		t.Fatalf("symlink canary: %v", err)
	}

	cfg, err := farm.ConfigFromEnv()
	if err != nil {
		t.Fatalf("ConfigFromEnv: %v", err)
	}
	gateSet := []byte("scenario-a\n")
	digest, err := pushScenarioTree(t.Context(), farm.NewSinkClient(cfg), scenariosDir, gateSet)
	if err != nil {
		t.Fatalf("pushScenarioTree: %v", err)
	}

	wantTree := t.TempDir()
	if err := os.WriteFile(filepath.Join(wantTree, "scenario-a.md"), scenarioBody, 0o600); err != nil {
		t.Fatalf("write expected scenario: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wantTree, boundGateSetFile), gateSet, 0o600); err != nil {
		t.Fatalf("write expected gate set: %v", err)
	}
	wantDigest, err := farm.TreeDigest(wantTree)
	if err != nil {
		t.Fatalf("TreeDigest(expected tree): %v", err)
	}
	if digest != wantDigest {
		t.Errorf("pushScenarioTree digest = %s, want regular-files-only digest %s", digest, wantDigest)
	}

	prefix := "scenarios/" + digest + "/"
	fake.mu.Lock()
	defer fake.mu.Unlock()
	for key, body := range fake.objects {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		if strings.HasSuffix(key, "/linked-canary.md") {
			t.Errorf("pushScenarioTree uploaded symlink as %q", key)
		}
		if bytes.Contains(body, canaryBody) {
			t.Errorf("pushScenarioTree uploaded outside-tree canary bytes as %q", key)
		}
	}
}

func TestFarmPush_ScenarioTreeSkipsSpecialFiles(t *testing.T) {
	fake := newFakeFarmS3()
	server := fake.server()
	defer server.Close()
	setFarmEnv(t, server.URL)

	// Unix-domain sockets have a short platform path limit, so t.TempDir's
	// test-name prefix is too long on macOS.
	scenariosDir, err := os.MkdirTemp("", "zcp-farm-special-") //nolint:usetesting
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(scenariosDir) })
	if err := os.WriteFile(filepath.Join(scenariosDir, "scenario-a.md"), []byte("scenario"), 0o600); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
	socketPath := filepath.Join(scenariosDir, "runner.sock")
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(t.Context(), "unix", socketPath)
	if err != nil {
		t.Skipf("Unix sockets are unavailable: %v", err)
	}
	defer func() { _ = listener.Close() }()

	cfg, err := farm.ConfigFromEnv()
	if err != nil {
		t.Fatalf("ConfigFromEnv: %v", err)
	}
	digest, err := pushScenarioTree(t.Context(), farm.NewSinkClient(cfg), scenariosDir, []byte("scenario-a\n"))
	if err != nil {
		t.Fatalf("pushScenarioTree with Unix socket: %v", err)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if _, ok := fake.objects["scenarios/"+digest+"/runner.sock"]; ok {
		t.Error("pushScenarioTree uploaded a Unix socket")
	}
}

func TestReadScenarioSnapshotFile_RejectsEntryReplacement(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "scenario.md")
	if err := os.WriteFile(source, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	expected, err := os.Lstat(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("replacement"), 0o644); err != nil {
		t.Fatal(err)
	}

	body, err := readScenarioSnapshotFile(source, expected)
	if err == nil {
		t.Fatalf("readScenarioSnapshotFile accepted replacement inode with body %q", body)
	}
}

func TestReadScenarioSnapshotFile_DoesNotFollowReplacementSymlink(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "scenario.md")
	canary := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(source, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(canary, []byte("outside-canary"), 0o644); err != nil {
		t.Fatal(err)
	}
	expected, err := os.Lstat(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(canary, source); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	body, err := readScenarioSnapshotFile(source, expected)
	if err == nil {
		t.Fatalf("readScenarioSnapshotFile followed replacement symlink with body %q", body)
	}
}

// TestFarmPull_Batch_WritesManifestAndSummary pins outcome 4 of the S15
// brief: `farm pull --batch <b>` also writes batches/<b>/manifest.json and
// batches/<b>/summary.json (when present) directly under <out>/, where
// `farm report <out>` reads them (a subdirectory would be graded as a run).
func TestFarmPull_Batch_WritesManifestAndSummary(t *testing.T) {
	fake := newFakeFarmS3()
	server := fake.server()
	defer server.Close()
	setFarmEnv(t, server.URL)

	manifestBody := []byte(`{
		"batch": "b2", "createdAt": "2026-09-10T00:00:00Z", "startedAt": "2026-09-10T00:00:00Z",
		"set": "gate", "candidateSha256": "cand", "evaluatorSha256": "eval", "scenariosDigest": "scen",
		"runs": [{"runId": "b2-recipe-a", "scenario": "recipe-a", "projectName": "zcp-farm-b2-recipe-a"}]
	}`)
	summaryBody := []byte(`{"batch": "b2", "finishedAt": "2026-09-10T01:00:00Z", "endedBy": "settled", "runs": []}`)
	fake.objects["batches/b2/manifest.json"] = manifestBody
	fake.objects["batches/b2/summary.json"] = summaryBody
	fake.objects["runs/b2-recipe-a/done.json"] = []byte(`{"runId":"b2-recipe-a","parts":{}}`)

	outDir := t.TempDir()
	var code int
	_, stderr := captureOutput(t, func() {
		code = runEvalFarm([]string{"pull", "--batch", "b2", "--out", outDir})
	})
	if code != 0 {
		t.Fatalf("runEvalFarm(pull --batch b2) = %d, stderr = %q", code, stderr)
	}

	gotManifest, err := os.ReadFile(filepath.Join(outDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read pulled manifest.json: %v", err)
	}
	if string(gotManifest) != string(manifestBody) {
		t.Errorf("pulled manifest.json = %q, want %q", gotManifest, manifestBody)
	}
	gotSummary, err := os.ReadFile(filepath.Join(outDir, "summary.json"))
	if err != nil {
		t.Fatalf("read pulled summary.json: %v", err)
	}
	if string(gotSummary) != string(summaryBody) {
		t.Errorf("pulled summary.json = %q, want %q", gotSummary, summaryBody)
	}
}

// TestFarmPush_GateSetResolvedNextToScenarios pins S22 finding 3: `farm
// push --scenarios <dir>` resolves the local gate-set file relative to
// --scenarios, never to cwd. cwd is set to an unrelated directory for every
// subtest, so a cwd-relative read would fail regardless of which case is
// under test. Default is "<scenariosDir>/../../farm/gate-set.txt" (the repo's
// eval/behavioral/scenarios + eval/farm layout); --gate-set
// overrides it; a resolution failure names the fully resolved path.
func TestFarmPush_GateSetResolvedNextToScenarios(t *testing.T) {
	root := filepath.Join(t.TempDir(), "eval")
	scenariosDir := filepath.Join(root, "behavioral", "scenarios")
	if err := os.MkdirAll(scenariosDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scenariosDir, "recipe-a.md"), []byte("# recipe-a\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	elsewhere := t.TempDir()
	t.Chdir(elsewhere)

	uploadedGateSet := func(t *testing.T, fake *fakeFarmS3) (key string, body []byte) {
		t.Helper()
		fake.mu.Lock()
		defer fake.mu.Unlock()
		for k := range fake.objects {
			if strings.HasPrefix(k, "sets/") && strings.HasSuffix(k, "/gate.txt") {
				key = k
			}
		}
		return key, fake.objects[key]
	}

	t.Run("default_resolved_next_to_scenarios", func(t *testing.T) {
		farmDir := filepath.Join(root, "farm")
		if err := os.MkdirAll(farmDir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		gateBody := []byte("recipe-a\n")
		if err := os.WriteFile(filepath.Join(farmDir, "gate-set.txt"), gateBody, 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		fake := newFakeFarmS3()
		server := fake.server()
		defer server.Close()
		setFarmEnv(t, server.URL)

		var code int
		stdout, stderr := captureOutput(t, func() {
			code = runEvalFarm([]string{"push", "--scenarios", scenariosDir})
		})
		if code != 0 {
			t.Fatalf("runEvalFarm(push) = %d, stderr = %q", code, stderr)
		}

		gateKey, got := uploadedGateSet(t, fake)
		if gateKey == "" {
			t.Fatalf("fake bucket has no sets/<digest>/gate.txt object; objects: %v", fake.objects)
		}
		if string(got) != string(gateBody) {
			t.Errorf("uploaded gate set = %q, want %q", got, gateBody)
		}
		if !strings.Contains(stdout, gateKey) {
			t.Errorf("stdout = %q, want it to contain the gate-set key %q", stdout, gateKey)
		}
	})

	t.Run("gate_set_flag_overrides_default", func(t *testing.T) {
		overrideDir := t.TempDir()
		overridePath := filepath.Join(overrideDir, "custom-gate-set.txt")
		overrideBody := []byte("recipe-override\n")
		if err := os.WriteFile(overridePath, overrideBody, 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		fake := newFakeFarmS3()
		server := fake.server()
		defer server.Close()
		setFarmEnv(t, server.URL)

		var code int
		stdout, stderr := captureOutput(t, func() {
			code = runEvalFarm([]string{"push", "--scenarios", scenariosDir, "--gate-set", overridePath})
		})
		if code != 0 {
			t.Fatalf("runEvalFarm(push) = %d, stderr = %q", code, stderr)
		}

		gateKey, got := uploadedGateSet(t, fake)
		if gateKey == "" {
			t.Fatalf("fake bucket has no sets/<digest>/gate.txt object; objects: %v", fake.objects)
		}
		if string(got) != string(overrideBody) {
			t.Errorf("uploaded gate set = %q, want override body %q", got, overrideBody)
		}
		if !strings.Contains(stdout, gateKey) {
			t.Errorf("stdout = %q, want it to contain the gate-set key %q", stdout, gateKey)
		}
	})

	t.Run("missing_default_names_resolved_path_in_error", func(t *testing.T) {
		missingRoot := t.TempDir()
		missingScenariosDir := filepath.Join(missingRoot, "behavioral", "scenarios")
		if err := os.MkdirAll(missingScenariosDir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(filepath.Join(missingScenariosDir, "recipe-a.md"), []byte("# recipe-a\n"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		// No ../../farm/gate-set.txt created — the default must fail.

		fake := newFakeFarmS3()
		server := fake.server()
		defer server.Close()
		setFarmEnv(t, server.URL)

		wantPath := filepath.Join(missingRoot, "farm", "gate-set.txt")

		var code int
		_, stderr := captureOutput(t, func() {
			code = runEvalFarm([]string{"push", "--scenarios", missingScenariosDir})
		})
		if code == 0 {
			t.Fatalf("runEvalFarm(push) = 0, want nonzero for a missing gate-set file")
		}
		if !strings.Contains(stderr, wantPath) {
			t.Errorf("stderr = %q, want it to name the resolved path %q", stderr, wantPath)
		}
	})
}
