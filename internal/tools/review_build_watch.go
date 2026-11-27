package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

type reviewKey struct {
	repo   string
	number int
}

type reviewAttempt struct {
	head, main, updatedAt       string
	tries                       int
	next                        time.Time
	running, completed, stopped bool
}

type reviewDone struct {
	key         reviewKey
	head        string
	main        string
	updatedAt   string
	unavailable bool
	err         error
}

type reviewObserver struct {
	read     func() (hq.MateStateDocument, error)
	log      func(string)
	deps     func(context.Context) (devGitDeps, bool)
	attempts map[reviewKey]*reviewAttempt
	done     chan reviewDone
	timer    *time.Timer
	alarm    <-chan time.Time
}

// KeepMateReviewBuild looks at startup and at each replacement of the Mate's
// state. Its only clock is the bounded unavailable retry; work runs beside
// the look so another change or state replacement cannot hold it up.
func KeepMateReviewBuild(ctx context.Context, env func() func(string) string, standupDone <-chan struct{}, log func(string)) error {
	rt := runtime.DetectFrom(env())
	if !rt.InContainer || !rt.MateEnabled {
		return nil
	}
	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	changed := watchMateState(watchCtx, hqIdentity(rt).State.Watch, func(err error) {
		log(fmt.Sprintf("Review build state watch: %v", err))
	})
	o := &reviewObserver{read: func() (hq.MateStateDocument, error) {
		return hqIdentity(runtime.DetectFrom(env())).ReadMateState()
	}, log: log, deps: liveDevGitDeps(env, log),
		attempts: make(map[reviewKey]*reviewAttempt), done: make(chan reviewDone)}
	o.run(watchCtx, changed, standupDone)
	return nil
}

func (o *reviewObserver) run(ctx context.Context, changed, standupDone <-chan struct{}) {
	defer func() {
		if o.timer != nil {
			o.timer.Stop()
		}
	}()
	o.look(ctx)
	for {
		o.arm()
		select {
		case <-ctx.Done():
			return
		case _, open := <-changed:
			if !open {
				changed = nil
				continue
			}
			o.look(ctx)
		case _, open := <-standupDone:
			if !open {
				standupDone = nil
				continue
			}
			// A stand-up can change the recorded stage set or recover a stage.
			for _, attempt := range o.attempts {
				if !attempt.running {
					attempt.completed, attempt.stopped = false, false
					attempt.next = time.Time{}
				}
			}
			o.look(ctx)
		case <-o.alarm:
			o.look(ctx)
		case result := <-o.done:
			o.finish(result)
			o.look(ctx)
		}
	}
}

func (o *reviewObserver) arm() {
	var first time.Time
	for _, attempt := range o.attempts {
		if !attempt.next.IsZero() && (first.IsZero() || attempt.next.Before(first)) {
			first = attempt.next
		}
	}
	if o.timer != nil {
		o.timer.Stop()
	}
	o.alarm = nil
	if first.IsZero() {
		return
	}
	delay := max(time.Until(first), 0)
	if o.timer == nil {
		o.timer = time.NewTimer(delay)
	} else {
		o.timer.Reset(delay)
	}
	o.alarm = o.timer.C
}

func (o *reviewObserver) schedule(a *reviewAttempt, now time.Time) {
	if delay := reviewRetryDelay(a.tries); delay != 0 {
		a.next = now.Add(delay)
	} else {
		a.next, a.stopped = time.Time{}, true
	}
}

func (o *reviewObserver) look(ctx context.Context) {
	doc, err := o.read()
	if err != nil || doc.Link != hq.MateLinkLinked || doc.Mate == nil || doc.Mate.AppID == nil {
		return
	}
	seen := make(map[reviewKey]bool)
	for _, change := range doc.Mate.Changes {
		if change.Head == nil {
			continue
		}
		key := reviewKey{repo: change.Repo, number: change.Number}
		seen[key] = true
		_, repo := reviewCurrent(doc.Mate, change.Repo, change.Number)
		if repo == nil {
			continue
		}
		main := ""
		if repo.MainHead != nil {
			main = *repo.MainHead
		}
		o.consider(ctx, key, change, *doc.Mate.AppID, main)
	}
	for _, repo := range doc.Mate.Repos {
		key := reviewKey{repo: repo.Name}
		seen[key] = true
		o.considerMain(ctx, key, repo, *doc.Mate.AppID)
	}
	for key, attempt := range o.attempts {
		if !seen[key] && !attempt.running {
			delete(o.attempts, key)
		}
	}
}

