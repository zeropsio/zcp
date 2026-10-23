package tools

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// workflowPoolMigrate routes zerops_workflow workflow="pool-migrate": a
// stateless multi-call narrowing that walks one part's dev service onto a dev
// pool (docs/spec-workflows.md §13). Every call re-reads live state and names
// the part's current step and the next exact call; the read side writes
// nothing anywhere.
const workflowPoolMigrate = "pool-migrate"

// The steps a part walks, in order (docs/spec-workflows.md §13.1).
const (
	poolMigrateStepProvision     = "provision"
	poolMigrateStepEnv           = "env"
	poolMigrateStepImage         = "image"
	poolMigrateStepMove          = "move"
	poolMigrateStepLease         = "lease"
	poolMigrateStepReadyToRetire = "ready-to-retire"
	poolMigrateStepRetiring      = "retiring"
	poolMigrateStepRetired       = "retired"
)

// poolMigrateStepOrder is the step sequence the response's steps list marks.
func poolMigrateStepOrder() []string {
	return []string{
		poolMigrateStepProvision, poolMigrateStepEnv, poolMigrateStepImage, poolMigrateStepMove,
		poolMigrateStepLease, poolMigrateStepReadyToRetire, poolMigrateStepRetiring, poolMigrateStepRetired,
	}
}

// A step's state in the response's steps list.
const (
	poolMigrateStateDone    = "done"
	poolMigrateStateCurrent = "current"
	poolMigrateStatePending = "pending"
)

// Response statuses: a candidate's current step, or a refusal.
const (
	poolMigrateStatusStep         = "step"
	poolMigrateStatusNotCandidate = "not-a-candidate"
)

// Refusal reasons — each names why a service is not a candidate
// (docs/spec-workflows.md §13 PM-9).
const (
	poolMigrateRefuseNotInContainer  = "not-in-container"
	poolMigrateRefusePoolOrBox       = "pool-or-box"
	poolMigrateRefuseNotAdopted      = "not-adopted"
	poolMigrateRefuseSimpleMode      = "simple-mode"
	poolMigrateRefuseLocalMode       = "local-mode"
	poolMigrateRefuseUnsupportedMode = "unsupported-mode"
	poolMigrateRefuseVariantB        = "variant-b"
	poolMigrateRefuseMultiPart       = "multi-part"
	poolMigrateRefuseNoOrigin        = "no-origin"
	poolMigrateRefuseDevNotLive      = "dev-not-live"
	poolMigrateRefusePoolInvalid     = "pool-hostname-invalid"
	poolMigrateRefusePoolTaken       = "pool-hostname-taken"
	poolMigrateRefuseDevUnreadable   = "dev-unreadable"
	poolMigrateRefuseCrewWorktrees   = "crew-worktrees"
)

// The part's pool, as the provision step imports it: `<stem>pool`, no
// container until a lease asks for one, at most three (docs/spec-workflows.md
// §13.1).
const (
	poolMigratePoolType      = "swarm@1"
	poolMigratePoolSuffix    = "pool"
	poolMigrateMinContainers = 0
	poolMigrateMaxContainers = 3
)

// The atoms carrying the flow's guidance: the plan on every answer, and the
// move step's on every answer that carries the census.
const (
	poolMigrateAtomID     = "pool-migrate-plan"
	poolMigrateMoveAtomID = "pool-migrate-move"
)

// poolMigrateOrigin is the remote a lease of the pool clones: the dev
// service's origin.
const poolMigrateOrigin = "origin"

// Where the work's branch comes from (docs/spec-workflows.md §13 PM-2).
const (
	poolMigrateWorkFromChange   = "change"
	poolMigrateWorkFromCurrent  = "current"
	poolMigrateWorkFromUpstream = "upstream"
)

// poolMigrateCrewRoot is where a crew lane's linked worktree lives on a dev
// service: `/var/www/.crew/<handle>`.
const poolMigrateCrewRoot = topology.CodeMountRoot + "/.crew/"

