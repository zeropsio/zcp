package tools

import (
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

// A Mate keeps its code in HQ, and a part it plans is born pooled
// (docs/spec-mate.md §2.8b B-9): a dev pool the agent leases a container of
// per branch, and a stage, the verified basis a production is promoted from.
// The classic route plans every runtime of a Mate as one part of that shape —
// `<part>pool` beside `<part>stage` (workflow.PoolOfStage) — and the plan's
// discover call creates the parts through the stand-up's pooled run
// (pooled_part.go). A dev service with its stage, made in a Mate before this
// rule or added with an old application, keeps working as it is: it is read,
// deployed and wired as before, and nothing moves it.

// isStageOf reports whether hostname is one of m's stages; false for a nil meta.
func isStageOf(m *workflow.ServiceMeta, hostname string) bool {
	_, ok := m.StageFor(hostname)
	return ok
}

// matePooledPart is the part a Mate's plan target names, by the pairing rule:
// its pool, its stage and the part. ok is false for a target that is no
// `<part>pool` beside its one `<part>stage` in a standard pair; the part is
// then the one the target most likely means, for the refusal.
func matePooledPart(rt workflow.RuntimeTarget) (pool, stage, part string, ok bool) {
	stages := rt.StageHostnames()
	if rt.EffectiveMode() != topology.PlanModeStandard {
		stages = nil
	}
	if stage, part, found := workflow.StageOfPool(rt.DevHostname); found {
		return rt.DevHostname, stage, part, len(stages) == 1 && stages[0] == stage
	}
	candidates := []string{stageNameFor(rt.DevHostname)}
	if len(stages) > 0 {
		candidates = append([]string{stages[0]}, candidates...)
	}
	for _, named := range candidates {
		if pool, part, found := workflow.PoolOfStage(named); found {
			return pool, named, part, false
		}
	}
	return "", "", "", false
}

// matePooledPlanError refuses a Mate's classic plan whose runtime is not a
// part's dev pool beside its one stage, before anything is recorded: the
// error names the pooled plan for that target. mate is
// runtime.Info.MateEnabled. Nil outside a Mate, or when every runtime is a
// part.
func matePooledPlanError(plan []workflow.BootstrapTarget, mate bool) *platform.PlatformError {
	if !mate {
		return nil
	}
	for _, target := range plan {
		rt := target.Runtime
		pool, stage, _, ok := matePooledPart(rt)
		if ok {
			continue
		}
		pe := platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Plan validation failed: target %q: this Mate keeps its code in HQ and creates each part of the app as a dev pool beside its stage, so a runtime is planned as devHostname %q, stageHostname %q, bootstrapMode \"standard\"", rt.DevHostname, pool, stage),
			fmt.Sprintf("Re-submit the plan with devHostname %q, stageHostname %q, the same type %q and bootstrapMode \"standard\" for this target; the call creates the repository, the pool and the stage. %s", pool, stage, rt.Type, matePooledPlanKeeps),
		)
		pe.Subcode = platform.SubcodeWorkerPlanShape
		return pe
	}
	return nil
}

// matePooledPlanKeeps is what the pooled rule leaves as it is, said wherever
// the rule is.
const matePooledPlanKeeps = "A dev service with its stage this project already has keeps working untouched; nothing moves it."

// matePooledPlanNote is the pooled rule a Mate's classic discover step states
// before any plan is submitted (matePooledPlanError refuses the plans it
// rules out).
const matePooledPlanNote = "**This Mate keeps its code in HQ and creates each part of the app as a dev pool beside its stage.** " +
	"Plan each runtime as `devHostname=\"<part>pool\"`, `stageHostname=\"<part>stage\"`, its type and `bootstrapMode=\"standard\"` (apppool → appstage), " +
	"and name the HQ repository all parts share with `repository` (one part defaults to its name). " +
	"Completing discover with that plan creates the repository, its first commit, the pools and the stages and builds them — it takes minutes and reports progress; " +
	"the same plan again continues where it stopped. A plan with a dev service or another pairing is refused. " + matePooledPlanKeeps

