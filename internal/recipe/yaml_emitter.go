package recipe

import (
	"fmt"
	"sort"
	"strings"
)

// RecipeAppRepoBase is the GitHub org where recipe app repos live. Kept
// in sync with internal/workflow (v2) so the published recipe tree is
// identical regardless of which engine ran.
const RecipeAppRepoBase = "https://github.com/zerops-recipe-apps/"

// Shape selects which import.yaml shape the emitter produces.
//
// A recipe run has two fundamentally different yaml shapes and
// conflating them is the leading cause of provision/finalize failure:
//
//   - ShapeWorkspace (provision-time): submitted via `zerops_import
//     content=<yaml>` to bring up the author's single working project.
//     Services-only (no `project:`), dev runtimes `startWithoutCode: true`
//     so they come up empty for SSHFS-mount-and-code, no `buildFromGit`
//     (the repos don't exist yet), no `zeropsSetup`, no preprocessor
//     expressions. Real project-level secrets are set separately via
//     `zerops_env project=true action=set` after the workspace is up.
//
//   - ShapeDeliverable (finalize, 6 tiers): the published template each
//     end-user clicks to deploy. Full `project:` block with
//     `envVariables` (shared secrets as `<@generateRandomString(<32>)>`
//     templates — evaluated once per end-user), every runtime has
//     `zeropsSetup: dev|prod` + `buildFromGit` pointing at the published
//     codebase repos. `${zeropsSubdomainHost}` stays literal for
//     end-user subdomain substitution at click-deploy.
//
// Plan §3 stays-list: v3 preserves v2's distinction between these two
// shapes. v2 enforces via a validator refusing `startWithoutCode` in
// deliverables (internal/tools/workflow_checks_finalize.go). v3 enforces
// by construction — ShapeWorkspace never emits buildFromGit/zeropsSetup,
// ShapeDeliverable never emits startWithoutCode.
type Shape string

const (
	ShapeWorkspace   Shape = "workspace"
	ShapeDeliverable Shape = "deliverable"
)

// EmitWorkspaceYAML renders the workspace import.yaml — submitted to
// `zerops_import content=<yaml>` at provision to bring up the author's
// working project. Services-only, dev+stage pairs per codebase, managed
// services with priority/mode. No `project:` block (project-level env
// vars are set via `zerops_env` after import).
func EmitWorkspaceYAML(plan *Plan) (string, error) {
	if plan == nil {
		return "", fmt.Errorf("nil plan")
	}
	var b strings.Builder
	writeWorkspaceServices(&b, plan)
	return b.String(), nil
}

// EmitDeliverableYAML renders the published import.yaml for one tier —
// the end-user-facing template. Full `project:` block, `zeropsSetup` +
// `buildFromGit` per runtime service, per-tier scaling/mode. Comments
// from plan.EnvComments; project-level env vars from plan.ProjectEnvVars
// (keyed by tier index as string). Output is deterministic — struct-
// field order, sorted env-var keys, sorted extra fields.
func EmitDeliverableYAML(plan *Plan, tierIndex int) (string, error) {
	tier, ok := TierAt(tierIndex)
	if !ok {
		return "", fmt.Errorf("tier index %d out of range", tierIndex)
	}
	if plan == nil {
		return "", fmt.Errorf("nil plan")
	}

	var b strings.Builder
	writePreprocessor(&b, plan)
	writeProject(&b, plan, tier)
	writeDeliverableServices(&b, plan, tier)
	return b.String(), nil
}

// EmitImportYAML is retained as a thin delegate to EmitDeliverableYAML
// for callers that haven't migrated. New callers pick a shape
// explicitly via EmitWorkspaceYAML / EmitDeliverableYAML.
func EmitImportYAML(plan *Plan, tierIndex int) (string, error) {
	return EmitDeliverableYAML(plan, tierIndex)
}

func writePreprocessor(b *strings.Builder, plan *Plan) {
	if plan.Research.NeedsAppSecret && plan.Research.AppSecretKey != "" {
		b.WriteString("#zeropsPreprocessor=on\n\n")
	}
}

