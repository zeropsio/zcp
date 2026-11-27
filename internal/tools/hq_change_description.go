package tools

import (
	"context"
	"errors"
	"fmt"
	"html"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A change's description is what the person reviews a Mate's change by — in
// the app's review, right after the verdict. It is the body of the change in
// HQ, and only the Mate writes it (zerops_workflow action="describe-change"):
// open-change can put the Mate's words on the change as it opens it. The Mate
// keeps working on top of an open change,
// so it rewrites the description as the change grows; a link to try the change
// can go stale, and the description is where what it does and how it was
// checked stays true.
//
// The words are kept with the change in the change ledger first and put on
// the change second, so an HQ that did not answer loses nothing — the
// pictures they show stay kept, and describe-change called again puts them
// on:
//
//   - the change on the branch named — by branch, or by a lease alias — or the
//     repository's only open one takes them at once; several open with none
//     named is a refusal listing each;
//   - with none open there is nothing to describe;
//   - words never reach a change they were not written for: when the change
//     merged or closed before they reached it, the Mate is told and nothing is
//     kept.
//
// describe-change never opens a change: describing is not a change.

// changeDescriptionMaxRunes bounds a description: HQ keeps at most 20000
// characters, and the review shows a page, not a log.
const changeDescriptionMaxRunes = 20000

// changePictureMaxBytes is the largest picture HQ keeps for a description.
const changePictureMaxBytes = 20 << 20

// changeDescriptionResult is describe-change's answer.
type changeDescriptionResult struct {
	Service string `json:"service"`
	// Parts are the parts whose dev pools lease the change's branch when
	// they are several, and so name no one service (changeService).
	Parts          []string `json:"parts,omitempty"`
	PullRequest    int      `json:"pullRequest,omitempty"`
	PullRequestURL string   `json:"pullRequestUrl,omitempty"`
	Described      bool     `json:"described"`
	Kept           bool     `json:"kept,omitempty"`
	// Labels are the described change's standing in HQ's answer to the
	// write (describedLabels), as status labels it.
	Labels  []string `json:"labels,omitempty"`
	Message string   `json:"message"`
}

// descriptionCopy identifies the checkout whose words are being written.
type descriptionCopy struct {
	name, root, branch, head string
	lease                    bool
}

// readDescriptionCopy reads Git before a change operation reaches HQ.
func readDescriptionCopy(ctx context.Context, ssh ops.SSHDeployer, stateDir, address string, target changeTarget, named string) (descriptionCopy, error) {
	var candidates []descriptionCopy
	switch {
	case target.Lease != "":
		candidates = append(candidates, descriptionCopy{name: target.Lease, root: ops.LeaseWorkDir, lease: true})
	case named != "" && target.Pair != nil && !target.Pair.DevIsPool():
		candidates = append(candidates, descriptionCopy{name: target.Pair.Hostname})
	default:
		leases, err := ops.LeaseBranches(stateDir)
		if err != nil {
			return descriptionCopy{}, fmt.Errorf("read the branch's leases: %w", err)
		}
		for _, lease := range leases {
			app, repo, ok := hq.RepoOfURL(address, lease.Repo)
			if !ok || app != target.AppID || repo != target.Repo || (target.Branch != "" && lease.Branch != target.Branch) || (named != "" && target.Pair != nil && lease.Pool != target.Pair.Hostname) {
				continue
			}
			candidates = append(candidates, descriptionCopy{name: lease.Alias, root: ops.LeaseWorkDir, lease: true})
		}
		if len(candidates) == 0 && named == "" {
			metas, err := workflow.ListServiceMetas(stateDir)
			if err != nil {
				return descriptionCopy{}, fmt.Errorf("read the repository's dev services: %w", err)
			}
			for _, meta := range metas {
				if hqPairWired(meta) && meta.HQ.AppID == target.AppID && meta.HQ.Repo == target.Repo && !meta.DevIsPool() {
					candidates = append(candidates, descriptionCopy{name: meta.Hostname})
				}
			}
		}
	}
	workcopy, err := uniqueDescriptionCopy(candidates)
	if err != nil {
		return descriptionCopy{}, err
	}
	if !workcopy.lease && workcopy.name != "" {
		workcopy.root = supervisorRoot(stateDir, workcopy.name)
		if workcopy.root == "" {
			workcopy.root = ops.DevRepoRoot
		}
	}
	if workcopy.name == "" {
		return descriptionCopy{}, missingDescriptionCopy(target, nil)
	}
	if ssh == nil {
		return descriptionCopy{}, errors.New("the addressed checkout was not read: name its lease or dev service and ensure SSH is available")
	}
	head, branch, dirty, err := ops.ReadEditHead(ctx, ssh, workcopy.name, workcopy.root)
	if err != nil {
		return descriptionCopy{}, fmt.Errorf("read the addressed checkout %s: %w", workcopy.name, err)
	}
	if !workcopy.lease && target.Branch != "" && branch != target.Branch {
		return descriptionCopy{}, missingDescriptionCopy(target, &workflow.WhereCopy{Name: workcopy.name, Branch: branch, Dirty: dirty})
	}
	if head == "" || branch == "" || branch == hqBase || (target.Branch != "" && branch != target.Branch) {
		return descriptionCopy{}, fmt.Errorf("the addressed checkout %s has branch %q and head %q; it must hold the named branch and a commit", workcopy.name, branch, head)
	}
	workcopy.head, workcopy.branch = head, branch
	return workcopy, nil
}

func missingDescriptionCopy(target changeTarget, classic *workflow.WhereCopy) error {
	branch := target.Branch
	message := "No working copy was selected."
	switch {
	case branch == "":
		branch = "<branch>"
	case classic != nil:
		message = fmt.Sprintf("The addressed working copy %s does not hold branch %q.", classic.Name, branch)
	default:
		message = fmt.Sprintf("No working copy was found for branch %q.", branch)
	}
	if classic == nil && target.Pair != nil && !target.Pair.DevIsPool() {
		classic = &workflow.WhereCopy{Name: target.Pair.Hostname, WorkUnread: "checkout was not read"}
	}
	step := workflow.MissingEditCopyStep(branch, classic)
	suggestion := step.Words()
	if classic != nil && step.Action == workflow.EditUnknown {
		suggestion += "; run zcp where"
	}
	if classic == nil {
		suggestion = fmt.Sprintf("zerops_lease action=acquire branch=%q", branch)
		if target.Pair != nil {
			suggestion += fmt.Sprintf(" pool=%q", target.Pair.Hostname)
		}
	}
	return platform.NewPlatformError(platform.ErrPrerequisiteMissing,
		message, suggestion+"; then describe the change.")
}

func uniqueDescriptionCopy(candidates []descriptionCopy) (descriptionCopy, error) {
	if len(candidates) > 1 {
		return descriptionCopy{}, errors.New("several copies can hold these words: name the addressed lease or dev service")
	}
	if len(candidates) == 0 {
		return descriptionCopy{}, nil
	}
	return candidates[0], nil
}

// readHandoverStep reads each copy and HQ through the same owners as where,
// then lets the shared planner decide what remains for the branch group.
func readHandoverStep(ctx context.Context, ssh ops.SSHDeployer, hqc hq.Client, stateDir string, target changeTarget, addressed descriptionCopy) (workflow.EditStep, *hq.Change) {
	group := []descriptionCopy{addressed}
	if addressed.lease {
		leases, err := ops.LeaseBranches(stateDir)
		if err != nil {
			return workflow.EditStep{Action: workflow.EditUnknown, Reason: "branch leases were not read"}, nil
		}
		for _, lease := range leases {
			app, repo, ok := hq.RepoOfURL(hqc.Address(), lease.Repo)
			if ok && app == target.AppID && repo == target.Repo && lease.Branch == addressed.branch && lease.Alias != addressed.name {
				group = append(group, descriptionCopy{name: lease.Alias, root: ops.LeaseWorkDir, branch: lease.Branch, lease: true})
			}
		}
	}
	copies := make([]workflow.EditCopy, 0, len(group))
	for _, copy := range group {
		item := workflow.EditCopy{Name: copy.name}
		if ssh == nil {
			item.Unread = "checkout was not read"
		} else if head, branch, dirty, err := ops.ReadEditHead(ctx, ssh, copy.name, copy.root); err != nil || head == "" || branch != addressed.branch {
			item.Unread = "checkout head, branch or work was not read"
		} else if ref, err := ops.ReadBranchRef(ctx, ssh, copy.name, copy.root, branch, head); err != nil {
			item.Unread = "branch remote ref or checkout history was not read"
		} else {
			item.Dirty, item.Ref = dirty, &ref
		}
		copies = append(copies, item)
	}
	current, err := hqc.OpenChangeOnBranch(ctx, target.Repo, addressed.branch)
	var change *workflow.EditChange
	if err == nil && current != nil {
		change = editChange(current, target.Repo)
	}
	return workflow.PlanEdit(copies, change, err == nil, false), current
}

func editChange(current *hq.Change, repo string) *workflow.EditChange {
	head := ""
	if current.Head != nil {
		head = *current.Head
	}
	return &workflow.EditChange{Number: current.Number, Repo: repo, Head: head, Ready: current.Ready, ReviewBuild: current.ReviewBuild, Behind: current.Behind, ChecksGivenUp: current.ChecksGivenUp != nil}
}

func describedAtHandedOverHead(number int, head string, step workflow.EditStep) bool {
	return step.Action == workflow.EditHandedOver && step.Head == head && step.Number == number
}

// describedHandover never says a build is running without HQ's observation.
func describedHandover(number int, head string, step workflow.EditStep, current *hq.Change, now time.Time) string {
	prefix := fmt.Sprintf("Change #%d described at %s", number, workflow.ShortEditHead(head))
	if describedAtHandedOverHead(number, head, step) {
		message := fmt.Sprintf("Change #%d is handed over at %s", number, workflow.ShortEditHead(head))
		build := ""
		if current != nil && current.ReviewBuild != nil && current.ReviewBuild.Head == head {
			for _, label := range describedLabels(*current, now) {
				switch {
				case label == "Checking it builds", label == "Not added yet: did not build", label == "Build passed", strings.HasPrefix(label, "Could not check the build"):
					build = "; " + label
				}
			}
		}
		message += build + "."
		message += ` Tell the person what is ready as a draft and direct them to Open draft for this change in Mate, never a raw address. The draft is visible only to them there. The person can choose Add to your app; the app changes only when adding succeeds. Before the first add, say the app gets its address when the draft is added. For an app that already has an address, direct them to Open <app name> and Copy link in Mate; it shows only added work. Never put a hostname or address in chat or the description, and never promise the app already has the draft. If adding fails and you fix it, explain the cause in one plain sentence and say you fixed this same draft and are trying again.`
		return message + " End your turn."
	}
	if step.Action == "commit" && step.Copy != "" {
		return fmt.Sprintf("%s, but %s holds uncommitted work: not handed over.", prefix, step.Copy)
	}
	if step.Action == workflow.EditHandedOver {
		return prefix + ", but the branch's change or commit has moved: not handed over."
	}
	return prefix + ", but not handed over: " + step.Words() + "."
}

// errCannotAttach is a picture HQ will never keep for a change — larger than
// it takes, or refused for what it is (pictureRefused): nothing is written
// then, never a description with a broken picture, and words that show it are
// dropped, since they could never go on. HQ refusing the Mate's key, its
// project or the change says nothing of the picture, and is not one.
var errCannotAttach = errors.New("a picture could not be attached")

// picturesGoneError is pictures a description shows that are no longer kept
// for the change it goes onto — pruned since the words were checked, or kept
// only as the address of another change's attachment. Such words can never
// go on: they are refused, never kept.
type picturesGoneError struct{ ids []string }

func (e *picturesGoneError) Error() string {
	verb := "is"
	if len(e.ids) > 1 {
		verb = "are"
	}
	return fmt.Sprintf("%s %s no longer kept, so it cannot be shown on this change", strings.Join(e.ids, ", "), verb)
}

// handleDescribeChange is zerops_workflow action="describe-change": the
// Mate's description of its change, set as the description of the open change
// the call names in HQ.
func handleDescribeChange(
	ctx context.Context,
	httpClient ops.HTTPDoer,
	ssh ops.SSHDeployer,
	rt runtime.Info,
	stateDir string,
	input WorkflowInput,
) (*mcp.CallToolResult, any, error) {
	return describeChange(ctx, httpClient, ssh, rt, stateDir, input, nil, nil)
}

// describeChange accepts the copy and change already selected by open-change;
// that copy's head was read before the open reached HQ.
func describeChange(ctx context.Context, httpClient ops.HTTPDoer, ssh ops.SSHDeployer, rt runtime.Info, stateDir string, input WorkflowInput, openedCopy *descriptionCopy, openedPick *changePick) (*mcp.CallToolResult, any, error) {
	hqc, linked := openHQ(httpClient, rt)
	if !linked {
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			"describe-change writes a change's description in this Mate's HQ, and this Mate is not linked to one yet.",
			"Without a change there is nothing to describe: say what the change does in your answer instead.",
		), WithRecoveryStatus()), nil, nil
	}
	text := strings.TrimSpace(input.Description)
	if refusal := describeTextRefusal(text); refusal != nil {
		return convertError(refusal, WithRecoveryStatus()), nil, nil
	}
	title := strings.TrimSpace(input.Title)
	if refusal := describeTitleRefusal(title); refusal != nil {
		return convertError(refusal, WithRecoveryStatus()), nil, nil
	}
	target, err := resolveChangeTarget(stateDir, hqc.Address(), changeAsk{Service: input.Service, Branch: input.Branch, Verb: changeVerbDescribe})
	if err != nil {
		return convertError(err, WithRecoveryStatus()), nil, nil
	}
	var workcopy descriptionCopy
	if openedCopy != nil {
		workcopy = *openedCopy
	} else {
		workcopy, err = readDescriptionCopy(ctx, ssh, stateDir, hqc.Address(), target, input.Service)
		if err != nil {
			return convertError(err, WithRecoveryStatus()), nil, nil
		}
	}
	target.Branch = workcopy.branch
	service, parts := changeService(stateDir, hqc.Address(), target, input.Service, target.Branch)
	kept := changesIn(hqChanges(stateDir), target.AppID, target.Repo)
	// The change the ledger names, when it names one, is checked before HQ
	// is asked anything: a picture this Mate keeps for no change is refused
	// at once.
	known, _ := target.pick(nil, kept, changeVerbDescribe)
	if refusal := describePicturesRefusal(stateDir, text, workflow.PictureTarget(target.AppID, target.Repo, known.Number)); refusal != nil {
		return convertError(refusal, WithRecoveryStatus()), nil, nil
	}

	// The ledger is only as fresh as the last read of the Mate's state, and
	// words are never put on a change that merged or closed in between:
	// they were written for it, and the next change is another.
	state, stateErr := hqc.ReadMateState()
	var fresh *hq.MateState
	if stateErr == nil {
		fresh = &state
		if openedPick == nil {
			if learned := learnDescribedLandings(stateDir, hqc.Address(), target, kept, state); learned != "" {
				return jsonResult(changeDescriptionResult{
					Service: service, Parts: parts,
					Message: learned + " The description was not written: the change it was for is no longer open, " +
						"and the next change is another one. If these words are about work no change holds yet, describe the change that holds it once it is open." +
						stillOpenWords(target, state),
				}), nil, nil
			}
		}
		kept = changesIn(hqChanges(stateDir), target.AppID, target.Repo)
	}
	pick, err := target.pick(fresh, kept, changeVerbDescribe)
	if openedPick != nil {
		pick, err = *openedPick, nil
	}
	if err != nil {
		return convertError(err, WithRecoveryStatus()), nil, nil
	}
	if pick.Branch != "" && pick.Branch != target.Branch {
		// The change picked names the branch the call did not: its part is
		// the one leasing that branch.
		service, parts = changeService(stateDir, hqc.Address(), target, input.Service, pick.Branch)
	}
	number := pick.Number
	if number == 0 && stateErr != nil {
		return jsonResult(changeDescriptionResult{Service: service, Parts: parts, Message: describeStateUnknown(hqc.Address(), describedWhere(target), stateErr)}), nil, nil
	}
	if number == 0 {
		// Words describe the work a change carries: nothing is kept for a
		// change that is not open.
		return jsonResult(changeDescriptionResult{
			Service: service, Parts: parts,
			Message: fmt.Sprintf("No change of this Mate is open %s in HQ, so there is nothing to describe yet. Describe the change once it is open and holds the work — %s.",
				describedWhere(target), reviewAskedOnlyThen(state.MergeLimited)),
		}), nil, nil
	}
	release, refusal := holdChangeToDescribe(ctx, stateDir, target, number)
	if refusal != nil {
		return refusal, nil, nil
	}
	defer release()
	if gone := describedChangeGone(stateDir, target, number, kept); gone != "" {
		return jsonResult(changeDescriptionResult{Service: service, Parts: parts, Message: gone}), nil, nil
	}
	// Checked before HQ was asked for another change than this one: its
	// pictures are checked for this one now.
	if number != known.Number {
		if refusal := describePicturesRefusal(stateDir, text, workflow.PictureTarget(target.AppID, target.Repo, number)); refusal != nil {
			return convertError(refusal, WithRecoveryStatus()), nil, nil
		}
	}
	ref := changeRef{AppID: target.AppID, Repo: target.Repo, Number: number}
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil {
		return convertError(fmt.Errorf("read the recorded stages for the description: %w", err), WithRecoveryStatus()), nil, nil
	}
	stageBacked := target.Repo != hq.RecipeRepo && len(reviewStages(metas, target.AppID, target.Repo)) > 0
	keepErr := keepChangeDescription(stateDir, ref, pick, text, title, workcopy.head)

	url := hqc.ChangeURL(target.AppID, target.Repo, number)
	answered, tries, away, err := putDescriptionRetried(ctx, hqc, stateDir, ref, text, title, workcopy.head, stageBacked, time.Now())
	if err != nil {
		result := changeDescriptionResult{Service: service, Parts: parts, PullRequest: number, PullRequestURL: url, Kept: true}
		readStep := func() workflow.EditStep {
			step, _ := readHandoverStep(ctx, ssh, hqc, stateDir, target, workcopy)
			return step
		}
		return descriptionWriteFailure(stateDir, ref, result, text, workcopy.head, hqc.Address(), tries, away, err, keepErr, readStep), nil, nil
	}
	step, current := readHandoverStep(ctx, ssh, hqc, stateDir, target, workcopy)
	message := describedHandover(number, workcopy.head, step, current, time.Now())
	if extra := applicationReadyWords(state) + mergeLimitedNote(state.MergeLimited); extra != "" {
		if prefix, found := strings.CutSuffix(message, " End your turn."); found {
			message = prefix + extra + " End your turn."
		} else {
			message += extra
		}
	}
	return jsonResult(changeDescriptionResult{
		Service:        service,
		Parts:          parts,
		PullRequest:    number,
		PullRequestURL: url,
		Described:      true,
		Labels:         describedLabels(answered, time.Now()),
		Message:        message,
	}), nil, nil
}

