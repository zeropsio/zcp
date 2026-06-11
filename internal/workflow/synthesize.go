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
//
// Scope-narrowing (audit F9 / Lever B): when envelope.WorkSession is
// set with a non-empty Services list, per-service axis matching narrows
// to in-scope hostnames only. Project services outside the work session
// scope contribute no per-service axis matches — atoms with per-service
// axes (modes / closeDeployModes / etc.) only fire for scope services.
// Atoms with no per-service axes ("service-agnostic") still render once
// per envelope, unaffected by scope. WorkSession=nil (idle phase, status
// without active session) preserves full-project axis matching.
func Synthesize(envelope StateEnvelope, corpus []KnowledgeAtom) ([]MatchedRender, error) {
	type pending struct {
		atom    KnowledgeAtom
		matches []int // -1 = atom is service-agnostic; otherwise indices into envelope.Services
	}
	// Heal empty deploy dimensions at the matcher boundary (same
	// normalizeDeployDims the snapshot builders apply — TOPO-1). The axis
	// matcher is exact-match (`slices.Contains`, which "" never satisfies),
	// so an envelope built outside the production snapshot path (tests,
	// fixtures) with an unset dimension would silently fail axes like
	// gitPushStates:[unconfigured, broken] that the healed production
	// snapshot satisfies. Copied first — the caller's slice stays untouched.
	healed := make([]ServiceSnapshot, len(envelope.Services))
	copy(healed, envelope.Services)
	for i := range healed {
		normalizeDeployDims(&healed[i])
	}
	envelope.Services = healed
	scope := workSessionScopeSet(envelope)
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
			if scope != nil && !scope[svc.Hostname] {
				continue
			}
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
	// seen dedupes (atomID, post-substitution body) tuples. Two renders of
	// the same atom that produce byte-identical text deliver the agent the
	// same instruction twice — pure waste. The natural case is an atom with
	// a service-scoped axis (e.g. runtimes) but no per-service placeholder
	// in its body, so per-service substitution is a no-op. After dedup the
	// atom renders 1× via whichever service got there first; downstream
	// consumers (RenderStatus / SynthesizeBodies / BodiesOf) read bodies,
	// not the per-render Service field, so no caller sees the collapse
	// (synthesize.go::BodiesOf, render.go::renderGuidance, tools/workflow.go
	// ::handleStatus). Cmds atoms (execute-cmds, verify-cmds, promote-stage)
	// have {hostname}/{stage-hostname} substitutions in their bodies, so
	// their post-substitution bodies differ per service — dedup correctly
	// leaves them at N renders.
	//
	// IMPORTANT: dedup runs AFTER placeholder substitution, so identical
	// bodies imply identical instructions (no per-host context lost).
	seen := make(map[string]struct{}, len(pendings))
	for _, p := range pendings {
		if p.atom.Reference {
			// Pointer-render: a reference atom that survived axis filtering
			// emits a one-line on-demand-fetch stub instead of its body. The
			// body stays in the corpus (single owner) and resolves via
			// zerops_knowledge uri="zerops://atoms/<id>" (the unified pull
			// retrieval). One stub per atom regardless of how many services
			// matched — the stub carries no per-service substitution, so
			// service identity is irrelevant.
			body := referenceStub(p.atom)
			key := p.atom.ID + "\x00" + body
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, MatchedRender{AtomID: p.atom.ID, Body: body, Service: nil})
			continue
		}
		if p.atom.Axes.MultiService == MultiServiceAggregate {
			// Aggregate mode: render once with `{services-list:TEMPLATE}`
			// directives expanded over the matching services. Outside any
			// directive the body sees the global primaryHostnames picker —
			// same fallback contract as service-agnostic atoms.
			matched := make([]ServiceSnapshot, 0, len(p.matches))
			for _, idx := range p.matches {
				if idx >= 0 {
					matched = append(matched, envelope.Services[idx])
				}
			}
			expanded, err := expandServicesListDirectives(p.atom.Body, matched)
			if err != nil {
				return nil, fmt.Errorf("atom %s: %w", p.atom.ID, err)
			}
			body := strings.NewReplacer(
				"{hostname}", globalHost,
				"{stage-hostname}", globalStage,
				"{project-name}", envelope.Project.Name,
			).Replace(expanded)
			if leak := findUnknownPlaceholder(body); leak != "" {
				return nil, fmt.Errorf("atom %s: unknown placeholder %q in atom body", p.atom.ID, leak)
			}
			key := p.atom.ID + "\x00" + body
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, MatchedRender{
				AtomID:  p.atom.ID,
				Body:    body,
				Service: nil,
			})
			continue
		}
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
			key := p.atom.ID + "\x00" + body
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, MatchedRender{
				AtomID:  p.atom.ID,
				Body:    body,
				Service: svc,
			})
		}
	}
	return out, nil
}

