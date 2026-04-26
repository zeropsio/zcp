package workflow

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/zeropsio/zcp/internal/content"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
)

// MatchedRender pairs a synthesized atom body with the service whose axes
// satisfied the atom's service-scoped declaration (when any). Service is
// nil for atoms without service-scoped axes — those atoms render once
// using the global primaryHostnames picker (covers envelope-wide atoms
// like idle-* or strategy-setup-*).
//
// Phase 2 (C2) of the pipeline-repair plan: atoms with service-scoped
// axes (modes, strategies, runtimes, deployStates, serviceStatus,
// triggers) bind their `{hostname}` / `{stage-hostname}` substitution to
// the matched service. Pre-fix the global picker was used for every
// atom, producing wrong-host commands in multi-service projects (an
// atom matched via service B could render service A's hostname).
type MatchedRender struct {
	AtomID  string
	Body    string
	Service *ServiceSnapshot
}

// Synthesize returns the ordered MatchedRenders for the given envelope.
// Algorithm:
//  1. Filter atoms whose envelope-wide axes match (phase, environment,
//     route, step, idleScenario).
//  2. For each surviving atom, find all services satisfying the atom's
//     service-scoped conjunction (modes ∧ strategies ∧ runtimes ∧
//     deployStates ∧ serviceStatus ∧ triggers — all per-service).
//  3. Sort by (priority asc, id asc) for determinism.
//  4. Render each (atom, service) pair: per-render replacer uses the
//     matched service's hostname/stage. Service-agnostic atoms render
//     once using the global primaryHostnames picker.
//  5. Reject unknown placeholders left in any rendered body.
//
// Compaction-safety invariant: for the same StateEnvelope JSON,
// Synthesize MUST return byte-identical output across calls. No wall-
// clock reads, no map iteration order, no randomness. Service-scoped
// atoms with multiple matching services render once per service in
// envelope's hostname-sorted order.
func Synthesize(envelope StateEnvelope, corpus []KnowledgeAtom) ([]MatchedRender, error) {
	type pending struct {
		atom    KnowledgeAtom
		matches []int // -1 = atom is service-agnostic; otherwise indices into envelope.Services
	}
	pendings := make([]pending, 0, len(corpus))
	for _, atom := range corpus {
		if !atomEnvelopeAxesMatch(atom, envelope) {
			continue
		}
		if !hasServiceScopedAxes(atom.Axes) {
			pendings = append(pendings, pending{atom: atom, matches: []int{-1}})
			continue
		}
		var idxs []int
		for i, svc := range envelope.Services {
			if serviceSatisfiesAxes(svc, atom.Axes) {
				idxs = append(idxs, i)
			}
		}
		if len(idxs) == 0 {
			continue
		}
		pendings = append(pendings, pending{atom: atom, matches: idxs})
	}
	sort.SliceStable(pendings, func(i, j int) bool {
		if pendings[i].atom.Priority != pendings[j].atom.Priority {
			return pendings[i].atom.Priority < pendings[j].atom.Priority
		}
		return pendings[i].atom.ID < pendings[j].atom.ID
	})

	globalHost, globalStage := primaryHostnames(envelope.Services)
	out := make([]MatchedRender, 0, len(pendings))
	for _, p := range pendings {
		for _, idx := range p.matches {
			var svc *ServiceSnapshot
			host, stage := globalHost, globalStage
			if idx >= 0 {
				svc = &envelope.Services[idx]
				host = svc.Hostname
				stage = svc.StageHostname
			}
			replacer := strings.NewReplacer(
				"{hostname}", host,
				"{stage-hostname}", stage,
				"{project-name}", envelope.Project.Name,
			)
			body := replacer.Replace(p.atom.Body)
			if leak := findUnknownPlaceholder(body); leak != "" {
				return nil, fmt.Errorf("atom %s: unknown placeholder %q in atom body", p.atom.ID, leak)
			}
			out = append(out, MatchedRender{
				AtomID:  p.atom.ID,
				Body:    body,
				Service: svc,
			})
		}
	}
	return out, nil
}

// SynthesizeBodies is the convenience adaptor for callers that only need
// the rendered text bodies (status / develop briefing / bootstrap guide).
// Equivalent to extracting `.Body` from `Synthesize`'s result.
func SynthesizeBodies(envelope StateEnvelope, corpus []KnowledgeAtom) ([]string, error) {
	matches, err := Synthesize(envelope, corpus)
	if err != nil {
		return nil, err
	}
	return BodiesOf(matches), nil
}

// BodiesOf extracts the Body field from a MatchedRender slice. Used by
// callers that don't need the per-atom service binding (e.g. legacy
// rendering paths that join bodies with separators).
func BodiesOf(matches []MatchedRender) []string {
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m.Body)
	}
	return out
}

