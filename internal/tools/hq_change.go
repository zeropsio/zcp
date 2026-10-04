package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A Mate's change lands its work on its repository's `main` (SPEC §3.2a).
// `main` moves only by HQ's merge, so the change is not a courtesy — it is
// the ONLY way the pair's code reaches it. HQ takes the Mate's push only on
// the branch of its own open change, so the change is opened first — and
// only when HQ says the candidate tree differs from `main` — and HEAD is
// pushed to its branch then. A Mate has at
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
	// upToDate: HQ says main already has the delivered tree; no change was opened or updated.
	upToDate bool
	// unreachable: HQ could not serve a step after its tries (hq_retry.go).
	unreachable bool
	// unchanged: the change's branch already carried HEAD, so the push sent
	// nothing.
	unchanged bool
}

// shipChange asks HQ whether the committed tree differs from main. If it does,
// the open change's branch is taken in, or a new change opened, and HEAD is
// pushed to it. Nothing is opened for a tree HQ says main already has. A step
// HQ cannot serve is tried as deliveryRetry paces it, then said; a refusal is
// said at once. Either way the work stays committed in the checkout, for the
// next delivery to send.
func shipChange(
	ctx context.Context,
	sshDeployer ops.SSHDeployer,
	stateDir string,
	hqc hq.Client,
	m *workflow.ServiceMeta,
	title string,
) shipOutcome {
	treeOutput, treeErr := sshDeployer.ExecSSH(ctx, m.Hostname, ops.BuildDeliveryTreeCommand(hqPairWorkingDir))
	if treeErr != nil {
		return shipOutcome{line: fmt.Sprintf("reading the delivered tree failed (%s)", gitPushErrorDetail(treeErr, treeOutput))}
	}
	tree := strings.TrimSpace(string(treeOutput))
	var (
		opened hq.OpenedChange
		err    error
	)
	tries := deliveryRetry.run(ctx, func() bool {
		callCtx, cancel := context.WithTimeout(ctx, hqCallTimeout)
		opened, err = hqc.OpenChange(callCtx, m.HQ.Repo, title, tree)
		cancel()
		return hqUnavailable(err)
	})
	if err != nil {
		if hqUnavailable(err) {
			return shipOutcome{unreachable: true, line: hqNotAnsweringLine(hqc.Address(), "opening its change", tries, hqNotServingWords(err))}
		}
		return shipOutcome{line: fmt.Sprintf("HQ refused to open a change on %q (%v)", m.HQ.Repo, err)}
	}
	if opened.Reason == "nothing_to_deliver" {
		clearLanding(stateDir, m)
		return shipOutcome{upToDate: true, line: "nothing to deliver: main already has this"}
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
	if !opened.Created {
		if stopped, ok := takeChangeIn(ctx, sshDeployer, hqc, m, change.Number, branch); !ok {
			return stopped
		}
	}
	output, tries, err := pushChangeBranch(ctx, sshDeployer, m.Hostname, branch)
	if err != nil {
		switch refusal := ops.ChangePushRefusal(string(output)); {
		case refusal == "change_closed" || refusal == "unknown_change":
			// It settled between the open and the push: the next delivery
			// opens the next one.
			clearChange(stateDir, m, change.Number)
			return shipOutcome{line: fmt.Sprintf("change #%d was merged or closed before its branch was pushed (%s); the next delivery opens the next change", change.Number, refusal)}
		case ops.GitRemoteUnavailable(string(output)):
			return shipOutcome{unreachable: true, line: hqNotAnsweringLine(hqc.Address(), "pushing "+branch, tries, gitNotServingWords(err, output))}
		}
		if cls := classifyTransportError(err, deployStrategyGitPush); cls != nil && cls.Category == topology.FailureClassCredential {
			hqMarkPushRefused(stateDir, m)
			return shipOutcome{line: fmt.Sprintf("HQ refused %s's push credential (%s). %s is marked as refused; the next stage deploy checks its credential against this Mate's current HQ credential again and delivers once it works — if HQ keeps refusing it, tell the person: this Mate's credential is HQ's to issue",
				m.Hostname, gitPushErrorDetail(err, output), m.Hostname)}
		}
		return shipOutcome{line: fmt.Sprintf("pushing %s failed (%s)", branch, gitPushErrorDetail(err, output))}
	}

	// The push landed — whatever the landing needed (an absorb, or nothing),
	// this delivery is done with it.
	clearLanding(stateDir, m)
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

// takeChangeIn takes the open change's branch, as HQ holds it now, into the
// pair's checkout before HEAD is pushed there (ops.BuildTakeChangeInCommand):
// HQ takes a change's branch only forward, and a commit on it the checkout
// lacks — Core's own, or a checkout lost and made again — would refuse every
// later push. ok is false with what stopped the delivery: a collision only
// the agent can settle, an HQ that could not serve the read after its tries,
// or a read that failed.
func takeChangeIn(ctx context.Context, sshDeployer ops.SSHDeployer, hqc hq.Client, m *workflow.ServiceMeta, number int, branch string) (shipOutcome, bool) {
	output, tries, err := gitAgainstHQ(ctx, sshDeployer, m.Hostname, ops.BuildTakeChangeInCommand(hqPairWorkingDir, branch))
	if err == nil {
		return shipOutcome{}, true
	}
	switch conflict := ops.DeliveryConflict(string(output)); {
	case conflict != "":
		return shipOutcome{line: fmt.Sprintf(
			"change #%d has moved on in HQ and this Mate's work changes the same lines (%s) — in %s's checkout run `git fetch origin && git merge origin/%s` and resolve it",
			number, conflict, m.Hostname, branch)}, false
	case ops.GitRemoteUnavailable(string(output)):
		return shipOutcome{unreachable: true, line: hqNotAnsweringLine(hqc.Address(), fmt.Sprintf("reading change #%d", number), tries, gitNotServingWords(err, output))}, false
	}
	return shipOutcome{line: fmt.Sprintf("taking change #%d's branch in failed (%s)", number, gitPushErrorDetail(err, output))}, false
}

// pushChangeBranch pushes the checkout's HEAD to branch, tried again while HQ
// cannot serve it.
func pushChangeBranch(ctx context.Context, sshDeployer ops.SSHDeployer, hostname, branch string) (output []byte, tries int, err error) {
	return gitAgainstHQ(ctx, sshDeployer, hostname, ops.BuildChangePushCommand(hqPairWorkingDir, branch))
}

// gitAgainstHQ runs command — git that reaches HQ — in hostname's checkout,
// tried again as deliveryRetry paces it while its output says HQ could not
// serve it (ops.GitRemoteUnavailable: not reached, or a 5xx). Every such
// command is safe to run again: a commit already made is not made twice, and
// a fetch, a merge already taken in or a push already sent changes nothing.
func gitAgainstHQ(ctx context.Context, sshDeployer ops.SSHDeployer, hostname, command string) (output []byte, tries int, err error) {
	tries = deliveryRetry.run(ctx, func() bool {
		output, err = sshDeployer.ExecSSH(ctx, hostname, command)
		return err != nil && ops.GitRemoteUnavailable(string(output))
	})
	return output, tries, err
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
		return fmt.Sprintf("change #%d is merged — this Mate's work is on %q now"+hqMergedNextClause, number, hqBase)
	}
	clearChange(stateDir, m, number)
	return fmt.Sprintf("change #%d was closed without merging — nothing of it is on %q"+hqClosedNextClause, number, hqBase)
}

// A read promises the next change; a delivery that just opened it no longer
// needs that promise in its news about the previous one.
const (
	hqMergedNextClause = "; its next change opens a new one"
	hqClosedNextClause = "; the next change opens a new one"
)

func changeOutcomeAfterOpen(note string) string {
	return strings.TrimSuffix(strings.TrimSuffix(note, hqMergedNextClause), hqClosedNextClause)
}

// deliveryLanding recovers the base of a legacy checkout whose local landing
// was cleared by an earlier delivery. HQ owns the landing. An open change is
// never cut; only an explicit delivery uses this.
func deliveryLanding(stateDir string, m *workflow.ServiceMeta, state hq.MateState) {
	if m.HQ.Change != 0 || m.HQ.Landed != nil {
		return
	}
	var latest *hq.MateChange
	for i := range state.Changes {
		c := &state.Changes[i]
		if c.Repo != m.HQ.Repo {
			continue
		}
		if c.State == hq.ChangeOpen {
			return
		}
		if c.State == hq.ChangeMerged && c.MergedSha != nil && c.LandedHead != nil &&
			(latest == nil || c.Number > latest.Number) {
			latest = c
		}
	}
	if latest != nil {
		recordLanding(stateDir, m, 0, *latest.MergedSha, *latest.LandedHead)
	}
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

// landingOf is the landing a pair records — S and H of the absorb — or two
// empty strings.
func landingOf(m *workflow.ServiceMeta) (commit, head string) {
	if m.HQ != nil && m.HQ.Landed != nil {
		return m.HQ.Landed.Commit, m.HQ.Landed.Head
	}
	return "", ""
}