// descriptionWriteFailure preserves only words that can still be put on this change.
func descriptionWriteFailure(stateDir string, ref changeRef, result changeDescriptionResult, text, head, address string, tries int, away describeAway, err, keepErr error, readStep func() workflow.EditStep) *mcp.CallToolResult {
	number := ref.Number
	var refused *hq.RefusedError
	if errors.As(err, &refused) && refused.Reason == "head_moved" && keepErr == nil {
		result.Message = fmt.Sprintf("HQ refused the description of change #%d because its head moved. The words remain kept for commit %s. Next: %s.", number, head, readStep().Words())
		return jsonResult(result)
	}
	var gone *picturesGoneError
	if errors.As(err, &gone) {
		forgetChangeDescription(stateDir, ref, text, head)
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Nothing was written onto change #%d: %v. %s", number, err, keptPicturesSentence(stateDir, ref.pictureTarget())),
			pictureRetakeSuggestion,
		), WithRecoveryStatus())
	}
	if errors.Is(err, errCannotAttach) {
		forgetChangeDescription(stateDir, ref, text, head)
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			fmt.Sprintf("Nothing was written onto change #%d: %v. HQ will not take that picture, so the description is dropped: words that show it could never go on.", number, err),
			"Describe the change again without that picture, or with another screenshot of the same thing.",
		), WithRecoveryStatus())
	}
	if keepErr != nil {
		return convertError(fmt.Errorf("HQ did not take the description of change #%d (%w), and it could not be kept either: %w", number, err, keepErr), WithRecoveryStatus())
	}
	if refusal := accessRefusal(err); refusal != "" {
		result.Message = fmt.Sprintf("Nothing was written onto change #%d: %s — this Mate's access, not the words or their pictures. "+
			"The description is kept; call describe-change again once the access is back, and if it is still refused, tell the person their Mate's access to HQ is refused.", number, refusal)
	} else if away != describeServed {
		result.Message = describeNotLanded(address, number, tries, away, err)
	} else {
		result.Message = fmt.Sprintf("HQ did not take the description of change #%d (%v). It is kept; call describe-change again to put it on #%d.", number, err, number)
	}
	return jsonResult(result)
}

