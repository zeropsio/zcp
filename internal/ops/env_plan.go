package ops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// EnvSource identifies which channel produced a key's final rendered
// value. See docs/spec-env-handling.md §3 for the channel model.
type EnvSource int

const (
	SourceProject          EnvSource = iota // Zerops project envVariables
	SourceYAMLSetup                         // zerops.yaml run.envVariables (refs resolved)
	SourceLocalOverlay                      // .env.local (user-authored)
	SourceBrownfieldImport                  // reserved for Theme 3 brownfield-adopt
)

func (s EnvSource) String() string {
	switch s {
	case SourceProject:
		return "project"
	case SourceYAMLSetup:
		return "yaml-setup"
	case SourceLocalOverlay:
		return "local-overlay"
	case SourceBrownfieldImport:
		return "brownfield-import"
	default:
		return fmt.Sprintf("unknown-source(%d)", int(s))
	}
}

// EnvScope describes the intended use of a key — informs UX surfaces
// (dry-run output, lifecycle atom guidance).
type EnvScope int

const (
	ScopeShared          EnvScope = iota // same value local + deployed
	ScopeDeployedRuntime                 // deployed-only (e.g. APP_ENV=production)
	ScopeLocalOverride                   // local-only override
	ScopeManagedRef                      // resolved from ${svc_var}
)

func (s EnvScope) String() string {
	switch s {
	case ScopeShared:
		return "shared"
	case ScopeDeployedRuntime:
		return "deployed-runtime"
	case ScopeLocalOverride:
		return "local-override"
	case ScopeManagedRef:
		return "managed-ref"
	default:
		return fmt.Sprintf("unknown-scope(%d)", int(s))
	}
}

// ConflictStatus tracks whether a key's final value comes from a
// single source (clean), an overlay overriding base (overridden), or
// a higher-precedence base shadowing a lower one (shadowed).
type ConflictStatus int

const (
	StatusClean      ConflictStatus = iota
	StatusOverridden                // .env.local won over base source
	StatusShadowed                  // higher base precedence won (yaml > project)
)

func (s ConflictStatus) String() string {
	switch s {
	case StatusClean:
		return "clean"
	case StatusOverridden:
		return "overridden"
	case StatusShadowed:
		return "shadowed"
	default:
		return fmt.Sprintf("unknown-status(%d)", int(s))
	}
}

// EnvKey carries one rendered env entry plus metadata sufficient for
// any sink (file write, dry-run diff, shell export, future surfaces).
type EnvKey struct {
	Key      string
	Value    string
	Source   EnvSource
	Scope    EnvScope
	Conflict ConflictStatus
}

// EnvPlan is a typed, formatter-agnostic representation of the
// rendered env state for a single setup target. Renderers are thin
// formatters over this plan (see Render). The plan does not write
// any file — all I/O lives in the caller.
//
// The provenance fields (OmittedPlatformKeys, TouchedServiceHostnames)
// capture build-time metadata callers need for UX surfaces:
//   - OmittedPlatformKeys lets the agent see which project-level keys
//     were filtered (deploy tokens, CDN URLs, runtime placeholders)
//     so it can confirm a missing key was filtered intentionally.
//   - TouchedServiceHostnames names the managed services whose env
//     was fetched during ${svc_var} resolution; useful for VPN-probe
//     hints and for telemetry on which services are referenced.
type EnvPlan struct {
	Setup                   string
	CWD                     string
	Keys                    []EnvKey
	OmittedPlatformKeys     []string
	TouchedServiceHostnames []string
	Generated               time.Time
}

// EnvSink selects the output format for Render.
type EnvSink int

const (
	SinkDotenv      EnvSink = iota // .env file content (KEY=VALUE lines)
	SinkShellExport                // export KEY=VALUE lines for shell-source
	SinkDryRunDiff                 // human-readable diff vs existing .env
)

// EnvDiff describes what would change if the plan were rendered to
// the existing .env file. See docs/spec-env-handling.md §6.1.
//
// Without persistent manifest tracking, ZCP cannot distinguish a
// "previously-managed key the user removed from sources" from a
// "user-direct edit to .env". Both cases surface as Unowned. The
// refuse-on-unowned policy (§6.2) protects user edits regardless of
// origin: caller must pass force=true to discard them.
type EnvDiff struct {
	Added    []string       `json:"added,omitempty"`    // in plan, not in existing .env
	Modified []DiffModified `json:"modified,omitempty"` // in both, value differs
	Unowned  []string       `json:"unowned,omitempty"`  // in existing .env, not in plan
}

