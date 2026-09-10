package eval

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// SeedMode controls the starting state of the project before the agent runs.
type SeedMode string

const (
	ModeEmpty    SeedMode = "empty"
	ModeImported SeedMode = "imported"
	ModeDeployed SeedMode = "deployed"
	// ModeSettled imports the fixture and waits for every non-system service
	// to reach a terminal status (ACTIVE/FAILED/READY_TO_DEPLOY/RUNNING/STOPPED).
	// Differs from ModeDeployed by NOT aborting on FAILED — used by recovery
	// scenarios that intentionally seed a broken runtime to test the agent's
	// FAILED-state recovery surface.
	ModeSettled SeedMode = "settled"
	// ModeBuilding imports a buildFromGit fixture and returns WHILE the first
	// build is still RUNNING (it does NOT poll the build to completion the way
	// ModeImported/ModeDeployed do). The spawned agent therefore boots mid-build,
	// so its first zerops_discover lands on a service that is building yet reads
	// status=READY_TO_DEPLOY — the recipe-first-deploy race. Fixture MUST be
	// buildFromGit (a process-less import has no build to be mid-flight).
	ModeBuilding SeedMode = "building"
)

// Scenario is one runnable eval scenario parsed from a markdown file with
// YAML frontmatter.
type Scenario struct {
	ID            string
	Description   string
	Seed          SeedMode
	Fixture       string
	PreseedScript string
	Prompt        string
	SourcePath    string

	// Behavioral-mode fields (optional). When Retrospective is non-nil the
	// scenario is intended for RunBehavioralScenario (two-shot resume: run +
	// post-hoc retrospective) instead of plain parse-only use. Tags/Area/
	// NotableFriction are descriptive metadata for the local Claude session to
	// surface and reason over — they do not gate the run.
	Tags            []string
	Area            string
	NotableFriction []NotableFrictionEntry
	Retrospective   *RetrospectiveConfig
	// ExcludeFromAll marks a scenario as EXCLUDED from `behavioral all` /
	// flow-eval `all` selection — enforced, not descriptive (unlike Tags,
	// which never gate selection). Use for scenarios that must never run
	// inside a routine full-suite sweep (e.g. they consume a live one-shot
	// credential such as a platform token delegation). Direct execution by
	// scenario id (`behavioral run --id <id>`) stays allowed regardless.
	ExcludeFromAll bool

	// User-sim fields (optional). UserPersona is a free-form prose block
	// describing the simulated user the runner spawns to answer agent
	// clarifying questions. Empty → default persona ("developer who initiated
	// the task"). UserSim configures the simulator transport (model override,
	// per-stage iteration cap, wall-time budget). Both default-safe.
	UserPersona string
	UserSim     *UserSimConfig

	// Verification (optional, behavioral mode only) declares platform-side
	// assertions the runner decides from a direct platform read and writes
	// as result rows to verification.json. In observe mode (the default)
	// the rows are advisory and decided after the retrospective, before
	// cleanup; in required mode they are frozen at task end and gate the CLI
	// exit (docs/spec-testing-architecture.md §10). See VerificationConfig.
	Verification *VerificationConfig
}

// UserSimConfig configures the user-sim simulator transport. All fields
// optional; runner applies sensible defaults when nil/zero. Per
// plans/flow-eval-usersim-2026-05-04.md.
type UserSimConfig struct {
	// Model overrides the default Haiku 4.5 user-sim model. Use the canonical
	// `claude-<family>-<version>` id; falls back to default when empty.
	Model string `yaml:"model"`
	// MaxTurns caps user-sim invocations per stage. 0 → runner default (10).
	MaxTurns int `yaml:"maxTurns"`
	// StageTimeoutSeconds caps wall-time per stage including agent + user-sim
	// turns. 0 → runner default (900s = 15min). Whole-number seconds for
	// frontmatter readability; runner converts to time.Duration.
	StageTimeoutSeconds int `yaml:"stageTimeoutSeconds"`
}