// appendMatePooledPlanNote adds matePooledPlanNote to the discover step's
// guide of a Mate's classic route — a session records no route for classic —
// and nothing outside a Mate, on another route or past discover.
func appendMatePooledPlanNote(resp *workflow.BootstrapResponse, engine *workflow.Engine, mate bool) {
	if !mate || resp == nil || resp.Current == nil || resp.Current.Name != workflow.StepDiscover {
		return
	}
	if route := bootstrapSessionRoute(engine); route != "" && route != workflow.BootstrapRouteClassic {
		return
	}
	resp.Current.DetailedGuide = strings.TrimSpace(resp.Current.DetailedGuide + "\n\n" + matePooledPlanNote)
}

// hqLaunchProductionRefusal refuses the whole launch-production workflow in a
// Mate, linked to its HQ or not yet: launch-production creates and
// imports its OWN production project on a user-owned remote
// (workflow_launch_production.go) — it has no case for an application's
// production, which can be a Zerops project or a Git repository branch
// the person adds with Put it live in Mate. mate is
// runtime.Info.MateEnabled. nil outside a Mate, so the classic route is
// untouched.
func hqLaunchProductionRefusal(httpClient ops.HTTPDoer, rt runtime.Info, stateDir string, mate bool) *mcp.CallToolResult {
	if !mate {
		return nil
	}
	return launchFailedResponse(nil, topology.BlockerCategoryOther, "wired_mate_production_is_the_groups",
		"This Mate keeps its code in HQ, where an application's production can be a Zerops project or a Git repository branch the person adds "+
			"with Put it live in Mate — launch-production creates its own production project outside the "+
			"application and has no case for that. "+hqLaunchProductionNextStep(httpClient, rt, stateDir))
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
//   - a merge in HQ, recorded or not — the Mate's own change merged, or
//     landed with another whose merge carried it → the code is on main;
//     point to Put it live;
//   - no change at all, or one closed without merging, its work not on
//     main → nothing of the Mate's work has reached main; it has to be
//     pushed and merged first.
//
// A Mate that cannot reach HQ at all — no key or HQ origin in its container —
// knows none of the three, and says so. In an application that limits who
// merges (limited, as the state read says) none of the three sends the person
// to merge or release: someone who merges there does both, and the rule
// follows (mergeLimitedRule).
func hqLaunchProductionNextStep(httpClient ops.HTTPDoer, rt runtime.Info, stateDir string) string {
	var open []string
	opened := 0
	merged := false
	limited := false
	if metas, err := workflow.ListServiceMetas(stateDir); err == nil {
		self := func() (hq.MateState, error) { return hq.MateState{}, hq.ErrNoIdentity }
		address := ""
		if hqc, linked := openHQ(httpClient, rt); linked {
			self, address = lazySelf(hqc), hqc.Address()
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
				return fmt.Sprintf("This Mate could not read its changes from HQ (%v), so whether its work reached main is unknown. Ask again once HQ can be reached; production is added with Put it live on the project's card and released with Release in Mate.", err)
			}
			if state.AppID == nil || *state.AppID != repo.AppID {
				continue
			}
			limited = state.MergeLimited
			for _, c := range changesIn(hqChanges(stateDir), repo.AppID, repo.Repo) {
				learnChangeLanding(stateDir, address, c, state, repoDevHost(stateDir, c.AppID, c.Repo))
			}
			// HQ keeps the merged history: read it there, rather than keeping
			// a second last-merge record.
			var numbers []int
			for _, change := range state.Changes {
				switch {
				case change.Repo != repo.Repo:
				case change.OnMain():
					merged = true
				case hq.ChangeIsOpen(change.State):
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
	production := "production is added with Put it live on the project's card and released with Release in Mate"
	if limited {
		production += ", by " + whoMerges(true)
	}
	production += "." + mergeLimitedNote(limited)
	if len(open) > 0 {
		lands, has := "it lands", "has"
		if opened > 1 {
			lands, has = "they land", "have"
		}
		first := "Tell the person to merge " + strings.Join(open, ", and ") + " first"
		if limited {
			first = "Tell the person that " + strings.Join(open, ", and ") + " " + has + " to be merged first"
		}
		return first + " — an open change is never in a release, which takes main as it is; once " + lands + ", " + production
	}
	if merged {
		return "Tell the person: " + production
	}
	return "Tell the person: none of this Mate's work has reached its repository's main yet — it goes there by a branch of your own, pushed and merged first, then " + production
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
