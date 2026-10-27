package tools

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A dev service serving several parts — the monorepo model: one repository,
// one dev service running every part's dev servers, one stage per part —
// moves onto one pool per part (docs/spec-workflows.md §13 PM-12). Each part
// walks the steps a single part does, on its own pool, one part per call:
// a call names a part by its stage or its pool, or walks the first part not
// yet ready to retire. The retire waits for every part and deletes the dev
// service once, after splitting the pair into one pool-keyed pair per part.

// poolMigratePartStageSuffix is the suffix a stage hostname drops to name its
// part's pool: `webstage` → `webpool`.
const poolMigratePartStageSuffix = "stage"

// poolMigratePart is one part a call walks: its stage ("" for a dev-only
// service), that stage's setup, the part's pool, and the dev service the part
// runs on (poolMigratePartDev).
type poolMigratePart struct {
	stage, setup, pool, dev string
}

// poolMigratePartState is one part of a dev serving several, as an answer
// lists it: its stage and setup, its pool, and the step it is at.
type poolMigratePartState struct {
	Stage string `json:"stage"`
	Setup string `json:"setup,omitempty"`
	Pool  string `json:"pool"`
	Step  string `json:"step,omitempty"`
	// Env is what the env step did on the part's pool, on the answer of a
	// pool-env call that copied every part's env.
	Env *poolMigratePartEnv `json:"env,omitempty"`
}

// poolMigratePartEnv is what the env step did on one part's pool: whether its
// env is the dev's now, the keys it wrote and those that did not take, and
// where the GIT_TOKEN stands.
type poolMigratePartEnv struct {
	Done     bool                     `json:"done"`
	Written  []ops.PoolMigrateEnvKey  `json:"written,omitempty"`
	Failed   []ops.PoolMigrateEnvSkip `json:"failed,omitempty"`
	GitToken string                   `json:"gitToken,omitempty"`
}

// poolMigrateEnvEveryPart is the env step of a dev serving several parts
// called without a part: every part whose pool is settled gets the dev's env
// (ops.ApplyPoolMigrateEnv) in this one call, a part whose pool is not stays
// at its step, and the answer is the read side's for the first part not yet
// ready to retire, every part listed with what the step did on its pool and
// every pool's env requests.
func poolMigrateEnvEveryPart(ctx context.Context, d poolMigrateDeps, dr *poolMigrateDevRead, input WorkflowInput) (*mcp.CallToolResult, any, error) {
	answers := make([]poolMigrateResponse, len(dr.parts))
	envs := make([]*ops.PoolMigrateEnv, len(dr.parts))
	for i := range dr.parts {
		call, final, failed := dr.partRead(ctx, d, i)
		switch {
		case failed != nil:
			return failed, nil, nil
		case final != nil:
			answers[i] = *final
			continue
		}
		env, err := ops.ApplyPoolMigrateEnv(ctx, d.client, d.ssh, call.envInput(d, input))
		if err != nil {
			return convertError(fmt.Errorf("pool-migrate: copy the env to %s: %w", call.pool.Name, err), WithRecoveryStatus()), nil, nil
		}
		envs[i] = env
		answers[i] = poolMigrateSettledAnswer(ctx, d, call, env)
	}
	resp := dr.partsAnswer(answers, max(dr.current(answers), 0))
	resp.EnvRequests = nil
	wrote := false
	for i, a := range answers {
		resp.EnvRequests = append(resp.EnvRequests, a.EnvRequests...)
		if env := envs[i]; env != nil {
			wrote = true
			resp.Parts[i].Env = &poolMigratePartEnv{Done: env.Done, Written: env.Written, Failed: env.Failed, GitToken: env.GitToken.State}
		}
	}
	if !wrote {
		resp.Warnings = append(resp.Warnings, "Nothing was written: a part's pool gets its env once it exists and its own processes have finished.")
	}
	resp.Action = actionPoolEnv
	return d.respond(resp), nil, nil
}

// poolMigrateDevRead is what a call read of the dev service before any part:
// the answer every part's starts from — the dev, its stages, its census and
// the work's branch — and the parts it serves.
type poolMigrateDevRead struct {
	input    WorkflowInput
	resp     poolMigrateResponse
	corpus   []workflow.KnowledgeAtom
	meta     *workflow.ServiceMeta
	dev      platform.ServiceStack
	byName   map[string]platform.ServiceStack
	services []platform.ServiceStack
	parts    []poolMigratePart
	// selected is the part the call named — by its stage or its pool — and
	// -1 when a dev serving several parts was named without one.
	selected int
}