// referenceStub renders a `reference: true` atom as a one-line pointer
// instead of its body. The stub names the topic (atom.Title) and the exact
// on-demand fetch that returns the full body. Format is stable so callers
// (goldens, coverage pins, the agent) can recognize a pointer-rendered atom.
//
// The fetch is the unified pull retrieval (spec-knowledge-architecture.md §4):
// `zerops_knowledge uri="zerops://atoms/<id>"` resolves via the tool-layer
// adapter (LookupReferenceAtomBody against the same embedded corpus). It
// ALWAYS resolves for reference atoms — TestReferenceAtoms_PointersResolve
// guarantees every Reference atomId is fetchable (no dead pointer, the
// masking-fallback failure mode) and envelope-substitution-free (so the
// placeholder-free fetch is safe).
func referenceStub(atom KnowledgeAtom) string {
	return fmt.Sprintf("**%s** — pull on demand: `zerops_knowledge uri=\"zerops://atoms/%s\"`",
		atom.Title, atom.ID)
}

// expandServicesListDirectives replaces each `{services-list:TEMPLATE}`
// directive in body with one rendering of TEMPLATE per matching service,
// joined with newlines. TEMPLATE may contain `{hostname}` and
// `{stage-hostname}` placeholder tokens; the expander substitutes them
// per service. Brace-matched parsing tracks nesting depth so TEMPLATE
// can carry placeholders without escape characters: `{` increments depth,
// `}` decrements, and the directive ends when depth returns to zero.
//
// Empty service list collapses the directive to "" (no error). Unbalanced
// directives — opening prefix without a matching close — return an error
// so the build fails loudly instead of silently emitting raw template text.
//
// Engine ticket E1: aggregate-mode atoms use this to enumerate matching
// services without duplicating the surrounding prose. The four migrated
// atoms (execute, verify, promote-stage, dynamic-runtime-start-container)
// are the initial consumers; further atoms migrate as the corpus drift.
func expandServicesListDirectives(body string, services []ServiceSnapshot) (string, error) {
	const prefix = "{services-list:"
	var out strings.Builder
	i := 0
	for i < len(body) {
		idx := strings.Index(body[i:], prefix)
		if idx < 0 {
			out.WriteString(body[i:])
			return out.String(), nil
		}
		out.WriteString(body[i : i+idx])
		start := i + idx + len(prefix)
		depth := 1
		j := start
		for j < len(body) && depth > 0 {
			switch body[j] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					break
				}
			}
			if depth == 0 {
				break
			}
			j++
		}
		if depth != 0 {
			return "", fmt.Errorf("services-list directive starting at offset %d is unterminated", i+idx)
		}
		template := body[start:j]
		for k, svc := range services {
			if k > 0 {
				out.WriteByte('\n')
			}
			out.WriteString(strings.NewReplacer(
				"{hostname}", svc.Hostname,
				"{stage-hostname}", svc.StageHostname,
			).Replace(template))
		}
		i = j + 1
	}
	return out.String(), nil
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
// environment, route, step, idleScenario, envelopeDeployStates).
// Service-scoped axes are evaluated separately per Synthesize so the
// matched service identity flows through.
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
	if len(atom.Axes.EnvelopeDeployStates) > 0 && !envelopeDeployStateMatches(env.Services, atom.Axes.EnvelopeDeployStates) {
		return false
	}
	if len(atom.Axes.ExportStatuses) > 0 && !slices.Contains(atom.Axes.ExportStatuses, env.ExportStatus) {
		return false
	}
	if len(atom.Axes.ManagedTypes) > 0 && !envelopeHasManagedType(env.Services, atom.Axes.ManagedTypes) {
		return false
	}
	return true
}

// envelopeHasManagedType reports whether the envelope contains a managed
// service whose bare base type is in want (RC-D / F7). Bare type is the
// canonical form before the mode-suffix + version (postgresql:single@18 →
// postgresql). Lets per-managed-type guidance fire only for dep types
// actually in scope.
func envelopeHasManagedType(services []ServiceSnapshot, want []string) bool {
	for _, svc := range services {
		if svc.RuntimeClass != topology.RuntimeManaged {
			continue
		}
		base, _, _ := strings.Cut(topology.CanonicalBareForm(svc.TypeVersion), "@")
		if slices.Contains(want, base) {
			return true
		}
	}
	return false
}

