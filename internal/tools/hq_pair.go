package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A Mate keeps its code in HQ, and only a dev/stage pair fits it: the dev
// half is the checkout the agent works and pushes from with plain git, the
// stage half the verified basis a production is promoted from. The rules
// below were each measured missing on 2026-09-17, when "create a todo app" on
// a fresh Mate got bootstrapMode simple — one service, nothing pushed, no
// change, and the person expanding it by hand:
//
//   - the classic route plans every runtime of such a Mate as a standard pair;
//   - a push to HQ is watched for no build and offers no integration, and a
//     wired pair's direct deploys are never redirected.

// isStageOf reports whether hostname is one of m's stages; false for a nil meta.
func isStageOf(m *workflow.ServiceMeta, hostname string) bool {
	_, ok := m.StageFor(hostname)
	return ok
}

// hqPairPlanError refuses a classic-route plan that gives a Mate a runtime
// with no stage half — enrolled with its HQ or not yet: the pairs it plans now
// are the pairs it keeps its code in. mate is runtime.Info.MateEnabled. Nil
// outside a Mate, or when every runtime is a standard pair.
func hqPairPlanError(plan []workflow.BootstrapTarget, mate bool) *platform.PlatformError {
	if !mate {
		return nil
	}
	for _, target := range plan {
		rt := target.Runtime
		if rt.EffectiveMode() == topology.PlanModeStandard && len(rt.StageRefs()) > 0 {
			continue
		}
		return platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Plan validation failed: target %q: this Mate keeps its code in HQ, where code lands on main through changes, so every runtime is a dev/stage pair — bootstrapMode %q with no stage half has no stage to check its work on or promote to production", rt.DevHostname, rt.EffectiveMode()),
			fmt.Sprintf("Re-submit the plan with runtime.bootstrapMode=\"standard\" and an explicit stageHostname for %q (for example devHostname %q, stageHostname %q).", rt.DevHostname, rt.DevHostname, stageNameFor(rt.DevHostname)),
		)
	}
	return nil
}

// hqLaunchProductionRefusal refuses the whole launch-production workflow in a
// Mate, enrolled with its HQ or not yet: launch-production creates and
// imports its OWN production project on a user-owned remote
// (workflow_launch_production.go) — it has no case for an application's
// production, which can be a Zerops project or a Git repository branch
// the person adds with Put it live in Mate. mate is
// runtime.Info.MateEnabled. nil outside a Mate, so the classic route is
// untouched.
func hqLaunchProductionRefusal(ctx context.Context, httpClient ops.HTTPDoer, stateDir string, mate bool) *mcp.CallToolResult {
	if !mate {
		return nil
	}
	return launchFailedResponse(nil, topology.BlockerCategoryOther, "wired_mate_production_is_the_groups",
		"This Mate keeps its code in HQ, where an application's production can be a Zerops project or a Git repository branch the person adds "+
			"with Put it live in Mate — launch-production creates its own production project outside the "+
			"application and has no case for that. "+hqLaunchProductionNextStep(ctx, httpClient, stateDir))
}