func writeProject(b *strings.Builder, plan *Plan, tier Tier) {
	comments := plan.EnvComments[envKey(tier)]
	writeComment(b, comments.Project, "")

	b.WriteString("project:\n")
	fmt.Fprintf(b, "  name: %s-%s\n", plan.Slug, tier.Suffix)
	if tier.CorePackage != "" {
		fmt.Fprintf(b, "  corePackage: %s\n", tier.CorePackage)
	}

	hasSecret := plan.Research.NeedsAppSecret && plan.Research.AppSecretKey != ""
	envVars := plan.ProjectEnvVars[envKey(tier)]
	// Run-22 R3-RC-3 — single-slot tiers (2-5; RunsDevContainer=false)
	// rewrite slot-named hostnames in URL values to bare hostnames
	// (`apistage-` → `api-`, `appdev-` → `app-`) and drop DEV_* keys.
	// The agent records the dev-pair (env 0/1) baseline; the engine
	// emits the per-tier shape so authors don't have to enumerate all
	// 6 envs by hand.
	if !tier.RunsDevContainer && len(envVars) > 0 {
		envVars = rewriteURLsForSingleSlot(envVars)
	}
	if !hasSecret && len(envVars) == 0 {
		b.WriteByte('\n')
		return
	}

	b.WriteString("  envVariables:\n")
	if hasSecret {
		fmt.Fprintf(b, "    %s: <@generateRandomString(<32>)>\n", plan.Research.AppSecretKey)
	}
	names := sortedKeys(envVars)
	for _, name := range names {
		if hasSecret && name == plan.Research.AppSecretKey {
			continue
		}
		fmt.Fprintf(b, "    %s: %s\n", name, envVars[name])
	}
	b.WriteByte('\n')
}

// rewriteURLsForSingleSlot rewrites slot-named hostnames in URL values
// to bare hostnames for single-slot tiers (envs 2-5). The dev-pair
// baseline (envs 0/1) carries `apistage-`/`appdev-` etc. because dev
// and stage exist as distinct services there; envs 2-5 collapse the
// pair into a bare hostname. This rewrite means agents author URL
// constants once at env 0/1 and the engine handles per-tier reshape.
//
// Run-22 R3-RC-3 — closes the channel sync gap between the agent's
// `zerops_env action=set` (live workspace) and `Plan.ProjectEnvVars`
// (tier yaml emit). The agent recorded provision-time URLs with
// dev-pair hostnames; the engine emits per-tier with the right shape.
//
// Rules:
//   - Drop entries whose key starts with `DEV_` (single-slot tiers
//     have no dev runtime; DEV_API_URL would dangle).
//   - Replace `apidev-`/`apistage-`/`appdev-`/`appstage-`/`workerdev-`/
//     `workerstage-` with bare `api-`/`app-`/`worker-`.
//   - Preserve `${zeropsSubdomainHost}` and any other `${...}` token.
//
// The rewrite is shallow string-replace — sufficient because URL values
// follow a fixed pattern (`https://<slot>-${zeropsSubdomainHost}-PORT...`).
// No need for parsed URL handling.
func rewriteURLsForSingleSlot(envVars map[string]string) map[string]string {
	out := make(map[string]string, len(envVars))
	replacements := []struct{ from, to string }{
		{"apidev-", "api-"},
		{"apistage-", "api-"},
		{"appdev-", "app-"},
		{"appstage-", "app-"},
		{"workerdev-", "worker-"},
		{"workerstage-", "worker-"},
	}
	for k, v := range envVars {
		if strings.HasPrefix(k, "DEV_") {
			continue
		}
		for _, r := range replacements {
			v = strings.ReplaceAll(v, r.from, r.to)
		}
		out[k] = v
	}
	return out
}

// writeWorkspaceServices emits the provision-time service list. Every
// codebase contributes a dev slot (startWithoutCode: true) and a stage
// slot (no startWithoutCode — waits at READY_TO_DEPLOY). Managed
// services land the same as deliverable (priority, mode, autoscaling).
// Shared-codebase workers get stage only; the host's dev slot runs both
// processes.
func writeWorkspaceServices(b *strings.Builder, plan *Plan) {
	b.WriteString("services:\n")
	// Use tier 0 as the scaling baseline for workspace — RuntimeMinRAM /
	// ManagedMinRAM at the dev-default level.
	baseTier, _ := TierAt(0)
	for _, cb := range plan.Codebases {
		if isRuntimeShared(cb, plan) {
			writeWorkspaceRuntimeStage(b, cb, baseTier)
			continue
		}
		writeWorkspaceRuntimeDev(b, cb, baseTier)
		writeWorkspaceRuntimeStage(b, cb, baseTier)
	}
	for _, svc := range plan.Services {
		writeNonRuntimeService(b, svc, baseTier, nil)
	}
}

