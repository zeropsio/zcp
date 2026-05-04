package eval

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stubSimRunner records every Reply call and returns canned responses in
// FIFO order. err is returned once if non-nil, then cleared.
type stubSimRunner struct {
	replies []string
	err     error
	calls   []string // captured prompts
	cursor  int
}

func (s *stubSimRunner) Reply(_ context.Context, prompt string) (string, error) {
	s.calls = append(s.calls, prompt)
	if s.err != nil {
		e := s.err
		s.err = nil
		return "", e
	}
	if s.cursor >= len(s.replies) {
		return "go ahead", nil
	}
	r := s.replies[s.cursor]
	s.cursor++
	return r, nil
}

// transcriptScripter writes successive transcript states into the same path.
// Each call to spawnAgentResume swaps in the next state — simulating the agent
// resuming and emitting more events. classify reads whatever is currently in
// the file. This lets us exercise the loop's classify-then-resume cadence
// without invoking real `claude`.
type transcriptScripter struct {
	path   string
	states []string // each entry is a full transcript file body to install on resume
	cursor int
	t      *testing.T
}

func (ts *transcriptScripter) install(state string) {
	if err := os.WriteFile(ts.path, []byte(state), 0o600); err != nil {
		ts.t.Fatalf("scripter install: %v", err)
	}
}

func (ts *transcriptScripter) resume(_ context.Context, _, _ /*sessionID, userMsg*/, transcriptFile string) error {
	if transcriptFile != ts.path {
		ts.t.Fatalf("scripter resume called with unexpected path %q (want %q)", transcriptFile, ts.path)
	}
	if ts.cursor >= len(ts.states) {
		return errors.New("scripter exhausted")
	}
	ts.install(ts.states[ts.cursor])
	ts.cursor++
	return nil
}

// loadFixture reads a canned testdata transcript verbatim.
func loadFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "usersim", name))
	if err != nil {
		t.Fatalf("load fixture %q: %v", name, err)
	}
	return string(b)
}

func TestRunUserSimLoop_TerminatesOnDone(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	transcript := filepath.Join(dir, "transcript.jsonl")

	ts := &transcriptScripter{path: transcript, t: t}
	ts.install(loadFixture(t, "done_via_text.jsonl"))

	sim := &stubSimRunner{}
	sc := &Scenario{Prompt: "Set up Go service", ID: "test"}
	res := &BehavioralResult{}

	if err := runUserSimLoop(context.Background(), sc, "session-1", transcript, sim, ts.resume, ClassifyTranscriptTail, res); err != nil {
		t.Fatalf("runUserSimLoop: %v", err)
	}
	if res.UserSim == nil {
		t.Fatal("UserSim: nil")
	}
	if res.UserSim.TerminatedBy != TerminatedAgentDone {
		t.Errorf("TerminatedBy: got %q, want %q", res.UserSim.TerminatedBy, TerminatedAgentDone)
	}
	if len(sim.calls) != 0 {
		t.Errorf("sim should not be called when agent is done; got %d call(s)", len(sim.calls))
	}
}

func TestRunUserSimLoop_TerminatesOnSatisfaction(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	transcript := filepath.Join(dir, "transcript.jsonl")

	ts := &transcriptScripter{path: transcript, t: t}
	ts.install(loadFixture(t, "waiting_question_mark.jsonl"))

	sim := &stubSimRunner{
		replies: []string{"Looks good, done — thanks for setting that up."},
	}
	sc := &Scenario{Prompt: "Set up Laravel app", ID: "test"}
	res := &BehavioralResult{}

	if err := runUserSimLoop(context.Background(), sc, "session-2", transcript, sim, ts.resume, ClassifyTranscriptTail, res); err != nil {
		t.Fatalf("runUserSimLoop: %v", err)
	}
	if res.UserSim.TerminatedBy != TerminatedSimSatisfied {
		t.Errorf("TerminatedBy: got %q, want %q", res.UserSim.TerminatedBy, TerminatedSimSatisfied)
	}
	if len(res.UserSim.Turns) != 1 {
		t.Errorf("turn count: got %d, want 1", len(res.UserSim.Turns))
	}
	// Resume should NOT have been called because the simulator terminated the
	// loop before any agent re-engagement was needed.
	if ts.cursor != 0 {
		t.Errorf("scripter cursor: got %d, want 0 (no resume after satisfied reply)", ts.cursor)
	}
}

