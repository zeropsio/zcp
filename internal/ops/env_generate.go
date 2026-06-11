package ops

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

// EnvGenerateDotenvOptions controls preview / force behavior for the
// generate-dotenv operation.
//
// Preview=true builds the plan, computes the diff, and returns both
// without writing. Used to show "what would change" before committing.
//
// Force=true bypasses the refuse-on-unowned safety check (caller has
// confirmed they want any user-direct .env edits to be discarded).
type EnvGenerateDotenvOptions struct {
	Preview bool
	Force   bool
}

// EnvDotenvResult contains the result of .env file generation.
//
// VPNHint is populated when at least one managed service referenced in
// the resolved env vars looked unreachable — local dev then almost
// certainly needs `zcli vpn up`. The probe is best-effort and non-
// blocking: even if probes fail, the .env file still lands. An empty
// hint means every probed host was reachable (or no probe happened
// because ServiceStack carried no port info).
//
// OmittedPlatformKeys lists project-level keys that the platform-
// internals denylist filtered out before writing .env (deploy tokens,
// CDN URLs, runtime placeholders). Surfaced for transparency so an
// agent can confirm a missing key was filtered intentionally rather
// than missing from the project. Yaml-defined overrides for the same
// names still ship to .env and are NOT listed here.
//
// Setup names the zerops.yaml setup block that was rendered (auto-
// picked from a single-block yaml, or supplied explicitly).
//
// Warnings carries non-fatal advisories (e.g. legacy serviceHostname
// usage). Empty when nothing notable happened.
//
// Diff is populated in two situations:
//   - Preview mode (caller passed Preview=true): no write occurred,
//     Diff describes what would change.
//   - Refused mode (Refused=true): write was refused due to unowned
//     edits in the existing .env, Diff lists the keys at risk.
//
// Refused signals the safety gate fired — caller must either move
// the unowned keys to .env.local, or retry with Force=true to discard
// them. The .env file on disk is unchanged when Refused=true.
type EnvDotenvResult struct {
	Path                string   `json:"path"`
	Setup               string   `json:"setup"`
	Services            int      `json:"services"`
	Variables           int      `json:"variables"`
	VPNHint             string   `json:"vpnHint,omitempty"`
	OmittedPlatformKeys []string `json:"omittedPlatformKeys,omitempty"`
	Warnings            []string `json:"warnings,omitempty"`
	Diff                *EnvDiff `json:"diff,omitempty"`
	Preview             bool     `json:"preview,omitempty"`
	Refused             bool     `json:"refused,omitempty"`
}

// platformInternalKeys is the denylist of project-level keys that ship
// inside a Zerops project for platform-internal use and have no value
// for a local .env (deploy tokens, CDN URLs, runtime-only placeholders).
// A user `git add -A` after generate-dotenv would otherwise publish the
// deploy token; verified live in suite 20260506-145922.
//
// The list is symmetric across container and local — these keys are
// platform-internal in any environment.
//
// NOT denylisted: APP_KEY, APP_SECRET, framework auto-secrets — local
// apps still need those. The .env remains secret-bearing on purpose;
// gitignore is the right protection. The denylist only removes keys
// that would never be read by user code.
var platformInternalKeys = map[string]bool{
	"ZCP_API_KEY":           true,
	GitTokenEnvKey:          true,
	LaunchTokenEnvKey:       true,
	"envIsolation":          true,
	"sshIsolation":          true,
	"apiCdnUrl":             true,
	"staticCdnUrl":          true,
	"storageCdnUrl":         true,
	"zeropsSubdomainHost":   true,
	"zeropsSubdomainString": true,
}

// maxRefExpansionDepth caps recursive expansion regardless of cycle
// detection — a defensive bound for pathological chains of length N
// where each ref is unique (cycle detection wouldn't fire). 16 levels
// is far past any realistic Zerops env-var template chain.
const maxRefExpansionDepth = 16

