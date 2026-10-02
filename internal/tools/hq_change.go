package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A Mate's change lands its work on its repository's `main` (SPEC §3.2a).
// `main` moves only by HQ's merge, so the change is not a courtesy — it is
// the ONLY way the pair's code reaches it. HQ takes the Mate's push only on
// the branch of its own open change, so the change is opened first — and
// only when there is something to deliver, the checkout ahead of `main` once
// `main` is taken in — and HEAD is pushed to its branch then. A Mate has at
// most one open change per repository: opening answers the open one, or the
// next number. Its number is recorded on the pair; what became of it is read
// from the Mate's own state in HQ.

// hqBase is the branch a change lands on.
const hqBase = "main"

// changeRef is what a caller reports about the change. Number is never 0 in
// a returned value — nil says "there is none". Its JSON is the shape the
// chat card and the client decode under `pullRequest`.
type changeRef struct {
	Repo    string `json:"repo"`
	Branch  string `json:"branch"`
	Base    string `json:"base"`
	Number  int    `json:"number"`
	Created bool   `json:"created"`
	URL     string `json:"url,omitempty"`
	// Described is true when this call put the description the Mate kept for
	// its change onto it (hq_change_description.go).
	Described bool `json:"described,omitempty"`
	// DescriptionNote says why the kept description did not go on — its
	// pictures could not be attached — and is "" otherwise.
	DescriptionNote string `json:"descriptionNote,omitempty"`
}

// changeTitle heads the change a pair's work lands through: the task in the
// person's words, the same line the delivery commits under, since that is
// what a person scans a list of changes for. "Mate: appdev" only when no work
// session says what the work was (the owner's timeline, 2026-09-17: two
// Mates' rows read the same words).
func changeTitle(stateDir string, m *workflow.ServiceMeta) string {
	if intent := workSessionIntent(stateDir); intent != "" {
		return intent
	}
	return changeFallbackTitle(m)
}

// changeFallbackTitle is zcp's own words for a change nothing named: what it
// opens with when no work session is open, and the one title it will replace
// once a session says what the work is.
func changeFallbackTitle(m *workflow.ServiceMeta) string {
	return "Mate: " + m.Hostname
}

// changeTitleRunes bounds a title: HQ keeps at most 120 characters.
const changeTitleRunes = 120

// workSessionIntent is the first line of this process's open work session's
// intent, cut to a title's length, or "" when no session is open. A NUL, which
// no text in HQ keeps, is dropped.
func workSessionIntent(stateDir string) string {
	ws, err := workflow.CurrentWorkSession(stateDir)
	if err != nil || ws == nil {
		return ""
	}
	intent, _, _ := strings.Cut(strings.TrimSpace(strings.ReplaceAll(ws.Intent, "\x00", "")), "\n")
	intent = strings.TrimSpace(intent)
	if runes := []rune(intent); len(runes) > changeTitleRunes {
		return strings.TrimSpace(string(runes[:changeTitleRunes-1])) + "…"
	}
	return intent
}

// shipOutcome is what shipping a pair's work as its change ended in.
type shipOutcome struct {
	// ref is the change that carries the work, nil when none does.
	ref *changeRef
	// line is what happened when the work did not reach a change: "" when
	// it did, or when there was nothing to deliver (upToDate).
	line string
	// upToDate: the checkout has nothing `main` lacks, so no change is open.
	upToDate bool
	// pending: HQ could not be reached; the delivery is recorded as owed.
	pending bool
	// unchanged: the change's branch already carried HEAD, so the push sent
	// nothing.
	unchanged bool
}

