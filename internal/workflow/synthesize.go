package workflow

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"

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

	hostname, stageHostname := primaryHostnames(envelope.Services)
	replacer := strings.NewReplacer(
		"{hostname}", hostname,
		"{stage-hostname}", stageHostname,
		"{project-name}", envelope.Project.Name,
	)

	out := make([]string, 0, len(matched))
	for _, atom := range matched {
		body := replacer.Replace(atom.Body)
		if leak := findUnknownPlaceholder(body); leak != "" {
			return nil, fmt.Errorf("atom %s: unknown placeholder %q in atom body", atom.ID, leak)
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
	if len(atom.Axes.DeployStates) > 0 && !anyServiceDeployState(env.Services, atom.Axes.DeployStates) {
		return false
	}

	// IdleScenario is idle-only like routes/steps are bootstrap-only. An atom
	// that declares idleScenarios filters out non-idle envelopes entirely and
	// further filters idle envelopes to the listed sub-cases.
	if len(atom.Axes.IdleScenarios) > 0 {
		if env.Phase != PhaseIdle || !slices.Contains(atom.Axes.IdleScenarios, env.IdleScenario) {
			return false
		}
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

// anyServiceDeployState matches only bootstrapped services. Non-bootstrapped
// services have no tracked deploy state — filtering them in would incorrectly
// surface first-deploy atoms on pure-adoption services that bootstrap never
// touched.
func anyServiceDeployState(services []ServiceSnapshot, set []DeployState) bool {
	for _, svc := range services {
		if !svc.Bootstrapped {
			continue
		}
		state := DeployStateNeverDeployed
		if svc.Deployed {
			state = DeployStateDeployed
		}
		if slices.Contains(set, state) {
			return true
		}
	}
	return false
}

// primaryHostnames returns the hostname and paired stage hostname used to
// substitute `{hostname}` / `{stage-hostname}` in atom bodies. Prefers
// dynamic runtimes (where the placeholder is most meaningful), then
// implicit-webserver, then static. The two picks are independent — a
// dynamic service provides the hostname even when only a static service
// has a stage hostname. Both empty when no runtime service exists.
//
// Supported placeholder keys consumed by Synthesize: {hostname},
// {stage-hostname}, {project-name}. {start-command} and other tokens in
// allowedSurvivingPlaceholders pass through untouched — the agent fills
// them from run-time context it already has.
func primaryHostnames(services []ServiceSnapshot) (hostname, stageHostname string) {
	order := []RuntimeClass{RuntimeDynamic, RuntimeImplicitWeb, RuntimeStatic}
	for _, want := range order {
		for _, svc := range services {
			if svc.RuntimeClass != want {
				continue
			}
			if hostname == "" {
				hostname = svc.Hostname
			}
			if stageHostname == "" && svc.StageHostname != "" {
				stageHostname = svc.StageHostname
			}
			if hostname != "" && stageHostname != "" {
				return hostname, stageHostname
			}
		}
	}
	return hostname, stageHostname
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
	"{provider}":       {},
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

// The atom corpus is embedded in the binary and immutable after `go build`,
// so we parse once and reuse. Hot paths call LoadAtomCorpus per-request
// (every bootstrap step, every immediate workflow); re-reading 74 files and
// re-parsing YAML frontmatter on each call was pure waste.
//
//nolint:gochecknoglobals // cache for embedded immutable corpus
var (
	corpusOnce sync.Once
	corpusVal  []KnowledgeAtom
	errCorpus  error
)

// LoadAtomCorpus returns the parsed atom corpus. First call reads and parses
// the embedded atom files; subsequent calls return the cached slice. Errors
// surface on the first malformed atom so the build fails loudly — a
// silently-skipped atom is a defect vector.
//
// The returned slice is shared; callers must not mutate it.
func LoadAtomCorpus() ([]KnowledgeAtom, error) {
	corpusOnce.Do(func() {
		files, err := content.ReadAllAtoms()
		if err != nil {
			errCorpus = fmt.Errorf("load atom corpus: %w", err)
			return
		}
		corpus := make([]KnowledgeAtom, 0, len(files))
		for _, f := range files {
			atom, err := ParseAtom(f.Content)
			if err != nil {
				errCorpus = fmt.Errorf("parse atom %s: %w", f.Name, err)
				return
			}
			corpus = append(corpus, atom)
		}
		corpusVal = corpus
	})
	return corpusVal, errCorpus
}