type poolMigrateResponse struct {
	Workflow string         `json:"workflow"`
	Phase    workflow.Phase `json:"phase"`
	Status   string         `json:"status"`
	// Service is the part's dev service — the pair's dev half when the call
	// named its stage.
	Service string   `json:"service"`
	Stages  []string `json:"stages,omitempty"`
	Pool    string   `json:"pool,omitempty"`
	Step    string   `json:"step,omitempty"`
	// Steps marks every step done, current or pending.
	Steps []poolMigrateStepState `json:"steps,omitempty"`
	Next  *poolMigrateNext       `json:"next,omitempty"`
	// NextUnavailable says why a step names no next call.
	NextUnavailable string `json:"nextUnavailable,omitempty"`
	// Census is the dev service's unsaved work, read on every step.
	Census *ops.PoolMigrateCensus `json:"census,omitempty"`
	// WorkBranch is the branch a lease of the pool checks out.
	WorkBranch *poolMigrateWorkBranch `json:"workBranch,omitempty"`
	Refusals   []poolMigrateRefusal   `json:"refusals,omitempty"`
	Warnings   []string               `json:"warnings,omitempty"`
	Guidance   string                 `json:"guidance"`
	Inputs     poolMigrateInputs      `json:"inputs"`
}

type poolMigrateStepState struct {
	Step  string `json:"step"`
	State string `json:"state"`
}

type poolMigrateNext struct {
	Tool string            `json:"tool"`
	Args map[string]string `json:"args,omitempty"`
	Why  string            `json:"why"`
}

type poolMigrateRefusal struct {
	Reason   string             `json:"reason"`
	Message  string             `json:"message"`
	Recovery *topology.Recovery `json:"recovery,omitempty"`
}

// poolMigrateWorkBranch is the work's branch: the branch on the origin a lease
// of the pool checks out, and where it comes from — or, in Missing, why none is
// named yet and what names one.
type poolMigrateWorkBranch struct {
	Branch  string `json:"branch,omitempty"`
	Source  string `json:"source,omitempty"`
	Missing string `json:"missing,omitempty"`
}

// poolMigrateInputs echoes what the next call passes again.
type poolMigrateInputs struct {
	Service string `json:"service"`
	Pool    string `json:"pool,omitempty"`
}