func TestRunUserSimLoop_InjectsReplyAndContinues(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	transcript := filepath.Join(dir, "transcript.jsonl")

	ts := &transcriptScripter{
		path: transcript,
		t:    t,
		// After the user-sim reply, agent resumes and emits a done state.
		states: []string{loadFixture(t, "done_via_text.jsonl")},
	}
	ts.install(loadFixture(t, "waiting_question_mark.jsonl"))

	sim := &stubSimRunner{
		replies: []string{"MariaDB is fine, just mention the substitution in your summary."},
	}
	sc := &Scenario{Prompt: "Set up Laravel app", ID: "test"}
	res := &BehavioralResult{}

	if err := runUserSimLoop(context.Background(), sc, "session-3", transcript, sim, ts.resume, ClassifyTranscriptTail, res); err != nil {
		t.Fatalf("runUserSimLoop: %v", err)
	}
	if res.UserSim.TerminatedBy != TerminatedAgentDone {
		t.Errorf("TerminatedBy: got %q, want %q", res.UserSim.TerminatedBy, TerminatedAgentDone)
	}
	if len(res.UserSim.Turns) != 1 {
		t.Errorf("turn count: got %d, want 1", len(res.UserSim.Turns))
	}
	if !strings.Contains(res.UserSim.Turns[0].Reply, "MariaDB") {
		t.Errorf("captured reply: got %q", res.UserSim.Turns[0].Reply)
	}
	if ts.cursor != 1 {
		t.Errorf("scripter cursor: got %d, want 1 (one resume executed)", ts.cursor)
	}
}

func TestRunUserSimLoop_TerminatesOnMaxIterations(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	transcript := filepath.Join(dir, "transcript.jsonl")

	// Always-waiting transcript so the loop never sees a `done` verdict —
	// each resume installs a fully distinct waiting state so loop-repeat
	// detection does not fire (the tail-200 window must differ enough that
	// Levenshtein-ratio crosses 0.15).
	waitingStates := []string{
		buildWaitingTranscript("Iteration A: which compiler version do you want — Go 1.22, 1.23, or 1.24? They have different module behaviors and a few stdlib gotchas worth flagging."),
		buildWaitingTranscript("Iteration B: should I use the simple mode for a single container, or the standard mode that gives you both a dev and a staging slot? The trade-off is cost vs. promotion safety."),
		buildWaitingTranscript("Iteration C: do you want me to enable Postgres or MariaDB? Postgres has better JSON support; MariaDB is simpler if you don't need that."),
		buildWaitingTranscript("Iteration D: ready to deploy? I'll push the build and verify the URL afterwards. Confirm to proceed or pick another mode first."),
		buildWaitingTranscript("Iteration E: should I include a redis/valkey cache for session storage, or skip caching for now? Adding it later is a separate redeploy."),
	}
	ts := &transcriptScripter{path: transcript, t: t, states: waitingStates}
	ts.install(buildWaitingTranscript("Iteration INITIAL: I'm starting with discovery — which runtime version should I provision for the new service? Pick one before I submit the plan."))

	sim := &stubSimRunner{
		replies: []string{"reply-1", "reply-2", "reply-3", "reply-4", "reply-5"},
	}
	sc := &Scenario{
		Prompt: "Run forever",
		ID:     "test",
		UserSim: &UserSimConfig{
			MaxTurns: 3, // override default 10
		},
	}
	res := &BehavioralResult{}

	if err := runUserSimLoop(context.Background(), sc, "session-4", transcript, sim, ts.resume, ClassifyTranscriptTail, res); err != nil {
		t.Fatalf("runUserSimLoop: %v", err)
	}
	if res.UserSim.TerminatedBy != TerminatedMaxIterations {
		t.Errorf("TerminatedBy: got %q, want %q", res.UserSim.TerminatedBy, TerminatedMaxIterations)
	}
	if len(res.UserSim.Turns) != 3 {
		t.Errorf("turn count: got %d, want 3 (cap)", len(res.UserSim.Turns))
	}
}