// DiffModified is one entry in EnvDiff.Modified — the key and both
// values so callers can show "what's about to change".
type DiffModified struct {
	Key  string `json:"key"`
	From string `json:"from"`
	To   string `json:"to"`
}

// IsClean returns true when the diff has no Added/Modified/Unowned
// entries — i.e. the existing .env already matches the plan.
func (d *EnvDiff) IsClean() bool {
	return len(d.Added) == 0 && len(d.Modified) == 0 && len(d.Unowned) == 0
}

// HasUnowned returns true when at least one key in the existing .env
// is not produced by any source. Default write refuses in this case
// (per docs/spec-env-handling.md §6.2) unless force=true.
func (d *EnvDiff) HasUnowned() bool {
	return len(d.Unowned) > 0
}

// DiffAgainstExisting computes the change set of rendering the plan
// over the file at envPath. Absent file → every plan key is Added.
// Read errors (other than not-found) are surfaced.
func (p *EnvPlan) DiffAgainstExisting(envPath string) (*EnvDiff, error) {
	existing, err := readDotenv(envPath)
	if err != nil {
		return nil, fmt.Errorf("read existing .env: %w", err)
	}
	planMap := make(map[string]string, len(p.Keys))
	for _, k := range p.Keys {
		planMap[k.Key] = k.Value
	}
	diff := &EnvDiff{}
	planKeys := make([]string, 0, len(planMap))
	for k := range planMap {
		planKeys = append(planKeys, k)
	}
	sort.Strings(planKeys)
	for _, k := range planKeys {
		newV := planMap[k]
		oldV, hadOld := existing[k]
		switch {
		case !hadOld:
			diff.Added = append(diff.Added, k)
		case oldV != newV:
			diff.Modified = append(diff.Modified, DiffModified{Key: k, From: oldV, To: newV})
		}
	}
	existingKeys := make([]string, 0, len(existing))
	for k := range existing {
		if _, inPlan := planMap[k]; !inPlan {
			existingKeys = append(existingKeys, k)
		}
	}
	sort.Strings(existingKeys)
	diff.Unowned = existingKeys
	return diff, nil
}

// readDotenv reads a .env-style file from path and parses it into a
// key→value map. Absent file → empty map + nil error (cleanly handles
// first-time generation). Comments and blanks skipped.
func readDotenv(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	out := map[string]string{}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		eq := strings.Index(line, "=")
		if eq < 1 {
			continue
		}
		out[strings.TrimSpace(line[:eq])] = line[eq+1:]
	}
	return out, nil
}

// SetupRequiredError is returned by BuildEnvPlan when zerops.yaml has
// multiple setup blocks and the caller did not specify which to use.
// Callers detect it via errors.As to surface available setups to the
// agent.
type SetupRequiredError struct {
	Available []string
}

func (e *SetupRequiredError) Error() string {
	return fmt.Sprintf(
		"setup parameter required; zerops.yaml has multiple setup blocks: %s",
		strings.Join(e.Available, ", "),
	)
}

// RefResolveTransientError indicates a ${svc_var} reference could not
// be resolved due to a likely-transient cause (Zerops API unreachable,
// VPN down, service not yet RUNNING). Distinct from yaml-invalid
// errors: classification lets the agent surface "retry after `zcli
// vpn up`" vs "fix the yaml".
//
// Caller's existing .env is untouched on this error (BuildEnvPlan
// returns before any write occurs). See docs/spec-env-handling.md §6.3.
type RefResolveTransientError struct {
	Service string
	Cause   error
}

func (e *RefResolveTransientError) Error() string {
	return fmt.Sprintf("transient resolve failure for service %q (likely VPN/API issue): %v", e.Service, e.Cause)
}

func (e *RefResolveTransientError) Unwrap() error {
	return e.Cause
}