// multiPart reports a dev serving several parts.
func (dr *poolMigrateDevRead) multiPart() bool { return len(dr.parts) > 1 }

// partPools are the parts' pools, in the order of the pair's stages.
func (dr *poolMigrateDevRead) partPools() []string {
	out := make([]string, 0, len(dr.parts))
	for _, p := range dr.parts {
		out = append(out, p.pool)
	}
	return out
}

// partAnswers are the read side's answers of every part, in order.
func (dr *poolMigrateDevRead) partAnswers(ctx context.Context, d poolMigrateDeps) ([]poolMigrateResponse, *mcp.CallToolResult) {
	answers := make([]poolMigrateResponse, len(dr.parts))
	for i := range dr.parts {
		resp, _, failed := dr.partAnswer(ctx, d, i)
		if failed != nil {
			return nil, failed
		}
		answers[i] = resp
	}
	return answers, nil
}

// current is the part a call walks: the one it named, else the first part not
// yet ready to retire; -1 when it named none and every part is ready.
func (dr *poolMigrateDevRead) current(answers []poolMigrateResponse) int {
	if dr.selected >= 0 {
		return dr.selected
	}
	return slices.IndexFunc(answers, func(a poolMigrateResponse) bool { return a.Step != poolMigrateStepReadyToRetire })
}

// partsAnswer is the answer for a dev serving several parts: part cur's own,
// every part listed with its step. The retire waits for every part, so a
// part that is ready while another is not names the call that walks the
// first unfinished one; once every part is ready the next call is the retire
// of the whole dev — and with no part named, the answer is the dev's alone,
// no part's pool, env or image in it.
func (dr *poolMigrateDevRead) partsAnswer(answers []poolMigrateResponse, cur int) poolMigrateResponse {
	states := make([]poolMigratePartState, 0, len(dr.parts))
	unfinished := -1
	for i, p := range dr.parts {
		states = append(states, poolMigratePartState{Stage: p.stage, Setup: p.setup, Pool: p.pool, Step: answers[i].Step})
		if unfinished < 0 && answers[i].Step != poolMigrateStepReadyToRetire {
			unfinished = i
		}
	}
	resp := answers[max(cur, 0)]
	resp.Parts = states
	dev := dr.meta.Hostname
	switch {
	case unfinished < 0:
		if cur < 0 {
			resp.Pool, resp.Part, resp.Inputs.Pool = "", "", ""
			resp.Env, resp.EnvRequests, resp.Image = nil, nil, nil
		}
		once := "It deletes " + dev + " once"
		if dr.meta.CodeVolume != nil {
			once = "It deletes each of them once, the code volume last,"
		}
		resp.Next = &poolMigrateNext{
			Tool: "zerops_workflow",
			Args: map[string]string{"workflow": workflowPoolMigrate, "action": actionRetire, "service": dev},
			Why: fmt.Sprintf("Every part's pool (%s) is leased on the work's branch. The retire's first call reads every part's guards again and names in wouldDestroy what deleting %s erases, changing nothing; ask the person, and only once they agreed by name send the acknowledged call it names. %s and leaves one pair per part, keyed by its pool.",
				strings.Join(dr.partPools(), ", "), poolMigrateRetireDeletes(dr.meta), once),
		}
		resp.NextUnavailable = ""
	case resp.Step == poolMigrateStepReadyToRetire:
		next := dr.parts[unfinished]
		resp.Next = &poolMigrateNext{
			Tool: "zerops_workflow",
			Args: map[string]string{"workflow": workflowPoolMigrate, "service": dev, "pool": next.pool},
			Why: fmt.Sprintf("The part of %s is ready to retire, and the retire waits for every part: the part of %s is at its %s step. This call walks it.",
				dr.parts[max(cur, 0)].stage, next.stage, answers[unfinished].Step),
		}
		resp.NextUnavailable = ""
	}
	return resp
}