func TestRunUserSimLoop_TerminatesOnStuckLoop(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	transcript := filepath.Join(dir, "transcript.jsonl")

	// Same waiting state served twice — second classify sees identical
	// agent text, loop-repeat detection fires. Build a transcript long
	// enough that the loop check runs in ratio mode (tail >= minLen).
	stuckText := "Should I substitute MariaDB for MySQL given that mysql isn't in the catalog? Please confirm before I proceed with the plan submission."
	stuckState := buildWaitingTranscript(stuckText)
	ts := &transcriptScripter{
		path:   transcript,
		t:      t,
		states: []string{stuckState, stuckState},
	}
	ts.install(stuckState)

	sim := &stubSimRunner{
		replies: []string{"yes substitute", "yes substitute"},
	}
	sc := &Scenario{Prompt: "Set up app", ID: "test"}
	res := &BehavioralResult{}

	if err := runUserSimLoop(context.Background(), sc, "session-5", transcript, sim, ts.resume, ClassifyTranscriptTail, res); err != nil {
		t.Fatalf("runUserSimLoop: %v", err)
	}
	if res.UserSim.TerminatedBy != TerminatedStuckLoop {
		t.Errorf("TerminatedBy: got %q, want %q", res.UserSim.TerminatedBy, TerminatedStuckLoop)
	}
	if !strings.Contains(res.UserSim.StuckOnQuestion, "substitute MariaDB") {
		t.Errorf("StuckOnQuestion: missing expected substring, got %q", res.UserSim.StuckOnQuestion)
	}
}

func TestRunUserSimLoop_TerminatesOnAgentError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	transcript := filepath.Join(dir, "transcript.jsonl")

	ts := &transcriptScripter{path: transcript, t: t}
	ts.install(loadFixture(t, "error_is_error.jsonl"))

	sim := &stubSimRunner{}
	sc := &Scenario{Prompt: "test", ID: "test"}
	res := &BehavioralResult{}

	if err := runUserSimLoop(context.Background(), sc, "session-err", transcript, sim, ts.resume, ClassifyTranscriptTail, res); err != nil {
		t.Fatalf("runUserSimLoop: %v", err)
	}
	if res.UserSim.TerminatedBy != TerminatedAgentError {
		t.Errorf("TerminatedBy: got %q, want %q", res.UserSim.TerminatedBy, TerminatedAgentError)
	}
}

func TestRunUserSimLoop_TerminatesOnAgentMaxTurns(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	transcript := filepath.Join(dir, "transcript.jsonl")

	ts := &transcriptScripter{path: transcript, t: t}
	ts.install(loadFixture(t, "error_max_turns.jsonl"))

	sim := &stubSimRunner{}
	sc := &Scenario{Prompt: "test", ID: "test"}
	res := &BehavioralResult{}

	if err := runUserSimLoop(context.Background(), sc, "session-mt", transcript, sim, ts.resume, ClassifyTranscriptTail, res); err != nil {
		t.Fatalf("runUserSimLoop: %v", err)
	}
	if res.UserSim.TerminatedBy != TerminatedAgentMaxTurns {
		t.Errorf("TerminatedBy: got %q, want %q", res.UserSim.TerminatedBy, TerminatedAgentMaxTurns)
	}
}

func TestRunUserSimLoop_PersonaUsedRecorded(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	transcript := filepath.Join(dir, "transcript.jsonl")

	ts := &transcriptScripter{path: transcript, t: t}
	ts.install(loadFixture(t, "done_via_text.jsonl"))

	cases := []struct {
		name        string
		persona     string
		wantPersona string
	}{
		{"default", "", "default"},
		{"override", "You are a strict reviewer.", "scenario-override"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sim := &stubSimRunner{}
			sc := &Scenario{Prompt: "do x", ID: "test", UserPersona: tc.persona}
			res := &BehavioralResult{}
			if err := runUserSimLoop(context.Background(), sc, "s", transcript, sim, ts.resume, ClassifyTranscriptTail, res); err != nil {
				t.Fatalf("runUserSimLoop: %v", err)
			}
			if res.UserSim.PersonaUsed != tc.wantPersona {
				t.Errorf("PersonaUsed: got %q, want %q", res.UserSim.PersonaUsed, tc.wantPersona)
			}
		})
	}
}

func TestBuildUserSimPrompt_StructureAndContent(t *testing.T) {
	t.Parallel()
	persona := "You are a strict reviewer."
	originalPrompt := "Set up a Laravel app, dev + staging."
	lastAgent := "Should I use MariaDB instead of MySQL?"
	turns := []UserSimTurn{
		{Iteration: 1, AgentTextExcerpt: "I'll start the bootstrap.", Reply: "go ahead"},
	}

	got := BuildUserSimPrompt(persona, originalPrompt, lastAgent, turns)

	wantSubstrings := []string{
		"Laravel app, dev + staging",
		"strict reviewer",
		"Should I use MariaDB",
		"go ahead",
		"Reply now",
	}
	for _, s := range wantSubstrings {
		if !strings.Contains(got, s) {
			t.Errorf("prompt missing %q", s)
		}
	}
}

