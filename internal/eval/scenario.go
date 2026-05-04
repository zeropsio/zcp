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
)

// Scenario is one runnable eval scenario parsed from a markdown file with
// YAML frontmatter.
type Scenario struct {
	ID            string
	Description   string
	Seed          SeedMode
	Fixture       string
	PreseedScript string
	Expect        Expectation
	FollowUp      []string
	Prompt        string
	SourcePath    string

	// Behavioral-mode fields (optional). When Retrospective is non-nil the
	// scenario is intended for RunBehavioralScenario instead of RunScenario:
	// the runner uses two-shot resume (run + post-hoc retrospective) instead
	// of one-shot grading. Tags/Area/NotableFriction are descriptive metadata
	// for the local Claude session to surface and reason over — they do not
	// gate the run.
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

// NotableFrictionEntry documents an expected pain-point for the local
// session to look for in the agent's retrospective. Informational only —
// not asserted by the runner.
type NotableFrictionEntry struct {
	ID              string   `yaml:"id"`
	Description     string   `yaml:"description"`
	SuspectedCauses []string `yaml:"suspectedCauses,omitempty"`
}

// Expectation captures assertions the grader runs against the agent's output.
type Expectation struct {
	MustCallTools     []string `yaml:"mustCallTools"`
	WorkflowCallsMin  int      `yaml:"workflowCallsMin"`
	MustEnterWorkflow []string `yaml:"mustEnterWorkflow"`
	FinalURLStatus    int      `yaml:"finalUrlStatus"`
	FinalURLHostname  string   `yaml:"finalUrlHostname"`
	ForbiddenPatterns []string `yaml:"forbiddenPatterns"`
	RequiredPatterns  []string `yaml:"requiredPatterns"`
	RequireAssessment bool     `yaml:"requireAssessment"`
	AtomsHit          []string `yaml:"atomsHit"`
	AutoClose         bool     `yaml:"autoClose"`
}

type scenarioFrontmatter struct {
	ID              string                 `yaml:"id"`
	Description     string                 `yaml:"description"`
	Seed            string                 `yaml:"seed"`
	Fixture         string                 `yaml:"fixture"`
	PreseedScript   string                 `yaml:"preseedScript"`
	Expect          Expectation            `yaml:"expect"`
	FollowUp        []string               `yaml:"followUp"`
	Tags            []string               `yaml:"tags"`
	Area            string                 `yaml:"area"`
	NotableFriction []NotableFrictionEntry `yaml:"notableFriction"`
	Retrospective   *RetrospectiveConfig   `yaml:"retrospective"`
	UserPersona     string                 `yaml:"userPersona"`
	UserSim         *UserSimConfig         `yaml:"userSim"`
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
		Expect:          fm.Expect,
		FollowUp:        fm.FollowUp,
		Prompt:          strings.TrimSpace(body),
		SourcePath:      path,
		Tags:            fm.Tags,
		Area:            fm.Area,
		NotableFriction: fm.NotableFriction,
		Retrospective:   fm.Retrospective,
		UserPersona:     strings.TrimSpace(fm.UserPersona),
		UserSim:         fm.UserSim,
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
	case ModeEmpty, ModeImported, ModeDeployed:
	default:
		return fmt.Errorf("invalid seed mode %q (want empty|imported|deployed)", s.Seed)
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
