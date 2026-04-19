package workflow

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/content"
)

// Synthesize returns the ordered guidance bodies for the given envelope.
// Algorithm:
//  1. Filter atoms whose declared axes all match the envelope.
//  2. Sort by priority (ascending: 1 first), then by id (lexicographic) for stability.
//  3. Substitute placeholders from envelope.
//  4. Return bodies in sorted order.
//
// Compaction-safety invariant: for the same StateEnvelope serialisation,
// Synthesize MUST return byte-identical output across calls. No wall-clock
// reads, no map iteration order, no randomness.
func Synthesize(envelope StateEnvelope, corpus []KnowledgeAtom) ([]string, error) {
	matched := make([]KnowledgeAtom, 0, len(corpus))
	for _, atom := range corpus {
		if atomMatches(atom, envelope) {
			matched = append(matched, atom)
		}
	}
	sort.SliceStable(matched, func(i, j int) bool {
		if matched[i].Priority != matched[j].Priority {
			return matched[i].Priority < matched[j].Priority
		}
		return matched[i].ID < matched[j].ID
	})

	out := make([]string, 0, len(matched))
	for _, atom := range matched {
		body, err := substitutePlaceholders(atom.Body, envelope)
		if err != nil {
			return nil, fmt.Errorf("atom %s: %w", atom.ID, err)
		}
		out = append(out, body)
	}
	return out, nil
}

// atomMatches reports whether an atom's axes permit the envelope.
// Empty axis slice = wildcard for that axis; phases is required non-empty
// by the parser so the empty case does not arise here.
func atomMatches(atom KnowledgeAtom, env StateEnvelope) bool {
	if !phaseInSet(env.Phase, atom.Axes.Phases) {
		return false
	}
	if len(atom.Axes.Environments) > 0 && !envInSet(env.Environment, atom.Axes.Environments) {
		return false
	}

	// Route is bootstrap-only. An atom that declares a Routes axis filters out
	// non-bootstrap envelopes entirely (there's no route to match against) and
	// further filters bootstrap envelopes to the listed routes.
	if len(atom.Axes.Routes) > 0 {
		if env.Bootstrap == nil || !routeInSet(env.Bootstrap.Route, atom.Axes.Routes) {
			return false
		}
	}

	// Step is bootstrap-only like route. An atom that declares a Steps axis
	// filters bootstrap envelopes to the named step; envelopes without a
	// Bootstrap summary (or with an empty Step) are filtered out.
	if len(atom.Axes.Steps) > 0 {
		if env.Bootstrap == nil || !stepInSet(env.Bootstrap.Step, atom.Axes.Steps) {
			return false
		}
	}

	// Service-scoped axes (mode, strategy, runtime) match when ANY service in
	// the envelope matches. Atoms declaring these axes are implicitly
	// per-service; the render layer decides whether to emit once or per-service.
	if len(atom.Axes.Modes) > 0 && !anyServiceMode(env.Services, atom.Axes.Modes) {
		return false
	}
	if len(atom.Axes.Strategies) > 0 && !anyServiceStrategy(env.Services, atom.Axes.Strategies) {
		return false
	}
	if len(atom.Axes.Runtimes) > 0 && !anyServiceRuntime(env.Services, atom.Axes.Runtimes) {
		return false
	}
	return true
}

func routeInSet(r BootstrapRoute, set []BootstrapRoute) bool {
	return slices.Contains(set, r)
}

func stepInSet(step string, set []string) bool {
	return slices.Contains(set, step)
}

func phaseInSet(p Phase, set []Phase) bool {
	return slices.Contains(set, p)
}

func envInSet(e Environment, set []Environment) bool {
	return slices.Contains(set, e)
}

func anyServiceMode(services []ServiceSnapshot, set []Mode) bool {
	for _, svc := range services {
		if slices.Contains(set, svc.Mode) {
			return true
		}
	}
	return false
}

func anyServiceStrategy(services []ServiceSnapshot, set []DeployStrategy) bool {
	for _, svc := range services {
		if slices.Contains(set, svc.Strategy) {
			return true
		}
	}
	return false
}

func anyServiceRuntime(services []ServiceSnapshot, set []RuntimeClass) bool {
	for _, svc := range services {
		if slices.Contains(set, svc.RuntimeClass) {
			return true
		}
	}
	return false
}

// substitutePlaceholders swaps `{key}` tokens in the atom body with values
// from the envelope. Supported keys:
//
//	{hostname}       — the first dynamic/static/implicit-webserver service
//	                   in envelope.Services (runtime-scoped atoms)
//	{stage-hostname} — paired stage hostname of that service, if any
//	{project-name}   — envelope.Project.Name
//	{start-command}  — left as-is (caller-populated; see note below)
//
// Unknown placeholders trigger an error so atoms cannot leak raw braces into
// the LLM payload.
//
// `{start-command}` is intentionally NOT substituted here — the specific
// start command varies per service and is supplied by the renderer when it
// has the service context. An atom that contains a literal `{start-command}`
// is passed through untouched and is expected to be rendered per-service by
// a higher layer (Phase 5).
func substitutePlaceholders(body string, env StateEnvelope) (string, error) {
	hostname := primaryRuntimeHostname(env.Services)
	stageHostname := primaryStageHostname(env.Services)

	replacements := map[string]string{
		"{hostname}":       hostname,
		"{stage-hostname}": stageHostname,
		"{project-name}":   env.Project.Name,
	}

	result := body
	for key, value := range replacements {
		result = strings.ReplaceAll(result, key, value)
	}

	// Detect any unknown `{...}` placeholder that wasn't substituted or
	// whitelisted. `{start-command}` is explicitly allowed to survive the pass.
	if leak := findUnknownPlaceholder(result); leak != "" {
		return "", fmt.Errorf("unknown placeholder %q in atom body", leak)
	}
	return result, nil
}