func TestBuildUserSimPrompt_DefaultPersonaWhenEmpty(t *testing.T) {
	t.Parallel()
	got := BuildUserSimPrompt("", "do thing", "what now?", nil)
	if !strings.Contains(got, "Compatible substitutions") {
		t.Errorf("default persona expected to mention 'Compatible substitutions', got:\n%s", got)
	}
}

func TestIsSatisfied_PicksUpMarkers(t *testing.T) {
	t.Parallel()
	cases := []struct {
		reply string
		want  bool
	}{
		{"Looks good, done — thanks!", true},
		{"that's all I needed for now", true},
		{"Thanks, looks good.", true},
		{"All set, ready to ship.", true},
		{"Perfect, done!", true},
		{"Let me think about it", false},
		{"Use MariaDB", false},
		{"Sure, go ahead.", false},
	}
	for _, tc := range cases {
		t.Run(tc.reply, func(t *testing.T) {
			t.Parallel()
			if got := isSatisfied(tc.reply); got != tc.want {
				t.Errorf("isSatisfied(%q) = %v, want %v", tc.reply, got, tc.want)
			}
		})
	}
}

func TestIsLoopRepeat_DetectsNearDuplicates(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{
			"identical_long",
			"Should I use MariaDB instead of MySQL given that mysql isn't in the catalog? Please confirm.",
			"Should I use MariaDB instead of MySQL given that mysql isn't in the catalog? Please confirm.",
			true,
		},
		{
			"near_long",
			"Should I use MariaDB instead of MySQL given that mysql isn't in the catalog? Please confirm.",
			"Should I use MariaDB instead of MySQL given that mysql isn't in the catalog? Please confirm!",
			true,
		},
		{
			"different_long",
			"Should I use MariaDB instead of MySQL given that mysql isn't in the catalog? Please confirm.",
			"Want me to deploy now? Build looks healthy and verify will run automatically afterwards.",
			false,
		},
		{"identical_short", "Yes?", "Yes?", true},
		{"different_short", "Yes?", "Run.", false},
		{"empty_prev", "", "Some new question?", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isLoopRepeat(tc.a, tc.b); got != tc.want {
				t.Errorf("isLoopRepeat(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// buildWaitingTranscript constructs a minimal-but-valid stream-json transcript
// where the agent's final assistant message + result.result are agentText.
// Used to author distinct "agent is waiting" states for max-iterations and
// stuck-loop tests without copying boilerplate. The init event session_id
// is fixed; the test isn't sensitive to its value.
func buildWaitingTranscript(agentText string) string {
	escaped := jsonEscape(agentText)
	return `{"type":"system","subtype":"init","cwd":"/var/www","session_id":"test-built","model":"claude-opus-4-6"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"` + escaped + `"}],"stop_reason":null},"session_id":"test-built"}
{"type":"result","subtype":"success","is_error":false,"num_turns":1,"result":"` + escaped + `","stop_reason":"end_turn","session_id":"test-built"}
`
}

// jsonEscape escapes a string for inclusion in a JSON string literal —
// quotes, backslashes, and control characters. Test-only helper.
func jsonEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// TestRunUserSimLoop_HonorsContextCancellation ensures the loop returns
// promptly when ctx is cancelled mid-iteration, without hanging in a
// classify or sim call.
func TestRunUserSimLoop_HonorsContextCancellation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	transcript := filepath.Join(dir, "transcript.jsonl")

	ts := &transcriptScripter{path: transcript, t: t}
	ts.install(loadFixture(t, "waiting_question_mark.jsonl"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled

	sim := &stubSimRunner{replies: []string{"reply"}}
	sc := &Scenario{Prompt: "x", ID: "t"}
	res := &BehavioralResult{}

	start := time.Now()
	err := runUserSimLoop(ctx, sc, "s", transcript, sim, ts.resume, ClassifyTranscriptTail, res)
	if time.Since(start) > 2*time.Second {
		t.Errorf("loop should return quickly on cancelled ctx, took %s", time.Since(start))
	}
	// On cancelled ctx we expect either a clean ctx error or terminated reason set;
	// the contract: don't deadlock. Either outcome acceptable.
	_ = err
	_ = res
}