// RetrospectiveConfig points at a retrospective prompt embedded in the binary
// under internal/eval/retrospective_prompts/<promptStyle>.md.
type RetrospectiveConfig struct {
	PromptStyle string `yaml:"promptStyle"`
}

// VerificationConfig declares the platform-side assertions of a behavioral
// scenario. Each block is optional. Mode decides what the rows mean:
// observe (default) writes them as advisory rows next to self-review.md and
// never gates; required freezes them at task end and carries the result to
// the CLI exit (docs/spec-testing-architecture.md §10.1).
type VerificationConfig struct {
	// Mode selects observe (default, warn-only) or required (deterministic
	// task result gate). See docs/spec-testing-architecture.md §10.1.
	Mode string `yaml:"mode,omitempty"`
	// ExpectedServices lists per-service assertions: hostname must exist,
	// status must match one of the allowed values, optional subdomain HTTP
	// probe, optional type-glob.
	ExpectedServices []ExpectedService `yaml:"expectedServices,omitempty"`
	// NoFailedProcesses asserts every project process is non-FAILED.
	// Catches "agent reports success but a background process died" gaps.
	NoFailedProcesses bool `yaml:"noFailedProcesses,omitempty"`
	// RetrospectiveMustNotMention is a list of phrases the retrospective
	// MUST NOT contain (substring match, case-insensitive). Use for
	// red-flag phrases the agent shouldn't admit to in success retros
	// (e.g. "smuggled", "hand-edited", "had to overwrite").
	RetrospectiveMustNotMention []string `yaml:"retrospectiveMustNotMention,omitempty"`
	// NodePostgresRecord declares the one application-oracle check
	// (docs/spec-testing-architecture.md §10.3): a Node runtime serving
	// POST/GET /records backed by a managed PostgreSQL database, plus an
	// unrelated service whose deployed artifact must not change. Counts as
	// an executable check for required mode.
	NodePostgresRecord *NodePostgresRecordConfig `yaml:"nodePostgresRecord,omitempty"`

	// Spec names the spec section this scenario proves (docs/spec-eval-farm.md
	// §4.1 FM-27) — a pointer only, never a restatement. The drift lint
	// (verification_vocab_test.go, FM-33) checks the named section exists.
	Spec string `yaml:"spec,omitempty"`
	// AllowFailed lists services whose FAILED process state is the
	// scenario's seeded starting point, not a violation (FM-28). Only
	// meaningful together with NoFailedProcesses: true.
	AllowFailed []string `yaml:"allowFailed,omitempty"`
	// Liveness configures the O2 liveness probe (FM-27 table): resolve the
	// named service's subdomain URL and expect a 2xx response whose body
	// contains Marker.
	Liveness *LivenessProbe `yaml:"liveness,omitempty"`
	// Unchanged is the standalone form of the "unrelated artifact unchanged"
	// row (FM-29): one hostname per entry, each graded independently of
	// whether NodePostgresRecord is also declared.
	Unchanged []string `yaml:"unchanged,omitempty"`
	// Never lists call-shape expressions (FM-30): a matching call anywhere
	// in the run's captured MCP stream fails the scenario. Grammar:
	// `<tool>` or `<tool>{k=v,k2=v2}` — see ParseCallShape.
	Never []string `yaml:"never,omitempty"`
	// AskWhen lists error codes (FM-31): advisory rows recording whether a
	// user-simulation turn occurred between the named error code appearing
	// and the next mutating call. Never gates the aggregated result.
	AskWhen []string `yaml:"askWhen,omitempty"`
	// Reach exists ONLY to be rejected at parse (FM-32): there is no
	// reach:/expected-route field in verification:. Never read after
	// validate() runs.
	Reach *yaml.Node `yaml:"reach,omitempty"`
	// LaunchShape configures the O6 launch-shape oracle (docs/spec-eval-farm.md
	// §4.4 O6): prod project exists, runtimes start without code, no
	// buildFromGit, first release is the first build, launch token never
	// in the transcript.
	LaunchShape *LaunchShapeConfig `yaml:"launchShape,omitempty"`
	// ArtifactPromotion configures the O7 artifact-promotion oracle
	// (docs/spec-eval-farm.md §4.4 O7): one entry per cross-deploy
	// promotion to verify.
	ArtifactPromotion []ArtifactPromotionEntry `yaml:"artifactPromotion,omitempty"`
	// NoFabricatedSecret enables the O8 oracle (docs/spec-eval-farm.md
	// §4.4 O8): an offline scan of the run's captured MCP tool-call
	// arguments for token-shaped values not among the scenario's declared
	// inputs.
	NoFabricatedSecret bool `yaml:"noFabricatedSecret,omitempty"`
}