// describedLabels labels the change HQ answered the describe with, as status
// labels a change (changeLabels): the write's own answer, so no state read
// older than it judges the change. An answer without ready comes from an
// older HQ and is read as ready.
func describedLabels(c hq.Change, now time.Time) []string {
	ready := true
	if c.Ready != nil {
		ready = *c.Ready
	}
	standing := workflow.HQChangeTold{State: c.State, Ready: &ready, Mergeability: c.Mergeability, Behind: c.Behind}
	reviewBuildAsTold(&standing, c.ReviewBuild, c.ChecksGivenUp != nil)
	return changeLabels(&standing, now)
}

// changeRef is one change of the Mate's in HQ: appID's repo, number.
type changeRef struct {
	AppID, Repo string
	Number      int
}

// pictureTarget is how the picture store names the change.
func (r changeRef) pictureTarget() string { return workflow.PictureTarget(r.AppID, r.Repo, r.Number) }

// describedWhere names where a describe looked for its change: the branch
// named, or the repository.
func describedWhere(target changeTarget) string {
	if target.Branch != "" {
		return fmt.Sprintf("on the branch %q in %q", target.Branch, target.Repo)
	}
	return fmt.Sprintf("on %q", target.Repo)
}

// learnDescribedLandings learns what became of the ledger's changes the
// describe could mean — the one on its branch, or, no branch named, any of
// the repository's — and says it of those no longer open (learnChangeLanding);
// "" when every one is open. Words are never put on another change then:
// with no branch named, which one they were for is the Mate's to say.
func learnDescribedLandings(stateDir, address string, target changeTarget, kept []workflow.HQChange, state hq.MateState) string {
	var learned []string
	for _, c := range kept {
		if target.Branch != "" && c.Branch != target.Branch {
			continue
		}
		if line := learnChangeLanding(stateDir, address, c, state); line != "" {
			learned = append(learned, sentenceOf(line))
		}
	}
	return strings.Join(learned, " ")
}

