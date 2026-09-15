package eval

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/capture"
	"github.com/zeropsio/zcp/internal/platform"
)

// TestSpawnClaudeResume_Retrospective_TextOnlyArgs pins the D18 argv
// contract (docs/spec-eval-farm.md §2.3 FM-13): the retrospective resume is
// TEXT-ONLY — no --mcp-config attached (unlike every other spawn path) and
// tools disabled via --tools "" — with the existing --max-turns 3 cap kept.
// Live gate1 bundles showed the model answering the briefing prompt by
// acting (Read/Write, Bash/Write) instead of replying in text, burning the
// turn cap; this argv shape is what stops that.
func TestSpawnClaudeResume_Retrospective_TextOnlyArgs(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	argvFile := filepath.Join(dir, "argv.txt")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argvFile + "\n" +
		`printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"session_id":"sess-x"}'` + "\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)

	runner := NewRunner(RunnerConfig{MCPConfig: "/should/not/appear.json", WorkDir: dir}, nil, platform.NewMock(), "p1")
	logFile := filepath.Join(dir, "retro.jsonl")
	if err := runner.spawnClaudeResume(context.Background(), "sess-x", "what did you do", logFile, captureProcessScope{}); err != nil {
		t.Fatalf("spawnClaudeResume: %v", err)
	}

	argvBytes, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatalf("read argv: %v", err)
	}
	argv := strings.Split(strings.TrimRight(string(argvBytes), "\n"), "\n")

	if strings.Contains(strings.Join(argv, " "), "--mcp-config") {
		t.Errorf("argv = %v, must not contain --mcp-config (retrospective is text-only)", argv)
	}
	if !hasConsecutiveArgs(argv, "--tools", "") {
		t.Errorf("argv = %v, want --tools \"\" (tools disabled)", argv)
	}
	if !hasConsecutiveArgs(argv, "--max-turns", "3") {
		t.Errorf("argv = %v, want --max-turns 3 kept", argv)
	}
}

// hasConsecutiveArgs reports whether argv contains flag immediately followed
// by value as adjacent elements.
func hasConsecutiveArgs(argv []string, flag, value string) bool {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == flag && argv[i+1] == value {
			return true
		}
	}
	return false
}

// TestLoadRetrospectivePrompt_BriefingFutureAgent_Embedded asserts the default
// retrospective prompt is embedded and loadable. Drift in embed path (e.g.
// rename or move) surfaces here, not as a runtime "prompt not found" during a
// live run.
func TestLoadRetrospectivePrompt_BriefingFutureAgent_Embedded(t *testing.T) {
	t.Parallel()
	body, err := LoadRetrospectivePrompt("briefing-future-agent")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !strings.Contains(body, "briefing a future agent") {
		t.Errorf("loaded prompt missing expected phrase 'briefing a future agent'; got first 200 chars:\n%s", firstN(body, 200))
	}
}

// TestLoadRetrospectivePrompt_UnknownStyle_ListsAvailable asserts the error
// message for an unknown style includes the available styles, so an operator
// who typoed the scenario.retrospective.promptStyle gets actionable feedback.
func TestLoadRetrospectivePrompt_UnknownStyle_ListsAvailable(t *testing.T) {
	t.Parallel()
	_, err := LoadRetrospectivePrompt("does-not-exist")
	if err == nil {
		t.Fatal("expected error for unknown style")
	}
	if !strings.Contains(err.Error(), "available:") {
		t.Errorf("error message missing 'available:' hint: %v", err)
	}
	if !strings.Contains(err.Error(), "briefing-future-agent") {
		t.Errorf("error message should list briefing-future-agent: %v", err)
	}
}

// TestLoadRetrospectivePrompt_PathTraversal_Rejected guards against a scenario
// whose retrospective.promptStyle field contains slashes or dots. Without this
// the embed.FS lookup could be coaxed into walking out of the prompts dir.
func TestLoadRetrospectivePrompt_PathTraversal_Rejected(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"../scenario", "foo/bar", "foo.bar", ""} {
		if _, err := LoadRetrospectivePrompt(bad); err == nil {
			t.Errorf("expected error for style %q", bad)
		}
	}
}

// TestExtractSessionID_FirstSystemEvent_Returned confirms the parser pulls the
// session_id from the first system event in a stream-json transcript.
func TestExtractSessionID_FirstSystemEvent_Returned(t *testing.T) {
	t.Parallel()
	jsonl := `{"type":"system","subtype":"init","session_id":"ses_abc123","cwd":"/var/www"}
{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}
{"type":"system","subtype":"init","session_id":"ses_should_not_match"}
`
	path := writeTmp(t, "transcript.jsonl", jsonl)
	got, err := extractSessionID(path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if got != "ses_abc123" {
		t.Errorf("session_id = %q, want ses_abc123", got)
	}
}

// TestExtractSessionID_MissingSystemEvent_Errors_WithPreview pins the error
// path: no system event with session_id → error mentioning the file and a
// preview of leading events for debugging.
func TestExtractSessionID_MissingSystemEvent_Errors_WithPreview(t *testing.T) {
	t.Parallel()
	jsonl := `{"type":"assistant","message":{"content":[{"type":"text","text":"alone"}]}}
{"type":"result","is_error":false}
`
	path := writeTmp(t, "transcript.jsonl", jsonl)
	_, err := extractSessionID(path)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "no system event") {
		t.Errorf("error message: %v", err)
	}
}

