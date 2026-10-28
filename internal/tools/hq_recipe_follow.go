package tools

import (
	"context"
	"encoding/json"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A second Mate, a stage and a production are all made from the group
// recipe's tiers, so the recipe follows the Mate's project (D36) without
// waiting for the next bootstrap, adopt, develop start or zcp start: on
// 2026-10-27 the Noola Mate's agent imported a Box and a runner pair, and its
// recipe caught up only when the Mate restarted the next morning. So a tool
// call that changes what the recipe reads asks for the recipe pass again
// (RecipeFollower), after it answered and off its path: the pass runs under
// the server's own context, which outlives the call's, once the calls stop
// coming for recipeFollowQuiet — a burst is one pass — and a call that lands
// while a pass runs is one more pass after it.
//
// What the recipe reads (composeGroupRecipeInputs) decides which calls ask:
// an import and a delete (the services a tier creates), a variable set or
// deleted and the organization's AI key put in the vault (what a tier
// carries, and which services a runtime reads), a pool migration's retire (a
// pair keyed by its pool), a deploy to a stage (the type the stage runs as),
// and any call that moves what zcp's records hold of the pairs
// (recipePairRecords: a setup recorded, a pair re-keyed or wired) — whatever
// that call answered, since the record moved. A scale and public access ask
// for none: they change no tier's shape, a tier main carries is never
// proposed over for them (recipeReplacements), and a scale has its own
// proposal (group-recipe scaling=<host>); the next pass carries them into a
// tier main lacks.
//
// The pass is the one zcp's start runs (groupRecipeUnasked): nobody asked
// for it, so a proposal a person closed unmerged is not opened again, and it
// runs one at a time with every other pass of the Mate (holdRecipePass). What
// it says goes to the log, once: a pass that says what the last one said — a
// pair the recipe still waits for, a tier still refused — says nothing, and
// nothing reaches a tool's answer.

// recipeFollowQuiet is how long the calls stay quiet before the pass they
// asked for runs: an agent's next call usually comes within it, so an
// import, its variables and the stage's deploy are one pass.
const recipeFollowQuiet = 30 * time.Second

// recipeFollowLongest bounds that wait from a burst's first call, so the
// recipe of a session that never pauses still follows.
const recipeFollowLongest = 3 * time.Minute

// recipeFollowPassBound bounds one pass, so a pass stuck past every bound of
// its own calls leaves the next call's pass to run.
const recipeFollowPassBound = 10 * time.Minute

// RecipeFollower runs a Mate's group recipe pass again after a tool call
// changed what the recipe reads. Nil outside a Mate, where Watch watches
// nothing.
type RecipeFollower struct {
	ctx      context.Context
	stateDir string
	pass     func(context.Context) string
	spawn    func(func())
	say      func(string)
	quiet    time.Duration
	longest  time.Duration

	mu sync.Mutex
	// wanted: a call asked for a pass that has not started; first and last
	// are when the burst's first and latest calls asked.
	wanted      bool
	first, last time.Time
	// running: a worker is waiting for the burst to settle or running a pass.
	running bool
	// said is the last line said.
	said string
}

// NewRecipeFollower is a Mate container's follower: its passes run under ctx
// — the server's own, which Stop ends — through spawn, the server's
// background work, and each pass's line goes to say once. Nil anywhere else
// (docs/spec-mate.md §2.0).
func NewRecipeFollower(ctx context.Context, client platform.Client, httpClient ops.HTTPDoer, rt runtime.Info, stateDir string, spawn func(func()), say func(string)) *RecipeFollower {
	if !rt.InContainer || !rt.MateEnabled || client == nil || httpClient == nil || stateDir == "" || spawn == nil {
		return nil
	}
	return newRecipeFollower(ctx, stateDir, func(ctx context.Context) string {
		return groupRecipeUnasked(ctx, client, httpClient, rt, stateDir)
	}, spawn, say, recipeFollowQuiet, recipeFollowLongest)
}

func newRecipeFollower(ctx context.Context, stateDir string, pass func(context.Context) string, spawn func(func()), say func(string), quiet, longest time.Duration) *RecipeFollower {
	return &RecipeFollower{ctx: ctx, stateDir: stateDir, pass: pass, spawn: spawn, say: say, quiet: quiet, longest: longest}
}

// Watch is told of a tool call as it starts — the tool's name and its raw
// arguments — and answers what to tell it once the call answered: whether it
// failed. It never waits on a pass.
func (f *RecipeFollower) Watch(tool string, args json.RawMessage) func(failed bool) {
	if f == nil {
		return func(bool) {}
	}
	call := recipeCallOf(tool, args, f.stateDir)
	if !call.changes && !call.records {
		return func(bool) {}
	}
	var before string
	if call.records {
		before = recipePairRecords(f.stateDir)
	}
	return func(failed bool) {
		switch {
		case call.records && recipePairRecords(f.stateDir) != before:
			f.follow()
		case call.changes && !failed:
			f.follow()
		}
	}
}

// follow asks for a pass: the worker that waits for the burst to settle is
// started unless one already runs. A server that stopped asks for none.
func (f *RecipeFollower) follow() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ctx.Err() != nil {
		return
	}
	now := time.Now()
	if !f.wanted {
		f.first = now
	}
	f.wanted, f.last = true, now
	if f.running {
		return
	}
	f.running = true
	f.spawn(f.work)
}

