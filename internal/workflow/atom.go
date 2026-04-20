package workflow

import (
	"bufio"
	"fmt"
	"strings"
)

// KnowledgeAtom is one piece of runtime-dependent guidance. Atoms live as
// .md files under internal/content/atoms/; their frontmatter declares the
// axis vector, their body is the rendered guidance.
type KnowledgeAtom struct {
	ID       string
	Axes     AxisVector
	Priority int
	Title    string
	Body     string
}

// AxisVector is the subset of envelope dimensions an atom applies to. Empty
// axes mean "any value for this dimension" (Phases is the exception — it
// MUST be non-empty).
type AxisVector struct {
	Phases        []Phase
	Modes         []Mode
	Environments  []Environment
	Strategies    []DeployStrategy
	Runtimes      []RuntimeClass
	Routes        []BootstrapRoute
	Steps         []string
	IdleScenarios []IdleScenario
	DeployStates  []DeployState
}

// ParseAtom parses a `.md` file body containing YAML frontmatter and a
// markdown body. It returns a KnowledgeAtom or an error if required fields
// are missing.
func ParseAtom(content string) (KnowledgeAtom, error) {
	front, body, err := splitFrontmatter(content)
	if err != nil {
		return KnowledgeAtom{}, err
	}
	fields, err := parseFrontmatter(front)
	if err != nil {
		return KnowledgeAtom{}, err
	}

	atom := KnowledgeAtom{
		ID:       fields["id"],
		Title:    fields["title"],
		Body:     strings.TrimSpace(body),
		Priority: atomPriority(fields["priority"]),
		Axes: AxisVector{
			Phases:        parsePhases(fields["phases"]),
			Modes:         parseModes(fields["modes"]),
			Environments:  parseEnvironments(fields["environments"]),
			Strategies:    parseStrategies(fields["strategies"]),
			Runtimes:      parseRuntimes(fields["runtimes"]),
			Routes:        parseRoutes(fields["routes"]),
			Steps:         parseYAMLList(fields["steps"]),
			IdleScenarios: parseIdleScenarios(fields["idleScenarios"]),
			DeployStates:  parseDeployStates(fields["deployStates"]),
		},
	}
	if atom.ID == "" {
		return atom, fmt.Errorf("atom missing required field: id")
	}
	if len(atom.Axes.Phases) == 0 {
		return atom, fmt.Errorf("atom %q missing required field: phases", atom.ID)
	}
	return atom, nil
}

// splitFrontmatter splits `---\n...\n---\n...` into frontmatter and body.
// Returns an error if the delimiters are malformed.
func splitFrontmatter(content string) (string, string, error) {
	if !strings.HasPrefix(content, "---\n") {
		return "", "", fmt.Errorf("atom missing opening frontmatter delimiter")
	}
	rest := content[4:]
	front, body, ok := strings.Cut(rest, "\n---\n")
	if !ok {
		return "", "", fmt.Errorf("atom missing closing frontmatter delimiter")
	}
	return front, body, nil
}

// parseFrontmatter reads `key: value` lines into a map. Arrays use the
// inline `[a, b, c]` form — list blocks are not supported because they
// add parser complexity without buying expressiveness for atom metadata.
func parseFrontmatter(front string) (map[string]string, error) {
	fields := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(front))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		colon := strings.Index(line, ":")
		if colon <= 0 {
			return nil, fmt.Errorf("atom frontmatter malformed line: %q", line)
		}
		key := strings.TrimSpace(line[:colon])
		value := strings.TrimSpace(line[colon+1:])
		value = strings.Trim(value, "\"")
		fields[key] = value
	}
	return fields, scanner.Err()
}

func atomPriority(raw string) int {
	if raw == "" {
		return 5
	}
	// Deliberate: invalid priority silently maps to default (5). Keeps the
	// parser tolerant while still deterministic.
	var n int
	_, err := fmt.Sscanf(raw, "%d", &n)
	if err != nil || n < 1 || n > 9 {
		return 5
	}
	return n
}

// parseYAMLList parses an inline `[a, b, c]` form into a slice of strings.
// Returns nil for an empty/missing value.
func parseYAMLList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if !strings.HasPrefix(raw, "[") || !strings.HasSuffix(raw, "]") {
		return nil
	}
	raw = raw[1 : len(raw)-1]
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

func parsePhases(raw string) []Phase {
	values := parseYAMLList(raw)
	out := make([]Phase, 0, len(values))
	for _, v := range values {
		out = append(out, Phase(v))
	}
	return out
}

func parseModes(raw string) []Mode {
	values := parseYAMLList(raw)
	out := make([]Mode, 0, len(values))
	for _, v := range values {
		out = append(out, Mode(v))
	}
	return out
}

func parseEnvironments(raw string) []Environment {
	values := parseYAMLList(raw)
	out := make([]Environment, 0, len(values))
	for _, v := range values {
		out = append(out, Environment(v))
	}
	return out
}

func parseStrategies(raw string) []DeployStrategy {
	values := parseYAMLList(raw)
	out := make([]DeployStrategy, 0, len(values))
	for _, v := range values {
		out = append(out, DeployStrategy(v))
	}
	return out
}

func parseRuntimes(raw string) []RuntimeClass {
	values := parseYAMLList(raw)
	out := make([]RuntimeClass, 0, len(values))
	for _, v := range values {
		out = append(out, RuntimeClass(v))
	}
	return out
}

func parseRoutes(raw string) []BootstrapRoute {
	values := parseYAMLList(raw)
	out := make([]BootstrapRoute, 0, len(values))
	for _, v := range values {
		out = append(out, BootstrapRoute(v))
	}
	return out
}

func parseIdleScenarios(raw string) []IdleScenario {
	values := parseYAMLList(raw)
	out := make([]IdleScenario, 0, len(values))
	for _, v := range values {
		out = append(out, IdleScenario(v))
	}
	return out
}

func parseDeployStates(raw string) []DeployState {
	values := parseYAMLList(raw)
	out := make([]DeployState, 0, len(values))
	for _, v := range values {
		out = append(out, DeployState(v))
	}
	return out
}