// shipChange carries the work in the pair's checkout — committed, with `main`
// taken in, ahead of `main` by ahead commits — to its change: the open one, or
// the next number opened with title, and HEAD pushed to its branch. Nothing
// is opened for a checkout `main` already has (ahead == 0). An HQ that cannot
// be reached leaves the delivery recorded as pending, for the next delivery,
// push or pass to finish (SPEC §3.2a); a refusal is said, and drops it.
func shipChange(
	ctx context.Context,
	sshDeployer ops.SSHDeployer,
	stateDir string,
	hqc hq.Client,
	m *workflow.ServiceMeta,
	title string,
	ahead int,
) shipOutcome {
	if ahead == 0 {
		clearPendingDelivery(stateDir, m)
		return shipOutcome{upToDate: true}
	}
	callCtx, cancel := context.WithTimeout(ctx, hqCallTimeout)
	opened, err := hqc.OpenChange(callCtx, m.HQ.Repo, title)
	cancel()
	if err != nil {
		if hq.IsUnavailable(err) {
			recordPendingDelivery(stateDir, m, title)
			return shipOutcome{pending: true, line: fmt.Sprintf("HQ could not be reached to open its change (%v)", err)}
		}
		clearPendingDelivery(stateDir, m)
		return shipOutcome{line: fmt.Sprintf("HQ refused to open a change on %q (%v)", m.HQ.Repo, err)}
	}
	change := opened.Change
	if !opened.Created && change.Title == changeFallbackTitle(m) && title != change.Title {
		// A change opened before any session named the work still reads
		// "Mate: appdev" beside every other Mate's. Best-effort: the change
		// is there either way, and the next delivery asks again.
		retitle := title
		editCtx, cancel := context.WithTimeout(ctx, hqCallTimeout)
		_, _ = hqc.EditChange(editCtx, m.HQ.Repo, change.Number, hq.ChangeEdit{Title: &retitle})
		cancel()
	}
	recordChange(stateDir, m, change.Number)

	branch := hqc.ChangeBranch(change.Number)
	output, err := pushChangeBranch(ctx, sshDeployer, m.Hostname, branch)
	if err != nil {
		switch refusal := ops.ChangePushRefusal(string(output)); {
		case refusal == "change_closed" || refusal == "unknown_change":
			// It settled between the open and the push: the next delivery
			// opens the next one.
			clearChange(stateDir, m, change.Number)
			recordPendingDelivery(stateDir, m, title)
			return shipOutcome{pending: true, line: fmt.Sprintf("change #%d was merged or closed before its branch was pushed (%s)", change.Number, refusal)}
		case ops.GitRemoteUnavailable(string(output)):
			recordPendingDelivery(stateDir, m, title)
			return shipOutcome{pending: true, line: fmt.Sprintf("HQ could not be reached to push %s (%s)", branch, gitPushErrorDetail(err, output))}
		}
		if cls := classifyTransportError(err, deployStrategyGitPush); cls != nil && cls.Category == topology.FailureClassCredential {
			hqMarkPushRefused(stateDir, m)
			return shipOutcome{line: fmt.Sprintf("HQ refused %s's push credential (%s). %s is marked as refused; the next stage deploy checks its credential against this Mate's current HQ credential again and delivers once it works — if HQ keeps refusing it, tell the person: this Mate's credential is HQ's to issue",
				m.Hostname, gitPushErrorDetail(err, output), m.Hostname)}
		}
		clearPendingDelivery(stateDir, m)
		return shipOutcome{line: fmt.Sprintf("pushing %s failed (%s)", branch, gitPushErrorDetail(err, output))}
	}

	// The push landed — whatever the landing needed (an absorb, or nothing),
	// this delivery is done with it, and with any delivery still owed.
	clearLanding(stateDir, m)
	clearPendingDelivery(stateDir, m)
	described, note := putKeptChangeDescription(ctx, hqc, stateDir, m, change.Number)
	return shipOutcome{unchanged: strings.Contains(string(output), "Everything up-to-date"), ref: &changeRef{
		Repo:            m.HQ.Repo,
		Branch:          branch,
		Base:            hqBase,
		Number:          change.Number,
		Created:         opened.Created,
		URL:             hqc.ChangeURL(m.HQ.AppID, m.HQ.Repo, change.Number),
		Described:       described,
		DescriptionNote: note,
	}}
}