// LaunchShapeConfig declares the O6 launch-shape oracle
// (docs/spec-eval-farm.md §4.4 O6).
type LaunchShapeConfig struct {
	// ProdProject is the prod project name (post-Scenario.Render
	// templating, e.g. "zcp-farm-{{runId}}-prod").
	ProdProject string `yaml:"prodProject"`
}

// ArtifactPromotionEntry declares one O7 artifact-promotion oracle entry
// (docs/spec-eval-farm.md §4.4 O7): a cross-deploy promotion from From's
// active build to To.
type ArtifactPromotionEntry struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

// LivenessProbe configures the O2 liveness check (FM-27 table): resolve
// Service's subdomain URL and expect a 2xx response whose body contains
// Marker. Row id: liveness/<service>/marker.
type LivenessProbe struct {
	Service string `yaml:"service"`
	Marker  string `yaml:"marker"`
}

// NodePostgresRecordConfig declares the hostnames the node-postgres
// application oracle resolves at task-end freeze (docs/spec-testing-architecture.md
// §10.3). Stage and Database are required when the block is present.
// Unrelated is optional — a topology with no third, unrelated host (e.g. a
// two-service api+db pair) omits it, and the verifier then emits three
// sub-rows (roundtrip, environment, db_row) instead of four; the standalone
// verification.unchanged field (FM-29) is the replacement "unrelated
// unchanged" check when it's needed. Environment is the literal the
// environment sub-row expects the GET body's `environment` field to equal;
// it defaults to "stage" when empty (NodePostgresVerifier.Verify), matching
// every scenario written before dev-only topologies needed a different
// literal.
type NodePostgresRecordConfig struct {
	Stage       string `yaml:"stage"`
	Database    string `yaml:"database"`
	Unrelated   string `yaml:"unrelated"`
	Environment string `yaml:"environment"`
}

// VerificationObserve and VerificationRequired are the two
// VerificationConfig.Mode values. "" (unset) means observe.
const (
	VerificationObserve  = "observe"
	VerificationRequired = "required"
)

// ExpectedService is one per-service assertion in a VerificationConfig.
type ExpectedService struct {
	Hostname       string          `yaml:"hostname"`
	Status         []string        `yaml:"status"`
	Type           string          `yaml:"type,omitempty"`
	SubdomainProbe *SubdomainProbe `yaml:"subdomainProbe,omitempty"`
}

// SubdomainProbe configures an HTTP probe against the service's subdomain
// URL. Path is appended to the URL; ExpectStatus accepts shapes "2xx",
// "3xx", "200", "200-299", or "any" (no status assertion).
type SubdomainProbe struct {
	Path         string `yaml:"path,omitempty"`
	ExpectStatus string `yaml:"expectStatus,omitempty"`
}

// VerificationFinding is one assertion outcome — pass when Severity is
// empty, otherwise "warn" or "fail".
type VerificationFinding struct {
	Severity string `json:"severity,omitempty"` // "", "warn", "fail"
	Check    string `json:"check"`
	Message  string `json:"message"`
}

// NotableFrictionEntry documents an expected pain-point for the local
// session to look for in the agent's retrospective. Informational only —
// not asserted by the runner.
type NotableFrictionEntry struct {
	ID              string   `yaml:"id"`
	Description     string   `yaml:"description"`
	SuspectedCauses []string `yaml:"suspectedCauses,omitempty"`
}

