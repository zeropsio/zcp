package workflow

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/topology"
)

// fixtureEnvelope builds a fully-populated envelope for wire tests:
// services, work session and bootstrap all set. `Generated` is a
// wall-clock UTC instant on purpose — `time.Now()` carries a monotonic
// reading that JSON cannot represent, so only a UTC fixture survives a
// round trip under `==`.
func fixtureEnvelope() StateEnvelope {
	at := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	return StateEnvelope{
		Phase:        PhaseDevelopActive,
		Environment:  EnvContainer,
		IdleScenario: "",
		SelfService:  &SelfService{Hostname: "zcp"},
		Project:      ProjectSummary{ID: "proj-1", Name: "z3-eval"},
		Services: []ServiceSnapshot{
			{
				Hostname:        "apidev",
				TypeVersion:     "nodejs@22",
				RuntimeClass:    topology.RuntimeDynamic,
				Status:          "ACTIVE",
				Bootstrapped:    true,
				Deployed:        true,
				Mode:            topology.ModeStandard,
				CloseDeployMode: topology.CloseModeAuto,
				GitPushState:    topology.GitPushConfigured,
				RemoteURL:       "https://github.com/acme/api",
				StageHostname:   "apistage",
				SetupName:       "api",
				StageSetupName:  "apistage",
			},
			{
				Hostname:     "db",
				TypeVersion:  "postgresql@16",
				RuntimeClass: topology.RuntimeManaged,
				Status:       "ACTIVE",
				Bootstrapped: true,
			},
		},
		WorkSession: &WorkSessionSummary{
			Intent:    "add health endpoint",
			Services:  []string{"apidev"},
			Roles:     map[string]string{"apidev": "required"},
			CreatedAt: at,
			Deploys: map[string][]AttemptInfo{
				"apidev": {{At: at, Success: true, Iteration: 1, Setup: "api"}},
			},
			Verifies: map[string][]AttemptInfo{
				"apidev": {{At: at, Success: true, Iteration: 1, Summary: "healthy"}},
			},
		},
		Bootstrap: &BootstrapSessionSummary{
			Route:  BootstrapRouteClassic,
			Step:   "provision",
			Intent: "node api with postgres",
		},
		Generated: at,
	}
}

// TestAppendEnvelope_RoundTrip is the core wire contract: what
// AppendEnvelope writes, ExtractEnvelope reads back verbatim.
func TestAppendEnvelope_RoundTrip(t *testing.T) {
	t.Parallel()

	want := fixtureEnvelope()
	text := AppendEnvelope("## Status\n\nPhase: develop-active\n", want)

	got, ok := ExtractEnvelope(text)
	if !ok {
		t.Fatalf("ExtractEnvelope: no envelope found in\n%s", text)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip lost data\n got: %+v\nwant: %+v", got, want)
	}
	if !got.Generated.Equal(want.Generated) {
		t.Errorf("Generated: got %v want %v", got.Generated, want.Generated)
	}
}

// TestAppendEnvelope_BlockShape pins the on-the-wire shape the mate
// reducer parses: the markdown is preserved, exactly one fenced block
// with the `json zcp-envelope` info string follows it, its body is
// compact single-line JSON, and nothing but whitespace follows the
// closing fence.
func TestAppendEnvelope_BlockShape(t *testing.T) {
	t.Parallel()

	body := "## Status\n\nPhase: develop-active\n"
	text := AppendEnvelope(body, fixtureEnvelope())

	if !strings.HasPrefix(text, body) {
		t.Errorf("markdown prefix not preserved:\n%s", text)
	}
	if n := strings.Count(text, "```"+EnvelopeFence); n != 1 {
		t.Errorf("want exactly 1 opening fence, got %d:\n%s", n, text)
	}

	open := strings.Index(text, "```"+EnvelopeFence)
	if open > 0 && text[open-1] != '\n' {
		t.Errorf("opening fence is not at a line start:\n%q", text[max(0, open-20):open+30])
	}

	rest := text[open+len("```"+EnvelopeFence):]
	rest = strings.TrimPrefix(rest, "\n")
	jsonLine, after, found := strings.Cut(rest, "\n")
	if !found {
		t.Fatalf("no closing fence after body:\n%s", text)
	}
	if strings.Contains(jsonLine, "\n") {
		t.Errorf("envelope JSON must be single-line, got:\n%s", jsonLine)
	}
	var probe StateEnvelope
	if err := json.Unmarshal([]byte(jsonLine), &probe); err != nil {
		t.Errorf("envelope body is not valid JSON: %v\n%s", err, jsonLine)
	}
	// json.Marshal is compact — no indentation whitespace after a colon.
	if strings.Contains(jsonLine, ": ") {
		t.Errorf("envelope JSON is not compact:\n%s", jsonLine)
	}

	if strings.TrimRight(after, "\n") != "```" {
		t.Errorf("closing fence must be the last content, got %q", after)
	}
}