// atomEnvelopeAxesMatch checks the envelope-wide axes (phase,
// environment, route, step, idleScenario). Service-scoped axes are
// evaluated separately per Synthesize so the matched service identity
// flows through.
func atomEnvelopeAxesMatch(atom KnowledgeAtom, env StateEnvelope) bool {
	if !phaseInSet(env.Phase, atom.Axes.Phases) {
		return false
	}
	if len(atom.Axes.Environments) > 0 && !envInSet(env.Environment, atom.Axes.Environments) {
		return false
	}
	if len(atom.Axes.Routes) > 0 {
		if env.Bootstrap == nil || !routeInSet(env.Bootstrap.Route, atom.Axes.Routes) {
			return false
		}
	}
	if len(atom.Axes.Steps) > 0 {
		if env.Bootstrap == nil || !stepInSet(env.Bootstrap.Step, atom.Axes.Steps) {
			return false
		}
	}
	if len(atom.Axes.IdleScenarios) > 0 {
		if env.Phase != PhaseIdle || !slices.Contains(atom.Axes.IdleScenarios, env.IdleScenario) {
			return false
		}
	}
	return true
}

// hasServiceScopedAxes reports whether the atom declares any axis whose
// match is per-service (modes / strategies / triggers / runtimes /
// deployStates / serviceStatus). Service-agnostic atoms render once
// using the global primaryHostnames picker.
func hasServiceScopedAxes(axes AxisVector) bool {
	return len(axes.Modes) > 0 ||
		len(axes.Strategies) > 0 ||
		len(axes.Triggers) > 0 ||
		len(axes.Runtimes) > 0 ||
		len(axes.DeployStates) > 0 ||
		len(axes.ServiceStatuses) > 0
}

// serviceSatisfiesAxes returns true when this single service satisfies
// every service-scoped axis declared on the atom. Empty axis = wildcard.
// Mirrors the pre-C2 anyServiceMatchesAll loop body but exposes the
// per-service decision so Synthesize can bind placeholder substitution
// to the matched service.
func serviceSatisfiesAxes(svc ServiceSnapshot, axes AxisVector) bool {
	if len(axes.Modes) > 0 && !slices.Contains(axes.Modes, svc.Mode) {
		return false
	}
	if len(axes.Strategies) > 0 && !slices.Contains(axes.Strategies, svc.Strategy) {
		return false
	}
	if len(axes.Triggers) > 0 && !slices.Contains(axes.Triggers, svc.Trigger) {
		return false
	}
	if len(axes.Runtimes) > 0 && !slices.Contains(axes.Runtimes, svc.RuntimeClass) {
		return false
	}
	if len(axes.DeployStates) > 0 {
		if !svc.Bootstrapped {
			return false
		}
		state := DeployStateNeverDeployed
		if svc.Deployed {
			state = DeployStateDeployed
		}
		if !slices.Contains(axes.DeployStates, state) {
			return false
		}
	}
	if len(axes.ServiceStatuses) > 0 && !slices.Contains(axes.ServiceStatuses, svc.Status) {
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
	order := []topology.RuntimeClass{topology.RuntimeDynamic, topology.RuntimeImplicitWeb, topology.RuntimeStatic}
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
	"{path}":             {}, // dev-server health path (/, /api/health, /status, ...)
	"{task-id}":          {}, // harness background-task id (Claude Code's Bash run_in_background id)
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
		// Skip `%{...}` curl/printf format specifiers — legitimate content
		// inside shell command examples (e.g. `curl -w '%{http_code}'`).
		if open > 0 && (body[open-1] == '$' || body[open-1] == '%') {
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
// stateless workflow (strategy setup, export). These workflows don't own a
// session; callers pass a pre-built envelope carrying whatever service-
// scoped context the atoms need (strategy, trigger, mode). For workflows
// that only filter on phase+environment (e.g. export), callers use
// SynthesizeImmediatePhase as a thin wrapper.
func SynthesizeImmediateWorkflow(env StateEnvelope) (string, error) {
	corpus, err := LoadAtomCorpus()
	if err != nil {
		return "", err
	}
	matches, err := Synthesize(env, corpus)
	if err != nil {
		return "", err
	}
	return strings.Join(BodiesOf(matches), "\n\n---\n\n"), nil
}

// SynthesizeImmediatePhase is the minimal form: phase + env, no services.
// Matches the original SynthesizeImmediateWorkflow signature for callers
// (e.g. export) that don't need service context.
func SynthesizeImmediatePhase(phase Phase, env Environment) (string, error) {
	return SynthesizeImmediateWorkflow(StateEnvelope{Phase: phase, Environment: env})
}

// SynthesizeStrategySetup returns the strategy-setup guidance for a given
// runtime and per-service snapshots. Wraps the envelope shape that
// PhaseStrategySetup atoms expect so tool handlers don't construct
// StateEnvelope inline.
func SynthesizeStrategySetup(rt runtime.Info, snapshots []ServiceSnapshot) (string, error) {
	return SynthesizeImmediateWorkflow(StateEnvelope{
		Phase:       PhaseStrategySetup,
		Environment: DetectEnvironment(rt),
		Services:    snapshots,
	})
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