// handlePoolMigrate is the read side of workflow="pool-migrate": it names the
// part's current step from direct reads only — the service list and process
// list a just-made import cannot be missing from — renders the pool stanza
// for the provision step, carries the census of the dev service's unsaved
// work and the work's branch on every step, and refuses a non-candidate with
// every reason it finds. It writes nothing: no state file, no meta, no env, no
// ref; the one SSH call is the census, read-only git on the dev service.
func handlePoolMigrate(
	ctx context.Context,
	projectID string,
	client platform.Client,
	sshDeployer ops.SSHDeployer,
	input WorkflowInput,
	stateDir string,
	rt runtime.Info,
) (*mcp.CallToolResult, any, error) {
	service := strings.TrimSpace(input.Service)
	poolInput := strings.TrimSpace(input.Pool)
	if service == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			`workflow="pool-migrate" needs service: the dev service to move onto a dev pool`,
			poolMigrateCandidatesHint(stateDir),
		), WithRecoveryStatus()), nil, nil
	}
	corpus, err := workflow.LoadAtomCorpus()
	if err != nil {
		return convertError(err, WithRecoveryStatus()), nil, nil
	}
	resp := poolMigrateResponse{
		Workflow: workflowPoolMigrate,
		Phase:    workflow.PhasePoolMigrateActive,
		Service:  service,
		Guidance: atomBody(corpus, poolMigrateAtomID),
		Inputs:   poolMigrateInputs{Service: service, Pool: poolInput},
	}

	if !rt.InContainer || sshDeployer == nil {
		return poolMigrateRefused(resp, poolMigrateRefusal{
			Reason:  poolMigrateRefuseNotInContainer,
			Message: "pool-migrate runs from the zcp service inside the project: it reads the dev service over SSH, and a pool's containers answer only there. Nothing was read or changed.",
		}), nil, nil
	}
	if client == nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Platform client unavailable — pool-migrate reads the project's services",
			"Ensure ZCP is configured with a Zerops API key (ZCP_API_KEY).",
		), WithRecoveryStatus()), nil, nil
	}

	// The direct read, never the ES search: a pool imported a moment ago is
	// absent from the search for seconds (docs/spec-workflows.md §3.5).
	live, err := client.ListServicesDirect(ctx, projectID)
	if err != nil {
		return convertError(fmt.Errorf("pool-migrate: list services: %w", err), WithRecoveryStatus()), nil, nil
	}
	byName := make(map[string]platform.ServiceStack, len(live))
	for _, svc := range live {
		byName[svc.Name] = svc
	}
	if svc, ok := byName[service]; ok && topology.IsPoolOrSandboxType(svc.ServiceStackTypeInfo.ServiceStackTypeVersionName) {
		return poolMigrateRefused(resp, poolMigrateRefusal{
			Reason:  poolMigrateRefusePoolOrBox,
			Message: fmt.Sprintf("%s is a %s service: already a pool or a Box, not a dev service to move onto one.", service, svc.ServiceStackTypeInfo.ServiceStackTypeVersionName),
		}), nil, nil
	}

	meta, err := workflow.FindServiceMeta(stateDir, service)
	if err != nil {
		return convertError(fmt.Errorf("pool-migrate: read the service's meta: %w", err), WithRecoveryStatus()), nil, nil
	}
	if meta == nil || !meta.IsComplete() {
		return poolMigrateRefused(resp, poolMigrateRefusal{
			Reason:  poolMigrateRefuseNotAdopted,
			Message: fmt.Sprintf("%s is not a dev service zcp tracks, or its bootstrap has not finished. Adopt it with zerops_workflow action=\"start\" workflow=\"bootstrap\" route=\"adopt\" (or finish the bootstrap), then call again.", service),
		}), nil, nil
	}
	dev := meta.Hostname
	resp.Service = dev
	resp.Inputs.Service = dev
	resp.Stages = meta.StageHostnames()

	refusals := poolMigrateMetaRefusals(meta, rt)
	devSvc, devLive := byName[dev]
	if !devLive {
		refusals = append(refusals, poolMigrateRefusal{
			Reason:  poolMigrateRefuseDevNotLive,
			Message: fmt.Sprintf("%s is not among the project's services (read directly); there is no dev service to move.", dev),
		})
	}
	pool := poolInput
	if pool == "" {
		pool = poolMigrateDefaultPool(dev)
	}
	if perr := platform.ValidateHostname(pool); perr != nil {
		refusals = append(refusals, poolMigrateRefusal{
			Reason:  poolMigrateRefusePoolInvalid,
			Message: fmt.Sprintf("The pool hostname %q cannot be a service hostname: %s. %s", pool, perr.Message, perr.Suggestion),
		})
		pool = ""
	} else if svc, ok := byName[pool]; ok && !topology.IsPoolType(svc.ServiceStackTypeInfo.ServiceStackTypeVersionName) {
		refusals = append(refusals, poolMigrateRefusal{
			Reason:  poolMigrateRefusePoolTaken,
			Message: fmt.Sprintf("%s is already a %s service in this project, not a pool. Pass pool=\"<free hostname>\" to name the part's pool otherwise, and the same pool on every later call.", pool, svc.ServiceStackTypeInfo.ServiceStackTypeVersionName),
		})
	}
	resp.Pool = pool
	if len(refusals) > 0 {
		return poolMigrateRefused(resp, refusals...), nil, nil
	}

	census, err := ops.ReadPoolMigrateCensus(ctx, sshDeployer, dev, topology.CodeMountRoot)
	if err != nil {
		return poolMigrateRefused(resp, poolMigrateRefusal{
			Reason:  poolMigrateRefuseDevUnreadable,
			Message: withSSHStderr("could not read "+dev+"'s repository over SSH", err) + ". A dev service the flow cannot read does not move: what it holds would be unknown.",
		}), nil, nil
	}
	if crew := poolMigrateCrewHandles(census); len(crew) > 0 {
		return poolMigrateRefused(resp, poolMigrateRefusal{
			Reason:  poolMigrateRefuseCrewWorktrees,
			Message: fmt.Sprintf("%s holds crew worktrees (%s). Crew lanes stay on a plain dev service until crewmates work on leases of a pool, so it does not move onto one; nothing was changed.", dev, strings.Join(crew, ", ")),
		}), nil, nil
	}

	resp.Census = census
	resp.WorkBranch = poolMigrateWorkBranchOf(meta, census, rt)
	if move := atomBody(corpus, poolMigrateMoveAtomID); move != "" {
		resp.Guidance += "\n\n" + move
	}

	poolSvc, poolLive := byName[pool]
	if !poolLive {
		stanza, warnings, err := renderPoolStanza(ctx, client, devSvc, pool, rt.ServiceName)
		if err != nil {
			return convertError(fmt.Errorf("pool-migrate: render the pool stanza: %w", err), WithRecoveryStatus()), nil, nil
		}
		resp.Warnings = warnings
		return poolMigrateAt(resp, poolMigrateStepProvision, &poolMigrateNext{
			Tool: "zerops_import",
			Args: map[string]string{"content": stanza},
			Why:  "Imports the part's pool. An import runs inside a develop task.",
		}, ""), nil, nil
	}

	activity, err := ops.ProjectActivity(ctx, client, projectID, map[string]string{poolSvc.ID: pool})
	if err != nil {
		return convertError(fmt.Errorf("pool-migrate: read the project's processes: %w", err), WithRecoveryStatus()), nil, nil
	}
	if len(activity[pool]) > 0 {
		return poolMigrateAt(resp, poolMigrateStepProvision, &poolMigrateNext{
			Tool: "zerops_process",
			Args: map[string]string{"action": "wait", "service": pool},
			Why:  "The pool's own processes are still running; the next step starts once they finish.",
		}, ""), nil, nil
	}
	return poolMigrateAt(resp, poolMigrateStepEnv, nil,
		"The env step gives the pool the dev service's own env, secrets included, with no value passing through you. This zcp names the step but does not run it yet; nothing changes the pool's env meanwhile."), nil, nil
}

