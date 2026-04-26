package workflow

import (
	"bufio"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/topology"
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
	Modes         []topology.Mode
	Environments  []Environment
	Strategies    []topology.DeployStrategy
	Triggers      []topology.PushGitTrigger // valid only alongside strategies: [push-git]
	Runtimes      []topology.RuntimeClass
	Routes        []BootstrapRoute
	Steps         []string
	IdleScenarios []IdleScenario
	DeployStates  []DeployState
	// EnvelopeDeployStates is the envelope-scoped twin of DeployStates:
	// the atom matches once if at least one bootstrapped service in the
	// envelope satisfies any of the listed states, and renders 1× via the
	// global primaryHostnames picker (no per-service iteration). Lets a
	// "rules" atom carry concept-level guidance once per envelope while a
	// sibling "cmds" atom carries per-host imperative tool calls. Empty =
	// no envelope-scoped deploy-state gate (atom matches independently of
	// deploy state). MUTUALLY EXCLUSIVE with DeployStates per atom — an
	// atom declaring both is rejected at parse time.
	EnvelopeDeployStates []DeployState
	// ServiceStatuses scopes the atom to a specific live service status
	// (`ServiceSnapshot.Status`) — typically the platform-side state like
	// `ACTIVE` or `READY_TO_DEPLOY`. Service-scoped: at least one service
	// in the envelope must match for the atom to fire. Empty = any status.
	ServiceStatuses []string
}

// validAtomFrontmatterKeys is the closed set of frontmatter keys an atom
// MAY declare. Anything outside this set is rejected at parse time —
// silent typos in axis names produced wildcard-broad atoms before strict
// validation landed (see plan-pipeline-repair.md C5). Update this set
// when adding a new axis or attribute, never to accommodate atom typos.
//
//nolint:gochecknoglobals // immutable lookup table
var validAtomFrontmatterKeys = map[string]struct{}{
	"id":                   {},
	"title":                {},
	"priority":             {},
	"phases":               {},
	"modes":                {},
	"environments":         {},
	"strategies":           {},
	"triggers":             {},
	"runtimes":             {},
	"routes":               {},
	"steps":                {},
	"idleScenarios":        {},
	"deployStates":         {},
	"envelopeDeployStates": {},
	"serviceStatus":        {},
	"references-fields":    {},
	"references-atoms":     {},
	"pinned-by-scenario":   {},
}

// listAxisKeys is the subset of frontmatter keys whose value MUST be in
// inline-list form (`[a, b, c]`) when non-empty. Scalar keys (id, title,
// priority) are not in this set and may appear with bare string values.
//
//nolint:gochecknoglobals // immutable lookup table
var listAxisKeys = map[string]struct{}{
	"phases":               {},
	"modes":                {},
	"environments":         {},
	"strategies":           {},
	"triggers":             {},
	"runtimes":             {},
	"routes":               {},
	"steps":                {},
	"idleScenarios":        {},
	"deployStates":         {},
	"envelopeDeployStates": {},
	"serviceStatus":        {},
	"references-fields":    {},
	"references-atoms":     {},
	"pinned-by-scenario":   {},
}

// validAtomEnumValues maps each axis key to its closed value set.
// Validated at parse time so a typo in an axis value (e.g. `develop` vs
// `develop-active`) fails the build instead of silently dropping an atom
// from filter results. Service status values aren't validated — they're
// platform-side strings (ACTIVE, READY_TO_DEPLOY, ...) outside ZCP's
// control.
//
//nolint:gochecknoglobals // immutable lookup table
var validAtomEnumValues = map[string]map[string]struct{}{
	"phases": {
		"idle":                {},
		"bootstrap-active":    {},
		"develop-active":      {},
		"develop-closed-auto": {},
		"recipe-active":       {},
		"strategy-setup":      {},
		"export-active":       {},
	},
	"modes": {
		"dev":         {},
		"stage":       {},
		"simple":      {},
		"standard":    {},
		"local-stage": {},
		"local-only":  {},
	},
	"environments": {
		"container": {},
		"local":     {},
	},
	"strategies": {
		"push-dev": {},
		"push-git": {},
		"manual":   {},
		"unset":    {},
	},
	"triggers": {
		"webhook": {},
		"actions": {},
		"unset":   {},
	},
	"runtimes": {
		"dynamic":            {},
		"static":             {},
		"implicit-webserver": {},
		"managed":            {},
		"unknown":            {},
	},
	"routes": {
		"recipe":  {},
		"classic": {},
		"adopt":   {},
		"resume":  {},
	},
	"steps": {
		"discover":  {},
		"provision": {},
		"close":     {},
	},
	"idleScenarios": {
		"empty":        {},
		"bootstrapped": {},
		"adopt":        {},
		"incomplete":   {},
		"orphan":       {},
	},
	"deployStates": {
		"never-deployed": {},
		"deployed":       {},
	},
	"envelopeDeployStates": {
		"never-deployed": {},
		"deployed":       {},
	},
}

