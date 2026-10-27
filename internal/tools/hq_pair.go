package tools

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
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
// with no stage half — linked to its HQ or not yet: the pairs it plans now
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
			fmt.Sprintf("Re-submit the plan with runtime.bootstrapMode=\"standard\" and an explicit stageHostname for %q (for example devHostname %q, stageHostname %q). %s", rt.DevHostname, rt.DevHostname, stageNameFor(rt.DevHostname), hqPairNoStage),
		)
	}
	return nil
}

// hqPairNoStage is what "no stage" means in a Mate, said wherever the pair
// rule is: the stage is created and left empty, never a question to the
// person.
const hqPairNoStage = "A person who wants no stage, or a dev service only, still gets the pair: create the stage and leave it empty — imported without code it stays READY_TO_DEPLOY with no containers until something deploys to it — and say so; there is nothing to ask the person."

// hqPairDiscoverNote is the pair rule a Mate's classic discover step states
// before any plan is submitted (hqPairPlanError refuses the plans it rules
// out). All three Mate sessions on 2026-10-27 learned it only from a refused
// plan, then asked the person.
const hqPairDiscoverNote = "**This Mate keeps its code in HQ, so every runtime is a dev/stage pair.** " +
	"Plan each runtime with `bootstrapMode=\"standard\"` and an explicit `stageHostname` (appdev → appstage); " +
	"a plan with a dev or simple runtime is refused. " + hqPairNoStage

// appendHQPairDiscoverNote adds hqPairDiscoverNote to the discover step's
// guide of a Mate's classic route — a session records no route for classic —
// and nothing outside a Mate, on another route or past discover.
func appendHQPairDiscoverNote(resp *workflow.BootstrapResponse, engine *workflow.Engine, mate bool) {
	if !mate || resp == nil || resp.Current == nil || resp.Current.Name != workflow.StepDiscover {
		return
	}
	if route := bootstrapSessionRoute(engine); route != "" && route != workflow.BootstrapRouteClassic {
		return
	}
	resp.Current.DetailedGuide = strings.TrimSpace(resp.Current.DetailedGuide + "\n\n" + hqPairDiscoverNote)
}

// hqLaunchProductionRefusal refuses the whole launch-production workflow in a
// Mate, linked to its HQ or not yet: launch-production creates and
// imports its OWN production project on a user-owned remote
// (workflow_launch_production.go) — it has no case for an application's
// production, which can be a Zerops project or a Git repository branch
// the person adds with Put it live in Mate. mate is
// runtime.Info.MateEnabled. nil outside a Mate, so the classic route is
// untouched.
func hqLaunchProductionRefusal(ctx context.Context, httpClient ops.HTTPDoer, rt runtime.Info, stateDir string, mate bool) *mcp.CallToolResult {
	if !mate {
		return nil
	}
	return launchFailedResponse(nil, topology.BlockerCategoryOther, "wired_mate_production_is_the_groups",
		"This Mate keeps its code in HQ, where an application's production can be a Zerops project or a Git repository branch the person adds "+
			"with Put it live in Mate — launch-production creates its own production project outside the "+
			"application and has no case for that. "+hqLaunchProductionNextStep(ctx, httpClient, rt, stateDir))
}