// BuildEnvPlan gathers env values from the three input channels and
// returns a typed plan. Channels merge with precedence: project (low)
// < yaml-setup < local-overlay (high). See docs/spec-env-handling.md
// §4 for rationale.
//
// setup selects a zerops.yaml setup block by name. Empty + single-block
// yaml: auto-pick. Empty + multi-block: returns *SetupRequiredError.
// Non-empty + not found: returns a "no such setup" error.
//
// Ref-resolution failures (Zerops API unreachable, unknown service
// referenced, missing variable) are surfaced as errors; the caller
// is responsible for not writing on error so prior .env stays intact.
func BuildEnvPlan(
	ctx context.Context,
	client platform.Client,
	projectID string,
	setup string,
	cwd string,
) (*EnvPlan, error) {
	return buildEnvPlanWith(ctx, client, projectID, setup, cwd, nil, nil)
}

// buildEnvPlanWith is the internal builder that accepts a pre-listed
// services slice and an optional brownfield-overrides map. Callers
// that need to use the same services list for follow-on work (e.g.
// EnvGenerateDotenv's VPN probe) pass services here to avoid a
// redundant ListServices call. Pass nil to have the builder list
// services itself when refs need resolving.
//
// brownfieldOverrides carries values from a brownfield-adopt pass
// (Theme 3); they merge between project and yaml-setup precedence —
// "user's previous truth" should win over project envVariables but
// be shadowed by anything explicitly named in the new zerops.yaml.
// Pass nil for the common (non-brownfield) path.
// three-channel merge so source precedence is auditable in one place.
//
//nolint:maintidx // single function intentionally consolidates the
func buildEnvPlanWith(
	ctx context.Context,
	client platform.Client,
	projectID string,
	setup string,
	cwd string,
	preListed []platform.ServiceStack,
	brownfieldOverrides map[string]string,
) (*EnvPlan, error) {
	if cwd == "" {
		cwd = "."
	}

	doc, err := ParseZeropsYml(cwd)
	if err != nil {
		return nil, fmt.Errorf("build env plan: %w", err)
	}

	setups := doc.SetupNames()
	switch {
	case setup == "" && len(setups) == 0:
		return nil, fmt.Errorf("build env plan: zerops.yaml has no setup blocks in %s", cwd)
	case setup == "" && len(setups) == 1:
		setup = setups[0]
	case setup == "" && len(setups) > 1:
		return nil, &SetupRequiredError{Available: setups}
	}

	entry := doc.FindEntry(setup)
	if entry == nil {
		return nil, platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("setup %q not in zerops.yaml; available: %s", setup, strings.Join(setups, ", ")),
			"Pick one of the listed setup names",
		)
	}

	keys := make(map[string]EnvKey)
	var omittedPlatformKeys []string

	// Pre-collect yaml-set keys so the denylist omission only applies to
	// project keys the user did NOT explicitly request via yaml. When a
	// user puts a denylisted key (e.g. ZCP_API_KEY) in run.envVariables,
	// they intend to override it locally; the project value is shadowed
	// rather than filtered, so it must not surface in OmittedPlatformKeys.
	yamlSetKeys := make(map[string]bool, len(entry.Run.EnvVariables))
	for k := range entry.Run.EnvVariables {
		yamlSetKeys[k] = true
	}

	// Channel 1 (lowest precedence): project.envVariables.
	// Filtered through the platform-internals denylist — those keys
	// exist for Zerops platform internals (deploy tokens, CDN URLs,
	// runtime-only placeholders) and have no meaning in a local .env.
	projectEnvs, err := client.GetProjectEnv(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("build env plan: fetch project env: %w", err)
	}
	for _, pe := range projectEnvs {
		if platformInternalKeys[pe.Key] {
			if !yamlSetKeys[pe.Key] {
				omittedPlatformKeys = append(omittedPlatformKeys, pe.Key)
			}
			continue
		}
		keys[pe.Key] = EnvKey{
			Key:      pe.Key,
			Value:    pe.Content,
			Source:   SourceProject,
			Scope:    ScopeShared,
			Conflict: StatusClean,
		}
	}
	sort.Strings(omittedPlatformKeys)

	// Channel 1.5 (between project and yaml-setup): brownfield
	// overrides. These came from a prior `.env` the user authored
	// before adopting Zerops; treated as "previous truth" — wins over
	// project but loses to anything explicitly named in zerops.yaml.
	for k, v := range brownfieldOverrides {
		conflict := StatusClean
		if existing, ok := keys[k]; ok && existing.Source == SourceProject {
			conflict = StatusShadowed
		}
		keys[k] = EnvKey{
			Key:      k,
			Value:    v,
			Source:   SourceBrownfieldImport,
			Scope:    ScopeShared,
			Conflict: conflict,
		}
	}

	// Channel 2 (middle precedence): zerops.yaml run.envVariables.
	// Refs of the form ${svc_var} are resolved against managed-service
	// envs via the existing refExpander.
	expanderCache := make(map[string][]platform.ServiceEnvVar)
	if len(entry.Run.EnvVariables) > 0 {
		services := preListed
		if services == nil {
			listed, listErr := ListProjectServices(ctx, client, projectID)
			if listErr != nil {
				return nil, fmt.Errorf("build env plan: list services: %w", listErr)
			}
			services = listed
		}
		classifier := NewEnvRefClassifier(services)
		serviceIndex := make(map[string]platform.ServiceStack, len(services))
		for _, s := range services {
			serviceIndex[s.Name] = s
		}
		// Project env is the fallback layer for a lone ref inside a
		// sibling's value (project vars inherit into every container live).
		projectEnvForRefs := make(map[string]string, len(projectEnvs))
		for _, pe := range projectEnvs {
			projectEnvForRefs[pe.Key] = pe.Content
		}
		expander := &refExpander{
			client:       client,
			classifier:   classifier,
			serviceIndex: serviceIndex,
			cache:        expanderCache,
			projectEnv:   projectEnvForRefs,
		}

		var unresolved []string
		// Stable iteration order for deterministic error messages.
		yamlKeys := make([]string, 0, len(entry.Run.EnvVariables))
		for k := range entry.Run.EnvVariables {
			yamlKeys = append(yamlKeys, k)
		}
		sort.Strings(yamlKeys)
		for _, envName := range yamlKeys {
			rawValue := entry.Run.EnvVariables[envName]
			expanded, unresolvedCount, expErr := expander.expandRefs(ctx, rawValue, "", map[string]bool{}, 0)
			if expErr != nil {
				return nil, fmt.Errorf("build env plan: %w", expErr)
			}
			if unresolvedCount > 0 {
				unresolved = append(unresolved, envName)
				continue
			}
			scope := ScopeShared
			if FindEnvRefs(rawValue) != nil {
				scope = ScopeManagedRef
			}
			conflict := StatusClean
			// Any key already in the map at this point came from a
			// lower-precedence source (project or brownfield) — yaml-
			// setup writing the same key shadows it.
			if _, ok := keys[envName]; ok {
				conflict = StatusShadowed
			}
			keys[envName] = EnvKey{
				Key:      envName,
				Value:    expanded,
				Source:   SourceYAMLSetup,
				Scope:    scope,
				Conflict: conflict,
			}
		}
		if len(unresolved) > 0 {
			return nil, fmt.Errorf(
				"build env plan: unresolved ${} refs in: %s",
				strings.Join(unresolved, ", "),
			)
		}
	}

	// Channel 3 (highest precedence): .env.local overlay.
	overlay, err := readEnvLocal(cwd)
	if err != nil {
		return nil, fmt.Errorf("build env plan: %w", err)
	}
	for k, v := range overlay {
		conflict := StatusClean
		scope := ScopeLocalOverride
		if existing, ok := keys[k]; ok {
			conflict = StatusOverridden
			// Preserve the base scope label — knowing what kind of value
			// is being overridden helps UX (dry-run shows "overriding a
			// managed-ref" vs "overriding a plain shared key").
			scope = existing.Scope
		}
		keys[k] = EnvKey{
			Key:      k,
			Value:    v,
			Source:   SourceLocalOverlay,
			Scope:    scope,
			Conflict: conflict,
		}
	}

	keyList := make([]EnvKey, 0, len(keys))
	for _, k := range keys {
		keyList = append(keyList, k)
	}
	sort.Slice(keyList, func(i, j int) bool {
		return keyList[i].Key < keyList[j].Key
	})

	touchedHosts := make([]string, 0, len(expanderCache))
	for host := range expanderCache {
		touchedHosts = append(touchedHosts, host)
	}
	sort.Strings(touchedHosts)

	return &EnvPlan{
		Setup:                   setup,
		CWD:                     cwd,
		Keys:                    keyList,
		OmittedPlatformKeys:     omittedPlatformKeys,
		TouchedServiceHostnames: touchedHosts,
		Generated:               time.Now().UTC(),
	}, nil
}

