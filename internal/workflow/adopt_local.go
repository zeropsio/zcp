package workflow

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
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
//     exactly one runtime. Zero runtimes → local-only (CloseDeployMode=manual
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
		if topology.IsManagedService(typeName) {
			managed = append(managed, s.Name)
			continue
		}
		runtimes = append(runtimes, s)
	}

	now := time.Now().UTC().Format("2006-01-02")
	result := &AdoptionResult{Managed: managed}

	switch len(runtimes) {
	case 0:
		// Local-only, zero runtimes: same shape as multi-runtime — close-
		// mode stays unset until the develop-strategy-review atom fires
		// post-deploy and the agent picks (git-push for an external
		// remote, manual for nothing-automated). Pre-Phase-9 the branch
		// pre-picked `manual` + `Confirmed=true`, hiding git-push from
		// the review path.
		meta := NewServiceMeta(project.Name, topology.PlanModeLocalOnly)
		meta.BootstrappedAt = now // adopted, not a fresh bootstrap (BootstrapSession stays "")
		if err := UpsertServiceMeta(stateDir, meta.Hostname, func(m *ServiceMeta, existed bool) error {
			if existed {
				return ErrSkipWrite // another process already adopted this project; don't clobber
			}
			*m = *meta
			return nil
		}); err != nil {
			return nil, fmt.Errorf("local auto-adopt: write local-only meta: %w", err)
		}
		result.Meta = meta
		return result, nil

	case 1:
		// Exactly one runtime: auto-link as stage. Strategy stays unset so
		// the router prompts for an explicit choice (auto / git-push /
		// manual). If the platform service is already ACTIVE, stamp
		// FirstDeployedAt — the adopted+ACTIVE case means code has landed
		// there before ZCP was aware of it.
		rt := runtimes[0]
		// TOPO-1: NewServiceMeta stamps GitPushState/BuildIntegration — this
		// local-stage branch previously omitted them, leaving them empty on
		// disk so the git-push-setup + build-integration atoms never fired
		// for the local-stage mode they target.
		meta := NewServiceMeta(project.Name, topology.PlanModeLocalStage)
		meta.StageHostname = rt.Name
		meta.BootstrappedAt = now
		if rt.Status == StatusActive {
			meta.FirstDeployedAt = time.Now().UTC().Format(time.RFC3339)
		}
		if err := UpsertServiceMeta(stateDir, meta.Hostname, func(m *ServiceMeta, existed bool) error {
			if existed {
				return ErrSkipWrite // another process already adopted this project; don't clobber
			}
			*m = *meta
			return nil
		}); err != nil {
			return nil, fmt.Errorf("local auto-adopt: write local-stage meta: %w", err)
		}
		result.Meta = meta
		result.StageAutoLinked = true

		// Gate A — best-effort cascade to populate StageSetupName.
		// LocalAutoAdopt runs at server start: no agent context to
		// receive a blocker, so any miss / error is silently swallowed.
		// The next setup-sensitive call from an agent reruns the
		// cascade and surfaces the blocker then.
		_, _ = ResolveCanonicalSetup(ctx, client, ResolveCanonicalSetupInput{
			StateDir:       stateDir,
			ServiceID:      rt.ID,
			TargetHostname: rt.Name,
			Mode:           topology.ModeStage,
		})
		return result, nil

	default:
		// Multiple runtimes: meta still written as local-only so the project
		// is consistently adopted, but NO stage auto-link — we refuse to
		// guess primary. User resolves via adopt-local subaction.
		// TOPO-1: stamp the three dims via NewServiceMeta (this multi-runtime
		// branch previously omitted GitPushState/BuildIntegration).
		meta := NewServiceMeta(project.Name, topology.PlanModeLocalOnly)
		meta.BootstrappedAt = now
		if err := UpsertServiceMeta(stateDir, meta.Hostname, func(m *ServiceMeta, existed bool) error {
			if existed {
				return ErrSkipWrite // another process already adopted this project; don't clobber
			}
			*m = *meta
			return nil
		}); err != nil {
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

// FormatLocalStateNote renders the plain-English instruction-text
// appendix describing the project's current local-adoption state. Called
// on every server start in local env (not just first-call) so an agent
// joining a project that was adopted in a previous session still sees
// the actionable hint — pre-Phase-10 the note was emitted only at first
// adoption and went silent thereafter, leaving second-server-start
// agents without recovery guidance.
//
// Three shapes:
//   - local-stage (Mode == PlanModeLocalStage): linkage statement + optional
//     managed-services hint.
//   - local-only with at least one runtime detected: leads with
//     `BEFORE running develop, link a runtime via adopt-local...` so the
//     actionable recovery sits up front, not buried at the end.
//   - local-only with no runtimes: legitimate end-state, no adopt-local
//     prompt — close-mode review handles the next step.
//
// Returns empty string when no local meta exists (container env, fresh
// state dir before adoption, container-only project).
func FormatLocalStateNote(metas []*ServiceMeta, services []platform.ServiceStack, projectName string) string {
	local := findLocalMeta(metas)
	if local == nil {
		return ""
	}
	if projectName == "" {
		projectName = local.Hostname
	}
	runtimes, managed := classifyServicesForNote(services)
	managedLine := ""
	if len(managed) > 0 {
		managedLine = fmt.Sprintf("Managed services detected: %s. Run `zcli vpn up <projectId>` on your machine for dev-time access.", strings.Join(managed, ", "))
	}

	switch {
	case local.Mode == topology.PlanModeLocalStage:
		base := fmt.Sprintf("Adopted project %q as local-stage (linked to %s).", projectName, local.StageHostname)
		if managedLine != "" {
			return base + " " + managedLine
		}
		return base

	case len(runtimes) > 0:
		runtimeNames := make([]string, len(runtimes))
		for i, r := range runtimes {
			runtimeNames[i] = r.Name
		}
		base := fmt.Sprintf(
			"Project %q is adopted as local-only. BEFORE running develop, link a runtime via "+
				"zerops_workflow action=\"adopt-local\" targetService=\"<hostname>\" — "+
				"detected runtimes: %s. "+
				"Close-mode options: `manual` (nothing automated), or `auto` after linking one runtime as stage first. "+
				"Delivery via git push is a separate dimension — run action=\"git-push-setup\" (works under either close-mode).",
			projectName, strings.Join(runtimeNames, ", "),
		)
		if managedLine != "" {
			return base + " " + managedLine
		}
		return base

	default:
		base := fmt.Sprintf(
			"Project %q is adopted as local-only. No Zerops runtime services exist. "+
				"Close-mode options: `manual` (nothing automated; `auto` needs a linked runtime first). "+
				"Delivery via git push is a separate dimension — run action=\"git-push-setup\".",
			projectName,
		)
		if managedLine != "" {
			return base + " " + managedLine
		}
		return base
	}
}

// findLocalMeta returns the single local-mode meta from a meta list, or
// nil. Local projects have at most one local-mode meta (the project-keyed
// one written by LocalAutoAdopt or upgraded by handleAdoptLocal).
func findLocalMeta(metas []*ServiceMeta) *ServiceMeta {
	for _, m := range metas {
		if m == nil {
			continue
		}
		if m.Mode == topology.PlanModeLocalOnly || m.Mode == topology.PlanModeLocalStage {
			return m
		}
	}
	return nil
}

// classifyServicesForNote splits services into runtime hostnames and
// managed-service hostnames for the local-state note. System services
// (proxies, internal stacks) are filtered out — they're not part of the
// user's mental model.
func classifyServicesForNote(services []platform.ServiceStack) (runtimes []platform.ServiceStack, managed []string) {
	for _, s := range services {
		if s.IsSystem() {
			continue
		}
		typeName := s.ServiceStackTypeInfo.ServiceStackTypeVersionName
		if topology.IsManagedService(typeName) {
			managed = append(managed, s.Name)
			continue
		}
		runtimes = append(runtimes, s)
	}
	return runtimes, managed
}