// TestExtractEnvelope_LastBlockWins pins the reducer rule: a stream may
// carry several envelope blocks (a concatenated transcript, guidance
// that quotes one); the newest state is the last block.
func TestExtractEnvelope_LastBlockWins(t *testing.T) {
	t.Parallel()

	older := fixtureEnvelope()
	older.Phase = PhaseIdle
	newer := fixtureEnvelope()
	newer.Phase = PhaseDevelopActive

	text := AppendEnvelope("first result", older)
	text += "\n\nsecond result\n"
	text = AppendEnvelope(text, newer)

	got, ok := ExtractEnvelope(text)
	if !ok {
		t.Fatalf("ExtractEnvelope: not found in\n%s", text)
	}
	if got.Phase != PhaseDevelopActive {
		t.Errorf("last block must win: got phase %q want %q", got.Phase, PhaseDevelopActive)
	}
}

// TestAppendEnvelope_Idempotent — appending to a text that already ends
// with an envelope block REPLACES that block, so a producer chain never
// emits two trailing envelopes. Only a trailing block is replaced;
// blocks embedded earlier in the text are content and stay untouched.
func TestAppendEnvelope_Idempotent(t *testing.T) {
	t.Parallel()

	env := fixtureEnvelope()
	once := AppendEnvelope("## Status\n", env)
	twice := AppendEnvelope(once, env)

	if once != twice {
		t.Errorf("AppendEnvelope not idempotent:\n once: %q\ntwice: %q", once, twice)
	}

	fresh := fixtureEnvelope()
	fresh.Phase = PhaseIdle
	replaced := AppendEnvelope(once, fresh)
	if n := strings.Count(replaced, "```"+EnvelopeFence); n != 1 {
		t.Errorf("want exactly 1 fence after replace, got %d:\n%s", n, replaced)
	}
	got, ok := ExtractEnvelope(replaced)
	if !ok || got.Phase != PhaseIdle {
		t.Errorf("replace did not take: ok=%v phase=%q", ok, got.Phase)
	}
}

// TestExtractEnvelope_Absent covers every "no state here" input the
// reducer must survive without a panic or a bogus envelope.
func TestExtractEnvelope_Absent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
	}{
		{"empty", ""},
		{"plain markdown", "## Status\n\nPhase: idle\n"},
		{"other fenced block", "```json\n{\"phase\":\"idle\"}\n```\n"},
		{"unterminated block", "text\n\n```" + EnvelopeFence + "\n{\"phase\":\"idle\"}\n"},
		{"malformed json", "text\n\n```" + EnvelopeFence + "\nnot json\n```\n"},
		{"fence mentioned mid-line", "the block is ```" + EnvelopeFence + " shaped\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, ok := ExtractEnvelope(tt.text); ok {
				t.Errorf("ExtractEnvelope(%q) = ok, want not-found", tt.text)
			}
		})
	}
}

// TestAppendEnvelope_Deterministic pins the compaction-safety
// invariant on the wire: the same envelope always produces the same
// bytes, so a reducer can dedupe by content.
func TestAppendEnvelope_Deterministic(t *testing.T) {
	t.Parallel()

	a := AppendEnvelope("## Status\n", fixtureEnvelope())
	for range 10 {
		if b := AppendEnvelope("## Status\n", fixtureEnvelope()); b != a {
			t.Fatalf("non-deterministic serialization:\n a: %s\n b: %s", a, b)
		}
	}
}

