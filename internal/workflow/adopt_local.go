package workflow

import (
	"context"
	"fmt"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// AdoptionResult is returned from LocalAutoAdopt. Meta is always non-nil on
// success — the local directory is always adopted as a ServiceMeta.
//
// UnlinkedRuntimes names Zerops runtimes that exist in the project but
// weren't auto-linked as stage. It is non-empty only when the project has
// multiple runtime services (ambiguous stage linkage) or — in practice —
// no runtimes at all (empty slice). The caller uses it to compose the
// adoption note for MCP instructions.
//
// StageAutoLinked reports whether the meta was written with a pre-filled
// StageHostname (local-stage topology, exactly one Zerops runtime was
// present). False for local-only (zero or multiple runtimes).
type AdoptionResult struct {
	Meta             *ServiceMeta
	UnlinkedRuntimes []string // runtimes detected but not auto-linked
	StageAutoLinked  bool
	Managed          []string // managed service hostnames detected (for the note)
}

// LocalAutoAdopt ensures a ServiceMeta exists for the local project and
// returns a description of what happened. Check-and-create semantics:
//
//   - If any ServiceMeta already exists on disk → returns nil result (no-op).
//     Caller treats nil result as "already initialized", emits no note.
//   - If empty → writes exactly one ServiceMeta with Hostname=project.Name,
//     classifies Zerops-side runtimes, and auto-links a stage when there's
//     exactly one runtime. Zero runtimes → local-only (DeployStrategy=manual
//     forced). Multiple runtimes → local-only with enumeration in
//     UnlinkedRuntimes for the note.
//
// Fail-fast on API errors: if GetProject or ListServices fails, the
// caller (typically server.New) should surface the error and refuse to
// start. A partial adoption is worse than no adoption.
//
// Managed services (db, cache, storage) are reported in the result for
// .env-bridge guidance in the note; they are NOT given their own
// ServiceMeta — managed state stays API-authoritative.
func LocalAutoAdopt(ctx context.Context, client platform.Client, projectID, stateDir string) (*AdoptionResult, error) {
	existing, err := ListServiceMetas(stateDir)
	if err != nil {
		return nil, fmt.Errorf("local auto-adopt: list metas: %w", err)
	}
	if len(existing) > 0 {
		return nil, nil //nolint:nilnil // no-op sentinel: caller checks result == nil
	}

	project, err := client.GetProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("local auto-adopt: get project: %w", err)
	}
	if project == nil || project.Name == "" {
		return nil, fmt.Errorf("local auto-adopt: project %q returned no name", projectID)
	}

	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("local auto-adopt: list services: %w", err)
	}

	var runtimes []platform.ServiceStack
	var managed []string
	for _, s := range services {
		if s.IsSystem() {
			continue
		}
		typeName := s.ServiceStackTypeInfo.ServiceStackTypeVersionName
		if IsManagedService(typeName) {
			managed = append(managed, s.Name)
			continue
		}
		runtimes = append(runtimes, s)
	}

	now := time.Now().UTC().Format("2006-01-02")
	result := &AdoptionResult{Managed: managed}

	switch len(runtimes) {
	case 0:
		// Local-only: no Zerops runtime to link. Strategy defaults to manual —
		// push-dev is meaningless (no target) and push-git remains a valid
		// opt-in (user can reconfigure via action=strategy), but unset here
		// so the router prompts the user rather than silently picking a
		// strategy.
		meta := &ServiceMeta{
			Hostname:         project.Name,
			Mode:             PlanModeLocalOnly,
			DeployStrategy:   StrategyManual,
			BootstrapSession: "", // adopted, not a fresh bootstrap
			BootstrappedAt:   now,
		}
		if err := WriteServiceMeta(stateDir, meta); err != nil {
			return nil, fmt.Errorf("local auto-adopt: write local-only meta: %w", err)
		}
		result.Meta = meta
		return result, nil

	case 1:
		// Exactly one runtime: auto-link as stage. Strategy stays unset so
		// the router prompts for an explicit choice (push-dev / push-git /
		// manual). If the platform service is already ACTIVE, stamp
		// FirstDeployedAt — the adopted+ACTIVE case means code has landed
		// there before ZCP was aware of it.
		rt := runtimes[0]
		meta := &ServiceMeta{
			Hostname:         project.Name,
			StageHostname:    rt.Name,
			Mode:             PlanModeLocalStage,
			BootstrapSession: "",
			BootstrappedAt:   now,
		}
		if rt.Status == StatusActive {
			meta.FirstDeployedAt = time.Now().UTC().Format(time.RFC3339)
		}
		if err := WriteServiceMeta(stateDir, meta); err != nil {
			return nil, fmt.Errorf("local auto-adopt: write local-stage meta: %w", err)
		}
		result.Meta = meta
		result.StageAutoLinked = true
		return result, nil

	default:
		// Multiple runtimes: meta still written as local-only so the project
		// is consistently adopted, but NO stage auto-link — we refuse to
		// guess primary. User resolves via adopt-local subaction.
		meta := &ServiceMeta{
			Hostname:         project.Name,
			Mode:             PlanModeLocalOnly,
			BootstrapSession: "",
			BootstrappedAt:   now,
		}
		if err := WriteServiceMeta(stateDir, meta); err != nil {
			return nil, fmt.Errorf("local auto-adopt: write ambiguous local-only meta: %w", err)
		}
		result.Meta = meta
		result.UnlinkedRuntimes = make([]string, 0, len(runtimes))
		for _, r := range runtimes {
			result.UnlinkedRuntimes = append(result.UnlinkedRuntimes, r.Name)
		}
		return result, nil
	}
}