// refExpander resolves `${...}` placeholders against the live API.
//
// The interpreter classifies each `${...}` body via the shared
// EnvRefClassifier:
//
//   - Cross-service hit (`${host_var}` matches a live hostname) → fetch
//     host's env vars and look up var. Tried regardless of nesting.
//   - Lone ref AND we're inside a source-service context (recursing
//     through a fetched value) → sibling lookup against the source
//     service's own env vars. Matches Zerops's deploy-time semantics for
//     templates like `connectionString =
//     postgresql://${user}:${password}@${hostname}:${port}` where the
//     lone refs resolve against the source service's siblings.
//   - Lone ref at top level (yaml run.envVariables) → left literal.
//     Project-level vars get appended later via GetProjectEnv; runtime-
//     only placeholders (`${zeropsSubdomainHost}`) resolve at deploy
//     time inside the container.
//
// cache is shared across all expandRefs calls within one
// EnvGenerateDotenv invocation: one GetServiceEnv per touched service,
// regardless of how many refs reference the service or how deeply they
// recurse. The project's full service list and classifier are built once
// in EnvGenerateDotenv and passed in — expandRefs never calls ListServices.
type refExpander struct {
	client       platform.Client
	classifier   *EnvRefClassifier
	serviceIndex map[string]platform.ServiceStack
	cache        map[string][]platform.ServiceEnvVar
	// projectEnv is the project-level env (key→value), the fallback for a
	// LONE ref inside a sibling's value: project vars inherit into every
	// container live (independent of isolation — spec §3), so a sibling
	// value like CONN=${BASE_HOST} resolves against project. The sibling
	// cache (slim + app-version) alone lacks this layer.
	projectEnv map[string]string
}

// expandRefs walks `value` and substitutes resolvable `${...}` refs.
// sourceService is "" at top level (yaml `run.envVariables`); inside a
// recursive call it names the service whose value we're currently
// expanding (lone refs there resolve against that service's siblings).
//
// visited carries the chain of `host.var` keys already resolved on this
// recursion path; re-encountering one is a cycle. Each recursive call
// gets its own copy so siblings at the same level don't false-positive.
//
// Returns:
//   - expanded string (with resolvable refs substituted, unresolved refs
//     left as their original `${...}` literal so partial-resolution is
//     debuggable),
//   - count of unresolved refs the caller can aggregate for error
//     messaging (0 means full success),
//   - infrastructure / cycle errors that abort the whole operation.
func (r *refExpander) expandRefs(ctx context.Context, value, sourceService string, visited map[string]bool, depth int) (string, int, error) {
	if depth > maxRefExpansionDepth {
		return "", 0, fmt.Errorf("ref expansion depth exceeded (>%d) at %q", maxRefExpansionDepth, value)
	}
	matches := FindEnvRefs(value)
	if len(matches) == 0 {
		return value, 0, nil
	}
	var sb strings.Builder
	unresolved := 0
	last := 0
	for _, m := range matches {
		sb.WriteString(value[last:m.Start])

		var svcHost, varName string
		projectFallback := false
		host, varPart, isCross := r.classifier.Classify(m.Body)
		switch {
		case isCross:
			svcHost, varName = host, varPart
		case sourceService != "":
			svcHost, varName = sourceService, m.Body
			// A lone ref inside a sibling's value may name a project var
			// (which inherits into every container live), not the sibling's
			// own — fall back to project env when the sibling lacks it.
			projectFallback = true
		default:
			// Lone ref at top level — leave literal so the platform
			// (project-level vars, runtime placeholders) can resolve it
			// at deploy time.
			sb.WriteString(m.Raw)
			last = m.End
			continue
		}

		key := svcHost + "." + varName
		if visited[key] {
			return "", 0, fmt.Errorf("circular reference at %s: chain re-enters %s", m.Raw, key)
		}

		if _, cached := r.cache[svcHost]; !cached {
			svc, ok := r.serviceIndex[svcHost]
			if !ok {
				if depth == 0 {
					// Top-level cross-service ref to an unknown host —
					// surface a specific error so the agent can fix the
					// yaml. Reuse FindService's "Available: ..." wording
					// for parity with other ops/* errors.
					services := make([]platform.ServiceStack, 0, len(r.serviceIndex))
					for _, s := range r.serviceIndex {
						services = append(services, s)
					}
					_, err := FindService(services, svcHost)
					return "", 0, err
				}
				// Inside a recursive expansion: maybe the fetched
				// template references a host ZCP doesn't model. Leave
				// literal so .env keeps the original ref visible.
				sb.WriteString(m.Raw)
				last = m.End
				unresolved++
				continue
			}
			envs, err := r.client.GetServiceEnv(ctx, svc.ID)
			if err != nil {
				// Wrap as transient — most fetch failures here are
				// VPN/API connectivity, not yaml-invalidity. Caller
				// inspects via errors.As (*RefResolveTransientError).
				return "", 0, &RefResolveTransientError{Service: svcHost, Cause: err}
			}
			// Enrich with yaml-baked run.envVariables from the active app
			// version — the slim /env omits them, so a ref to a sibling's
			// run.envVariables var (e.g. ${app_API_URL}) would otherwise be
			// unresolvable. Lifecycle-aware: nil for managed / never-deployed
			// (those legitimately have no yaml-baked layer). Spec §1/§6.
			//
			// E1: this path deliberately calls AppVersionEnvVars DIRECTLY (not via
			// ServiceHigherLayers) to keep the flat slim∪yaml cache shape. A non-nil
			// ybErr is a LIVE sibling's transient fetch failure (VPN/API), NOT a yaml
			// typo — propagate the same recovery-typed error the slim fetch uses (F4)
			// so the agent gets "run zcli vpn up", not "fix your yaml".
			yb, ybErr := AppVersionEnvVars(ctx, r.client, svc)
			if ybErr != nil {
				return "", 0, &RefResolveTransientError{Service: svcHost, Cause: ybErr}
			}
			if len(yb) > 0 {
				envs = append(envs, yb...)
			}
			r.cache[svcHost] = envs
		}

		rawVal, found := findEnvValue(r.cache[svcHost], varName)
		if !found && projectFallback {
			if pv, ok := r.projectEnv[varName]; ok {
				rawVal, found = pv, true
			}
		}
		if !found {
			sb.WriteString(m.Raw)
			last = m.End
			// A never-deployed runtime sibling's yaml-baked vars aren't on
			// the platform yet — keep the ref literal but DON'T count it
			// unresolved (don't hard-fail the local .env for a ref that'll
			// resolve once the sibling deploys). A managed/live miss IS a
			// real typo → count it (hard error downstream). Spec §1.
			if svc, ok := r.serviceIndex[svcHost]; !ok || !IsRuntimeNeverDeployed(svc) {
				unresolved++
			}
			continue
		}

		nextVisited := make(map[string]bool, len(visited)+1)
		for k := range visited {
			nextVisited[k] = true
		}
		nextVisited[key] = true
		expanded, sub, err := r.expandRefs(ctx, rawVal, svcHost, nextVisited, depth+1)
		if err != nil {
			return "", 0, err
		}
		unresolved += sub
		sb.WriteString(expanded)
		last = m.End
	}
	sb.WriteString(value[last:])
	return sb.String(), unresolved, nil
}