func (o *reviewObserver) consider(ctx context.Context, key reviewKey, change hq.MateChange, app, main string) {
	a := o.attempts[key]
	if a == nil || a.head != *change.Head || a.main != main || (a.stopped && reviewUpdated(change.UpdatedAt, a.updatedAt)) {
		a = &reviewAttempt{head: *change.Head, main: main, updatedAt: change.UpdatedAt}
		o.attempts[key] = a
	} else if reviewUpdated(change.UpdatedAt, a.updatedAt) {
		a.updatedAt = change.UpdatedAt
	}
	now := time.Now()
	action := reviewNext(change, a.running, now)
	if action == reviewNothing {
		if !a.running && change.State != hq.ChangeAdding && change.State != hq.ChangeMerging {
			delete(o.attempts, key)
		}
		return
	}
	if a.stopped || a.completed && a.next.IsZero() {
		return
	}
	if !a.next.IsZero() {
		if now.Before(a.next) {
			return
		}
		a.next = time.Time{}
		a.tries++
	}
	o.start(ctx, key, change, app, a, now)
}

func (o *reviewObserver) considerMain(ctx context.Context, key reviewKey, repo hq.MateRepo, app string) {
	if repo.Adding {
		// An adding build may activate a candidate that HQ later declines. Even
		// an unchanged main must be checked again when adding ends.
		if attempt := o.attempts[key]; attempt != nil {
			attempt.completed = false
			attempt.stopped = false
			attempt.next, attempt.tries = time.Time{}, 0
		}
	}
	if !reviewMainNext(repo, false) {
		return
	}
	main := *repo.MainHead
	a := o.attempts[key]
	if a == nil || a.main != main {
		a = &reviewAttempt{head: main, main: main}
		o.attempts[key] = a
	}
	if a.running || a.stopped || a.completed && a.next.IsZero() {
		return
	}
	now := time.Now()
	if !a.next.IsZero() {
		if now.Before(a.next) {
			return
		}
		a.next = time.Time{}
		a.tries++
	}
	d, ok := o.deps(ctx)
	if !ok {
		o.schedule(a, now)
		return
	}
	metas, err := workflow.ListServiceMetas(d.stateDir)
	if err != nil {
		o.log(fmt.Sprintf("Main stage could not read pair records: %v", err))
		o.schedule(a, now)
		return
	}
	stages := reviewStages(metas, app, repo.Name)
	if len(stages) == 0 {
		a.completed = true
		return
	}
	hqc, err := hq.Open(d.httpClient, hqIdentity(d.rt))
	if err != nil {
		o.schedule(a, now)
		return
	}
	a.running = true
	go func() {
		unavailable, err := executeReviewMain(ctx, d, hqc, app, repo.Name, main, metas, stages, o.read, o.log)
		select {
		case o.done <- reviewDone{key: key, head: main, main: main, unavailable: unavailable, err: err}:
		case <-ctx.Done():
		}
	}()
}

func (o *reviewObserver) start(ctx context.Context, key reviewKey, change hq.MateChange, app string, a *reviewAttempt, now time.Time) {
	d, ok := o.deps(ctx)
	if !ok {
		o.schedule(a, now)
		return
	}
	metas, err := workflow.ListServiceMetas(d.stateDir)
	if err != nil {
		o.log(fmt.Sprintf("Review build could not read pair records: %v", err))
		o.schedule(a, now)
		return
	}
	stages := reviewStages(metas, app, change.Repo)
	hqc, err := hq.Open(d.httpClient, hqIdentity(d.rt))
	if err != nil {
		o.log(fmt.Sprintf("Review build could not open HQ: %v", err))
		o.schedule(a, now)
		return
	}
	a.running = true
	go func() {
		unavailable, updatedAt, runErr := func() (unavailable bool, updatedAt string, err error) {
			defer func() {
				if r := recover(); r != nil {
					unavailable, err = true, fmt.Errorf("worker stopped: %v", r)
				}
			}()
			return executeReviewBuild(ctx, d, hqc, app, change, metas, stages, o.read, o.log)
		}()
		select {
		case o.done <- reviewDone{key: key, head: *change.Head, main: a.main, updatedAt: updatedAt, unavailable: unavailable, err: runErr}:
		case <-ctx.Done():
		}
	}()
}

func (o *reviewObserver) finish(result reviewDone) {
	a := o.attempts[result.key]
	if a == nil || a.head != result.head || a.main != result.main {
		return
	}
	if reviewUpdated(result.updatedAt, a.updatedAt) {
		a.updatedAt = result.updatedAt
	}
	a.running, a.completed = false, true
	if result.err != nil {
		o.log(fmt.Sprintf("Review build: %v", result.err))
	}
	if result.unavailable {
		if result.err != nil {
			o.schedule(a, time.Now())
		}
	}
}

func reviewResolveSetups(ctx context.Context, hqc hq.Client, app, repo string, metas []*workflow.ServiceMeta, stages []topology.StageRef) error {
	for _, stage := range stages {
		if stage.SetupName == "" {
			read, err := hqc.Once().RecipeTier(ctx, hq.RecipeTierMate)
			if err != nil {
				return fmt.Errorf("read tier: %w", err)
			}
			if read.State != hq.RecipePresent {
				return fmt.Errorf("the Mate tier is not present")
			}
			tier, err := workflow.ParseMateTier(read.ImportYAML, hqc.Address(), app, metas...)
			if err != nil {
				return fmt.Errorf("read tier: %w", err)
			}
			return reviewStageSetups(stages, tier, repo)
		}
	}
	return nil
}
