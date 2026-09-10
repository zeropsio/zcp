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
// is the evaluator's own digest. Both keys are printed. --gate-set is
// passed explicitly because the real checkout's eval/farm/gate-set.txt does
// not sit at S22 finding 3's default location relative to
// eval/behavioral/scenarios (that default assumes gate-set.txt is a sibling
// of the scenarios dir itself) — see eval/farm/README.md's push usage.
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
	gateSetPath := filepath.Join(repoRoot, "eval", "farm", "gate-set.txt")

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
			"push", "--evaluator", evaluatorPath, "--scenarios", scenariosDir, "--gate-set", gateSetPath,
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
// under test. Default is "<scenariosDir>/../farm/gate-set.txt"; --gate-set
// overrides it; a resolution failure names the fully resolved path.
func TestFarmPush_GateSetResolvedNextToScenarios(t *testing.T) {
	root := t.TempDir()
	scenariosDir := filepath.Join(root, "scenarios")
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
		missingScenariosDir := filepath.Join(missingRoot, "scenarios")
		if err := os.MkdirAll(missingScenariosDir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(filepath.Join(missingScenariosDir, "recipe-a.md"), []byte("# recipe-a\n"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		// No farm/gate-set.txt sibling created — the default must fail.

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