// poolMigrateMetaRefusals are the refusals the meta alone decides: the mode,
// a shared code volume, several parts, and an origin to move the work through.
func poolMigrateMetaRefusals(meta *workflow.ServiceMeta, rt runtime.Info) []poolMigrateRefusal {
	dev := meta.Hostname
	var out []poolMigrateRefusal
	mode := meta.Mode
	if mode == "" {
		mode = topology.ModeStandard
	}
	switch mode {
	case topology.ModeDev, topology.ModeStandard:
	case topology.ModeSimple:
		out = append(out, poolMigrateRefusal{
			Reason:  poolMigrateRefuseSimpleMode,
			Message: fmt.Sprintf("%s runs in simple mode: it deploys onto itself and serves what it runs. Only a dev service, alone or the dev half of a dev/stage pair, moves onto a pool.", dev),
		})
	case topology.ModeLocalStage, topology.ModeLocalOnly:
		out = append(out, poolMigrateRefusal{
			Reason:  poolMigrateRefuseLocalMode,
			Message: fmt.Sprintf("%s is tracked in %s mode: its code lives on a local machine, not in a dev service, so there is nothing to move onto a pool.", dev, mode),
		})
	case topology.ModeStage:
		out = append(out, poolMigrateRefusal{
			Reason:  poolMigrateRefuseUnsupportedMode,
			Message: fmt.Sprintf("%s is tracked in %s mode; only a dev service moves onto a pool.", dev, mode),
		})
	}
	if meta.CodeVolume != nil {
		out = append(out, poolMigrateRefusal{
			Reason:  poolMigrateRefuseVariantB,
			Message: fmt.Sprintf("%s shares the code volume %s with other dev services; a dev service on a code volume does not move onto a pool yet.", dev, meta.CodeVolume.Hostname),
		})
	}
	if stages := meta.StageHostnames(); len(stages) > 1 {
		out = append(out, poolMigrateRefusal{
			Reason:  poolMigrateRefuseMultiPart,
			Message: fmt.Sprintf("%s serves several parts (stages %s); a dev service serving several parts does not move onto a pool yet.", dev, strings.Join(stages, ", ")),
		})
	}
	if r := poolMigrateOriginRefusal(meta, rt); r != nil {
		out = append(out, *r)
	}
	return out
}

// poolMigrateOriginRefusal refuses a dev service whose work has no origin to
// move through: in a Mate, a pair not wired to HQ; elsewhere, a pair without
// git-push or with an origin a lease cannot clone (https only, no userinfo).
func poolMigrateOriginRefusal(meta *workflow.ServiceMeta, rt runtime.Info) *poolMigrateRefusal {
	dev := meta.Hostname
	if rt.MateEnabled {
		if meta.HQ != nil && meta.HQ.Repo != "" {
			return nil
		}
		return &poolMigrateRefusal{
			Reason:  poolMigrateRefuseNoOrigin,
			Message: fmt.Sprintf("%s is not wired to this Mate's HQ yet, so its work has no origin to move through. A develop task wires it (zerops_workflow action=\"start\" workflow=\"develop\"); then call again.", dev),
		}
	}
	setup := &topology.Recovery{Tool: "zerops_workflow", Action: actionGitPushSetup, Args: map[string]string{"service": dev}}
	if meta.GitPushState != topology.GitPushConfigured || meta.RemoteURL == "" {
		return &poolMigrateRefusal{
			Reason:   poolMigrateRefuseNoOrigin,
			Message:  fmt.Sprintf("%s has no git origin to move its work through. Set one up with zerops_workflow action=\"git-push-setup\" service=%q, then call again.", dev, dev),
			Recovery: setup,
		}
	}
	if u, err := url.Parse(meta.RemoteURL); err != nil || u.Scheme != "https" || u.User != nil {
		return &poolMigrateRefusal{
			Reason:   poolMigrateRefuseNoOrigin,
			Message:  fmt.Sprintf("%s's origin is not a plain https URL, and a pool's lease clones only https origins without credentials in the URL. Point it at the repository's https URL with zerops_workflow action=\"git-push-setup\" service=%q, then call again.", dev, dev),
			Recovery: setup,
		}
	}
	return nil
}