// poolMigrateParts are the parts the meta's dev serves and the part the call
// names, with the refusals the parts' pool hostnames and the call's inputs
// decide. A dev serving one part (or none: a dev-only service) has one part,
// its pool the `pool` input or `<stem>pool`, and it is the part the call
// walks. A dev serving several has a part per stage, each part's pool named
// after its stage (`<stage without its stage suffix>pool`, never an input);
// a call names one by its stage (service) or its pool (pool), and a pool
// naming no part is refused.
func poolMigrateParts(meta *workflow.ServiceMeta, service, poolInput string, byName map[string]platform.ServiceStack) ([]poolMigratePart, int, []poolMigrateRefusal) {
	if len(meta.Stages) <= 1 {
		part := poolMigratePart{stage: meta.FirstStage().Hostname, setup: meta.FirstStage().SetupName, pool: poolInput, dev: poolMigratePartDev(meta, meta.FirstStage().Hostname)}
		if part.pool == "" {
			part.pool = poolMigrateDefaultPool(meta.Hostname)
		}
		if r := poolMigratePoolRefusal(part.pool, byName, ""); r != nil {
			if r.Reason == poolMigrateRefusePoolInvalid {
				part.pool = ""
			}
			return []poolMigratePart{part}, 0, []poolMigrateRefusal{*r}
		}
		return []poolMigratePart{part}, 0, nil
	}
	var refusals []poolMigrateRefusal
	parts := make([]poolMigratePart, 0, len(meta.Stages))
	owner := map[string]string{}
	for _, stage := range meta.Stages {
		part := poolMigratePart{stage: stage.Hostname, setup: stage.SetupName, pool: poolMigratePartPool(stage.Hostname), dev: poolMigratePartDev(meta, stage.Hostname)}
		if r := poolMigratePoolRefusal(part.pool, byName, stage.Hostname); r != nil {
			refusals = append(refusals, *r)
		} else if other, taken := owner[part.pool]; taken {
			refusals = append(refusals, poolMigrateRefusal{
				Reason:  poolMigrateRefusePoolTaken,
				Message: fmt.Sprintf("%s would be the pool of both the part of %q and the part of %q: a part's pool is named after its stage, and each part moves onto a pool of its own.", part.pool, other, stage.Hostname),
			})
		}
		owner[part.pool] = stage.Hostname
		parts = append(parts, part)
	}
	selected := slices.IndexFunc(parts, func(p poolMigratePart) bool { return p.stage == service })
	if poolInput != "" {
		byPool := slices.IndexFunc(parts, func(p poolMigratePart) bool { return p.pool == poolInput })
		switch {
		case byPool < 0:
			refusals = append(refusals, poolMigrateRefusal{
				Reason:  poolMigrateRefusePoolNotAPart,
				Message: fmt.Sprintf("%s serves several parts, each moving onto a pool named after its stage (%s); %q is none of them. Pass the pool of the part to walk, or name the part by its stage as service.", meta.Hostname, poolMigratePartList(parts), poolInput),
			})
		case selected >= 0 && selected != byPool:
			refusals = append(refusals, poolMigrateRefusal{
				Reason:  poolMigrateRefusePoolNotAPart,
				Message: fmt.Sprintf("service names the part of %s, whose pool is %s, while pool %q is the part of %s's: name one part.", parts[selected].stage, parts[selected].pool, poolInput, parts[byPool].stage),
			})
		default:
			selected = byPool
		}
	}
	return parts, selected, refusals
}

// poolMigratePoolRefusal refuses a part's pool hostname the platform would
// not take, or one a service that is no pool holds; nil when it is free or
// the part's pool already. stage names the part when the dev serves several.
func poolMigratePoolRefusal(pool string, byName map[string]platform.ServiceStack, stage string) *poolMigrateRefusal {
	named, recovery := pool, `Pass pool="<free hostname>" to name the part's pool otherwise, and the same pool on every later call.`
	invalid := fmt.Sprintf("The pool hostname %q", pool)
	if stage != "" {
		named = fmt.Sprintf("%s, the pool of the part of %s,", pool, stage)
		invalid = fmt.Sprintf("The pool of the part of %s, %q,", stage, pool)
		recovery = "A part's pool is named after its stage, so the part does not move while that hostname is taken."
	}
	if perr := platform.ValidateHostname(pool); perr != nil {
		return &poolMigrateRefusal{
			Reason:  poolMigrateRefusePoolInvalid,
			Message: fmt.Sprintf("%s cannot be a service hostname: %s. %s", invalid, perr.Message, perr.Suggestion),
		}
	}
	if svc, ok := byName[pool]; ok && !topology.IsPoolType(svc.ServiceStackTypeInfo.ServiceStackTypeVersionName) {
		return &poolMigrateRefusal{
			Reason:  poolMigrateRefusePoolTaken,
			Message: fmt.Sprintf("%s is already a %s service in this project, not a pool. %s", named, svc.ServiceStackTypeInfo.ServiceStackTypeVersionName, recovery),
		}
	}
	return nil
}

