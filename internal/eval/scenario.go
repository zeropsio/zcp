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