// envelopeDeployStateMatches reports whether ANY bootstrapped service in
// the envelope satisfies one of the atom's declared envelope-scoped deploy
// states. Used by atoms that carry envelope-level guidance gated on the
// project containing at least one matching service. Service-scoped
// DeployStates remains the default — see AxisVector.EnvelopeDeployStates
// docs for when to pick which.
//
// Bootstrapped=false services are skipped: deploy state is only meaningful
// once the bootstrap pipeline has stamped a service. This mirrors
// serviceSatisfiesAxes's handling of DeployStates.
func envelopeDeployStateMatches(services []ServiceSnapshot, want []DeployState) bool {
	for _, svc := range services {
		if !svc.Bootstrapped {
			continue
		}
		state := DeployStateNeverDeployed
		if svc.Deployed {
			state = DeployStateDeployed
		}
		if slices.Contains(want, state) {
			return true
		}
	}
	return false
}

// workSessionScopeSet returns the set of in-scope hostnames from the
// envelope's WorkSession, or nil when there is no active session (idle
// phase, status without an active develop session). The nil return is
// the signal "no scope filter — match against full env.Services".
//
// Lever B of audit F9: per-service axis matching narrows to scope when
// a session is active so atoms like `develop-close-mode-auto-*` only
// fire for in-scope services rather than the project's full service
// list. WorkSession=nil leaves matching unaffected so idle / status
// flows render the same surface they always did.
func workSessionScopeSet(envelope StateEnvelope) map[string]bool {
	if envelope.WorkSession == nil || len(envelope.WorkSession.Services) == 0 {
		return nil
	}
	scope := make(map[string]bool, len(envelope.WorkSession.Services))
	for _, h := range envelope.WorkSession.Services {
		// Out-of-scope services ("leave this untouched this session") must not
		// receive per-service COMMAND guidance — close-mode DECISION lists,
		// promote-stage templates, per-service deploy Next entries. Including
		// them told the agent to deploy/promote a service the user explicitly
		// declared off-limits (Wave-6 existing-standard-appdev-only-reminders:
		// appstage outOfScope → got a "Promote dev to stage" template + a deploy
		// Next). They stay VISIBLE only via the WorkSession "Out of scope this
		// session" reminder, which render.go derives from ws.Roles directly —
		// independent of synthesis — so dropping them here does not hide them.
		// Deferred services remain IN scope: the agent declared them part of the
		// work, just not gating auto-close.
		if envelope.WorkSession.Roles[h] == RoleOutOfScope {
			continue
		}
		scope[h] = true
	}
	return scope
}

// hasServiceScopedAxes reports whether the atom declares any axis whose
// match is per-service (modes / closeDeployModes / gitPushStates /
// buildIntegrations / runtimes / deployStates / serviceStatus).
// Service-agnostic atoms render once using the global primaryHostnames
// picker.
func hasServiceScopedAxes(axes AxisVector) bool {
	return len(axes.Modes) > 0 ||
		len(axes.CloseDeployModes) > 0 ||
		len(axes.GitPushStates) > 0 ||
		len(axes.BuildIntegrations) > 0 ||
		len(axes.Runtimes) > 0 ||
		len(axes.RuntimeBases) > 0 ||
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
	if len(axes.CloseDeployModes) > 0 && !slices.Contains(axes.CloseDeployModes, svc.CloseDeployMode) {
		return false
	}
	if len(axes.GitPushStates) > 0 && !slices.Contains(axes.GitPushStates, svc.GitPushState) {
		return false
	}
	if len(axes.BuildIntegrations) > 0 && !slices.Contains(axes.BuildIntegrations, svc.BuildIntegration) {
		return false
	}
	if len(axes.Runtimes) > 0 && !slices.Contains(axes.Runtimes, svc.RuntimeClass) {
		return false
	}
	if len(axes.RuntimeBases) > 0 && !matchesRuntimeBase(svc.TypeVersion, axes.RuntimeBases) {
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

// matchesRuntimeBase reports whether a service's TypeVersion belongs to
// one of the listed runtime bases. The base is the canonical bare form
// before the `@version` suffix:
//
//	nodejs@22            → nodejs
//	alpine/nodejs@22     → nodejs    (composite OS prefix stripped)
//	ubuntu/python@3.12   → python    (composite OS prefix stripped)
//	php-nginx@8.4        → php-nginx (no family expansion — concrete base)
//	postgresql:single@18 → postgresql (managed mode suffix stripped)
//
// Empty `bases` means the axis is not enforced (caller should not invoke
// this function in that case).
func matchesRuntimeBase(typeVersion string, bases []string) bool {
	bare := topology.CanonicalBareForm(typeVersion)
	base, _, _ := strings.Cut(bare, "@")
	return slices.Contains(bases, base)
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
	"{workingDir}":     {}, // local-machine checkout dir for git-push setup
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