// poolMigrateDefaultPool is the part's pool hostname: the dev hostname without
// its `dev` suffix, then `pool` (`appdev` → `apppool`, `web` → `webpool`).
func poolMigrateDefaultPool(dev string) string {
	stem := strings.TrimSuffix(dev, "dev")
	if stem == "" {
		stem = dev
	}
	return stem + poolMigratePoolSuffix
}

// poolMigrateCrewHandles names the crew lanes a dev service holds: every
// linked worktree registered under /var/www/.crew/, a missing (prunable) one
// included, by handle.
func poolMigrateCrewHandles(census *ops.PoolMigrateCensus) []string {
	var handles []string
	for _, t := range census.Trees {
		rest, ok := strings.CutPrefix(t.Path, poolMigrateCrewRoot)
		if !ok {
			continue
		}
		handle, _, _ := strings.Cut(rest, "/")
		if handle != "" && !slices.Contains(handles, handle) {
			handles = append(handles, handle)
		}
	}
	sort.Strings(handles)
	return handles
}

// poolMigrateWorkBranchOf names the work's branch — what a lease of the pool
// checks out from the origin (docs/spec-workflows.md §13 PM-2): in a Mate the
// open change's branch, else the dev checkout's own branch when HQ takes a
// push to it; outside a Mate the dev checkout's branch's upstream on origin.
// Where none is named yet, Missing says what names one.
func poolMigrateWorkBranchOf(meta *workflow.ServiceMeta, census *ops.PoolMigrateCensus, rt runtime.Info) *poolMigrateWorkBranch {
	if rt.MateEnabled && meta.HQ != nil && meta.HQ.Change != 0 && meta.HQ.Branch != "" {
		return &poolMigrateWorkBranch{Branch: meta.HQ.Branch, Source: poolMigrateWorkFromChange}
	}
	var main ops.PoolMigrateTree
	if len(census.Trees) > 0 {
		main = census.Trees[0]
	}
	branch := main.Branch
	if branch == "" {
		return &poolMigrateWorkBranch{Missing: "the dev service's checkout is on no branch (a detached HEAD), so a lease has none to check out: put the work on a branch of your own (`git switch -c <branch>`) and push it (`git push -u origin <branch>`)."}
	}
	if rt.MateEnabled {
		if branch == hqBase || strings.HasPrefix(branch, "mate/") {
			return &poolMigrateWorkBranch{Missing: fmt.Sprintf("the dev service's checkout is on %q, a branch HQ takes no push to: %q moves only by a person's merge, and a mate/ branch only as an open change's. Move the work onto a branch of your own (`git switch -c <branch>`) and push it (`git push -u origin <branch>`).", branch, hqBase)}
		}
		return &poolMigrateWorkBranch{Branch: branch, Source: poolMigrateWorkFromCurrent}
	}
	switch up := main.Upstream; {
	case up == nil:
		return &poolMigrateWorkBranch{Missing: fmt.Sprintf("the dev service's checkout is on %q, which tracks no branch on origin, so a lease has none to check out: push it (`git push -u origin %s`).", branch, branch)}
	case up.Remote != poolMigrateOrigin:
		return &poolMigrateWorkBranch{Missing: fmt.Sprintf("the dev service's checkout is on %q, which tracks %s/%s, and a lease clones origin: push it there (`git push -u origin %s`).", branch, up.Remote, up.Branch, branch)}
	default:
		return &poolMigrateWorkBranch{Branch: up.Branch, Source: poolMigrateWorkFromUpstream}
	}
}

// poolStanzaDoc is the provision step's import: the part's pool alone.
type poolStanzaDoc struct {
	Services []poolStanzaService `yaml:"services"`
}

type poolStanzaService struct {
	Hostname            string              `yaml:"hostname"`
	Type                string              `yaml:"type"`
	MinContainers       int                 `yaml:"minContainers"`
	MaxContainers       int                 `yaml:"maxContainers"`
	VerticalAutoscaling *poolStanzaVertical `yaml:"verticalAutoscaling,omitempty"`
	// Vault holds the pool's own service env. A service-level envVariables
	// block is deprecated in the import schema, and sshIsolation reaches the
	// pool's containers only as a service env variable.
	Vault map[string]string `yaml:"vault"`
}

