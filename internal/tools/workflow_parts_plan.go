package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// repoPartsPlanError reads the repo of every existing dev runtime a plan
// pairs with stages, before the plan is accepted: when its zerops.yml
// declares several parts (ops.ReadPartsSignal), the pair holds one stage per
// part, built with that part's setup (docs/spec-mate.md §2.8a.4). A plan
// that leaves a part without a stage, or names a stage with no part, is
// refused with the plan made from the repo — so a multi-part pair's stages
// come from the repo, never from a guess made before it was read. nil
// outside a container, for a single-part repo, and when the repo cannot be
// read. adopt adds that the adopt route attaches only existing services.
func repoPartsPlanError(ctx context.Context, plan []workflow.BootstrapTarget, ssh ops.SSHDeployer, rt runtime.Info, adopt bool) *platform.PlatformError {
	if !rt.InContainer || ssh == nil {
		return nil
	}
	fixed := slices.Clone(plan)
	var reasons []string
	for i, target := range plan {
		r := target.Runtime
		if !r.IsExisting || r.EffectiveMode() != topology.PlanModeStandard {
			continue
		}
		sig, err := ops.ReadPartsSignal(ctx, ssh, r.DevHostname, "")
		if err != nil || len(sig.YAMLParts) < 2 {
			continue
		}
		stages, reason := stagesForParts(r.StageRefs(), sig.YAMLParts)
		if reason == "" {
			continue
		}
		reasons = append(reasons, fmt.Sprintf("target %q: its repo's zerops.yml declares the parts %s, and the pair holds one stage per part, built with that part's setup — %s",
			r.DevHostname, quoteAll(sig.YAMLParts), reason))
		r.ExplicitStage, r.StageSetupName, r.Stages = "", "", stages
		fixed[i].Runtime = r
	}
	if len(reasons) == 0 {
		return nil
	}
	planText := "runtime.stages listing {hostname, setupName} for every part"
	if planJSON, err := json.Marshal(fixed); err == nil {
		planText = "plan=" + string(planJSON)
	}
	suggestion := "Re-submit the plan with one stage per part: " + planText
	if adopt {
		suggestion = "The adopt route attaches only existing services: import each stage that does not exist yet (zerops_import, startWithoutCode: true), then re-submit the plan with one stage per part: " + planText
	}
	pe := platform.NewPlatformError(platform.ErrInvalidParameter,
		"Plan validation failed: "+strings.Join(reasons, "; "), suggestion)
	pe.Subcode = platform.SubcodeWorkerPlanShape
	return pe
}

// stagesForParts is the pair's stages made from the repo's parts: a stage the
// plan already gives a part keeps its hostname, a part without one gets
// <part>stage, and a stage built with a setup the repo does not declare yet
// (a part being added) stays. reason says what the planned stages miss, ""
// when every part has a stage and every stage a part.
func stagesForParts(planned []topology.StageRef, parts []string) ([]topology.StageRef, string) {
	bySetup := map[string]string{}
	var unnamed []string
	for _, s := range planned {
		if s.SetupName == "" {
			unnamed = append(unnamed, s.Hostname)
			continue
		}
		bySetup[s.SetupName] = s.Hostname
	}
	stages := make([]topology.StageRef, 0, len(parts))
	var missing []string
	for _, part := range parts {
		host, ok := bySetup[part]
		if !ok {
			missing = append(missing, part)
			host = part + "stage"
		}
		stages = append(stages, topology.StageRef{Hostname: host, SetupName: part})
	}
	for _, s := range planned {
		if s.SetupName != "" && !slices.Contains(parts, s.SetupName) {
			stages = append(stages, s)
		}
	}
	var reason []string
	if len(unnamed) > 0 {
		reason = append(reason, "stage "+quoteAll(unnamed)+" names no part")
	}
	if len(missing) > 0 {
		reason = append(reason, "part "+quoteAll(missing)+" has no stage")
	}
	return stages, strings.Join(reason, ", ")
}

func quoteAll(names []string) string {
	quoted := make([]string, 0, len(names))
	for _, n := range names {
		quoted = append(quoted, fmt.Sprintf("%q", n))
	}
	return strings.Join(quoted, ", ")
}