// stillOpenWords names, for a describe that named no branch, the changes
// still open in the repository and the call that describes each; "" when a
// branch was named or none is open.
func stillOpenWords(target changeTarget, state hq.MateState) string {
	if target.Branch != "" {
		return ""
	}
	open := target.openCandidates(&state, nil)
	if len(open) == 0 {
		return ""
	}
	named := make([]string, 0, len(open))
	for _, c := range open {
		named = append(named, fmt.Sprintf("#%d with zerops_workflow action=\"describe-change\" branch=%q description=\"…\"", c.Number, c.Branch))
	}
	return " If they are for a change still open, name its branch: " + strings.Join(named, ", or ") + "."
}

// describedChangeGone is what a describe says when the change it is about to
// write was forgotten while it waited for the change: a pass learned it
// merged or closed meanwhile. "" when it is still kept, or was not kept
// before the wait.
func describedChangeGone(stateDir string, target changeTarget, number int, keptBefore []workflow.HQChange) string {
	if !slices.ContainsFunc(keptBefore, func(c workflow.HQChange) bool { return c.Number == number }) {
		return ""
	}
	if _, found, err := workflow.ReadHQChange(stateDir, target.AppID, target.Repo, number); err != nil || found {
		return ""
	}
	return fmt.Sprintf("The description was not written: change #%d in %q is no longer open — a pass learned what became of it while this describe waited, and said so. If these words are about work no change holds yet, describe the change that holds it once it is open.",
		number, target.Repo)
}