// readEnvLocal reads .env.local from cwd. Absent file → empty map +
// nil error (overlay simply contributes nothing). Comments and blanks
// skipped. Format is permissive KEY=VALUE; raw value preserved (no
// quote stripping) so dotenv loaders see exactly what the user wrote.
func readEnvLocal(cwd string) (map[string]string, error) {
	path := filepath.Join(cwd, ".env.local")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("read .env.local: %w", err)
	}
	out := map[string]string{}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		eq := strings.Index(line, "=")
		if eq < 1 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		out[key] = line[eq+1:]
	}
	return out, nil
}

// Render formats the plan for the given sink.
//
// SinkDryRunDiff requires the existing .env path; use RenderDiff
// directly when the diff is already computed, or call
// DiffAgainstExisting + RenderDiff explicitly. Render(SinkDryRunDiff)
// returns an error because the sink needs side data the plan alone
// does not carry.
func (p *EnvPlan) Render(sink EnvSink) ([]byte, error) {
	switch sink {
	case SinkDotenv:
		return p.renderDotenv(), nil
	case SinkShellExport:
		return p.renderShellExport(), nil
	case SinkDryRunDiff:
		return nil, fmt.Errorf("SinkDryRunDiff requires diff input; call DiffAgainstExisting + RenderDiff")
	default:
		return nil, fmt.Errorf("unknown sink: %d", int(sink))
	}
}