// hqLaunchProductionNextStep says what is true of THIS Mate's own
// repositories rather than a lecture. A change the ledger keeps is only
// forgotten when a pass reads HQ, and that pass is backoff-gated — so a
// change the person already merged can still be sitting here as "open" the
// moment this runs. The Mate's own state is read fresh first, so "merge #N
// first" is never said about work that already landed. Three states, never
// collapsed into "nothing open means merged":
//   - changes still open after the fresh read → name each once, per
//     repository however many pairs share it, and tell the person to merge
//     them;
//   - a merge in HQ, recorded or not → the code is on main; point to Put it
//     live;
//   - no change at all, or one closed without merging → nothing of the
//     Mate's work has reached main; it has to be pushed and merged first.
//
// A Mate that cannot reach HQ at all — no key or HQ origin in its container —
// knows none of the three, and says so.
func hqLaunchProductionNextStep(ctx context.Context, httpClient ops.HTTPDoer, rt runtime.Info, stateDir string) string {
	var open []string
	opened := 0
	merged := false
	if metas, err := workflow.ListServiceMetas(stateDir); err == nil {
		self := func() (hq.MateState, error) { return hq.MateState{}, hq.ErrNoIdentity }
		address := ""
		if hqc, linked := openHQ(httpClient, rt); linked {
			self, address = lazySelf(ctx, hqc), hqc.Address()
		}
		var repos []changeRef
		for _, m := range metas {
			if hqPairWired(m) && !slices.Contains(repos, changeRef{AppID: m.HQ.AppID, Repo: m.HQ.Repo}) {
				repos = append(repos, changeRef{AppID: m.HQ.AppID, Repo: m.HQ.Repo})
			}
		}
		slices.SortFunc(repos, func(a, b changeRef) int { return strings.Compare(a.Repo, b.Repo) })
		for _, repo := range repos {
			state, err := self()
			if err != nil {
				return fmt.Sprintf("This Mate could not read its changes from HQ (%v), so whether its work reached main is unknown. Ask again once HQ can be reached; production is added with Put it live and released with Release, both offered in this conversation and on the project's card in Mate.", err)
			}
			if state.AppID == nil || *state.AppID != repo.AppID {
				continue
			}
			for _, c := range changesIn(hqChanges(stateDir), repo.AppID, repo.Repo) {
				learnChangeLanding(stateDir, address, c, state)
			}
			// HQ keeps the merged history: read it there, rather than keeping
			// a second last-merge record.
			var numbers []int
			for _, change := range state.Changes {
				switch {
				case change.Repo != repo.Repo:
				case change.State == hq.ChangeMerged:
					merged = true
				case change.State == hq.ChangeOpen:
					numbers = append(numbers, change.Number)
				}
			}
			if len(numbers) > 0 {
				slices.Sort(numbers)
				open = append(open, repo.Repo+" "+changeNumbersWords(numbers))
				opened += len(numbers)
			}
		}
	}
	if len(open) > 0 {
		lands := "it lands"
		if opened > 1 {
			lands = "they land"
		}
		return "Tell the person to merge " + strings.Join(open, ", and ") +
			" first — an open change is never in a release, which takes main as it is; once " + lands + ", production is added with Put it live and released with Release, both offered in this conversation and on the project's card in Mate."
	}
	if merged {
		return "Tell the person: production is added with Put it live and released with Release, both offered in this conversation and on the project's card in Mate."
	}
	return "Tell the person: none of this Mate's work has reached its repository's main yet — it goes there by a branch of your own, pushed and merged first, then production is added with Put it live and released with Release, both offered in this conversation and on the project's card in Mate."
}

// changeNumbersWords names change numbers as one list: "#2", "#2 and #6",
// "#2, #6 and #7".
func changeNumbersWords(numbers []int) string {
	named := make([]string, 0, len(numbers))
	for _, n := range numbers {
		named = append(named, fmt.Sprintf("#%d", n))
	}
	if len(named) == 1 {
		return named[0]
	}
	return strings.Join(named[:len(named)-1], ", ") + " and " + named[len(named)-1]
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
func hqRemoteOfThisMate(remoteURL string, rt runtime.Info) bool {
	return ops.IsHQRemote(remoteURL, hqAddress(rt))
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
func hqHasProduction(ctx context.Context, httpClient ops.HTTPDoer, rt runtime.Info) bool {
	hqc, linked := openHQ(httpClient, rt)
	if !linked {
		return false
	}
	state, err := hqc.Self(ctx)
	return err == nil && state.Production != nil && *state.Production
}