// describeTextRefusal refuses words no description can carry: none, longer
// than HQ keeps, or a NUL — which no text in HQ keeps. Nil for words that can
// go on; their pictures are checked for the change (describePicturesRefusal).
func describeTextRefusal(text string) *platform.PlatformError {
	if text == "" {
		return platform.NewPlatformError(
			platform.ErrInvalidParameter,
			`describe-change needs the words: pass description="…" in markdown.`,
			"Say what the change does and why, and how you checked it.",
		)
	}
	if strings.ContainsRune(text, 0) {
		return platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"The description holds a NUL character, which no description keeps.",
			"Pass the words without it.",
		)
	}
	if n := utf8.RuneCountInString(text); n > changeDescriptionMaxRunes {
		return platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("The description is %d characters long; a change's description holds at most %d.", n, changeDescriptionMaxRunes),
			"Keep it to what the change does and why, and how you checked it — logs and long output belong in the conversation, not in the review.",
		)
	}
	return nil
}

// describePicturesRefusal refuses words that show a picture the change target
// (workflow.PictureTarget) cannot show — none kept, or one kept only as the
// address of another change's attachment — or, as itself, a store that cannot
// be read. Nil when every picture can go on.
func describePicturesRefusal(stateDir, text, target string) error {
	missing, err := missingPictures(stateDir, text, target)
	if err != nil {
		return fmt.Errorf("read this Mate's pictures: %w", err)
	}
	if len(missing) == 0 {
		return nil
	}
	return platform.NewPlatformError(
		platform.ErrInvalidParameter,
		fmt.Sprintf("The description shows %s, and this Mate keeps no such picture. %s",
			strings.Join(missing, ", "), keptPicturesSentence(stateDir, target)),
		pictureRetakeSuggestion,
	)
}