// hqLaunchProductionNextStep says what is true of THIS Mate's own pairs rather
// than a lecture. A change the ledger keeps is only forgotten when a pass
// reads HQ, and that pass is backoff-gated — so a change the person already
// merged can still be sitting here as "open" the moment this runs. The
// Mate's own state is read fresh first, so "merge #N first" is never said
// about work that already landed. Three states, never collapsed into
// "nothing open means merged":
//   - a change still open after the fresh read → name it, tell the person to
//     merge it;
//   - a merge in HQ, recorded on the pair or not → the code is on main;
//     point to Put it live;
//   - no change at all, or one closed without merging → nothing of this
//     pair's work has reached main; it has to be pushed and merged first.
//
// A Mate that cannot reach HQ at all — no enrollment yet, or one it cannot
// read — knows none of the three, and says so.
func hqLaunchProductionNextStep(ctx context.Context, httpClient ops.HTTPDoer, stateDir string) string {
	if problem := hqEnrollmentProblem(); problem != "" {
		return fmt.Sprintf("Whether this Mate's work reached main is HQ's to say, and %s. Ask again once it is enrolled; production is added with Put it live and released with Release, both offered in this conversation and on the project's card in Mate.", problem)
	}
	var open []string
	merged := false
	if metas, err := workflow.ListServiceMetas(stateDir); err == nil {
		self := func() (hq.MateState, error) { return hq.MateState{}, hq.ErrNotEnrolled }
		if hqc, enrolled := openHQ(httpClient); enrolled {
			self = lazySelf(ctx, hqc)
		}
		for _, m := range metas {
			if !hqPairWired(m) {
				continue
			}
			state, err := self()
			if err != nil {
				return fmt.Sprintf("This Mate could not read its changes from HQ (%v), so whether its work reached main is unknown. Ask again once HQ can be reached; production is added with Put it live and released with Release, both offered in this conversation and on the project's card in Mate.", err)
			}
			if state.AppID == nil || *state.AppID != m.HQ.AppID {
				continue
			}
			for _, c := range changesIn(hqChanges(stateDir), m.HQ.AppID, m.HQ.Repo) {
				learnChangeLanding(stateDir, c, state)
			}
			// HQ keeps the merged history: read it there, rather than keeping
			// a second last-merge record.
			for _, change := range state.Changes {
				if change.Repo == m.HQ.Repo && change.State == hq.ChangeMerged {
					merged = true
				}
			}
			if change := openChangeIn(state, m.HQ.Repo); change != nil {
				open = append(open, fmt.Sprintf("%s's change #%d on %q", m.Hostname, change.Number, m.HQ.Repo))
			}
		}
	}
	if len(open) > 0 {
		return "Tell the person to merge " + strings.Join(open, " and ") +
			" first — once it lands, production is added with Put it live and released with Release, both offered in this conversation and on the project's card in Mate."
	}
	if merged {
		return "Tell the person: production is added with Put it live and released with Release, both offered in this conversation and on the project's card in Mate."
	}
	return "Tell the person: none of this Mate's work has reached its repository's main yet — it goes there by a branch of your own, pushed and merged first, then production is added with Put it live and released with Release, both offered in this conversation and on the project's card in Mate."
}

// stageNameFor suggests the stage half's hostname: appdev → appstage,
// todoapp → todoappstage.
func stageNameFor(devHostname string) string {
	if strings.HasSuffix(devHostname, "dev") && len(devHostname) > len("dev") {
		return strings.TrimSuffix(devHostname, "dev") + "stage"
	}
	return devHostname + "stage"
}

// hqPairWiredThrough reports whether a pair is wired through: its repository
// in HQ, its checkout descending from `main`, and a state that lets it push.
func hqPairWiredThrough(m *workflow.ServiceMeta) bool {
	return hqPairWired(m) && hqPairPushes(m.GitPushState)
}

// hqPairPushes reports whether a wired pair's state lets it push: set up, or
// marked broken by a git-push setup an older zcp ran — a mark on the
// credential, not on the wiring, so the pair is not wired again for it.
func hqPairPushes(state topology.GitPushState) bool {
	return state == topology.GitPushConfigured || state == topology.GitPushBroken
}

// hqRemoteOfThisMate reports whether a remote is a repository on this Mate's
// HQ, where nothing builds from a Mate's change.
func hqRemoteOfThisMate(remoteURL string) bool {
	return ops.IsHQRemote(remoteURL, hqAddress())
}

// hqHandoffNote is what the person needs from the Mate's closing message in a
// Mate that keeps its code in HQ: the change to review, and what they do next —
// in the words Mate shows under that message (Review, Put it live), never a
// path through other pages. Put it live is Mate's only while the application
// has no production, so it is named only while HQ says of none
// (hasProduction false, or an HQ that cannot say). Once production runs a
// release, Mate's turn note gives its address, and that, not the Mate's
// preview, is the app.
func hqHandoffNote(hasProduction bool) string {
	next := ", or, while the application has no production, Put it live to merge it and release it to production in one go"
	if hasProduction {
		next = ""
	}
	return "In your closing message tell the person: the change to review, by its link, and that they can press Review to look it over and merge it" + next + ". Once Mate has told you production runs a release, that address is the app: give it for anything real, and yours only as their preview."
}

// hqHasProduction is whether HQ says the Mate's application has a
// production; false where it says none, cannot be read, or is older than
// the answer.
func hqHasProduction(ctx context.Context, httpClient ops.HTTPDoer) bool {
	hqc, enrolled := openHQ(httpClient)
	if !enrolled {
		return false
	}
	state, err := hqc.Self(ctx)
	return err == nil && state.Production != nil && *state.Production
}