// pushChangeWaitBudget bounds how long a push waits out an HQ that answers
// 503 — a standby while a deploy runs two HQs side by side for about 20 s,
// which git reports without the Retry-After — before the delivery is pending.
// Package-level so a test does not wait it out.
var (
	pushChangeWaitBudget = 20 * time.Second
	pushChangeWait       = 5 * time.Second
)

// pushChangeBranch pushes the checkout's HEAD to branch, trying again while
// HQ answers 503, within pushChangeWaitBudget.
func pushChangeBranch(ctx context.Context, sshDeployer ops.SSHDeployer, hostname, branch string) ([]byte, error) {
	waited := time.Duration(0)
	for {
		output, err := sshDeployer.ExecSSH(ctx, hostname, ops.BuildChangePushCommand(hqPairWorkingDir, branch))
		if err == nil || !strings.Contains(string(output), "returned error: 503") || waited+pushChangeWait > pushChangeWaitBudget {
			return output, err
		}
		waited += pushChangeWait
		select {
		case <-ctx.Done():
			return output, err
		case <-time.After(pushChangeWait):
		}
	}
}

// recordChange stamps the change's number on the pair, in memory and on disk.
// Best-effort on disk: a pass that could not write it opens the same change
// again next time and finds it, which costs a call and nothing else.
func recordChange(stateDir string, m *workflow.ServiceMeta, number int) {
	m.HQ.Change = number
	_ = workflow.UpsertServiceMeta(stateDir, m.Hostname, func(meta *workflow.ServiceMeta, existed bool) error {
		if !existed || meta.HQ == nil {
			return nil
		}
		meta.HQ.Change = number
		return nil
	})
}

// hqLearnLanding reads what became of the pair's change on record from the
// Mate's own state in HQ, freshly read, and says so once. Nothing pushes this
// fact: a person merges in the app's review, and a design that waited to be
// told would be right for one caller and silently wrong for the rest. Both
// deliveries and passes ask, so a merge landed since the last read is known
// before the caller acts on the pair's landing.
//
// The recorded number is cleared as soon as the change is no longer open, so
// the pair's next delivery opens the next change — and a merged one records
// what landed it, which that delivery absorbs. A change HQ no longer lists
// for the Mate reads as closed. Returns "" while the change is open, which
// is the ordinary state and worth no words.
func hqLearnLanding(stateDir string, m *workflow.ServiceMeta, state hq.MateState) string {
	if m == nil || m.HQ == nil || m.HQ.Change == 0 {
		return ""
	}
	number := m.HQ.Change
	var found *hq.MateChange
	for i := range state.Changes {
		if c := state.Changes[i]; c.Repo == m.HQ.Repo && c.Number == number {
			found = &state.Changes[i]
			break
		}
	}
	if found != nil && found.State == hq.ChangeOpen {
		return ""
	}
	if found != nil && found.State == hq.ChangeMerged && found.MergedSha != nil && found.LandedHead != nil {
		// HQ squashes: the squash shares no history with the branch that
		// became it, so recording what landed it (rather than just clearing
		// the number) is what lets the next delivery fold it in as a real
		// merge (BuildAbsorbLandedChangeCommand).
		recordLanding(stateDir, m, number, *found.MergedSha, *found.LandedHead)
		// Never claims WHEN it is absorbed — the caller may be the very
		// delivery about to do it. Just the fact.
		return fmt.Sprintf("change #%d is merged — this Mate's work is on %q now; its next change opens a new one", number, hqBase)
	}
	clearChange(stateDir, m, number)
	return fmt.Sprintf("change #%d was closed without merging — nothing of it is on %q; the next change opens a new one", number, hqBase)
}