// RenderDiff formats a human-readable summary of an EnvDiff. The
// output is meant for surfacing to agents/users in dry-run mode; it
// is not a replacement for the structured EnvDiff JSON.
func (p *EnvPlan) RenderDiff(diff *EnvDiff) []byte {
	var sb strings.Builder
	sb.WriteString("# Dry-run for setup ")
	sb.WriteString(p.Setup)
	sb.WriteString("\n")
	if diff.IsClean() {
		sb.WriteString("# .env is already in sync with sources — no changes.\n")
		return []byte(sb.String())
	}
	if len(diff.Added) > 0 {
		sb.WriteString("# Added (")
		sb.WriteString(strconv.Itoa(len(diff.Added)))
		sb.WriteString(")\n")
		for _, k := range diff.Added {
			sb.WriteString("+ ")
			sb.WriteString(k)
			sb.WriteByte('\n')
		}
	}
	if len(diff.Modified) > 0 {
		sb.WriteString("# Modified (")
		sb.WriteString(strconv.Itoa(len(diff.Modified)))
		sb.WriteString(")\n")
		for _, m := range diff.Modified {
			sb.WriteString("~ ")
			sb.WriteString(m.Key)
			sb.WriteString(": ")
			sb.WriteString(m.From)
			sb.WriteString(" -> ")
			sb.WriteString(m.To)
			sb.WriteByte('\n')
		}
	}
	if len(diff.Unowned) > 0 {
		sb.WriteString("# Unowned (")
		sb.WriteString(strconv.Itoa(len(diff.Unowned)))
		sb.WriteString(") — keys in current .env not produced by any source.\n")
		sb.WriteString("# Move them to .env.local to preserve, or pass force=true to discard.\n")
		for _, k := range diff.Unowned {
			sb.WriteString("? ")
			sb.WriteString(k)
			sb.WriteByte('\n')
		}
	}
	return []byte(sb.String())
}

func (p *EnvPlan) renderDotenv() []byte {
	var sb strings.Builder
	sb.WriteString("# Generated by ZCP from project envVariables, zerops.yaml setup ")
	sb.WriteString(p.Setup)
	sb.WriteString(", and .env.local overlay.\n")
	sb.WriteString("# Do not edit directly — changes will be discarded on next regeneration.\n")
	sb.WriteString("# For local-only overrides, edit .env.local instead.\n\n")
	for _, k := range p.Keys {
		sb.WriteString(k.Key)
		sb.WriteByte('=')
		sb.WriteString(k.Value)
		sb.WriteByte('\n')
	}
	return []byte(sb.String())
}

func (p *EnvPlan) renderShellExport() []byte {
	var sb strings.Builder
	sb.WriteString("# Generated by ZCP — eval to load env into current shell.\n")
	for _, k := range p.Keys {
		sb.WriteString("export ")
		sb.WriteString(k.Key)
		sb.WriteByte('=')
		sb.WriteString(shellQuote(k.Value))
		sb.WriteByte('\n')
	}
	return []byte(sb.String())
}