type scenarioFrontmatter struct {
	ID              string                 `yaml:"id"`
	Description     string                 `yaml:"description"`
	Seed            string                 `yaml:"seed"`
	Fixture         string                 `yaml:"fixture"`
	PreseedScript   string                 `yaml:"preseedScript"`
	Tags            []string               `yaml:"tags"`
	Area            string                 `yaml:"area"`
	NotableFriction []NotableFrictionEntry `yaml:"notableFriction"`
	Retrospective   *RetrospectiveConfig   `yaml:"retrospective"`
	UserPersona     string                 `yaml:"userPersona"`
	UserSim         *UserSimConfig         `yaml:"userSim"`
	Verification    *VerificationConfig    `yaml:"verification"`
	ExcludeFromAll  bool                   `yaml:"excludeFromAll"`
}

// ParseScenario reads a scenario markdown file and returns the parsed structure.
// The file must start with YAML frontmatter (between --- delimiters) followed by
// a markdown body used verbatim as the agent prompt. A prompt/userPersona may
// carry {{runId}}/{{projectId}} placeholders — ParseScenario only rejects an
// unrecognized {{...}} token; it does not substitute. Call Scenario.Render to
// substitute placeholder values before using Prompt/UserPersona for a run
// (internal/eval/scenario_template.go).
func ParseScenario(path string) (*Scenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read scenario %q: %w", path, err)
	}

	front, body, err := splitFrontmatter(string(data))
	if err != nil {
		return nil, fmt.Errorf("scenario %q: %w", path, err)
	}

	var fm scenarioFrontmatter
	if err := yaml.Unmarshal([]byte(front), &fm); err != nil {
		return nil, fmt.Errorf("scenario %q: parse frontmatter: %w", path, err)
	}
	if err := rejectUnknownVerificationFields(front); err != nil {
		return nil, fmt.Errorf("scenario %q: %w", path, err)
	}

	sc := &Scenario{
		ID:              fm.ID,
		Description:     fm.Description,
		Seed:            SeedMode(fm.Seed),
		Fixture:         fm.Fixture,
		PreseedScript:   fm.PreseedScript,
		Prompt:          strings.TrimSpace(body),
		SourcePath:      path,
		Tags:            fm.Tags,
		Area:            fm.Area,
		NotableFriction: fm.NotableFriction,
		Retrospective:   fm.Retrospective,
		UserPersona:     strings.TrimSpace(fm.UserPersona),
		UserSim:         fm.UserSim,
		Verification:    fm.Verification,
		ExcludeFromAll:  fm.ExcludeFromAll,
	}

	if err := rejectUnknownTemplateTokens(sc.Prompt); err != nil {
		return nil, fmt.Errorf("scenario %q: prompt: %w", path, err)
	}
	if err := rejectUnknownTemplateTokens(sc.UserPersona); err != nil {
		return nil, fmt.Errorf("scenario %q: userPersona: %w", path, err)
	}

	if err := sc.validate(); err != nil {
		return nil, fmt.Errorf("scenario %q: %w", path, err)
	}

	return sc, nil
}

// rejectUnknownVerificationFields strictly re-decodes just the
// `verification:` sub-node of the frontmatter against VerificationConfig's
// known field set (yaml.Decoder.KnownFields), independent of the lenient
// whole-frontmatter decode above. A misspelled verification field (e.g.
// `launcShape`) is rejected here rather than silently ignored.
func rejectUnknownVerificationFields(front string) error {
	// The lenient whole-frontmatter decode in ParseScenario already
	// surfaced any structural YAML error before this runs, so a second
	// structural failure here is unexpected — surfaced rather than
	// swallowed.
	var raw map[string]yaml.Node
	if err := yaml.Unmarshal([]byte(front), &raw); err != nil {
		return fmt.Errorf("re-parse frontmatter for verification field check: %w", err)
	}
	node, ok := raw["verification"]
	if !ok {
		return nil
	}
	data, err := yaml.Marshal(&node)
	if err != nil {
		return fmt.Errorf("re-marshal verification node: %w", err)
	}
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	var strict VerificationConfig
	if err := dec.Decode(&strict); err != nil {
		return fmt.Errorf("verification: %w", err)
	}
	return nil
}