// writeWorkspaceRuntimeDev emits a dev runtime slot in workspace shape:
// startWithoutCode: true, no zeropsSetup, no buildFromGit, subdomain
// access for non-worker services.
func writeWorkspaceRuntimeDev(b *strings.Builder, cb Codebase, tier Tier) {
	host := cb.Hostname + "dev"
	fmt.Fprintf(b, "  - hostname: %s\n", host)
	fmt.Fprintf(b, "    type: %s\n", cb.BaseRuntime)
	if cb.Role == RoleAPI {
		b.WriteString("    priority: 5\n")
	}
	b.WriteString("    startWithoutCode: true\n")
	b.WriteString("    maxContainers: 1\n")
	if !cb.IsWorker {
		b.WriteString("    enableSubdomainAccess: true\n")
	}
	writeAutoscaling(b, serviceKindRuntime, tier)
	b.WriteByte('\n')
}

// writeWorkspaceRuntimeStage emits a stage runtime slot in workspace
// shape. Stage services omit startWithoutCode — they wait at
// READY_TO_DEPLOY until the first cross-deploy from dev.
func writeWorkspaceRuntimeStage(b *strings.Builder, cb Codebase, tier Tier) {
	host := cb.Hostname + "stage"
	fmt.Fprintf(b, "  - hostname: %s\n", host)
	fmt.Fprintf(b, "    type: %s\n", cb.BaseRuntime)
	if cb.Role == RoleAPI {
		b.WriteString("    priority: 5\n")
	}
	if !cb.IsWorker {
		b.WriteString("    enableSubdomainAccess: true\n")
	}
	writeAutoscaling(b, serviceKindRuntime, tier)
	b.WriteByte('\n')
}

// writeDeliverableServices emits the finalize-time service list per
// tier. Tiers 0-1 are dev-pair (dev+stage per codebase); tiers 2-5 are
// single-slot (api/app/worker only). Runtime services carry
// zeropsSetup + buildFromGit pointing at the published codebase repos.
//
// Run-13 §Y2D: when a dev-pair runtime falls back to the bare-codebase
// EnvComments key for the dev slot, the SAME prose used to render
// above the stage slot too (both calls fall through Y2's
// slot-then-bare lookup). devEmittedFallback tracks the rendered
// fallback per codebase so the stage slot can suppress the duplicate.
// Distinct slot-keyed comments (apidev / apistage both set) still
// render normally — Y2D suppresses fallback duplication only.
func writeDeliverableServices(b *strings.Builder, plan *Plan, tier Tier) {
	b.WriteString("services:\n")
	comments := plan.EnvComments[envKey(tier)].Service

	for _, cb := range plan.Codebases {
		switch {
		case tier.RunsDevContainer && isRuntimeShared(cb, plan):
			writeRuntimeStage(b, plan, cb, tier, comments, "") // shared worker: stage only
		case tier.RunsDevContainer:
			var devEmittedFallback string
			if comments[cb.Hostname+"dev"] == "" && comments[cb.Hostname] != "" {
				devEmittedFallback = comments[cb.Hostname]
			}
			writeRuntimeDev(b, plan, cb, comments)
			writeRuntimeStage(b, plan, cb, tier, comments, devEmittedFallback)
		default:
			writeRuntimeSingle(b, plan, cb, tier, comments)
		}
	}
	for _, svc := range plan.Services {
		writeNonRuntimeService(b, svc, tier, comments)
	}
}

// writeRuntimeDev emits a tier-0/1 dev slot for a codebase.
func writeRuntimeDev(b *strings.Builder, plan *Plan, cb Codebase, comments map[string]string) {
	host := cb.Hostname + "dev"
	// Look up by slot host first; fall back to bare codebase name.
	// Brief instructs agents to record under the bare codebase name
	// (`env/<N>/import-comments/api`); emitter must honor that.
	// Run-12 §Y2.
	comment := comments[host]
	if comment == "" {
		comment = comments[cb.Hostname]
	}
	writeComment(b, comment, "  ")
	fmt.Fprintf(b, "  - hostname: %s\n", host)
	fmt.Fprintf(b, "    type: %s\n", devRuntimeType(cb))
	if cb.Role == RoleAPI {
		b.WriteString("    priority: 5\n")
	}
	b.WriteString("    zeropsSetup: dev\n")
	writeRuntimeBuildFromGit(b, plan, cb)
	if !cb.IsWorker {
		b.WriteString("    enableSubdomainAccess: true\n")
	}
	b.WriteString("    verticalAutoscaling:\n      minRam: 1\n\n")
}