// EnvGenerateDotenv builds an EnvPlan for the given setup target,
// renders it as .env content, and writes the file atomically. It is a
// thin wrapper over BuildEnvPlan + EnvPlan.Render(SinkDotenv).
//
// setup names the zerops.yaml setup block to render. Empty + single-
// block yaml: auto-pick. Empty + multi-block: returns *SetupRequiredError.
// Empty + zero-block yaml: error. Non-empty: validate entry + non-empty
// run.envVariables (back-compat).
//
// `run.envVariables` is the canonical schema location; the JSON schema
// rejects envVariables at the setup-entry top level (only
// build.envVariables / run.envVariables are valid).
func EnvGenerateDotenv(
	ctx context.Context,
	client platform.Client,
	projectID string,
	setup string,
	workingDir string,
	opts EnvGenerateDotenvOptions,
) (*EnvDotenvResult, error) {
	if workingDir == "" {
		workingDir = "."
	}

	doc, err := ParseZeropsYml(workingDir)
	if err != nil {
		return nil, fmt.Errorf("generate-dotenv: %w", err)
	}

	// Auto-pick when caller didn't specify and yaml has a single block.
	// Multi-block yaml with empty setup falls through to BuildEnvPlan
	// which returns *SetupRequiredError listing available setups.
	if setup == "" {
		setups := doc.SetupNames()
		if len(setups) == 1 {
			setup = setups[0]
		}
	}

	// When the caller named a setup explicitly, require the entry to exist.
	// We do NOT require non-empty run.envVariables: a setup with no
	// run.envVariables is still a valid local-bridge shape when project
	// envs (or .env.local) contribute — BuildEnvPlan layers those in. The
	// "nothing to render" case is caught below on the assembled plan, not
	// on the run.envVariables input (which would reject a valid
	// project-only .env).
	if setup != "" && doc.FindEntry(setup) == nil {
		return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("no setup entry for %q in zerops.yaml", setup),
			"Check that zerops.yaml has a setup: entry matching the supplied name")
	}

	// Single ListServices call powers both the plan builder (for
	// classifier + index) and the VPN probe (for port info). Without
	// this, BuildEnvPlan would list once and probe would list again —
	// pinned by TestEnvGenerateDotenv_ListServices_CalledOncePerBatch.
	services, err := ListProjectServices(ctx, client, projectID)
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}

	plan, err := buildEnvPlanWith(ctx, client, projectID, setup, workingDir, services, nil)
	if err != nil {
		// Translate the BuildEnvPlan ref-resolve error into the legacy
		// PlatformError shape the existing tests / MCP clients expect.
		msg := err.Error()
		if strings.Contains(msg, "unresolved ${} refs in:") {
			parts := strings.SplitN(msg, "unresolved ${} refs in: ", 2)
			if len(parts) == 2 {
				return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
					fmt.Sprintf("could not resolve env vars: %s", parts[1]),
					"Check that referenced services are running and have the expected env var keys")
			}
		}
		return nil, err
	}

	// Result-based "nothing to render" guard (replaces the old
	// run.envVariables-input guard): error only when NO channel —
	// run.envVariables, project envs, .env.local — produced a key. A
	// setup with no run.envVariables but contributing project envs renders
	// fine and does not reach here.
	if len(plan.Keys) == 0 {
		return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("no env vars to render for setup %q — run.envVariables is empty and no project envs or .env.local contribute", setup),
			"Add run.envVariables to zerops.yaml, set project-level env vars, or add a .env.local")
	}

	envPath := filepath.Join(workingDir, ".env")
	diff, err := plan.DiffAgainstExisting(envPath)
	if err != nil {
		return nil, fmt.Errorf("compute diff: %w", err)
	}

	result := &EnvDotenvResult{
		Path:                envPath,
		Setup:               plan.Setup,
		Services:            len(plan.TouchedServiceHostnames),
		Variables:           len(plan.Keys),
		OmittedPlatformKeys: plan.OmittedPlatformKeys,
		Diff:                diff,
	}

	// Preview mode: surface plan + diff, do NOT write. Returns before
	// any I/O so the caller can inspect what would change without
	// touching the .env on disk.
	if opts.Preview {
		result.Preview = true
		return result, nil
	}

	// Refuse-on-unowned safety gate (docs/spec-env-handling.md §6.2):
	// the existing .env contains keys not produced by any source —
	// those are user-direct edits that would be lost on write. Caller
	// must either move them to .env.local or set Force=true.
	if diff.HasUnowned() && !opts.Force {
		result.Refused = true
		return result, nil
	}

	// Advisory lock around the write path (§6.4). Concurrent regens of
	// the same setup serialize; different setups don't block each
	// other. Acquired only here (not in preview / refused branches —
	// those are read-only, no contention).
	release, err := acquireDotenvLock(workingDir, plan.Setup)
	if err != nil {
		return nil, fmt.Errorf("acquire lock: %w", err)
	}
	defer release()

	rendered, err := plan.Render(SinkDotenv)
	if err != nil {
		return nil, fmt.Errorf("render dotenv: %w", err)
	}
	if err := atomicWriteFile(envPath, rendered, 0600); err != nil {
		return nil, fmt.Errorf("write .env: %w", err)
	}

	// Best-effort VPN probe. Only runs when cross-service refs were
	// used (TouchedServiceHostnames non-empty); a .env with only
	// project-level vars doesn't need VPN to work locally. The probe
	// reuses the services slice already fetched above.
	if len(plan.TouchedServiceHostnames) > 0 {
		if hint := probeTouchedServices(ctx, projectID, services, plan.TouchedServiceHostnames); hint != "" {
			result.VPNHint = hint
		}
	}
	return result, nil
}