// validateAtomFrontmatter is the strict pre-parse gate (plan C5). It runs
// before any axis-specific parser sees the value, so a malformed atom
// fails ParseAtom with a precise message naming the offending key — never
// silently degrades to a wildcard-broad atom.
//
// Three checks (in order, first failure wins):
//  1. Every declared key is in validAtomFrontmatterKeys (no typos).
//  2. Every list-axis key with a non-empty value is in `[...]` form (no
//     bare scalars where a list is required).
//  3. Every list-axis value is in validAtomEnumValues for that axis when
//     the axis has a closed set (serviceStatus / references-* / pinned-*
//     are not enum-validated — open sets).
func validateAtomFrontmatter(fields map[string]string) error {
	for key := range fields {
		if _, ok := validAtomFrontmatterKeys[key]; !ok {
			return fmt.Errorf("unknown atom frontmatter key %q (valid keys: id, title, priority, phases, modes, environments, strategies, triggers, runtimes, routes, steps, idleScenarios, deployStates, envelopeDeployStates, serviceStatus, references-fields, references-atoms, pinned-by-scenario)", key)
		}
	}
	for key, raw := range fields {
		if _, isList := listAxisKeys[key]; !isList {
			continue
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if !strings.HasPrefix(raw, "[") || !strings.HasSuffix(raw, "]") {
			return fmt.Errorf("atom frontmatter key %q must be inline list form `[a, b, c]`, got %q", key, raw)
		}
	}
	for key, validSet := range validAtomEnumValues {
		raw := strings.TrimSpace(fields[key])
		if raw == "" || !strings.HasPrefix(raw, "[") || !strings.HasSuffix(raw, "]") {
			continue
		}
		inner := strings.TrimSpace(raw[1 : len(raw)-1])
		if inner == "" {
			continue
		}
		for v := range strings.SplitSeq(inner, ",") {
			v = strings.TrimSpace(v)
			if v == "" {
				continue
			}
			if _, ok := validSet[v]; !ok {
				return fmt.Errorf("atom frontmatter key %q has invalid value %q (axis %s accepts: %s)", key, v, key, sortedEnumKeys(validSet))
			}
		}
	}
	return nil
}

// sortedEnumKeys returns a deterministic comma-separated list of the
// valid values for an axis. Used in error messages so the offender sees
// the closed set directly without grepping the source.
func sortedEnumKeys(set map[string]struct{}) string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

// ParseAtom parses a `.md` file body containing YAML frontmatter and a
// markdown body. It returns a KnowledgeAtom or an error if required fields
// are missing or if frontmatter fails strict validation.
func ParseAtom(content string) (KnowledgeAtom, error) {
	front, body, err := splitFrontmatter(content)
	if err != nil {
		return KnowledgeAtom{}, err
	}
	fields, err := parseFrontmatter(front)
	if err != nil {
		return KnowledgeAtom{}, err
	}
	if err := validateAtomFrontmatter(fields); err != nil {
		return KnowledgeAtom{}, err
	}

	atom := KnowledgeAtom{
		ID:       fields["id"],
		Title:    fields["title"],
		Body:     strings.TrimSpace(body),
		Priority: atomPriority(fields["priority"]),
		Axes: AxisVector{
			Phases:               parsePhases(fields["phases"]),
			Modes:                parseModes(fields["modes"]),
			Environments:         parseEnvironments(fields["environments"]),
			Strategies:           parseStrategies(fields["strategies"]),
			Triggers:             parseTriggers(fields["triggers"]),
			Runtimes:             parseRuntimes(fields["runtimes"]),
			Routes:               parseRoutes(fields["routes"]),
			Steps:                parseYAMLList(fields["steps"]),
			IdleScenarios:        parseIdleScenarios(fields["idleScenarios"]),
			DeployStates:         parseDeployStates(fields["deployStates"]),
			EnvelopeDeployStates: parseDeployStates(fields["envelopeDeployStates"]),
			ServiceStatuses:      parseYAMLList(fields["serviceStatus"]),
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
	if len(atom.Axes.DeployStates) > 0 && len(atom.Axes.EnvelopeDeployStates) > 0 {
		return atom, fmt.Errorf("atom %q declares both deployStates (service-scoped) and envelopeDeployStates (envelope-scoped) — pick one; an atom is either per-service or once-per-envelope", atom.ID)
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

func parseModes(raw string) []topology.Mode {
	values := parseYAMLList(raw)
	out := make([]topology.Mode, 0, len(values))
	for _, v := range values {
		out = append(out, topology.Mode(v))
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

func parseStrategies(raw string) []topology.DeployStrategy {
	values := parseYAMLList(raw)
	out := make([]topology.DeployStrategy, 0, len(values))
	for _, v := range values {
		out = append(out, topology.DeployStrategy(v))
	}
	return out
}

// parseTriggers reads the optional `triggers:` frontmatter field —
// filters strategy-setup atoms to the webhook/actions sub-branch.
func parseTriggers(raw string) []topology.PushGitTrigger {
	values := parseYAMLList(raw)
	out := make([]topology.PushGitTrigger, 0, len(values))
	for _, v := range values {
		out = append(out, topology.PushGitTrigger(v))
	}
	return out
}

func parseRuntimes(raw string) []topology.RuntimeClass {
	values := parseYAMLList(raw)
	out := make([]topology.RuntimeClass, 0, len(values))
	for _, v := range values {
		out = append(out, topology.RuntimeClass(v))
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