type poolStanzaVertical struct {
	CPUMode string  `yaml:"cpuMode,omitempty"`
	MinCPU  int     `yaml:"minCpu,omitempty"`
	MaxCPU  int     `yaml:"maxCpu,omitempty"`
	MinRAM  float64 `yaml:"minRam,omitempty"`
	MaxRAM  float64 `yaml:"maxRam,omitempty"`
	MinDisk float64 `yaml:"minDisk,omitempty"`
	MaxDisk float64 `yaml:"maxDisk,omitempty"`
}

// renderPoolStanza renders the import the provision step names: the pool
// hostname, swarm@1, no containers until a lease asks for one, at most
// poolMigrateMaxContainers, the dev service's own vertical scale (read by id,
// a direct read), and the sshIsolation that admits this zcp service. It never
// carries a credential: GIT_TOKEN reaches the pool in the env step.
func renderPoolStanza(ctx context.Context, client platform.Client, dev platform.ServiceStack, pool, zcpHostname string) (string, []string, error) {
	svc := poolStanzaService{
		Hostname:      pool,
		Type:          poolMigratePoolType,
		MinContainers: poolMigrateMinContainers,
		MaxContainers: poolMigrateMaxContainers,
		Vault:         map[string]string{"sshIsolation": "vpn service@" + zcpHostname},
	}
	var warnings []string
	scaling, err := ops.FetchServiceScaling(ctx, client, dev.ID)
	switch {
	case err != nil:
		warnings = append(warnings, fmt.Sprintf("%s's scale could not be read (%v); the stanza leaves the pool's verticalAutoscaling to the platform's defaults.", dev.Name, err))
	case scaling != nil:
		v := poolStanzaVertical{
			CPUMode: scaling.CPUMode,
			MinCPU:  scaling.MinCPU, MaxCPU: scaling.MaxCPU,
			MinRAM: scaling.MinRAM, MaxRAM: scaling.MaxRAM,
			MinDisk: scaling.MinDisk, MaxDisk: scaling.MaxDisk,
		}
		if v != (poolStanzaVertical{}) {
			svc.VerticalAutoscaling = &v
		}
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(poolStanzaDoc{Services: []poolStanzaService{svc}}); err != nil {
		return "", nil, fmt.Errorf("encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return "", nil, fmt.Errorf("encode: %w", err)
	}
	return buf.String(), warnings, nil
}

// poolMigrateAt answers a candidate's current step.
func poolMigrateAt(resp poolMigrateResponse, step string, next *poolMigrateNext, nextUnavailable string) *mcp.CallToolResult {
	resp.Status = poolMigrateStatusStep
	resp.Step = step
	resp.Next = next
	resp.NextUnavailable = nextUnavailable
	order := poolMigrateStepOrder()
	current := slices.Index(order, step)
	for i, s := range order {
		state := poolMigrateStatePending
		switch {
		case i < current:
			state = poolMigrateStateDone
		case i == current:
			state = poolMigrateStateCurrent
		}
		resp.Steps = append(resp.Steps, poolMigrateStepState{Step: s, State: state})
	}
	return jsonResult(resp)
}

// poolMigrateRefused answers a non-candidate: every reason found, no step and
// no next call.
func poolMigrateRefused(resp poolMigrateResponse, refusals ...poolMigrateRefusal) *mcp.CallToolResult {
	resp.Status = poolMigrateStatusNotCandidate
	resp.Refusals = refusals
	return jsonResult(resp)
}

// poolMigrateCandidatesHint names the dev services this project tracks, for a
// call that named none.
func poolMigrateCandidatesHint(stateDir string) string {
	metas, _ := workflow.ListServiceMetas(stateDir)
	var devs []string
	for _, m := range metas {
		switch m.Mode {
		case "", topology.ModeDev, topology.ModeStandard:
			devs = append(devs, m.Hostname)
		case topology.ModeStage, topology.ModeSimple, topology.ModeLocalStage, topology.ModeLocalOnly:
		}
	}
	sort.Strings(devs)
	if len(devs) == 0 {
		return `Pass service="<dev hostname>"; this project tracks no dev service yet.`
	}
	return fmt.Sprintf(`Pass service="<dev hostname>". Dev services this project tracks: %s.`, strings.Join(devs, ", "))
}