// TestExtractSelfReview_AssistantTextEvents_Joined asserts assistant text
// events from the retrospective JSONL are concatenated into the self-review.
// Non-assistant events and non-text content blocks are skipped.
func TestExtractSelfReview_AssistantTextEvents_Joined(t *testing.T) {
	t.Parallel()
	jsonl := `{"type":"system","subtype":"init","session_id":"ses_x"}
{"type":"assistant","message":{"content":[{"type":"text","text":"First paragraph."}]}}
{"type":"user","message":{"content":[{"type":"tool_result","content":"unrelated"}]}}
{"type":"assistant","message":{"content":[{"type":"text","text":"Second paragraph."},{"type":"tool_use","name":"x"}]}}
{"type":"result","is_error":false}
`
	path := writeTmp(t, "retro.jsonl", jsonl)
	got, err := extractSelfReview(path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	want := "First paragraph.\n\nSecond paragraph."
	if got != want {
		t.Errorf("self-review =\n%q\nwant\n%q", got, want)
	}
}

// TestExtractSelfReview_NoAssistantText_Errors pins the failure mode where the
// retrospective contains no extractable text (e.g. agent emitted only tool
// calls). RunBehavioralScenario uses this to write the .Error field.
func TestExtractSelfReview_NoAssistantText_Errors(t *testing.T) {
	t.Parallel()
	jsonl := `{"type":"system","subtype":"init","session_id":"ses_x"}
{"type":"result","is_error":false}
`
	path := writeTmp(t, "retro.jsonl", jsonl)
	if _, err := extractSelfReview(path); err == nil {
		t.Fatal("expected error for empty retrospective")
	}
}

// TestDetectCompaction covers all three signal forms — keeps the heuristic
// honest if Claude Code stream-json adds a new compaction signal we'd miss.
func TestDetectCompaction(t *testing.T) {
	t.Parallel()
	for name, jsonl := range map[string]string{
		"compact_boundary subtype": `{"type":"system","subtype":"compact_boundary"}`,
		"compacted bool":           `{"type":"system","subtype":"init","session_id":"x","compacted":true}`,
		"prose marker":             `{"type":"assistant","message":{"content":[{"type":"text","text":"Previous Conversation Compacted"}]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := writeTmp(t, "retro.jsonl", jsonl)
			if !detectCompaction(path) {
				t.Errorf("expected compaction detected in %s", name)
			}
		})
	}

	t.Run("clean transcript", func(t *testing.T) {
		t.Parallel()
		path := writeTmp(t, "retro.jsonl", `{"type":"assistant","message":{"content":[{"type":"text","text":"normal post-hoc reply"}]}}`)
		if detectCompaction(path) {
			t.Error("false positive on clean transcript")
		}
	})
}

// TestParseScenario_Behavioral_FieldsPopulated round-trips the committed
// behavioral scenario through ParseScenario and asserts the new
// Tags/Area/Retrospective/NotableFriction fields populate.
func TestParseScenario_Behavioral_FieldsPopulated(t *testing.T) {
	t.Parallel()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Dir(filepath.Dir(wd)) // .../zcp
	path := filepath.Join(repoRoot, "eval", "behavioral", "scenarios", "greenfield-node-postgres-dev-stage.md")
	sc, err := ParseScenario(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !sc.IsBehavioral() {
		t.Fatal("scenario should be behavioral (retrospective set)")
	}
	if sc.Retrospective.PromptStyle != "briefing-future-agent" {
		t.Errorf("promptStyle = %q", sc.Retrospective.PromptStyle)
	}
	if len(sc.Tags) == 0 {
		t.Error("tags empty")
	}
	if sc.Area == "" {
		t.Error("area empty")
	}
	if len(sc.NotableFriction) != 2 {
		t.Errorf("notableFriction count = %d, want 2", len(sc.NotableFriction))
	}
	if sc.Seed != ModeEmpty {
		t.Errorf("seed = %q, want empty", sc.Seed)
	}
	if sc.Verification == nil || len(sc.Verification.ExpectedServices) != 3 || !sc.Verification.NoFailedProcesses {
		t.Fatalf("verification = %+v, want three services and no-failed-process check", sc.Verification)
	}
}

// TestBehavioralRun_MetaCarriesDigestsAndCredentialMode pins the meta.json
// digest/credential fields docs/spec-eval-farm.md §1.2 FM-4 and §2.4 FM-16
// add: evaluatorSha256 (self hash), credentialMode derived from env presence
// only, and the forbidden-state warning when both credential kinds are set.
func TestBehavioralRun_MetaCarriesDigestsAndCredentialMode(t *testing.T) {
	t.Parallel()

	t.Run("evaluator self hash matches an independently computed sha256", func(t *testing.T) {
		t.Parallel()
		exe, err := os.Executable()
		if err != nil {
			t.Fatalf("os.Executable: %v", err)
		}
		raw, err := os.ReadFile(exe)
		if err != nil {
			t.Fatalf("read test binary: %v", err)
		}
		sum := sha256.Sum256(raw)
		want := hex.EncodeToString(sum[:])

		got, err := evaluatorSelfSHA256()
		if err != nil {
			t.Fatalf("evaluatorSelfSHA256: %v", err)
		}
		if got != want {
			t.Fatalf("evaluatorSelfSHA256 = %s, want %s", got, want)
		}
	})

	// Owner decision (spec 79ced2cc, FM-16): the farm's agent credential is
	// OAuth-only. credentialFieldsFromPresence reports presence only, never
	// a value: "oauth-token" when CLAUDE_CODE_OAUTH_TOKEN is set (regardless
	// of ANTHROPIC_API_KEY — that combination is exactly the disallowed
	// state farm/report.go's ReportRun blocks on, not something meta.json
	// itself refuses to record), and anthropicAPIKeyPresent=true whenever
	// ANTHROPIC_API_KEY is set.
	t.Run("credential fields from presence, OAuth-only", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name           string
			hasAPIKey      bool
			hasOAuth       bool
			wantCredential string
			wantAPIKey     bool
		}{
			{"oauth token only", false, true, "oauth-token", false},
			{"api key only", true, false, "", true},
			{"neither", false, false, "", false},
			{"both — api key present alongside oauth", true, true, "oauth-token", true},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				credential, apiKeyPresent := credentialFieldsFromPresence(tc.hasAPIKey, tc.hasOAuth)
				if credential != tc.wantCredential {
					t.Errorf("credential = %q, want %q", credential, tc.wantCredential)
				}
				if apiKeyPresent != tc.wantAPIKey {
					t.Errorf("anthropicAPIKeyPresent = %t, want %t", apiKeyPresent, tc.wantAPIKey)
				}
			})
		}
	})
}

// TestObservedModelFromProviderCapture_NoProviderFile_ReturnsEmpty pins the
// "else empty" half of FM-16's modelObserved rule: a capture window with no
// (or unreadable) provider.jsonl never fabricates a model name.
func TestObservedModelFromProviderCapture_NoProviderFile_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	if got := observedModelFromProviderCapture(t.TempDir()); got != "" {
		t.Fatalf("observedModelFromProviderCapture = %q, want empty", got)
	}
	if got := observedModelFromProviderCapture(""); got != "" {
		t.Fatalf("observedModelFromProviderCapture(\"\") = %q, want empty", got)
	}
}

// TestBehavioralResult_ModelObservedAtFreeze pins finding E7: ModelObserved
// is computed when the result is finalized (writeBehavioralResult), not at
// newBehavioralResult time — at construction the capture's provider.jsonl is
// still empty (no agent request has been sent yet), so reading it there
// always misses the model. The provider log here is written AFTER the
// result is constructed, mirroring the real timeline.
func TestBehavioralResult_ModelObservedAtFreeze(t *testing.T) {
	t.Parallel()
	sessionDir := t.TempDir()
	client := platform.NewMock()
	cfg := RunnerConfig{Capture: &capture.Connection{CaptureID: "cap1", ProxyURL: "http://127.0.0.1:1", SessionDir: sessionDir}}
	runner := NewRunner(cfg, nil, client, "p1")

	sc := &Scenario{ID: "sc1"}
	result := runner.newBehavioralResult(sc, "suite1", time.Now())
	if result.ModelObserved != "" {
		t.Fatalf("ModelObserved = %q at construction, want empty (no capture traffic yet)", result.ModelObserved)
	}

	body := []byte(`{"model":"claude-fake-model"}`)
	rec := capture.Record{
		Seq: 1, Time: time.Now(), SessionID: "cap1", Kind: capture.RecordProviderRequestBody,
		ExchangeID: "ex1", BodyBase64: base64.StdEncoding.EncodeToString(body),
	}
	line, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal record: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "provider.jsonl"), append(line, '\n'), 0o600); err != nil {
		t.Fatalf("write provider.jsonl: %v", err)
	}

	outDir := t.TempDir()
	if err := runner.writeBehavioralResult(outDir, result); err != nil {
		t.Fatalf("writeBehavioralResult: %v", err)
	}
	if result.ModelObserved != "claude-fake-model" {
		t.Fatalf("ModelObserved = %q, want claude-fake-model", result.ModelObserved)
	}
}

func writeTmp(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write tmp: %v", err)
	}
	return p
}

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// TestScenarioBaseline_CapturesEveryUnchangedHostname pins FM-29: the
// baseline recorded at scenario start covers the union of
// verification.unchanged and nodePostgresRecord.unrelated — exactly those
// hostnames, deduplicated, each with the active app-version id a
// ListServicesDirect read reports for it.
func TestScenarioBaseline_CapturesEveryUnchangedHostname(t *testing.T) {
	t.Parallel()
	sc := &Scenario{Verification: &VerificationConfig{
		Unchanged:          []string{"hostA", "hostB"},
		NodePostgresRecord: &NodePostgresRecordConfig{Stage: "appstage", Database: "db", Unrelated: "hostB"},
	}}
	client := platform.NewMock().WithServicesDirect([]platform.ServiceStack{
		{Name: "hostA", ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "av-A"}},
		{Name: "hostB", ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "av-B"}},
		{Name: "hostC", ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "av-C"}},
	})
	runner := NewRunner(RunnerConfig{}, nil, client, "p1")

	hostnames := scenarioBaselineHostnames(sc)
	if len(hostnames) != 2 {
		t.Fatalf("scenarioBaselineHostnames = %v, want exactly [hostA hostB] (union, deduped)", hostnames)
	}

	result := &BehavioralResult{}
	runner.recordScenarioBaseline(context.Background(), hostnames, result)
	if result.Baseline == nil {
		t.Fatal("expected Baseline to be recorded")
	}
	want := map[string]string{"hostA": "av-A", "hostB": "av-B"}
	if len(result.Baseline.AppVersions) != len(want) {
		t.Fatalf("Baseline.AppVersions = %+v, want exactly %+v", result.Baseline.AppVersions, want)
	}
	for host, av := range want {
		if result.Baseline.AppVersions[host] != av {
			t.Errorf("Baseline.AppVersions[%q] = %q, want %q", host, result.Baseline.AppVersions[host], av)
		}
	}
	if _, present := result.Baseline.AppVersions["hostC"]; present {
		t.Errorf("Baseline.AppVersions must not include hostC (not declared unchanged/unrelated)")
	}
}

// TestScenarioBaseline_IncludesArtifactPromotionFrom pins finding E2: the
// baseline hostname set also covers every declared
// verification.artifactPromotion[].from — without it, the O7
// dev_unchanged row (docs/spec-eval-farm.md §4.4 O7) has no baseline to
// compare against and blocks on every cross-deploy run.
func TestScenarioBaseline_IncludesArtifactPromotionFrom(t *testing.T) {
	t.Parallel()
	sc := &Scenario{Verification: &VerificationConfig{
		Unchanged: []string{"hostA"},
		ArtifactPromotion: []ArtifactPromotionEntry{
			{From: "appdev", To: "appstage"},
			{From: "hostA", To: "hostB"}, // dup of Unchanged's hostA: must not duplicate
		},
	}}

	hostnames := scenarioBaselineHostnames(sc)
	want := []string{"hostA", "appdev"}
	if len(hostnames) != len(want) {
		t.Fatalf("scenarioBaselineHostnames = %v, want %v (unchanged + deduped artifactPromotion[].from)", hostnames, want)
	}
	for i, h := range want {
		if hostnames[i] != h {
			t.Errorf("scenarioBaselineHostnames[%d] = %q, want %q (got %v)", i, hostnames[i], h, hostnames)
		}
	}
}

// TestBehavioralRun_RetrospectiveMaxTurns_RecordedMissingNotExecutionError
// pins docs/spec-eval-farm.md §2.3 FM-13: a text-only retrospective that
// still exhausts its turn cap (result line subtype "error_max_turns",
// non-zero exit) is recorded as retrospectiveFile present / selfReviewFile
// empty / meta.error prefixed "retrospective: missing:" — the already-frozen
// task result is untouched, and the farm execution dimension stays "ok"
// (the retrospective's self-review is optional evidence, not execution).
func TestBehavioralRun_RetrospectiveMaxTurns_RecordedMissingNotExecutionError(t *testing.T) { // non-parallel: process environment
	h := newBehavioralHarness(t)
	script := `#!/bin/sh
if [ "$1" = "--resume" ]; then
    printf '%s\n' '{"type":"result","subtype":"error_max_turns","is_error":true,"num_turns":3,"session_id":"offline-probe"}'
    exit 1
fi
printf '%s\n' '{"type":"system","subtype":"init","session_id":"offline-probe","model":"fake-offline"}'
printf '%s\n' '{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Done."}]}}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"session_id":"offline-probe","result":"Done."}'
`
	h.writeClaudeScript(t, script)
	scenarioPath := h.writeScenario(t, requiredExpectedAppScenario("required-retro-maxturns"))
	mock := platform.NewMock().WithServicesDirect([]platform.ServiceStack{{ID: "app-1", Name: "app", Status: "ACTIVE"}})
	cfg := h.config()
	cfg.Capture = &capture.Connection{CaptureID: "owned", ProxyURL: "http://127.0.0.1:1", SessionDir: t.TempDir()}
	cfg.CaptureOwned = true
	h.requiredBinding(t, &cfg)
	runner := NewRunner(cfg, nil, &freshThenRealClient{Client: mock}, "offline-project")

	result, err := runner.RunBehavioralScenario(context.Background(), scenarioPath, "suite")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(result.Error, "retrospective: missing:") {
		t.Errorf("result.Error = %q, want prefix %q", result.Error, "retrospective: missing:")
	}
	if result.SelfReviewFile != "" {
		t.Errorf("SelfReviewFile = %q, want empty", result.SelfReviewFile)
	}
	if result.RetrospectiveFile == "" {
		t.Error("RetrospectiveFile empty, want the retrospective log path recorded")
	}
	if result.Task == nil || result.Task.Result != CheckPassed {
		t.Fatalf("Task = %+v, want passed (untouched by retrospective failure)", result.Task)
	}
	if got := ExecutionDimension(result); got != "ok" {
		t.Errorf("ExecutionDimension(result) = %q, want ok (retrospective-missing must not become an execution error)", got)
	}
}

// TestBehavioralResult_UsageFromTranscriptResultLines_Summed pins brief
// S14's cost fields: main sums every "result" line in the transcript (the
// fresh run plus every user-sim resume, which append their own result event
// to the same file); retrospective comes from retrospective.jsonl's own
// result line; totalCostUsd is the sum of both.
func TestBehavioralResult_UsageFromTranscriptResultLines_Summed(t *testing.T) {
	t.Parallel()
	transcript := `{"type":"assistant","message":{"content":[{"type":"text","text":"working"}]}}
{"type":"result","subtype":"success","total_cost_usd":0.01,"usage":{"input_tokens":100,"output_tokens":50,"cache_creation_input_tokens":10,"cache_read_input_tokens":5},"num_turns":3,"duration_ms":1000}
{"type":"result","subtype":"success","total_cost_usd":0.02,"usage":{"input_tokens":200,"output_tokens":75,"cache_creation_input_tokens":20,"cache_read_input_tokens":15},"num_turns":4,"duration_ms":2000}
{"type":"result","subtype":"success","total_cost_usd":0.03,"usage":{"input_tokens":300,"output_tokens":25,"cache_creation_input_tokens":30,"cache_read_input_tokens":25},"num_turns":5,"duration_ms":3000}
`
	transcriptPath := writeTmp(t, "transcript.jsonl", transcript)
	retro := `{"type":"result","subtype":"success","total_cost_usd":0.005,"usage":{"input_tokens":40,"output_tokens":10,"cache_creation_input_tokens":1,"cache_read_input_tokens":2},"num_turns":1,"duration_ms":500}
`
	retroPath := writeTmp(t, "retrospective.jsonl", retro)

	usage := computeBehavioralUsage(transcriptPath, retroPath)
	if usage == nil {
		t.Fatal("computeBehavioralUsage returned nil, want a summary")
	}
	if usage.Main == nil {
		t.Fatal("Main is nil, want the summed transcript usage")
	}
	wantMain := UsagePhase{CostUsd: 0.06, InputTokens: 600, OutputTokens: 150, CacheCreationInputTokens: 60, CacheReadInputTokens: 45, NumTurns: 12, DurationMs: 6000}
	if *usage.Main != wantMain {
		t.Errorf("Main = %+v, want %+v", *usage.Main, wantMain)
	}
	if usage.Retrospective == nil {
		t.Fatal("Retrospective is nil, want the retrospective.jsonl result line")
	}
	wantRetro := UsagePhase{CostUsd: 0.005, InputTokens: 40, OutputTokens: 10, CacheCreationInputTokens: 1, CacheReadInputTokens: 2, NumTurns: 1, DurationMs: 500}
	if *usage.Retrospective != wantRetro {
		t.Errorf("Retrospective = %+v, want %+v", *usage.Retrospective, wantRetro)
	}
	if want := 0.065; usage.TotalCostUsd < want-1e-9 || usage.TotalCostUsd > want+1e-9 {
		t.Errorf("TotalCostUsd = %v, want %v", usage.TotalCostUsd, want)
	}
}

// TestBehavioralResult_UsageAbsent_Omitted pins brief S14: an absent
// transcript/retrospective file, or one with no "result" line, produces a
// nil usage summary — never a zeroed one.
func TestBehavioralResult_UsageAbsent_Omitted(t *testing.T) {
	t.Parallel()
	t.Run("both files missing", func(t *testing.T) {
		t.Parallel()
		if got := computeBehavioralUsage(filepath.Join(t.TempDir(), "no-such-transcript.jsonl"), filepath.Join(t.TempDir(), "no-such-retro.jsonl")); got != nil {
			t.Errorf("computeBehavioralUsage = %+v, want nil", got)
		}
	})
	t.Run("empty paths", func(t *testing.T) {
		t.Parallel()
		if got := computeBehavioralUsage("", ""); got != nil {
			t.Errorf("computeBehavioralUsage(\"\", \"\") = %+v, want nil", got)
		}
	})
	t.Run("files exist but carry no result line", func(t *testing.T) {
		t.Parallel()
		transcriptPath := writeTmp(t, "transcript.jsonl", `{"type":"assistant","message":{"content":[{"type":"text","text":"working"}]}}`+"\n")
		retroPath := writeTmp(t, "retrospective.jsonl", `{"type":"system","subtype":"init","session_id":"x"}`+"\n")
		if got := computeBehavioralUsage(transcriptPath, retroPath); got != nil {
			t.Errorf("computeBehavioralUsage = %+v, want nil", got)
		}
	})
}

// TestScenarioRuntimeInputs_CarriesUserSimTurnsAndMutatingTools pins the
// askWhen wiring (docs/spec-eval-farm.md §4.1 FM-31): the runtime inputs the
// verifier grades against carry the user-sim loop's recorded turns and the
// mutating-tool vocabulary the caller configured — without either, every
// askWhen row would read "not asked" regardless of what happened.
func TestScenarioRuntimeInputs_CarriesUserSimTurnsAndMutatingTools(t *testing.T) {
	t.Parallel()
	mutating := map[string]bool{"zerops_deploy": true}
	r := &Runner{config: RunnerConfig{MutatingTools: mutating}}
	turns := []UserSimTurn{{Iteration: 1}}
	result := &BehavioralResult{TranscriptFile: "/tmp/transcript.jsonl", UserSim: &UserSimResult{Turns: turns}}

	got := r.scenarioRuntimeInputs(&Scenario{ID: "s"}, result)

	if !got.MutatingTools["zerops_deploy"] {
		t.Errorf("MutatingTools = %v, want the configured set", got.MutatingTools)
	}
	if len(got.UserSimTurns) != 1 {
		t.Errorf("UserSimTurns = %v, want the user-sim loop's turns", got.UserSimTurns)
	}
	if got.TranscriptPath != "/tmp/transcript.jsonl" {
		t.Errorf("TranscriptPath = %q", got.TranscriptPath)
	}
}

// TestSeed_ExpectMismatch_AgentNeverSpawned pins docs/spec-eval-farm.md
// §4.5 FM-63 at the RunBehavioralScenario level: when the scenario's
// seed.expect does not hold, the fake "claude" spawner is never invoked
// (proven by the absence of a marker file the script would otherwise
// touch), result.Preparation carries the "mismatch:" prefix, result.Error
// stays empty (a preparation mismatch is not an execution error), and
// result.Task grades not-run (no declared verification checks — vacuously
// every declared row is not-run).
func TestSeed_ExpectMismatch_AgentNeverSpawned(t *testing.T) { // non-parallel: process environment
	h := newBehavioralHarness(t)
	spawnMarker := filepath.Join(h.root, "spawn-marker")
	t.Setenv("SPAWN_MARKER", spawnMarker)
	script := "#!/bin/sh\n" +
		": > \"$SPAWN_MARKER\"\n" +
		"printf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"offline-probe\",\"model\":\"fake-offline\"}'\n" +
		"printf '%s\\n' '{\"type\":\"assistant\",\"message\":{\"role\":\"assistant\",\"content\":[{\"type\":\"text\",\"text\":\"Done.\"}]}}'\n" +
		"printf '%s\\n' '{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"session_id\":\"offline-probe\",\"result\":\"Done.\"}'\n"
	h.writeClaudeScript(t, script)
	scenario := "---\n" +
		"id: seed-expect-mismatch-no-spawn\n" +
		"seed:\n" +
		"  mode: empty\n" +
		"  expect:\n" +
		"    services: [{hostname: app, status: [ACTIVE]}]\n" +
		"retrospective:\n" +
		"  promptStyle: briefing-future-agent\n" +
		"---\n" +
		"Do the thing.\n"
	scenarioPath := h.writeScenario(t, scenario)
	mock := platform.NewMock().WithServicesDirect([]platform.ServiceStack{{ID: "app-1", Name: "app", Status: "FAILED"}})
	runner := NewRunner(h.config(), nil, mock, "offline-project")

	result, err := runner.RunBehavioralScenario(context.Background(), scenarioPath, "suite")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(result.Preparation, "mismatch:") {
		t.Errorf("Preparation = %q, want a mismatch: prefix", result.Preparation)
	}
	if result.Error != "" {
		t.Errorf("Error = %q, want empty (a preparation mismatch is not an execution error)", result.Error)
	}
	if _, statErr := os.Stat(spawnMarker); !os.IsNotExist(statErr) {
		t.Error("spawn marker exists — the agent was spawned despite the seed.expect mismatch")
	}
	if result.Task == nil || result.Task.Result != CheckNotRun {
		t.Errorf("Task = %+v, want not-run", result.Task)
	}
}

// TestRun_RequiredEnvVarMissing_BlocksPreparation_AgentNeverSpawned pins
// docs/spec-eval-farm.md §4.5 (requiredEnvVars extension) /
// spec-scenarios.md §9.2 rule 7: a scenario's requiredEnvVars entry that is
// unset/empty in the evaluator's own environment is a preparation mismatch
// routed through the same path as a seed.expect mismatch — result.Preparation
// carries "mismatch: resource <NAME> missing", result.Error stays empty, and
// the fake "claude" spawner is never invoked (proven by the absence of the
// spawn-marker file it would otherwise touch).
func TestRun_RequiredEnvVarMissing_BlocksPreparation_AgentNeverSpawned(t *testing.T) { // non-parallel: process environment
	const missingVar = "ZCP_EVAL_TEST_REQUIRED_ENV_MISSING"
	t.Setenv(missingVar, "")

	h := newBehavioralHarness(t)
	spawnMarker := filepath.Join(h.root, "spawn-marker")
	t.Setenv("SPAWN_MARKER", spawnMarker)
	script := "#!/bin/sh\n" +
		": > \"$SPAWN_MARKER\"\n" +
		"printf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"offline-probe\",\"model\":\"fake-offline\"}'\n" +
		"printf '%s\\n' '{\"type\":\"assistant\",\"message\":{\"role\":\"assistant\",\"content\":[{\"type\":\"text\",\"text\":\"Done.\"}]}}'\n" +
		"printf '%s\\n' '{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"session_id\":\"offline-probe\",\"result\":\"Done.\"}'\n"
	h.writeClaudeScript(t, script)
	scenario := "---\n" +
		"id: required-env-var-missing-no-spawn\n" +
		"seed: empty\n" +
		"requiredEnvVars:\n" +
		"  - " + missingVar + "\n" +
		"retrospective:\n" +
		"  promptStyle: briefing-future-agent\n" +
		"---\n" +
		"Do the thing.\n"
	scenarioPath := h.writeScenario(t, scenario)
	mock := platform.NewMock().WithServicesDirect([]platform.ServiceStack{{ID: "app-1", Name: "app", Status: "ACTIVE"}})
	runner := NewRunner(h.config(), nil, mock, "offline-project")

	result, err := runner.RunBehavioralScenario(context.Background(), scenarioPath, "suite")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantMsg := "mismatch: resource " + missingVar + " missing"
	if result.Preparation != wantMsg {
		t.Errorf("Preparation = %q, want %q", result.Preparation, wantMsg)
	}
	if result.Error != "" {
		t.Errorf("Error = %q, want empty (a preparation mismatch is not an execution error)", result.Error)
	}
	if _, statErr := os.Stat(spawnMarker); !os.IsNotExist(statErr) {
		t.Error("spawn marker exists — the agent was spawned despite the missing requiredEnvVars entry")
	}
	if result.Task == nil || result.Task.Result != CheckNotRun {
		t.Errorf("Task = %+v, want not-run", result.Task)
	}
}

// TestRun_RequiredEnvVarPresent_Proceeds pins the non-blocking side: a
// requiredEnvVars entry that IS present (non-empty) in the evaluator's
// environment does not block preparation — the agent is spawned normally.
func TestRun_RequiredEnvVarPresent_Proceeds(t *testing.T) { // non-parallel: process environment
	const presentVar = "ZCP_EVAL_TEST_REQUIRED_ENV_PRESENT"
	t.Setenv(presentVar, "some-value")

	h := newBehavioralHarness(t)
	spawnMarker := filepath.Join(h.root, "spawn-marker")
	t.Setenv("SPAWN_MARKER", spawnMarker)
	script := "#!/bin/sh\n" +
		": > \"$SPAWN_MARKER\"\n" +
		"printf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"offline-probe\",\"model\":\"fake-offline\"}'\n" +
		"printf '%s\\n' '{\"type\":\"assistant\",\"message\":{\"role\":\"assistant\",\"content\":[{\"type\":\"text\",\"text\":\"Done.\"}]}}'\n" +
		"printf '%s\\n' '{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"session_id\":\"offline-probe\",\"result\":\"Done.\"}'\n"
	h.writeClaudeScript(t, script)
	scenario := "---\n" +
		"id: required-env-var-present-proceeds\n" +
		"seed: empty\n" +
		"requiredEnvVars:\n" +
		"  - " + presentVar + "\n" +
		"retrospective:\n" +
		"  promptStyle: briefing-future-agent\n" +
		"---\n" +
		"Do the thing.\n"
	scenarioPath := h.writeScenario(t, scenario)
	mock := platform.NewMock().WithServicesDirect([]platform.ServiceStack{{ID: "app-1", Name: "app", Status: "ACTIVE"}})
	runner := NewRunner(h.config(), nil, mock, "offline-project")

	result, err := runner.RunBehavioralScenario(context.Background(), scenarioPath, "suite")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Preparation != "" {
		t.Errorf("Preparation = %q, want empty (requiredEnvVars entry is present)", result.Preparation)
	}
	if _, statErr := os.Stat(spawnMarker); statErr != nil {
		t.Errorf("spawn marker missing — the agent was never spawned despite the requiredEnvVars entry being present: %v", statErr)
	}
}

// TestRun_GitRepoReset_MissingPAT_BlocksPreparation_AgentNeverSpawned pins
// docs/spec-eval-farm.md §3.3 FM-67: a scenario declaring gitRepoReset resets
// that repo BEFORE seed — checked here without any network access reaching
// GitHub, by leaving ZCP_E2E_GITHUB_PAT unset so resetScenarioGitRepo fails
// deterministically. The failure is a preparation mismatch (not an execution
// error), routed through the same path as a seed.expect/requiredEnvVars
// mismatch: the agent is never spawned.
func TestRun_GitRepoReset_MissingPAT_BlocksPreparation_AgentNeverSpawned(t *testing.T) { // non-parallel: process environment
	t.Setenv(GitHubPATEnvVar, "")

	h := newBehavioralHarness(t)
	spawnMarker := filepath.Join(h.root, "spawn-marker")
	t.Setenv("SPAWN_MARKER", spawnMarker)
	script := "#!/bin/sh\n" +
		": > \"$SPAWN_MARKER\"\n" +
		"printf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"offline-probe\",\"model\":\"fake-offline\"}'\n" +
		"printf '%s\\n' '{\"type\":\"assistant\",\"message\":{\"role\":\"assistant\",\"content\":[{\"type\":\"text\",\"text\":\"Done.\"}]}}'\n" +
		"printf '%s\\n' '{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"session_id\":\"offline-probe\",\"result\":\"Done.\"}'\n"
	h.writeClaudeScript(t, script)
	const repoURL = "https://github.com/krls2020/eval2"
	scenario := "---\n" +
		"id: git-repo-reset-missing-pat-no-spawn\n" +
		"seed: empty\n" +
		"gitRepoReset: " + repoURL + "\n" +
		"retrospective:\n" +
		"  promptStyle: briefing-future-agent\n" +
		"---\n" +
		"Do the thing.\n"
	scenarioPath := h.writeScenario(t, scenario)
	mock := platform.NewMock().WithServicesDirect([]platform.ServiceStack{{ID: "app-1", Name: "app", Status: "ACTIVE"}})
	runner := NewRunner(h.config(), nil, mock, "offline-project")

	result, err := runner.RunBehavioralScenario(context.Background(), scenarioPath, "suite")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantMsg := "mismatch: git repo " + repoURL + " not clean: " + GitHubPATEnvVar + " not set in this process's environment"
	if result.Preparation != wantMsg {
		t.Errorf("Preparation = %q, want %q", result.Preparation, wantMsg)
	}
	if result.Error != "" {
		t.Errorf("Error = %q, want empty (a preparation mismatch is not an execution error)", result.Error)
	}
	if _, statErr := os.Stat(spawnMarker); !os.IsNotExist(statErr) {
		t.Error("spawn marker exists — the agent was spawned despite the git repo reset failure")
	}
	if result.Task == nil || result.Task.Result != CheckNotRun {
		t.Errorf("Task = %+v, want not-run", result.Task)
	}
}

// TestRun_GitRepoReset_RequiredMode_ResetsBeforeSeedAndAfterRun pins
// docs/spec-eval-farm.md §3.3 FM-67's second half: the shared repo is reset
// AGAIN once the run is over, in EVERY verification mode. Required mode keeps
// the run project (retention, §10.2) and so skips project cleanup — the repo
// reset must not ride on that defer, or the next cell to claim the repo
// inherits whatever the agent pushed (observed live on gf-cargo-4). The reset
// is injected so no network reaches GitHub.
func TestRun_GitRepoReset_RequiredMode_ResetsBeforeSeedAndAfterRun(t *testing.T) { // non-parallel: process environment
	t.Setenv(GitHubPATEnvVar, "offline-pat")

	h := newBehavioralHarness(t)
	script := "#!/bin/sh\n" +
		"printf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"offline-probe\",\"model\":\"fake-offline\"}'\n" +
		"printf '%s\\n' '{\"type\":\"assistant\",\"message\":{\"role\":\"assistant\",\"content\":[{\"type\":\"text\",\"text\":\"Done.\"}]}}'\n" +
		"printf '%s\\n' '{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"session_id\":\"offline-probe\",\"result\":\"Done.\"}'\n"
	h.writeClaudeScript(t, script)
	const repoURL = "https://github.com/krls2020/eval2"
	scenario := "---\n" +
		"id: git-repo-reset-required-mode\n" +
		"seed: empty\n" +
		"gitRepoReset: " + repoURL + "\n" +
		"retrospective:\n" +
		"  promptStyle: briefing-future-agent\n" +
		"verification:\n" +
		"  mode: required\n" +
		"  expectedServices:\n" +
		"    - hostname: app\n" +
		"      status: [ACTIVE]\n" +
		"---\n" +
		"Do the thing.\n"
	scenarioPath := h.writeScenario(t, scenario)
	mock := platform.NewMock().WithServicesDirect([]platform.ServiceStack{{ID: "app-1", Name: "app", Status: "ACTIVE"}})
	cfg := h.config()
	cfg.Capture = &capture.Connection{CaptureID: "owned", ProxyURL: "http://127.0.0.1:1", SessionDir: t.TempDir()}
	cfg.CaptureOwned = true
	h.requiredBinding(t, &cfg)
	runner := NewRunner(cfg, nil, &freshThenRealClient{Client: mock}, "offline-project")

	var resets []string
	runner.gitRepoReset = func(_ context.Context, sc *Scenario) error {
		resets = append(resets, sc.GitRepoReset)
		return nil
	}

	result, err := runner.RunBehavioralScenario(context.Background(), scenarioPath, "suite")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Preparation != "" || result.Error != "" {
		t.Fatalf("Preparation = %q, Error = %q, want a clean run", result.Preparation, result.Error)
	}
	if len(resets) != 2 || resets[0] != repoURL || resets[1] != repoURL {
		t.Fatalf("git repo resets = %v, want exactly two (before seed, after run) for %s", resets, repoURL)
	}
}
