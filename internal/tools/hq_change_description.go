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
// zcp opens the change titled by what it holds and nothing more, because that
// is all zcp knows. The Mate keeps working on top of an open change,
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
	Message        string   `json:"message"`
}

// errCannotAttach is a picture HQ will never keep for a change — larger than
// it takes, or refused for what it is: nothing is written then, never a
// description with a broken picture, and words that show it are dropped,
// since they could never go on. HQ refusing for now (429, a 5xx) is not one.
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
	rt runtime.Info,
	stateDir string,
	input WorkflowInput,
) (*mcp.CallToolResult, any, error) {
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
	state, stateErr := describeSelf(ctx, hqc, known.Number != 0)
	var fresh *hq.MateState
	if stateErr == nil {
		fresh = &state
		if learned := learnDescribedLandings(stateDir, hqc.Address(), target, kept, state); learned != "" {
			return jsonResult(changeDescriptionResult{
				Service: service, Parts: parts,
				Message: learned + " The description was not written: the change it was for is no longer open, " +
					"and the next change is another one. If these words are about work no change holds yet, describe the change that holds it once it is open." +
					stillOpenWords(target, state),
			}), nil, nil
		}
		kept = changesIn(hqChanges(stateDir), target.AppID, target.Repo)
	}
	pick, err := target.pick(fresh, kept, changeVerbDescribe)
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
			Message: fmt.Sprintf("No change of this Mate is open %s in HQ, so there is nothing to describe yet. Describe the change once it is open and holds the work — the person is asked to review it only then.",
				describedWhere(target)),
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
	keepErr := keepChangeDescription(ctx, stateDir, ref, pick, text, title)

	url := hqc.ChangeURL(target.AppID, target.Repo, number)
	if tries, away, err := putDescriptionRetried(ctx, hqc, stateDir, ref, text, title); err != nil {
		var gone *picturesGoneError
		if errors.As(err, &gone) {
			forgetChangeDescription(ctx, stateDir, ref, text, title)
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("Nothing was written onto change #%d: %v. %s", number, err, keptPicturesSentence(stateDir, ref.pictureTarget())),
				pictureRetakeSuggestion,
			), WithRecoveryStatus()), nil, nil
		}
		if errors.Is(err, errCannotAttach) {
			forgetChangeDescription(ctx, stateDir, ref, text, title)
			return convertError(platform.NewPlatformError(
				platform.ErrPrerequisiteMissing,
				fmt.Sprintf("Nothing was written onto change #%d: %v. HQ will not take that picture, so the description is dropped: words that show it could never go on.",
					number, err),
				"Describe the change again without that picture, or with another screenshot of the same thing.",
			), WithRecoveryStatus()), nil, nil
		}
		if keepErr != nil {
			return convertError(fmt.Errorf("HQ did not take the description of change #%d (%w), and it could not be kept either: %w",
				number, err, keepErr), WithRecoveryStatus()), nil, nil
		}
		if away != describeServed {
			return jsonResult(changeDescriptionResult{
				Service: service, Parts: parts, PullRequest: number, PullRequestURL: url, Kept: true,
				Message: describeNotLanded(hqc.Address(), number, tries, away, err),
			}), nil, nil
		}
		return jsonResult(changeDescriptionResult{
			Service:        service,
			Parts:          parts,
			PullRequest:    number,
			PullRequestURL: url,
			Kept:           true,
			Message: fmt.Sprintf("HQ did not take the description of change #%d (%v). It is kept; call describe-change again to put it on #%d.",
				number, err, number),
		}), nil, nil
	}
	return jsonResult(changeDescriptionResult{
		Service:        service,
		Parts:          parts,
		PullRequest:    number,
		PullRequestURL: url,
		Described:      true,
		Message: fmt.Sprintf("Change #%d carries this description now and asks for the person's review — they review it by these words in the app. A push that moves it makes it a draft again, until you describe it again.",
			number),
	}), nil, nil
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
		named = append(named, fmt.Sprintf("#%d with zerops_workflow action=\"describe-change\" branch=%q", c.Number, c.Branch))
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
func keepChangeDescription(ctx context.Context, stateDir string, ref changeRef, pick changePick, text, title string) error {
	invocation, err := hq.InvocationFromContext(ctx)
	if err != nil {
		return err
	}
	kept := &workflow.ChangeDescription{Text: text, Title: title, Change: ref.Number, Invocation: &invocation}
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
// description with a broken picture.
func putChangeDescription(ctx context.Context, hqc hq.Client, stateDir string, ref changeRef, text, title string) error {
	body, err := changeBody(ctx, hqc, stateDir, ref, text)
	if err != nil {
		return err
	}
	edit := hq.ChangeEdit{Body: &body}
	if title != "" {
		edit.Title = &title
	}
	callCtx, cancel := context.WithTimeout(ctx, hqCallTimeout)
	defer cancel()
	if _, err := hqc.EditChange(callCtx, ref.Repo, ref.Number, edit); err != nil {
		return err
	}
	forgetChangeDescription(ctx, stateDir, ref, text, title)
	return nil
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
			var refused *hq.RefusedError
			if errors.As(err, &refused) && refusedForGood(refused) {
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

// refusedForGood reports whether HQ's refusal of an attachment is one it
// will repeat: a 4xx other than a timeout or too many requests.
func refusedForGood(refused *hq.RefusedError) bool {
	return refused.Status >= 400 && refused.Status < 500 &&
		refused.Status != http.StatusRequestTimeout && refused.Status != http.StatusTooManyRequests
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
// only for this exact accepted call, text and title. Historical unknown
// provenance and a successor with identical words are never relabelled.
func forgetChangeDescription(ctx context.Context, stateDir string, ref changeRef, text, title string) {
	invocation, err := hq.InvocationFromContext(ctx)
	if err != nil {
		return
	}
	_ = workflow.UpsertHQChange(stateDir, ref.AppID, ref.Repo, ref.Number, func(c *workflow.HQChange, existed bool) error {
		if !existed || c.Description == nil || c.Description.Text != text || c.Description.Title != title || c.Description.Invocation == nil || *c.Description.Invocation != invocation {
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