// TestAppendEnvelope_BlockSizeBudget documents the wire cost of the
// block and pins it well under the headroom RenderStatus leaves below
// the 32 KB MCP transport cap (ComposeBodyBudget is 24 KB).
func TestAppendEnvelope_BlockSizeBudget(t *testing.T) {
	t.Parallel()

	idle := len(AppendEnvelope("", StateEnvelope{Phase: PhaseIdle}))

	// A standard bootstrapped project: two dev/stage runtime pairs plus a
	// work session — the shape a lifecycle strip renders in practice.
	four := fixtureEnvelope()
	for _, host := range []string{"webdev", "workerdev"} {
		svc := four.Services[0]
		svc.Hostname = host
		svc.StageHostname = strings.TrimSuffix(host, "dev") + "stage"
		svc.SetupName = strings.TrimSuffix(host, "dev")
		svc.StageSetupName = svc.StageHostname
		four.Services = append(four.Services, svc)
	}
	four.WorkSession.Services = []string{"apidev", "webdev", "workerdev"}
	full := len(AppendEnvelope("", four))
	t.Logf("envelope block bytes: idle=%d fixture(4 services + work session)=%d", idle, full)

	const headroom = 32*1024 - ComposeBodyBudget // 8 KB of scaffold room
	if full >= headroom {
		t.Errorf("fixture envelope block %d B exceeds the %d B scaffold headroom", full, headroom)
	}
}

// jsonCarrierResponse mimics a tool response whose result text is one JSON
// document: the envelope rides as a top-level `envelope` key beside the
// tool's own fields, because appending a markdown fence would stop the
// document parsing as JSON.
type jsonCarrierResponse struct {
	Status   string         `json:"status"`
	Hostname string         `json:"hostname"`
	Envelope *StateEnvelope `json:"envelope,omitempty"`
}

// TestExtractEnvelope_JSONDocumentCarrier pins the second carrier: when
// the whole result text is a JSON object, the reducer reads `.envelope`
// rather than looking for a fenced block.
func TestExtractEnvelope_JSONDocumentCarrier(t *testing.T) {
	t.Parallel()

	want := fixtureEnvelope()
	payload, err := json.Marshal(jsonCarrierResponse{
		Status: "DEPLOYED", Hostname: "apidev", Envelope: &want,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got, ok := ExtractEnvelope(string(payload))
	if !ok {
		t.Fatalf("ExtractEnvelope: no envelope in JSON document\n%s", payload)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip lost data\n got: %+v\nwant: %+v", got, want)
	}
}

// TestExtractEnvelope_JSONDocumentAbsent — a JSON result whose envelope
// computation failed carries no `envelope` key at all (omitempty), and is
// byte-identical to the pre-envelope output. The reducer must read that as
// "no state here", not as an empty envelope.
func TestExtractEnvelope_JSONDocumentAbsent(t *testing.T) {
	t.Parallel()

	payload, err := json.Marshal(jsonCarrierResponse{Status: "DEPLOYED", Hostname: "apidev"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(payload), "envelope") {
		t.Errorf("a nil envelope must be omitted entirely, got %s", payload)
	}
	if _, ok := ExtractEnvelope(string(payload)); ok {
		t.Errorf("ExtractEnvelope(%s) = ok, want not-found", payload)
	}
}

// TestExtractEnvelope_CarrierPrecedence — the two carriers never collide,
// but a reducer fed a JSON document must not be confused by a fence that
// happens to appear inside one of its string values.
func TestExtractEnvelope_CarrierPrecedence(t *testing.T) {
	t.Parallel()

	decoy := fixtureEnvelope()
	decoy.Phase = PhaseIdle
	fenced := AppendEnvelope("log tail", decoy)

	carried := fixtureEnvelope()
	carried.Phase = PhaseDevelopActive
	payload, err := json.Marshal(jsonCarrierResponse{
		Status: fenced, Hostname: "apidev", Envelope: &carried,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got, ok := ExtractEnvelope(string(payload))
	if !ok {
		t.Fatalf("ExtractEnvelope: not found")
	}
	if got.Phase != PhaseDevelopActive {
		t.Errorf("JSON carrier must win over a fence inside a value: got %q", got.Phase)
	}
}