// describeTitleRefusal refuses a title no change can carry: more than one
// line, a NUL, or longer than HQ keeps. Nil for "" — no title given — and for
// one that can go on.
func describeTitleRefusal(title string) *platform.PlatformError {
	switch {
	case strings.ContainsRune(title, 0):
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			"The title holds a NUL character, which no title keeps.", "Pass the title without it.")
	case strings.ContainsAny(title, "\r\n"):
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			"The title is more than one line; a change's title is one line.",
			"Say what this repository's change does in a few words; the rest belongs in the description.")
	case utf8.RuneCountInString(title) > changeTitleRunes:
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("The title is %d characters long; a change's title holds at most %d.", utf8.RuneCountInString(title), changeTitleRunes),
			"Say what this repository's change does in a few words; the rest belongs in the description.")
	}
	return nil
}

// noRepositoryYet is what to do for a pair with no repository in HQ yet.
const noRepositoryYet = `A pair gets its repository once this Mate is in an application in HQ; then push a branch of your own and open its change with zerops_workflow action="open-change".`

// keepChangeDescription keeps the words — and the title given with them —
// with the change they were written for in the ledger, recording the change
// on its branch when the ledger did not keep it yet.
func keepChangeDescription(stateDir string, ref changeRef, pick changePick, text, title, head string) error {
	kept := &workflow.ChangeDescription{Text: text, Title: title, Change: ref.Number, Head: head}
	if err := workflow.UpsertHQChange(stateDir, ref.AppID, ref.Repo, ref.Number, func(c *workflow.HQChange, existed bool) error {
		if !existed {
			c.Branch, c.Title, c.Source = pick.Branch, pick.Title, workflow.HQChangeFromState
		}
		c.Description = kept
		return nil
	}); err != nil {
		return fmt.Errorf("keep the description of change #%d: %w", ref.Number, err)
	}
	return nil
}

// putChangeDescription sets text as the change's description — its pictures
// attached to the change first (changeBody) — and title, when one is given,
// as its title, in one edit: HQ asks the person to review the change from
// then, at the head it has. Once HQ took it, the words the ledger kept are
// forgotten, unless newer ones were kept meanwhile, which stay for the next
// put. Nothing is written when a picture could not be attached: never a
// description with a broken picture. It answers the change as HQ answered the
// edit.
func putChangeDescription(ctx context.Context, hqc hq.Client, stateDir string, ref changeRef, text, title, head string, stageBacked bool, at time.Time) (hq.Change, error) {
	body, err := changeBody(ctx, hqc, stateDir, ref, text)
	if err != nil {
		return hq.Change{}, err
	}
	edit := descriptionEdit(body, title, head, stageBacked, at)
	callCtx, cancel := context.WithTimeout(ctx, hqCallTimeout)
	defer cancel()
	change, err := hqc.EditChange(callCtx, ref.Repo, ref.Number, edit)
	if change.StateRev > 0 {
		if recordErr := rememberChangeRevision(stateDir, ref, change.StateRev); recordErr != nil {
			return hq.Change{}, fmt.Errorf("record HQ's write after edit: %w", recordErr)
		}
	}
	if err != nil {
		return hq.Change{}, err
	}
	forgetChangeDescription(stateDir, ref, text, head)
	return change, nil
}