// primaryRuntimeHostname picks a stable hostname for `{hostname}` expansion.
// Prefers dynamic runtimes (where the placeholder is most meaningful), then
// static, then implicit-webserver. Returns "" if no runtime service exists.
func primaryRuntimeHostname(services []ServiceSnapshot) string {
	order := []RuntimeClass{RuntimeDynamic, RuntimeImplicitWeb, RuntimeStatic}
	for _, want := range order {
		for _, svc := range services {
			if svc.RuntimeClass == want {
				return svc.Hostname
			}
		}
	}
	return ""
}

func primaryStageHostname(services []ServiceSnapshot) string {
	order := []RuntimeClass{RuntimeDynamic, RuntimeImplicitWeb, RuntimeStatic}
	for _, want := range order {
		for _, svc := range services {
			if svc.RuntimeClass == want && svc.StageHostname != "" {
				return svc.StageHostname
			}
		}
	}
	return ""
}

// allowedSurvivingPlaceholders are `{...}` tokens an atom is allowed to emit
// into the LLM payload unchanged — the LLM is expected to substitute them
// from run-time context it already has (the zerops.yaml it just wrote, the
// task the user gave it, a naming scheme the agent chose, etc.).
var allowedSurvivingPlaceholders = map[string]struct{}{
	"{start-command}":    {},
	"{task-description}": {},
	"{your-description}": {},
	"{next-task}":        {},
	"{port}":             {},
	"{name}":             {},
	"{token}":            {},
	"{url}":              {},
	"{runtimeVersion}":   {},
	"{runtimeBase}":      {},
	// cicd + export placeholders — agent fills from project context.
	"{setup}":          {},
	"{serviceId}":      {},
	"{targetHostname}": {},
	"{devHostname}":    {},
	"{repoUrl}":        {},
	"{owner}":          {},
	"{repoName}":       {},
	"{repo}":           {},
	"{branchName}":     {},
	"{branch}":         {},
	"{zeropsToken}":    {},
	"{runtime}":        {},
}

// findUnknownPlaceholder scans body for `{...}` tokens that are neither
// substituted nor whitelisted. Returns the first offender or "".
// `${...}` tokens are skipped — they are shell/env-var references (e.g.
// `${db_connectionString}`, `${hostname_varName}`) and not atom placeholders.
func findUnknownPlaceholder(body string) string {
	i := 0
	for i < len(body) {
		open := strings.IndexByte(body[i:], '{')
		if open < 0 {
			return ""
		}
		open += i
		closeIdx := strings.IndexByte(body[open:], '}')
		if closeIdx < 0 {
			return ""
		}
		closeIdx += open
		token := body[open : closeIdx+1]
		// Skip `${...}` shell-style env var refs — these belong to the
		// generated zerops.yaml the agent will write, not to us.
		if open > 0 && body[open-1] == '$' {
			i = closeIdx + 1
			continue
		}
		// Skip non-placeholder braces (e.g. code fences containing JSON).
		// Placeholders are `{word-with-dashes}` only, no whitespace or braces inside.
		if isPlaceholderToken(token) {
			if _, ok := allowedSurvivingPlaceholders[token]; !ok {
				return token
			}
		}
		i = closeIdx + 1
	}
	return ""
}

func isPlaceholderToken(token string) bool {
	if len(token) < 3 || token[0] != '{' || token[len(token)-1] != '}' {
		return false
	}
	inner := token[1 : len(token)-1]
	if inner == "" {
		return false
	}
	for _, r := range inner {
		if r == ' ' || r == '\n' || r == '\t' || r == '{' || r == '}' || r == '"' {
			return false
		}
	}
	return true
}

// SynthesizeImmediateWorkflow returns the atom-composed guidance body for a
// stateless workflow (cicd, export). These workflows don't own a session and
// filter atoms by Phase alone, so the envelope is a minimal
// `{Phase, Environment}` pair. Callers prepend any dynamic service-context
// header before returning to the LLM.
func SynthesizeImmediateWorkflow(phase Phase, env Environment) (string, error) {
	envelope := StateEnvelope{
		Phase:       phase,
		Environment: env,
	}
	corpus, err := LoadAtomCorpus()
	if err != nil {
		return "", err
	}
	bodies, err := Synthesize(envelope, corpus)
	if err != nil {
		return "", err
	}
	return strings.Join(bodies, "\n\n---\n\n"), nil
}

// LoadAtomCorpus reads all embedded atoms, parses each into a KnowledgeAtom,
// and returns the corpus. Errors surface on the first malformed atom so the
// build fails loudly — a silently-skipped atom is a defect vector.
func LoadAtomCorpus() ([]KnowledgeAtom, error) {
	files, err := content.ReadAllAtoms()
	if err != nil {
		return nil, fmt.Errorf("load atom corpus: %w", err)
	}
	corpus := make([]KnowledgeAtom, 0, len(files))
	for _, f := range files {
		atom, err := ParseAtom(f.Content)
		if err != nil {
			return nil, fmt.Errorf("parse atom %s: %w", f.Name, err)
		}
		corpus = append(corpus, atom)
	}
	return corpus, nil
}
