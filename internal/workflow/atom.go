package workflow

import (
	"bufio"
	"fmt"
	"regexp"
	"sort"
	"strings"

	contentpkg "github.com/zeropsio/zcp/internal/content"
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
	Phases       []Phase
	Modes        []topology.Mode
	Environments []Environment
	Strategies   []topology.DeployStrategy
	Triggers     []topology.PushGitTrigger // valid only alongside strategies: [push-git]
	// CloseDeployModes scopes by the per-pair close-mode dimension (see
	// topology.CloseDeployMode). Service-scoped: at least one matching
	// service in the envelope must declare a close mode in this set.
	// Empty = no close-mode gate.
	//
	// New axis introduced by deploy-strategy decomposition Phase 1
	// (plans/deploy-strategy-decomposition-2026-04-28.md). The synthesizer
	// filter wiring (serviceSatisfiesAxes) lands in Phase 3 once
	// ServiceSnapshot exposes the field; until then the parser accepts
	// the axis but no atom declares it.
	CloseDeployModes []topology.CloseDeployMode
	// GitPushStates scopes by the per-pair git-push-capability dimension
	// (see topology.GitPushState). Service-scoped. Empty = no gate.
	// Same Phase 1 / Phase 3 wiring schedule as CloseDeployModes.
	GitPushStates []topology.GitPushState
	// BuildIntegrations scopes by the per-pair ZCP-managed CI integration
	// (see topology.BuildIntegration). Service-scoped. Empty = no gate.
	// Same Phase 1 / Phase 3 wiring schedule as CloseDeployModes.
	BuildIntegrations []topology.BuildIntegration
	Runtimes          []topology.RuntimeClass
	Routes            []BootstrapRoute
	Steps             []string
	IdleScenarios     []IdleScenario
	DeployStates      []DeployState
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
	// MultiService toggles single-render aggregation for atoms whose body
	// needs per-service iteration only inside discrete `{services-list:
	// TEMPLATE}` directives, not for the whole body. Default ""
	// (`MultiServicePerService`) means the legacy per-matching-service
	// loop: render the body once per matching service with `{hostname}`
	// substituted from that service. `aggregate` (`MultiServiceAggregate`)
	// means single render: the renderer expands every embedded
	// `{services-list:TEMPLATE}` directive over the matching services,
	// joined with newlines, and emits one MatchedRender with Service=nil.
	// Plain `{hostname}` outside a directive falls back to the global
	// primaryHostnames picker (same contract as service-agnostic atoms).
	MultiService MultiServiceMode
}

// MultiServiceMode is the closed enum for the AxisVector.MultiService
// scalar axis. Empty value = legacy per-service render. `aggregate` =
// single render with inline `{services-list:TEMPLATE}` expansion (engine
// ticket E1; eliminates structural per-service duplication for atoms
// whose body is mostly envelope-scoped prose with one or two per-service
// command blocks).
type MultiServiceMode string