// writeRuntimeStage emits a tier-0/1 stage slot for a codebase.
//
// devEmittedFallback is the bare-codebase comment writeRuntimeDev
// already emitted (empty when the dev slot used a slot-keyed comment
// or had none). When the stage slot's own fallback resolves to the
// same string, suppress it — Y2D dedup. Distinct slot-keyed comments
// still render normally.
func writeRuntimeStage(b *strings.Builder, plan *Plan, cb Codebase, tier Tier, comments map[string]string, devEmittedFallback string) {
	host := cb.Hostname + "stage"
	// Same slot-then-bare fallback as writeRuntimeDev. Run-12 §Y2.
	comment := comments[host]
	if comment == "" {
		comment = comments[cb.Hostname]
		// Run-13 §Y2D — suppress when the dev slot already emitted
		// the same fallback prose; otherwise both slots carry the
		// identical block.
		if comment == devEmittedFallback {
			comment = ""
		}
	}
	writeComment(b, comment, "  ")
	fmt.Fprintf(b, "  - hostname: %s\n", host)
	fmt.Fprintf(b, "    type: %s\n", cb.BaseRuntime)
	if cb.Role == RoleAPI {
		b.WriteString("    priority: 5\n")
	}
	c, _ := cb.Role.Contract()
	fmt.Fprintf(b, "    zeropsSetup: %s\n", c.ZeropsSetupProd)
	writeRuntimeBuildFromGit(b, plan, cb)
	if !cb.IsWorker {
		b.WriteString("    enableSubdomainAccess: true\n")
	}
	writeAutoscaling(b, serviceKindRuntime, tier)
	b.WriteByte('\n')
}

// writeRuntimeSingle emits a single-entry runtime service (tier 2-5).
func writeRuntimeSingle(b *strings.Builder, plan *Plan, cb Codebase, tier Tier, comments map[string]string) {
	writeComment(b, comments[cb.Hostname], "  ")
	fmt.Fprintf(b, "  - hostname: %s\n", cb.Hostname)
	fmt.Fprintf(b, "    type: %s\n", cb.BaseRuntime)
	if cb.Role == RoleAPI {
		b.WriteString("    priority: 5\n")
	}
	c, _ := cb.Role.Contract()
	fmt.Fprintf(b, "    zeropsSetup: %s\n", c.ZeropsSetupProd)
	writeRuntimeBuildFromGit(b, plan, cb)
	if !cb.IsWorker {
		b.WriteString("    enableSubdomainAccess: true\n")
	}
	if tier.RuntimeMinContainers >= 2 {
		fmt.Fprintf(b, "    minContainers: %d\n", tier.RuntimeMinContainers)
	}
	writeAutoscaling(b, serviceKindRuntime, tier)
	b.WriteByte('\n')
}

// writeNonRuntimeService emits a managed / storage / utility service.
// comments may be nil (workspace shape has no comments).
func writeNonRuntimeService(b *strings.Builder, svc Service, tier Tier, comments map[string]string) {
	if comments != nil {
		writeComment(b, comments[svc.Hostname], "  ")
	}
	fmt.Fprintf(b, "  - hostname: %s\n", svc.Hostname)
	fmt.Fprintf(b, "    type: %s\n", svc.Type)
	if svc.Priority > 0 {
		fmt.Fprintf(b, "    priority: %d\n", svc.Priority)
	}

	switch svc.Kind {
	case ServiceKindManaged:
		mode := tier.ServiceMode
		// Run-12 §Y3 — downgrade tier-5 HA to NON_HA for managed
		// service families that don't support HA on Zerops. SupportsHA
		// is set during plan composition (mergePlan) but fall back to
		// the family table here so emit is correct even when fixtures
		// or test plans pass Service literals directly.
		if mode == modeHA && !svc.SupportsHA && !managedServiceSupportsHA(svc.Type) {
			mode = modeNonHA
		}
		fmt.Fprintf(b, "    mode: %s\n", mode)
		writeAutoscaling(b, serviceKindManaged, tier)
	case ServiceKindStorage:
		b.WriteString("    objectStorageSize: 1\n")
		b.WriteString("    objectStoragePolicy: private\n")
	case ServiceKindUtility:
		b.WriteString("    zeropsSetup: app\n")
		writeAutoscaling(b, serviceKindUtility, tier)
	}
	// Extra pass-through fields in deterministic order.
	for _, k := range sortedKeys(svc.ExtraFields) {
		fmt.Fprintf(b, "    %s: %s\n", k, svc.ExtraFields[k])
	}
	b.WriteByte('\n')
}

