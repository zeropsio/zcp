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

	// User-sim fields (optional). UserPersona is a free-form prose block
	// describing the simulated user the runner spawns to answer agent
	// clarifying questions. Empty → default persona ("developer who initiated
	// the task"). UserSim configures the simulator transport (model override,
	// per-stage iteration cap, wall-time budget). Both default-safe.
	UserPersona string
	UserSim     *UserSimConfig

	// Verification (optional, behavioral mode only) asserts platform-side
	// outcomes BEFORE cleanup wipes services. Captures the gap exposed by
	// Tier-1 kanban retros: agent self-reports "Kanban is live" but the
	// cleanup hook deletes services before manual verify can confirm. With
	// Verification set, the runner queries the live platform between
	// retrospective + cleanup and writes findings to verification.json
	// alongside self-review.md. See VerificationConfig for the schema.
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

// VerificationConfig declares post-run platform-side assertions the runner
// evaluates between retrospective + cleanup. Each block is optional; an
// empty VerificationConfig produces no findings (no-op).
//
// Captured findings land in verification.json alongside self-review.md.
// Sprint 3 wires this to behavioral runs; failures are warn-only at this
// stage (the suite verdict still propagates from the retrospective). A
// later sprint may promote findings to gate the exit code.
type VerificationConfig struct {
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
}

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
}

// ParseScenario reads a scenario markdown file and returns the parsed structure.
// The file must start with YAML frontmatter (between --- delimiters) followed by
// a markdown body used verbatim as the agent prompt.
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
	}

	if err := sc.validate(); err != nil {
		return nil, fmt.Errorf("scenario %q: %w", path, err)
	}

	return sc, nil
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