// FormatAdoptionNote renders the plain-English instruction-text appendix
// describing what auto-adopt did. Three shapes matching plan §2.10; the
// exact strings are test-asserted so don't drift lightly. Returns empty
// string when result is nil (already-initialized path — no note emitted).
func FormatAdoptionNote(result *AdoptionResult) string {
	if result == nil || result.Meta == nil {
		return ""
	}
	project := result.Meta.Hostname
	managedLine := ""
	if len(result.Managed) > 0 {
		managedLine = fmt.Sprintf("Managed services detected: %s. Run `zcli vpn up <projectId>` on your machine for dev-time access.", joinServiceNames(result.Managed))
	}

	switch {
	case result.StageAutoLinked:
		base := fmt.Sprintf("Adopted project %q as local-stage (linked to %s).", project, result.Meta.StageHostname)
		if managedLine != "" {
			return base + " " + managedLine
		}
		return base

	case len(result.UnlinkedRuntimes) > 0:
		return fmt.Sprintf(
			"Adopted project %q as local-only. Multiple Zerops runtime services exist (%s) — none linked as stage. "+
				"Strategy options: `push-git` (push to an external remote, ZCP doesn't track what happens downstream) or `manual` (nothing automated). "+
				"`push-dev` requires linking one runtime as stage first: zerops_workflow action=\"adopt-local\" targetService=\"<chosen-hostname>\".",
			project, joinServiceNames(result.UnlinkedRuntimes),
		)

	default:
		base := fmt.Sprintf(
			"Adopted project %q as local-only. No Zerops runtime services exist. "+
				"Strategy options: `push-git` (push to an external remote; whatever happens downstream is your setup, ZCP doesn't track) or `manual`.",
			project,
		)
		if managedLine != "" {
			return base + " " + managedLine
		}
		return base
	}
}

// joinServiceNames is a tiny helper keeping formatAdoptionNote scannable.
// Not exported; duplicated here rather than pulling in strings.Join to keep
// the signature clear in the template strings.
func joinServiceNames(names []string) string {
	if len(names) == 0 {
		return ""
	}
	out := names[0]
	for _, n := range names[1:] {
		out += ", " + n
	}
	return out
}

// MigrateLegacyLocalMetas used to rewrite pre-A.4 local metas in place
// (Hostname=<stage-host>, Mode ∈ {standard,simple,dev}, Environment=local
// → Hostname=project.Name, Mode=local-stage). That migration shipped in
// Release A; under phase B.3 the Environment field itself was dropped,
// removing the signal the migration relied on to identify legacy rows.
//
// Users who upgrade straight from pre-A.4 to post-B.3 without having
// booted an A-series binary keep their legacy meta on disk — it will
// look like a fresh container meta (no Environment field, Mode=standard
// etc.) and filter-out of local router paths cleanly. The practical
// upgrade path is still to hit A first (which rewrites the state dir
// idempotently), so this lossy skip is acceptable.
//
// The function is kept as a no-op so server.New doesn't have to branch
// on "did we run B.3 already" logic — call-site stays unchanged.
func MigrateLegacyLocalMetas(_ context.Context, _ platform.Client, _, _ string, _ []*ServiceMeta) error {
	return nil
}
