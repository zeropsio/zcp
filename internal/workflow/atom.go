package workflow

import (
	"bufio"
	"fmt"
	"regexp"
	"strings"
)

// referencesFieldPattern matches a pkg.Type.Field entry in atom
// references-fields frontmatter (e.g. "ops.DeployResult.Status",
// "tools.envChangeResult.RestartedServices"). Type names may be
// unexported — loadAtomReferenceFieldIndex indexes all struct types
// regardless of visibility, and JSON serialization does not hide them.
// Used by ParseAtom to reject malformed entries early.
var referencesFieldPattern = regexp.MustCompile(`^[a-z_]+\.[A-Za-z][A-Za-z0-9_]*\.[A-Za-z][A-Za-z0-9_]*$`)

// KnowledgeAtom is one piece of runtime-dependent guidance. Atoms live as
// .md files under internal/content/atoms/; their frontmatter declares the
// axis vector, their body is the rendered guidance.
type KnowledgeAtom struct {
	ID       string
	Axes     AxisVector
	Priority int
	Title    string
	Body     string

	// ReferencesFields lists Go struct fields in pkg.Type.Field form
	// (e.g. ops.DeployResult.Status) that this atom cites from response
	// or envelope shapes. Validated by TestAtomReferenceFieldIntegrity
	// (Phase 2) — every entry must resolve to a real field via AST scan.
	ReferencesFields []string
	// ReferencesAtoms lists atom IDs this atom cross-references (body
	// prose points the reader at another atom for a consolidated topic).
	// Validated by TestAtomReferencesAtomsIntegrity (Phase 2).
	ReferencesAtoms []string
	// PinnedByScenarios lists scenario test names that pin this atom's
	// appearance in the synthesized body. Informational; helps future
	// edits locate downstream test expectations.
	PinnedByScenarios []string
}

// AxisVector is the subset of envelope dimensions an atom applies to. Empty
// axes mean "any value for this dimension" (Phases is the exception — it
// MUST be non-empty).
type AxisVector struct {
	Phases        []Phase
	Modes         []Mode
	Environments  []Environment
	Strategies    []DeployStrategy
	Triggers      []PushGitTrigger // valid only alongside strategies: [push-git]
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
			Triggers:      parseTriggers(fields["triggers"]),
			Runtimes:      parseRuntimes(fields["runtimes"]),
			Routes:        parseRoutes(fields["routes"]),
			Steps:         parseYAMLList(fields["steps"]),
			IdleScenarios: parseIdleScenarios(fields["idleScenarios"]),
			DeployStates:  parseDeployStates(fields["deployStates"]),
		},
		ReferencesFields:  parseYAMLList(fields["references-fields"]),
		ReferencesAtoms:   parseYAMLList(fields["references-atoms"]),
		PinnedByScenarios: parseYAMLList(fields["pinned-by-scenario"]),
	}
	if atom.ID == "" {
		return atom, fmt.Errorf("atom missing required field: id")
	}
	if len(atom.Axes.Phases) == 0 {
		return atom, fmt.Errorf("atom %q missing required field: phases", atom.ID)
	}
	for _, ref := range atom.ReferencesFields {
		if !referencesFieldPattern.MatchString(ref) {
			return atom, fmt.Errorf("atom %q references-fields entry %q is not pkg.Type.Field form (e.g. ops.DeployResult.Status)", atom.ID, ref)
		}
	}
	// references-atoms entries are validated by parseYAMLList (empty
	// strings filtered out) and by TestAtomReferencesAtomsIntegrity in
	// Phase 2 (target atom must exist).
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

// parseTriggers reads the optional `triggers:` frontmatter field —
// filters strategy-setup atoms to the webhook/actions sub-branch.
func parseTriggers(raw string) []PushGitTrigger {
	values := parseYAMLList(raw)
	out := make([]PushGitTrigger, 0, len(values))
	for _, v := range values {
		out = append(out, PushGitTrigger(v))
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