// descriptionEdit binds the words and the optional take to the same head.
func descriptionEdit(body, title, head string, stageBacked bool, at time.Time) hq.ChangeEdit {
	edit := hq.ChangeEdit{Body: &body, ExpectedHead: head}
	if title != "" {
		edit.Title = &title
	}
	if stageBacked {
		at = at.UTC()
		edit.ReviewBuild = &hq.ReviewBuildWrite{Head: head, Step: "taken", At: at.Format(time.RFC3339), Until: at.Add(30 * time.Minute).Format(time.RFC3339)}
	}
	return edit
}

// changeBody is text as it is published on change number: every picture it
// shows attached to the change — once per change, the address HQ serves it at
// remembered with the picture — and written as an <img> with its size
// (pictureTag).
func changeBody(ctx context.Context, hqc hq.Client, stateDir string, ref changeRef, text string) (string, error) {
	ids := workflow.PictureRefs(text)
	if len(ids) == 0 {
		return text, nil
	}
	target := ref.pictureTarget()
	// Every picture is read before any is attached: the words go on whole
	// or not at all. One already on this change needs only its address.
	pics := make(map[string]workflow.Picture, len(ids))
	pngs := make(map[string][]byte, len(ids))
	var gone []string
	for _, id := range ids {
		pic, png, err := workflow.KeptPicture(stateDir, id)
		if err != nil && !errors.Is(err, workflow.ErrPictureNotKept) {
			return "", fmt.Errorf("read picture %s: %w", id, err)
		}
		if err != nil || (png == nil && pic.Uploads[target] == "") {
			gone = append(gone, id)
			continue
		}
		pics[id], pngs[id] = pic, png
	}
	if len(gone) > 0 {
		return "", &picturesGoneError{ids: gone}
	}
	tags := make(map[string]func(alt string) string, len(ids))
	for _, id := range ids {
		pic, png := pics[id], pngs[id]
		url := pic.Uploads[target]
		if url == "" {
			if len(png) > changePictureMaxBytes {
				return "", fmt.Errorf("%w: %s is larger than the 20 MiB a picture of a change may be", errCannotAttach, id)
			}
			callCtx, cancel := context.WithTimeout(ctx, hqCallTimeout)
			kept, err := hqc.Attach(callCtx, ref.Repo, ref.Number, png)
			cancel()
			if kept.StateRev > 0 {
				if recordErr := rememberChangeRevision(stateDir, ref, kept.StateRev); recordErr != nil {
					return "", fmt.Errorf("record HQ's write after attachment: %w", recordErr)
				}
			}
			var refused *hq.RefusedError
			if errors.As(err, &refused) && pictureRefused(refused) {
				return "", fmt.Errorf("%w: HQ refused %s (%s)", errCannotAttach, id, hqRefusalWords(refused))
			}
			if err != nil {
				return "", err
			}
			url = hqc.AttachmentURL(kept.Path)
			_ = workflow.RecordPictureUpload(stateDir, id, target, url)
		}
		width, height := pic.Width, pic.Height
		tags[id] = func(alt string) string { return pictureTag(alt, url, width, height) }
	}
	return workflow.ReplacePictureRefs(text, func(alt, id string) string { return tags[id](alt) }), nil
}

// pictureDisplayWidth is the widest a picture is drawn in a description:
// about the width of the review's column. Sized to the column, it is drawn
// true, and the review reserves the same shape.
const pictureDisplayWidth = 720

// pictureTag is a picture as a published description carries it: an <img>
// whose width and height give its shape at the width it is drawn, so whoever
// reads the description reserves its box before the bytes arrive. The
// picture itself keeps every pixel.
func pictureTag(alt, url string, width, height int) string {
	if width <= 0 || height <= 0 {
		return fmt.Sprintf(`<img alt="%s" src="%s">`, html.EscapeString(alt), html.EscapeString(url))
	}
	if width > pictureDisplayWidth {
		height = int(math.Round(float64(height) * pictureDisplayWidth / float64(width)))
		width = pictureDisplayWidth
	}
	return fmt.Sprintf(`<img alt="%s" width="%d" height="%d" src="%s">`, html.EscapeString(alt), width, height, html.EscapeString(url))
}

// pictureRetakeSuggestion is what to do about a picture this Mate does not
// keep.
const pictureRetakeSuggestion = "Show a picture it keeps, or take the screenshot again with zerops_browser screenshot=true and show the picture its result names — or leave the picture out."