// clearChange forgets the number a pair recorded, in memory and on disk, once
// HQ says the change is no longer open without merging. number is the change
// the read was FOR: the disk write applies only while the fresh meta still
// records it — a concurrent delivery may have moved the pair on to a newer
// change between the read and the write.
func clearChange(stateDir string, m *workflow.ServiceMeta, number int) {
	m.HQ.Change = 0
	_ = workflow.UpsertServiceMeta(stateDir, m.Hostname, func(meta *workflow.ServiceMeta, existed bool) error {
		if !existed || meta.HQ == nil || meta.HQ.Change != number {
			return nil
		}
		meta.HQ.Change = 0
		return nil
	})
}

// recordLanding forgets the change's number and records what merged it, in
// memory and on disk. The landing rides on the pair until clearLanding proves
// a delivery absorbed it (or proved it needed no absorbing) by pushing
// successfully. number guards the disk write exactly as clearChange's does.
func recordLanding(stateDir string, m *workflow.ServiceMeta, number int, commit, head string) {
	m.HQ.Change = 0
	m.HQ.Landed = &workflow.LandedChange{Commit: commit, Head: head}
	_ = workflow.UpsertServiceMeta(stateDir, m.Hostname, func(meta *workflow.ServiceMeta, existed bool) error {
		if !existed || meta.HQ == nil || meta.HQ.Change != number {
			return nil
		}
		meta.HQ.Change = 0
		meta.HQ.Landed = &workflow.LandedChange{Commit: commit, Head: head}
		return nil
	})
}

// clearLanding forgets a recorded landing once a delivery has absorbed it (or
// proven it needed no absorbing) by pushing successfully — never from a read:
// reading the outcome only learns of a landing, and only a delivery's own
// push proves it is done with it.
func clearLanding(stateDir string, m *workflow.ServiceMeta) {
	if m.HQ.Landed == nil {
		return
	}
	m.HQ.Landed = nil
	_ = workflow.UpsertServiceMeta(stateDir, m.Hostname, func(meta *workflow.ServiceMeta, existed bool) error {
		if !existed || meta.HQ == nil {
			return nil
		}
		meta.HQ.Landed = nil
		return nil
	})
}

// recordPendingDelivery records that the pair owes a delivery HQ could not be
// reached for, under title — kept from the first time it could not.
func recordPendingDelivery(stateDir string, m *workflow.ServiceMeta, title string) {
	if m.HQ.Pending != nil {
		return
	}
	pending := &workflow.PendingDelivery{Title: title, Since: time.Now().UTC().Format(time.RFC3339)}
	m.HQ.Pending = pending
	_ = workflow.UpsertServiceMeta(stateDir, m.Hostname, func(meta *workflow.ServiceMeta, existed bool) error {
		if !existed || meta.HQ == nil || meta.HQ.Pending != nil {
			return workflow.ErrSkipWrite
		}
		meta.HQ.Pending = pending
		return nil
	})
}

// clearPendingDelivery forgets a delivery owed, once one has reached HQ or HQ
// refused it.
func clearPendingDelivery(stateDir string, m *workflow.ServiceMeta) {
	if m.HQ.Pending == nil {
		return
	}
	m.HQ.Pending = nil
	_ = workflow.UpsertServiceMeta(stateDir, m.Hostname, func(meta *workflow.ServiceMeta, existed bool) error {
		if !existed || meta.HQ == nil || meta.HQ.Pending == nil {
			return workflow.ErrSkipWrite
		}
		meta.HQ.Pending = nil
		return nil
	})
}