// serviceKind* constants classify autoscaling branches. Decoupled from
// ServiceKind so runtime autoscaling can share the same emitter entry
// point as codebases.
type emitKind int

const (
	serviceKindRuntime emitKind = iota
	serviceKindManaged
	serviceKindUtility
)

// writeAutoscaling emits the verticalAutoscaling block for a service kind
// at a tier. Values come from the tier struct — no prose.
func writeAutoscaling(b *strings.Builder, kind emitKind, tier Tier) {
	b.WriteString("    verticalAutoscaling:\n")
	if tier.CPUMode != "" && kind != serviceKindUtility {
		fmt.Fprintf(b, "      cpuMode: %s\n", tier.CPUMode)
	}
	var minRAM float64
	switch kind {
	case serviceKindRuntime:
		minRAM = tier.RuntimeMinRAM
	case serviceKindManaged:
		minRAM = tier.ManagedMinRAM
	case serviceKindUtility:
		minRAM = tier.ManagedMinRAM
	}
	fmt.Fprintf(b, "      minRam: %s\n", fmtFloat(minRAM))
	if tier.MinFreeRAMGB > 0 && kind != serviceKindUtility {
		fmt.Fprintf(b, "      minFreeRamGB: %s\n", fmtFloat(tier.MinFreeRAMGB))
	}
}

// writeRuntimeBuildFromGit emits the buildFromGit URL. Suffix routing:
// worker-separate → "-worker"; worker-shared → host codebase's suffix;
// api role → "-api"; everything else → "-app".
func writeRuntimeBuildFromGit(b *strings.Builder, plan *Plan, cb Codebase) {
	fmt.Fprintf(b, "    buildFromGit: %s%s%s\n", RecipeAppRepoBase, plan.Slug, runtimeRepoSuffix(plan, cb))
}

func runtimeRepoSuffix(plan *Plan, cb Codebase) string {
	switch {
	case cb.IsWorker && cb.SharesCodebaseWith == "":
		return "-worker"
	case cb.IsWorker && cb.SharesCodebaseWith != "":
		for _, host := range plan.Codebases {
			if host.Hostname == cb.SharesCodebaseWith && host.Role == RoleAPI {
				return "-api"
			}
		}
		return "-app"
	case cb.Role == RoleAPI:
		return "-api"
	default:
		return "-app"
	}
}

// devRuntimeType returns the runtime type string for a dev slot. Same as
// base runtime — dev containers use the same family as stage/prod, only
// zeropsSetup differs.
func devRuntimeType(cb Codebase) string { return cb.BaseRuntime }

// isRuntimeShared returns true when this codebase shares its source with
// another (the host). Shared runtime workers skip their own dev slot
// because the host's dev slot runs both processes from one mount.
func isRuntimeShared(cb Codebase, _ *Plan) bool {
	return cb.IsWorker && cb.SharesCodebaseWith != ""
}

// writeComment wraps free-form text as # comment lines at the given indent.
// Preserves explicit newlines. No-op for empty/whitespace input.
func writeComment(b *strings.Builder, text, indent string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	width := max(80-len(indent)-2, 20)
	for _, line := range wrapPara(text, width) {
		if line == "" {
			fmt.Fprintf(b, "%s#\n", indent)
			continue
		}
		// Strip a leading `# ` or `#` from agent-authored lines so
		// re-prefixing produces single-`#` comments. Run-12 §Y1.
		line = strings.TrimPrefix(line, "# ")
		line = strings.TrimPrefix(line, "#")
		line = strings.TrimSpace(line)
		if line == "" {
			fmt.Fprintf(b, "%s#\n", indent)
			continue
		}
		fmt.Fprintf(b, "%s# %s\n", indent, line)
	}
}

// wrapPara wraps text to width at word boundaries, preserving explicit
// newlines as paragraph breaks.
func wrapPara(text string, width int) []string {
	var out []string
	for para := range strings.SplitSeq(text, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		cur := words[0]
		for _, w := range words[1:] {
			if len(cur)+1+len(w) > width {
				out = append(out, cur)
				cur = w
				continue
			}
			cur += " " + w
		}
		out = append(out, cur)
	}
	return out
}

// envKey returns the tier's key in per-tier maps ("0"..."5").
func envKey(t Tier) string { return fmt.Sprintf("%d", t.Index) }

// sortedKeys returns the keys of a map[string]string in lex order.
func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