// pictureRefused reports HQ refusing an attachment for the picture itself:
// larger than it takes (413 too_large) or no raster picture (400 invalid,
// not_raster). Only these say the words showing it can never go on; a refusal
// of the Mate's key, its project or the change keeps them.
func pictureRefused(refused *hq.RefusedError) bool {
	switch {
	case refused.Status == http.StatusRequestEntityTooLarge && refused.Code == "too_large":
		return true
	case refused.Status == http.StatusBadRequest && refused.Code == "invalid" && refused.Reason == "not_raster":
		return true
	}
	return false
}

// accessRefusal names HQ refusing this Mate's access — HQ's word on its key
// (key_refused, key_too_wide, key_not_sensitive, not_a_mate), Zerops refusing
// it, or its project gone — for the words a describe keeps; "" for any other
// answer.
func accessRefusal(err error) string {
	var refused *hq.RefusedError
	if !errors.As(err, &refused) {
		return ""
	}
	if project := changeProjectRefusal(refused); project != "" {
		return project
	}
	switch refused.Code {
	case "key_refused", "key_too_wide", "key_not_sensitive", "not_a_mate":
		return fmt.Sprintf("HQ refused this Mate's key (%d %s)", refused.Status, hqRefusalWords(refused))
	}
	return ""
}

// keptPicturesSentence says which pictures this Mate keeps for the change
// target, and for how long.
func keptPicturesSentence(stateDir, target string) string {
	rule := fmt.Sprintf("each for %s after it is taken (past %d MiB of pictures the oldest go first), every one a kept description shows, and one already on a change for that change",
		pictureKeepWords(), workflow.PictureStoreBytes>>20)
	ids, err := workflow.KeptPictures(stateDir, target)
	if err != nil || len(ids) == 0 {
		return "It keeps no picture now; it keeps a screenshot " + rule + "."
	}
	return "It keeps " + keptPicturesWords(ids) + ", " + rule + "."
}

// missingPictures is the pictures text shows that the change target cannot
// show: none kept, or one whose file is gone and whose address is another
// change's. Any other failure to read the store is an error, never a
// picture gone.
func missingPictures(stateDir, text, target string) ([]string, error) {
	var missing []string
	for _, id := range workflow.PictureRefs(text) {
		pic, png, err := workflow.KeptPicture(stateDir, id)
		if err != nil && !errors.Is(err, workflow.ErrPictureNotKept) {
			return nil, err
		}
		if err != nil || (png == nil && pic.Uploads[target] == "") {
			missing = append(missing, id)
		}
	}
	return missing, nil
}

// forgetChangeDescription drops the words the ledger keeps for the change —
// only while they are still text: a describe that kept newer words after
// these were read keeps them.
func forgetChangeDescription(stateDir string, ref changeRef, text, head string) {
	_ = workflow.UpsertHQChange(stateDir, ref.AppID, ref.Repo, ref.Number, func(c *workflow.HQChange, existed bool) error {
		if !existed || c.Description == nil || c.Description.Text != text || c.Description.Head != head {
			return workflow.ErrSkipWrite
		}
		c.Description = nil
		return nil
	})
}

// sentenceOf makes one of the outcome lines a sentence of its own.
func sentenceOf(line string) string {
	if line == "" {
		return ""
	}
	return strings.ToUpper(line[:1]) + line[1:] + "."
}

// keptPicturesShown is how many runs of kept pictures a refusal names: the
// newest, so the list stays a line however many are kept.
const keptPicturesShown = 8

// keptPicturesWords names the kept pictures ids lists (oldest first) as runs
// — "shot-4, shot-50 to shot-52" — the newest keptPicturesShown of them, with
// how many older ones there are besides.
func keptPicturesWords(ids []string) string {
	type run struct{ first, last, count int }
	var runs []run
	for _, id := range ids {
		n, err := strconv.Atoi(strings.TrimPrefix(id, "shot-"))
		if err != nil {
			continue
		}
		if len(runs) > 0 && runs[len(runs)-1].last == n-1 {
			runs[len(runs)-1].last = n
			runs[len(runs)-1].count++
			continue
		}
		runs = append(runs, run{first: n, last: n, count: 1})
	}
	var words []string
	if len(runs) > keptPicturesShown {
		older := 0
		for _, r := range runs[:len(runs)-keptPicturesShown] {
			older += r.count
		}
		noun := "pictures"
		if older == 1 {
			noun = "picture"
		}
		words = append(words, fmt.Sprintf("%d older %s", older, noun))
		runs = runs[len(runs)-keptPicturesShown:]
	}
	for _, r := range runs {
		switch r.count {
		case 1:
			words = append(words, fmt.Sprintf("shot-%d", r.first))
		case 2:
			words = append(words, fmt.Sprintf("shot-%d, shot-%d", r.first, r.last))
		default:
			words = append(words, fmt.Sprintf("shot-%d to shot-%d", r.first, r.last))
		}
	}
	return strings.Join(words, ", ")
}

// pictureKeepWords is how long a picture is kept at most, in days: the byte
// bound can prune it sooner.
func pictureKeepWords() string {
	return fmt.Sprintf("up to %d days", int(workflow.PictureKeepFor/(24*time.Hour)))
}
