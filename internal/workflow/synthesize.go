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

	// Service-scoped axes (mode, strategy, trigger, runtime, deployState) must
	// all be satisfied by the SAME service. Disjunction (ANY service satisfies
	// axis X while OTHER satisfies Y) would fire atoms whose placeholder body
	// references a service that the atom isn't actually about — e.g.
	// `develop-strategy-review (deployStates=[deployed], strategies=[unset])`
	// would fire when service A is deployed+push-dev and service B is
	// never-deployed+unset, despite no single service being both deployed AND
	// unset.
	hasServiceScope := len(atom.Axes.Modes) > 0 ||
		len(atom.Axes.Strategies) > 0 ||
		len(atom.Axes.Triggers) > 0 ||
		len(atom.Axes.Runtimes) > 0 ||
		len(atom.Axes.DeployStates) > 0 ||
		len(atom.Axes.ServiceStatuses) > 0
	if hasServiceScope && !anyServiceMatchesAll(env.Services, atom.Axes) {
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

// anyServiceMatchesAll reports whether any single service satisfies every
// declared service-scoped axis. An empty axis slice is a wildcard for that
// axis. Non-bootstrapped services are skipped for the deployState check —
// they have no tracked deploy state, and firing first-deploy atoms on pure-
// adoption services bootstrap never touched would be a regression.
func anyServiceMatchesAll(services []ServiceSnapshot, axes AxisVector) bool {
	for _, svc := range services {
		if len(axes.Modes) > 0 && !slices.Contains(axes.Modes, svc.Mode) {
			continue
		}
		if len(axes.Strategies) > 0 && !slices.Contains(axes.Strategies, svc.Strategy) {
			continue
		}
		if len(axes.Triggers) > 0 && !slices.Contains(axes.Triggers, svc.Trigger) {
			continue
		}
		if len(axes.Runtimes) > 0 && !slices.Contains(axes.Runtimes, svc.RuntimeClass) {
			continue
		}
		if len(axes.DeployStates) > 0 {
			if !svc.Bootstrapped {
				continue
			}
			state := DeployStateNeverDeployed
			if svc.Deployed {
				state = DeployStateDeployed
			}
			if !slices.Contains(axes.DeployStates, state) {
				continue
			}
		}
		if len(axes.ServiceStatuses) > 0 && !slices.Contains(axes.ServiceStatuses, svc.Status) {
			continue
		}
		return true
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
	bodies, err := Synthesize(env, corpus)
	if err != nil {
		return "", err
	}
	return strings.Join(bodies, "\n\n---\n\n"), nil
}

// SynthesizeImmediatePhase is the minimal form: phase + env, no services.
// Matches the original SynthesizeImmediateWorkflow signature for callers
// (e.g. export) that don't need service context.
func SynthesizeImmediatePhase(phase Phase, env Environment) (string, error) {
	return SynthesizeImmediateWorkflow(StateEnvelope{Phase: phase, Environment: env})
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