func (s *Scenario) validate() error {
	if s.ID == "" {
		return fmt.Errorf("id required")
	}
	switch s.Seed {
	case ModeEmpty, ModeImported, ModeDeployed, ModeSettled, ModeBuilding:
	default:
		return fmt.Errorf("invalid seed mode %q (want empty|imported|deployed|settled|building)", s.Seed)
	}
	if s.Seed != ModeEmpty && s.Fixture == "" {
		return fmt.Errorf("fixture required for seed=%s", s.Seed)
	}
	if s.Prompt == "" {
		return fmt.Errorf("prompt body required")
	}
	if s.Retrospective != nil && s.Retrospective.PromptStyle == "" {
		return fmt.Errorf("retrospective.promptStyle required when retrospective is set")
	}
	if s.Verification != nil {
		switch s.Verification.Mode {
		case "", VerificationObserve, VerificationRequired:
		default:
			return fmt.Errorf("invalid verification.mode %q (want observe|required)", s.Verification.Mode)
		}
		if s.Verification.Mode == VerificationRequired {
			if len(s.Verification.ExpectedServices) == 0 && !s.Verification.NoFailedProcesses && s.Verification.NodePostgresRecord == nil {
				return fmt.Errorf("verification.mode required needs at least one executable check (expectedServices, noFailedProcesses, or nodePostgresRecord; retrospectiveMustNotMention is advisory and does not count)")
			}
		}
		if npr := s.Verification.NodePostgresRecord; npr != nil {
			if npr.Stage == "" || npr.Database == "" {
				return fmt.Errorf("verification.nodePostgresRecord requires stage and database hostnames (unrelated is optional)")
			}
		}
		if s.Verification.Reach != nil {
			return fmt.Errorf("verification.reach is not a supported field (FM-32: no reach:/expected-route field — the observed route is coverage data, never an assertion)")
		}
		for _, expr := range s.Verification.Never {
			if _, err := ParseCallShape(expr); err != nil {
				return fmt.Errorf("verification.never: %w", err)
			}
		}
		if s.Verification.LaunchShape != nil && s.Verification.LaunchShape.ProdProject == "" {
			return fmt.Errorf("verification.launchShape requires prodProject")
		}
		for _, ap := range s.Verification.ArtifactPromotion {
			if ap.From == "" || ap.To == "" {
				return fmt.Errorf("verification.artifactPromotion entries require from and to")
			}
		}
	}
	if s.UserSim != nil {
		if s.UserSim.MaxTurns < 0 {
			return fmt.Errorf("userSim.maxTurns must be >= 0 (got %d)", s.UserSim.MaxTurns)
		}
		if s.UserSim.StageTimeoutSeconds < 0 {
			return fmt.Errorf("userSim.stageTimeoutSeconds must be >= 0 (got %d)", s.UserSim.StageTimeoutSeconds)
		}
	}
	return nil
}

// IsBehavioral reports whether the scenario is intended for behavioral
// (two-shot resume) execution. Detected by presence of retrospective config.
func (s *Scenario) IsBehavioral() bool {
	return s.Retrospective != nil
}

// IsRequired reports whether the scenario's verification.mode is "required"
// (a deterministic task-result gate) rather than the default "observe".
func (s *Scenario) IsRequired() bool {
	return s.Verification != nil && s.Verification.Mode == VerificationRequired
}

// splitFrontmatter returns the YAML block between the first two --- lines and
// the body after. Errors if the file doesn't start with ---.
func splitFrontmatter(content string) (front, body string, err error) {
	trimmed := strings.TrimLeft(content, "\n\r\t ")
	if !strings.HasPrefix(trimmed, "---") {
		return "", "", fmt.Errorf("missing frontmatter: file must start with ---")
	}

	// Skip past the opening ---.
	rest := strings.TrimPrefix(trimmed, "---")
	rest = strings.TrimLeft(rest, "\n\r")

	f, after, ok := strings.Cut(rest, "\n---")
	if !ok {
		return "", "", fmt.Errorf("missing frontmatter: closing --- not found")
	}
	return f, strings.TrimLeft(after, "\n\r"), nil
}
