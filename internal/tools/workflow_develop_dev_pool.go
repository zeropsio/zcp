package tools

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// developDevPoolAtomID is the atom carrying a part's dev-pool loop: lease a
// container, work in it, push, sync, release.
const developDevPoolAtomID = "develop-dev-pool"

// devPoolStart is what a develop start makes of the project's Swarm pools
// (docs/spec-workflows.md §8 DP-29): the pools it answers with the dev-pool
// loop, or a refusal of a scope naming pools beside hosts develop cannot take
// together with them. Zero when the start takes its usual path.
type devPoolStart struct {
	pools   []string
	refusal *platform.PlatformError
}

// classifyDevPoolStart reads a develop start's scope against the project's
// live services. A pool counts unless a pair is keyed by it — that one keeps
// its own refusal (devPoolInScope). A scope naming only pools gets their loop
// whatever else the project holds; with no scope, a project whose only
// runtimes are pools and that zcp holds no record of gets the loop of every
// pool. Pools named beside other hosts are refused naming the lease once zcp
// holds records (before that, a runtime still to adopt answers
// ADOPT_REQUIRED); in a project of pools only, a scope naming any other host
// is refused naming the pools. services nil — the list could not be read —
// leaves the start as it was.
func classifyDevPoolStart(scope []string, services []platform.ServiceStack, self string, metas []*workflow.ServiceMeta) devPoolStart {
	keyed := developRuntimes(metas).devPools
	var pools []string
	appRuntime := false
	for i := range services {
		svc := &services[i]
		if svc.IsSystem() || (self != "" && svc.Name == self) {
			continue
		}
		switch topology.RuntimeClassFor(svc.ServiceStackTypeInfo.ServiceStackTypeVersionName) {
		case topology.RuntimePool:
			if keyed[svc.Name] == nil {
				pools = append(pools, svc.Name)
			}
		case topology.RuntimeManaged, topology.RuntimeSandbox:
		case topology.RuntimeDynamic, topology.RuntimeStatic, topology.RuntimeImplicitWeb, topology.RuntimeUnknown:
			appRuntime = true
		}
	}
	if len(pools) == 0 {
		return devPoolStart{}
	}
	slices.Sort(pools)
	poolsOnly := len(metas) == 0 && !appRuntime

	var named, others []string
	for _, h := range scope {
		switch {
		case h == "" || slices.Contains(named, h) || slices.Contains(others, h):
		case slices.Contains(pools, h):
			named = append(named, h)
		default:
			others = append(others, h)
		}
	}
	slices.Sort(named)
	switch {
	case len(named) > 0 && len(others) == 0:
		return devPoolStart{pools: named}
	case len(named) == 0 && len(others) == 0 && poolsOnly:
		return devPoolStart{pools: pools}
	case len(others) > 0 && poolsOnly:
		return devPoolStart{refusal: platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("scope names %v, no service develop works on: this project's only runtimes are the Swarm pools %v, and develop deploys and verifies no pool", others, pools),
			fmt.Sprintf("Work on a part's dev pool in a container leased on the work's branch: %s. Start develop with scope=%s — or no scope — for the dev-pool loop.",
				devPoolLeaseCall(pools), workflow.ListArg(pools)))}
	case len(named) > 0 && len(metas) > 0:
		return devPoolStart{refusal: platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("scope names %s, %s: develop deploys and verifies no pool — a part's dev pool is worked on in a container leased on the work's branch", strings.Join(named, ", "), poolNoun(len(named))),
			fmt.Sprintf("Work in a lease — %s — and scope only the runtime services: scope=%s.", devPoolLeaseCall(named), workflow.ListArg(others)))}
	}
	return devPoolStart{}
}

// devPoolSelf is zcp's own service hostname inside a container — never a
// runtime of the project's — and "" elsewhere.
func devPoolSelf(rt runtime.Info) string {
	if rt.InContainer {
		return rt.ServiceName
	}
	return ""
}

// poolNoun names one Swarm pool or several.
func poolNoun(n int) string {
	if n == 1 {
		return "a Swarm pool"
	}
	return "Swarm pools"
}

// devPoolLeaseCall is the acquire of a container of each of pools, written
// as the call is made.
func devPoolLeaseCall(pools []string) string {
	if len(pools) == 1 {
		return fmt.Sprintf(`zerops_lease action="acquire" pool=%q repo="<https clone url>" branch="<branch>"`, pools[0])
	}
	return fmt.Sprintf(`zerops_lease action="acquire" pools=%s repo="<https clone url>" branch="<branch>"`, workflow.ListArg(pools))
}

// renderDevPoolBriefing answers a develop start whose scope names only Swarm
// pools, or one in a project whose only runtimes are pools
// (docs/spec-workflows.md §8 DP-29). It opens no work session — develop
// deploys and verifies runtime services, and a pool's work is a lease — and
// answers with the envelope as status reads it, the dev-pool loop, and the
// acquire as the next step.
func renderDevPoolBriefing(ctx context.Context, engine *workflow.Engine, client platform.Client, projectID string, rt runtime.Info, pools []string) (*mcp.CallToolResult, any, error) {
	envelope, err := workflow.ComputeEnvelope(ctx, client, engine.StateDir(), projectID, rt, time.Now(),
		leaseEnvelopeOptions(client, projectID, engine.StateDir(), rt, leaseStatusTimeout)...)
	if err != nil {
		return convertError(wrapStageErr("Compute envelope", err), WithRecoveryStatus()), nil, nil
	}
	corpus, err := workflow.LoadAtomCorpus()
	if err != nil {
		return convertError(wrapStageErr("Load knowledge atoms", err), WithRecoveryStatus()), nil, nil
	}
	guidance := []string{devPoolBriefingLead(pools)}
	if body := atomBody(corpus, developDevPoolAtomID); body != "" {
		guidance = append(guidance, body)
	}
	plan := workflow.Plan{Primary: workflow.LeaseAcquireAction(pools)}
	return briefingResult(workflow.Response{Envelope: envelope, Guidance: guidance, Plan: &plan}), nil, nil
}

// devPoolBriefingLead heads the dev-pool briefing: why no session opened, and
// what to do with a pool that turns out to be an app's.
func devPoolBriefingLead(pools []string) string {
	list := strings.Join(pools, ", ")
	return fmt.Sprintf("### No develop session — %s: work in a lease\n\n"+
		"No develop session was opened: develop deploys and verifies runtime services, and nothing deploys or verifies a pool itself. "+
		"A part's dev pool is worked on in a container leased on the work's branch — acquire it (Next below), then work, commit, sync and release in it as the loop below says. "+
		"A pool an app calls with `${<pool>_API_TOKEN}` is that app's infrastructure, not a dev pool: leave it to the app, as a lease takes one of its containers.\n", list)
}