// finishPendingDelivery finishes a delivery HQ could not be reached for,
// without anyone asking (SPEC §3.2a): on a clean checkout of the Mate's
// branch it takes `main` in — and a landing of its own change — and ships
// what it then holds as the change, the way the delivery would have. A
// checkout with work not committed, or on another branch, is left as it is:
// the next delivery commits it and finishes the owed one with it. Returns
// the line worth saying, "" while HQ still does not answer.
func finishPendingDelivery(
	ctx context.Context,
	client platform.Client,
	sshDeployer ops.SSHDeployer,
	rt runtime.Info,
	stateDir string,
	hqc hq.Client,
	m *workflow.ServiceMeta,
) string {
	if !checkoutOnMateBranch(ctx, sshDeployer, m) {
		return ""
	}
	if err := hqEnsurePushCredential(ctx, client, sshDeployer, rt.ProjectID, stateDir, hqc, m); err != nil {
		return ""
	}
	landedCommit, landedHead := landingOf(m)
	output, err := sshDeployer.ExecSSH(ctx, m.Hostname, ops.BuildDeliverySyncCommand(hqPairWorkingDir, landedCommit, landedHead))
	ahead, found := ops.DeliveryAhead(string(output))
	if err != nil || !found {
		return ""
	}
	shipped := shipChange(ctx, sshDeployer, stateDir, hqc, m, m.HQ.Pending.Title, ahead)
	switch {
	case shipped.ref != nil:
		return fmt.Sprintf("the delivery HQ could not be reached for is done: %s is on %s, and change #%d (%s) carries it to %q",
			m.Hostname, shipped.ref.Branch, shipped.ref.Number, shipped.ref.URL, hqBase)
	case shipped.pending:
		return ""
	case shipped.upToDate:
		return ""
	}
	return "the delivery HQ could not be reached for did not complete: " + shipped.line
}

// checkoutOnMateBranch reports whether the pair's checkout is clean and on the
// Mate's own branch — the one state a pass may take `main` into it, silently.
func checkoutOnMateBranch(ctx context.Context, sshDeployer ops.SSHDeployer, m *workflow.ServiceMeta) bool {
	if sshDeployer == nil || m == nil || m.HQ == nil || m.HQ.Branch == "" {
		return false
	}
	status, err := sshDeployer.ExecSSH(ctx, m.Hostname, gitStatusPorcelainCmd(hqPairWorkingDir))
	if err != nil || strings.TrimSpace(string(status)) != "" {
		return false
	}
	branch, err := sshDeployer.ExecSSH(ctx, m.Hostname, gitCurrentBranchCmd(hqPairWorkingDir))
	return err == nil && strings.TrimSpace(string(branch)) == m.HQ.Branch
}

// gitCurrentBranchCmd reads the branch name workingDir's HEAD is on.
func gitCurrentBranchCmd(workingDir string) string {
	return fmt.Sprintf(`git -C %s rev-parse --abbrev-ref HEAD 2>/dev/null`, ops.ShellQuote(workingDir))
}

// absorbLandedChangeOnCheckout is the point where a pass that just learned a
// change merged folds that landing into the pair's own checkout right there
// — so the Mate's NEXT task starts on current code instead of waiting for its
// next delivery to notice (MB-26). Best-effort and silent: a dirty tree, a
// checkout not on the Mate's own branch, an SSH failure, or a real conflict
// all leave the checkout exactly as it was — the delivery is the
// authoritative path and reports any real conflict there.
func absorbLandedChangeOnCheckout(ctx context.Context, sshDeployer ops.SSHDeployer, m *workflow.ServiceMeta) {
	if !checkoutOnMateBranch(ctx, sshDeployer, m) {
		return
	}
	landedCommit, landedHead := landingOf(m)
	_, _ = sshDeployer.ExecSSH(ctx, m.Hostname, ops.BuildDeliverySyncCommand(hqPairWorkingDir, landedCommit, landedHead))
}

// landingOf is the landing a pair records — S and H of the absorb — or two
// empty strings.
func landingOf(m *workflow.ServiceMeta) (commit, head string) {
	if m.HQ != nil && m.HQ.Landed != nil {
		return m.HQ.Landed.Commit, m.HQ.Landed.Head
	}
	return "", ""
}