const (
	// MultiServicePerService is the default: per-matching-service iteration.
	// One MatchedRender per service.
	MultiServicePerService MultiServiceMode = ""
	// MultiServiceAggregate renders the atom once. Per-service content
	// must live inside `{services-list:TEMPLATE}` directives; everything
	// else renders once with envelope-scoped substitutions.
	MultiServiceAggregate MultiServiceMode = "aggregate"
)

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
	"closeDeployModes":     {}, // deploy-decomp P1 — replaces strategies on the close-mode dimension
	"gitPushStates":        {}, // deploy-decomp P1 — orthogonal git-push capability dimension
	"buildIntegrations":    {}, // deploy-decomp P1 — orthogonal ZCP-managed CI integration dimension
	"runtimes":             {},
	"routes":               {},
	"steps":                {},
	"idleScenarios":        {},
	"deployStates":         {},
	"envelopeDeployStates": {},
	"serviceStatus":        {},
	"multiService":         {},
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
	"closeDeployModes":     {},
	"gitPushStates":        {},
	"buildIntegrations":    {},
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
	"closeDeployModes": {
		"unset":    {},
		"auto":     {},
		"git-push": {},
		"manual":   {},
	},
	"gitPushStates": {
		"unconfigured": {},
		"configured":   {},
		"broken":       {},
		"unknown":      {},
	},
	"buildIntegrations": {
		"none":    {},
		"webhook": {},
		"actions": {},
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

// validScalarEnumValues maps scalar (non-list) frontmatter keys to their
// closed value sets. `multiService` is currently the only scalar enum
// axis — `aggregate` is the single allowed non-empty value (empty means
// per-service render, the legacy default). Validated alongside list-axis
// enums so a typo in the value (e.g. `multiService: aggreate`) fails the
// build instead of silently rendering as default.
//
//nolint:gochecknoglobals // immutable lookup table
var validScalarEnumValues = map[string]map[string]struct{}{
	"multiService": {
		"aggregate": {},
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
			return fmt.Errorf("unknown atom frontmatter key %q (valid keys: id, title, priority, phases, modes, environments, strategies, triggers, closeDeployModes, gitPushStates, buildIntegrations, runtimes, routes, steps, idleScenarios, deployStates, envelopeDeployStates, serviceStatus, multiService, references-fields, references-atoms, pinned-by-scenario)", key)
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
	for key, validSet := range validScalarEnumValues {
		raw := strings.TrimSpace(fields[key])
		if raw == "" {
			continue
		}
		if _, ok := validSet[raw]; !ok {
			return fmt.Errorf("atom frontmatter key %q has invalid value %q (axis %s accepts: %s)", key, raw, key, sortedEnumKeys(validSet))
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
		Body:     strings.TrimSpace(contentpkg.StripAxisMarkers(body)),
		Priority: atomPriority(fields["priority"]),
		Axes: AxisVector{
			Phases:               parsePhases(fields["phases"]),
			Modes:                parseModes(fields["modes"]),
			Environments:         parseEnvironments(fields["environments"]),
			Strategies:           parseStrategies(fields["strategies"]),
			Triggers:             parseTriggers(fields["triggers"]),
			CloseDeployModes:     parseCloseDeployModes(fields["closeDeployModes"]),
			GitPushStates:        parseGitPushStates(fields["gitPushStates"]),
			BuildIntegrations:    parseBuildIntegrations(fields["buildIntegrations"]),
			Runtimes:             parseRuntimes(fields["runtimes"]),
			Routes:               parseRoutes(fields["routes"]),
			Steps:                parseYAMLList(fields["steps"]),
			IdleScenarios:        parseIdleScenarios(fields["idleScenarios"]),
			DeployStates:         parseDeployStates(fields["deployStates"]),
			EnvelopeDeployStates: parseDeployStates(fields["envelopeDeployStates"]),
			ServiceStatuses:      parseYAMLList(fields["serviceStatus"]),
			MultiService:         MultiServiceMode(strings.TrimSpace(fields["multiService"])),
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

// parseCloseDeployModes reads the optional `closeDeployModes:` frontmatter
// field — filters atoms by the per-pair developer choice for what the
// develop workflow auto-does at close. Introduced by the deploy-strategy
// decomposition (plans/deploy-strategy-decomposition-2026-04-28.md). One
// of three orthogonal axes that replace the legacy `strategies:` /
// `triggers:` pair on the post-decomposition corpus.
func parseCloseDeployModes(raw string) []topology.CloseDeployMode {
	values := parseYAMLList(raw)
	out := make([]topology.CloseDeployMode, 0, len(values))
	for _, v := range values {
		out = append(out, topology.CloseDeployMode(v))
	}
	return out
}

// parseGitPushStates reads the optional `gitPushStates:` frontmatter
// field — filters atoms by per-pair git-push capability state.
func parseGitPushStates(raw string) []topology.GitPushState {
	values := parseYAMLList(raw)
	out := make([]topology.GitPushState, 0, len(values))
	for _, v := range values {
		out = append(out, topology.GitPushState(v))
	}
	return out
}

// parseBuildIntegrations reads the optional `buildIntegrations:` frontmatter
// field — filters atoms by per-pair ZCP-managed CI integration. Distinct
// from `triggers:` (which is on the legacy push-git axis); axes coexist
// during the migration window.
func parseBuildIntegrations(raw string) []topology.BuildIntegration {
	values := parseYAMLList(raw)
	out := make([]topology.BuildIntegration, 0, len(values))
	for _, v := range values {
		out = append(out, topology.BuildIntegration(v))
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