// atomicWriteFile writes data to path via a temp file + rename. The
// rename is the atomic step on POSIX filesystems — readers either see
// the old file or the new one, never a half-written state. Concurrent
// regens of the same file still race on read-modify-write semantics;
// Phase 0E adds an advisory lock for serialization.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp.*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		cleanup()
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// probeTouchedServices attempts one TCP dial per touched managed
// service. Returns a hint string when any probe fails, empty when
// all succeed (or no port info is available to probe against). The
// services slice is reused from the caller's earlier ListServices —
// pinned by TestEnvGenerateDotenv_ListServices_CalledOncePerBatch.
func probeTouchedServices(ctx context.Context, projectID string, services []platform.ServiceStack, touchedHosts []string) string {
	if len(touchedHosts) == 0 {
		return ""
	}
	serviceIndex := make(map[string]platform.ServiceStack, len(services))
	for _, s := range services {
		serviceIndex[s.Name] = s
	}
	for _, host := range touchedHosts {
		svc, ok := serviceIndex[host]
		if !ok || len(svc.Ports) == 0 {
			continue
		}
		if !ProbeManagedReachable(ctx, host, svc.Ports[0].Port) {
			return fmt.Sprintf("Managed service %q not reachable on port %d — run `zcli vpn up %s` and retry local dev.", host, svc.Ports[0].Port, projectID)
		}
	}
	return ""
}

// findEnvValue returns a key's value and whether it was present. The found
// bool is load-bearing: a legitimately-empty value ("") must be distinguished
// from an absent key, or an empty sibling var gets miscounted as unresolved.
func findEnvValue[T platform.EnvAccessor](envs []T, key string) (string, bool) {
	for _, e := range envs {
		if e.GetKey() == key {
			return e.GetContent(), true
		}
	}
	return "", false
}