// poolMigrateOwnerRefusals refuses each part's pool that is another pair's: a
// pair other than dev's is keyed by it or names it (a dev moved onto it
// before, or a part provisioned as a pool), or zcp gave it another dev
// service's env in that dev's own migration (PM-4's record). Its env step
// would write dev's env — its secrets, a GIT_TOKEN, the pool's default
// repository — over a pool serving another app, and the re-key would refuse
// only at the retire. Two dev services can land on one pool by default
// (`app` and `appdev` both name `apppool`). Read before any write; reads only
// zcp's own records.
func poolMigrateOwnerRefusals(stateDir, dev string, parts []poolMigratePart, byName map[string]platform.ServiceStack) ([]poolMigrateRefusal, error) {
	var refusals []poolMigrateRefusal
	for _, part := range parts {
		if part.pool == "" {
			continue
		}
		held := ""
		pair, err := workflow.FindServiceMeta(stateDir, part.pool)
		if err != nil {
			return nil, fmt.Errorf("read the pair naming %s: %w", part.pool, err)
		}
		switch {
		case pair != nil && pair.Hostname != dev:
			held = fmt.Sprintf("the pair keyed by %q holds it as its dev pool", pair.Hostname)
			if pair.Hostname != part.pool {
				held = fmt.Sprintf("the pair keyed by %q names it", pair.Hostname)
			}
		default:
			if svc, ok := byName[part.pool]; ok {
				if owner := ops.PoolMigrateEnvOwner(stateDir, part.pool, svc.ID); owner != "" && owner != dev {
					held = fmt.Sprintf("zcp gave it %s's env in that dev service's own migration onto it", owner)
				}
			}
		}
		if held == "" {
			continue
		}
		named, recovery := part.pool, `Pass pool="<free hostname>" to name the part's pool otherwise, and the same pool on every later call; nothing was written to `+part.pool+"."
		if part.stage != "" && len(parts) > 1 {
			named = fmt.Sprintf("%s, the pool of the part of %s,", part.pool, part.stage)
			recovery = "A part's pool is named after its stage, so the part does not move while that pool is another pair's; nothing was written to it."
		}
		refusals = append(refusals, poolMigrateRefusal{
			Reason:  poolMigrateRefusePoolTaken,
			Message: fmt.Sprintf("%s is another pair's pool: %s. Its env step would write %s's env over it. %s", named, held, dev, recovery),
		})
	}
	return refusals, nil
}

// poolMigratePartPool is the pool of the part whose stage is stage: the
// stage hostname without its `stage` suffix, then `pool` (`webstage` →
// `webpool`, `web` → `webpool`).
func poolMigratePartPool(stage string) string {
	stem := strings.TrimSuffix(stage, poolMigratePartStageSuffix)
	if stem == "" {
		stem = stage
	}
	return stem + poolMigratePoolSuffix
}

// poolMigratePartDev is the dev service the part of stage runs on, whose env
// and scale its pool takes: the pair's dev half — or, for a variant-B pair,
// the dev service of the pair named like the stage (`apistage` → `apidev` or
// `api`), as the stand-up pairs a stage with its dev service — else the dev
// half, which serves every part no dev service of its own is named for.
func poolMigratePartDev(meta *workflow.ServiceMeta, stage string) string {
	if meta.CodeVolume == nil || stage == "" {
		return meta.Hostname
	}
	stem := strings.TrimSuffix(stage, poolMigratePartStageSuffix)
	for _, host := range append([]string{meta.Hostname}, meta.CodeVolume.DevServiceHostnames()...) {
		if host == stem+poolMigrateDevSuffix || host == stem {
			return host
		}
	}
	return meta.Hostname
}

// poolMigratePartList names each part's pool beside its stage.
func poolMigratePartList(parts []poolMigratePart) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, p.pool+" for "+p.stage)
	}
	return strings.Join(out, ", ")
}