// work runs the passes asked for, one at a time, until none is wanted.
func (f *RecipeFollower) work() {
	for f.settled() {
		ctx, cancel := context.WithTimeout(f.ctx, recipeFollowPassBound)
		line := f.pass(ctx)
		cancel()
		f.tell(line)
	}
}

// settled waits until the calls have been quiet for f.quiet, or f.longest
// since the burst's first, and takes the pass they asked for. false ends the
// worker: nothing is wanted, or the server stopped.
func (f *RecipeFollower) settled() bool {
	for {
		f.mu.Lock()
		if !f.wanted || f.ctx.Err() != nil {
			f.running = false
			f.mu.Unlock()
			return false
		}
		due := f.last.Add(f.quiet)
		if bound := f.first.Add(f.longest); bound.Before(due) {
			due = bound
		}
		wait := time.Until(due)
		if wait <= 0 {
			f.wanted = false
			f.mu.Unlock()
			return true
		}
		f.mu.Unlock()
		timer := time.NewTimer(wait)
		select {
		case <-f.ctx.Done():
		case <-timer.C:
		}
		timer.Stop()
	}
}

// tell says a pass's line unless it is empty or the last one said.
func (f *RecipeFollower) tell(line string) {
	f.mu.Lock()
	repeat := line == "" || line == f.said
	if !repeat {
		f.said = line
	}
	f.mu.Unlock()
	if !repeat && f.say != nil {
		f.say(line)
	}
}

// recipeCall is what one tool call can change of what the group recipe
// reads.
type recipeCall struct {
	// changes: answered without an error, the call changed it.
	changes bool
	// records: the call can move what zcp's records hold of the pairs
	// (recipePairRecords), read before and after it.
	records bool
}

// recipeCallPeek is all recipeCallOf reads of a call's arguments.
type recipeCallPeek struct {
	Action        string             `json:"action"`
	TargetService string             `json:"targetService"`
	Targets       []recipeCallTarget `json:"targets"`
}

// recipeCallTarget is a batch deploy's target, as recipeCallOf reads it.
type recipeCallTarget struct {
	TargetService string `json:"targetService"`
}

// recipeCallOf is what the call of tool with args can change of what the
// recipe reads, by the tool and, for a tool of several actions, the action.
// Arguments that do not parse read as none.
func recipeCallOf(tool string, args json.RawMessage, stateDir string) recipeCall {
	var peek recipeCallPeek
	if len(args) > 0 {
		_ = json.Unmarshal(args, &peek)
	}
	switch tool {
	case "zerops_import", "zerops_delete":
		return recipeCall{changes: true}
	case "zerops_env":
		return recipeCall{changes: slices.Contains([]string{"set", "delete", "ai-key"}, peek.Action)}
	case "zerops_workflow":
		return recipeCall{changes: peek.Action == actionRetire, records: true}
	case "zerops_deploy":
		return recipeCall{changes: recipeStageHost(stateDir, peek.TargetService), records: true}
	case "zerops_deploy_batch":
		stage := slices.ContainsFunc(peek.Targets, func(t recipeCallTarget) bool {
			return recipeStageHost(stateDir, t.TargetService)
		})
		return recipeCall{changes: stage, records: true}
	}
	return recipeCall{}
}

// recipeStageHost reports whether host is a stage of a pair zcp records.
func recipeStageHost(stateDir, host string) bool {
	m, err := workflow.FindServiceMeta(stateDir, host)
	if err != nil || m == nil {
		return false
	}
	_, stage := m.StageFor(host)
	return stage
}

// recipePairRecord is what the recipe reads of one pair's record: which
// pairs it carries and waits for (groupRecipeWaits), the repository each
// builds from, the dev pool that keys it and the dev service that pool took
// over, and the setups each half builds.
type recipePairRecord struct {
	Hostname   string                `json:"hostname"`
	Complete   bool                  `json:"complete"`
	HQApp      string                `json:"hqApp,omitempty"`
	HQRepo     string                `json:"hqRepo,omitempty"`
	RemoteURL  string                `json:"remoteUrl,omitempty"`
	GitPush    topology.GitPushState `json:"gitPush,omitempty"`
	DevPool    bool                  `json:"devPool,omitempty"`
	RetiredDev string                `json:"retiredDev,omitempty"`
	Setup      string                `json:"setup,omitempty"`
	Stages     []topology.StageRef   `json:"stages,omitempty"`
}

// recipePairRecords is what zcp's records hold of the pairs that the recipe
// reads, as one string; "" when they cannot be read.
func recipePairRecords(stateDir string) string {
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil {
		return ""
	}
	records := make([]recipePairRecord, 0, len(metas))
	for _, m := range metas {
		record := recipePairRecord{Hostname: m.Hostname, Complete: m.IsComplete(), RemoteURL: m.RemoteURL, GitPush: m.GitPushState,
			DevPool: m.DevIsPool(), Setup: m.PrimarySetupName, Stages: m.Stages}
		if m.HQ != nil {
			record.HQApp, record.HQRepo = m.HQ.AppID, m.HQ.Repo
		}
		if m.DevIsPool() && m.DevPool.MigratedFrom != nil {
			record.RetiredDev = m.DevPool.MigratedFrom.Hostname
		}
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Hostname < records[j].Hostname })
	data, err := json.Marshal(records)
	if err != nil {
		return ""
	}
	return string(data)
}
