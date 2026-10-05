package tools

import (
	"context"
	"errors"
	"fmt"
	"html"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A change's description is what the person reviews a Mate's change by — in
// the app's review, right after the verdict. It is the body of the change in
// HQ, and only the Mate writes it (zerops_workflow action="describe-change"):
// zcp opens the change with the task as its title and nothing more, because
// the task is all zcp knows. The Mate keeps working on top of an open change,
// so it rewrites the description as the change grows; a link to try the change
// can go stale, and the description is where what it does and how it was
// checked stays true.
//
// The words are kept on the pair first and put on the change second, so an
// HQ that did not answer, or a change not open yet, loses nothing:
//
//   - the pair's open change takes them at once;
//   - with none open they wait for the one the pair opens next (shipChange
//     puts them on it);
//   - words never reach a change they were not written for: when the change
//     merged or closed before they reached it, the Mate is told and nothing is
//     kept, and words kept for a change that is gone are dropped rather than
//     put on the next change's.
//
// describe-change never opens a change: describing is not a change.

// changeDescriptionMaxRunes bounds a description: HQ keeps at most 20000
// characters, and the review shows a page, not a log.
const changeDescriptionMaxRunes = 20000

// changePictureMaxBytes is the largest picture HQ keeps for a description.
const changePictureMaxBytes = 20 << 20

// changeDescriptionResult is describe-change's answer.
type changeDescriptionResult struct {
	Service        string `json:"service"`
	PullRequest    int    `json:"pullRequest,omitempty"`
	PullRequestURL string `json:"pullRequestUrl,omitempty"`
	Described      bool   `json:"described"`
	Kept           bool   `json:"kept,omitempty"`
	Message        string `json:"message"`
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
// Mate's description of its change, set as the description of the pair's
// open change in HQ, or kept until one is open.
func handleDescribeChange(
	ctx context.Context,
	httpClient ops.HTTPDoer,
	stateDir string,
	input WorkflowInput,
) (*mcp.CallToolResult, any, error) {
	hqc, enrolled := openHQ(httpClient)
	if !enrolled {
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			"describe-change writes a change's description in this Mate's HQ, and this Mate is not enrolled with one yet.",
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
	meta, refusal := pairToDescribe(stateDir, input.Service)
	if refusal != nil {
		return refusal, nil, nil
	}
	if refusal := describePicturesRefusal(stateDir, text, pictureTargetOf(meta, meta.HQ.Change)); refusal != nil {
		return convertError(refusal, WithRecoveryStatus()), nil, nil
	}

	// The change on record is only as fresh as the last read of the Mate's
	// state, and words are never put on one that merged or closed in
	// between: they were written for it, and the next change is another.
	state, stateErr := hqc.Self(ctx)
	if stateErr == nil {
		if learned := hqLearnLanding(stateDir, meta, state); learned != "" {
			return jsonResult(changeDescriptionResult{
				Service: meta.Hostname,
				Message: sentenceOf(learned) + " The description was not written: the change it was for is no longer open, " +
					"and the next change is another one. If these words are about work not delivered yet, describe the change " +
					nextDeliveryOf(meta) + ` opens once it holds that work (zerops_workflow action="describe-change"): it asks for review only then.`,
			}), nil, nil
		}
	}

	number := meta.HQ.Change
	if number == 0 && stateErr == nil {
		// Nothing on record — the record of an open change may not have been
		// written. The Mate's own state names its open change in the
		// repository, if there is one.
		if open := openChangeIn(state, meta.HQ.Repo); open != 0 {
			recordChange(stateDir, meta, open)
			number = open
		}
	}

	if number == 0 {
		// Words describe the work a change carries, and the change the next
		// delivery opens carries whatever the pair holds then: nothing is
		// kept for it.
		return jsonResult(changeDescriptionResult{
			Service: meta.Hostname,
			Message: fmt.Sprintf("No change is open from %s yet, so there is nothing to describe: %s opens it as a draft. Describe it once it holds the work — the person is asked to review it only then.",
				meta.Hostname, nextDeliveryOf(meta)),
		}), nil, nil
	}
	keepErr := keepChangeDescription(stateDir, meta, text, title, number)

	url := hqc.ChangeURL(meta.HQ.AppID, meta.HQ.Repo, number)
	if err := putChangeDescription(ctx, hqc, stateDir, meta, number, text, title); err != nil {
		var gone *picturesGoneError
		if errors.As(err, &gone) {
			forgetChangeDescription(stateDir, meta, text)
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("Nothing was written onto change #%d: %v. %s", number, err, keptPicturesSentence(stateDir, pictureTargetOf(meta, number))),
				pictureRetakeSuggestion,
			), WithRecoveryStatus()), nil, nil
		}
		if errors.Is(err, errCannotAttach) {
			forgetChangeDescription(stateDir, meta, text)
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
		return jsonResult(changeDescriptionResult{
			Service:        meta.Hostname,
			PullRequest:    number,
			PullRequestURL: url,
			Kept:           true,
			Message: fmt.Sprintf("HQ did not take the description of change #%d (%v). It is kept, and goes onto #%d with %s unless that delivery changes its work; call describe-change again to try now.",
				number, err, number, nextDeliveryOf(meta)),
		}), nil, nil
	}
	return jsonResult(changeDescriptionResult{
		Service:        meta.Hostname,
		PullRequest:    number,
		PullRequestURL: url,
		Described:      true,
		Message: fmt.Sprintf("Change #%d carries this description now and asks for the person's review — they review it by these words in the app. A push that moves it makes it a draft again, until you describe it again.",
			number),
	}), nil, nil
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

// pictureTargetOf is how the picture store names change number of m's
// repository.
func pictureTargetOf(m *workflow.ServiceMeta, number int) string {
	return workflow.PictureTarget(m.HQ.AppID, m.HQ.Repo, number)
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

// openChangeIn is the number of the Mate's open change in repo as its own
// state names it, 0 for none.
func openChangeIn(state hq.MateState, repo string) int {
	for _, c := range state.Changes {
		if c.Repo == repo && c.State == hq.ChangeOpen {
			return c.Number
		}
	}
	return 0
}

// pairToDescribe is the pair a description is for: the one service names,
// either half, or the only pair with a repository in HQ. Otherwise the
// refusal that says which pairs there are.
func pairToDescribe(stateDir, service string) (*workflow.ServiceMeta, *mcp.CallToolResult) {
	if service != "" {
		meta, err := workflow.FindServiceMeta(stateDir, service)
		if err != nil {
			return nil, convertError(fmt.Errorf("read the pair %q: %w", service, err), WithRecoveryStatus())
		}
		if meta == nil {
			return nil, convertError(platform.NewPlatformError(
				platform.ErrServiceNotFound,
				fmt.Sprintf("No pair of this Mate is named %q.", service),
				"Pass the dev or the stage hostname of the pair whose change this describes.",
			), WithRecoveryStatus())
		}
		if !hqPairWired(meta) {
			return nil, convertError(platform.NewPlatformError(
				platform.ErrPrerequisiteMissing,
				fmt.Sprintf("%s has no repository in HQ yet, so it has no change to describe.", meta.Hostname),
				"A pair gets its repository once this Mate is in an application in HQ; its first stage deploy opens the change.",
			), WithRecoveryStatus())
		}
		return meta, nil
	}
	metas, _ := workflow.ListServiceMetas(stateDir)
	var wired []*workflow.ServiceMeta
	for _, m := range metas {
		if hqPairWired(m) {
			wired = append(wired, m)
		}
	}
	switch len(wired) {
	case 1:
		return wired[0], nil
	case 0:
		return nil, convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			"No pair of this Mate has its repository in HQ yet, so there is no change to describe.",
			"A pair gets its repository once this Mate is in an application in HQ; its first stage deploy opens the change.",
		), WithRecoveryStatus())
	}
	sort.Slice(wired, func(i, j int) bool { return wired[i].Hostname < wired[j].Hostname })
	names := make([]string, 0, len(wired))
	for _, m := range wired {
		names = append(names, fmt.Sprintf("service=%q", m.Hostname))
	}
	return nil, convertError(platform.NewPlatformError(
		platform.ErrServiceRequired,
		"This Mate has several pairs with a repository in HQ, each with its own change: say which change this describes.",
		"Pass "+strings.Join(names, " or ")+".",
	), WithRecoveryStatus())
}

// keepChangeDescription records the words — and the title given with them —
// on the pair, in memory and on disk, for the change they were written for.
func keepChangeDescription(stateDir string, m *workflow.ServiceMeta, text, title string, number int) error {
	kept := &workflow.ChangeDescription{Text: text, Title: title, Change: number}
	if err := workflow.UpsertServiceMeta(stateDir, m.Hostname, func(meta *workflow.ServiceMeta, existed bool) error {
		if !existed || meta.HQ == nil {
			return fmt.Errorf("%s no longer has its repository in HQ", m.Hostname)
		}
		meta.HQ.ChangeDescription = kept
		return nil
	}); err != nil {
		return fmt.Errorf("keep the description of %s's change: %w", m.Hostname, err)
	}
	m.HQ.ChangeDescription = kept
	return nil
}

// putChangeDescription sets text as change number's description — its
// pictures attached to the change first (changeBody) — and title, when one is
// given, as its title, in one edit: HQ asks the person to review the change
// from then, at the head it has. Once HQ took it, the words the pair kept are
// forgotten, unless newer ones were kept meanwhile, which stay for the next
// put. Nothing is written when a picture could not be attached: never a
// description with a broken picture.
func putChangeDescription(ctx context.Context, hqc hq.Client, stateDir string, m *workflow.ServiceMeta, number int, text, title string) error {
	body, err := changeBody(ctx, hqc, stateDir, m, number, text)
	if err != nil {
		return err
	}
	edit := hq.ChangeEdit{Body: &body}
	if title != "" {
		edit.Title = &title
	}
	callCtx, cancel := context.WithTimeout(ctx, hqCallTimeout)
	defer cancel()
	if _, err := hqc.EditChange(callCtx, m.HQ.Repo, number, edit); err != nil {
		return err
	}
	forgetChangeDescription(stateDir, m, text)
	return nil
}

// changeBody is text as it is published on change number: every picture it
// shows attached to the change — once per change, the address HQ serves it at
// remembered with the picture — and written as an <img> with its size
// (pictureTag).
func changeBody(ctx context.Context, hqc hq.Client, stateDir string, m *workflow.ServiceMeta, number int, text string) (string, error) {
	ids := workflow.PictureRefs(text)
	if len(ids) == 0 {
		return text, nil
	}
	target := pictureTargetOf(m, number)
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
			kept, err := hqc.Attach(callCtx, m.HQ.Repo, number, png)
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

// putKeptChangeDescription puts what the pair keeps onto change number — the
// one a delivery or a push has just reached — and reports whether it did, or,
// when a picture could not go with it, why not (the words stay kept). Read
// fresh from disk: the caller's copy of the pair can predate a describe that
// landed while it worked. Words go on only while the push that reached the
// change moved nothing: they describe the change as it was when they were
// written, so a push that moved it — or opened it — drops them (stale), and
// the change stays a draft until the Mate describes it again. Words kept for
// another change are dropped too: that change is gone.
func putKeptChangeDescription(ctx context.Context, hqc hq.Client, stateDir string, m *workflow.ServiceMeta, number int, moved bool) (described bool, note string, stale bool) {
	fresh, err := workflow.FindServiceMeta(stateDir, m.Hostname)
	if err != nil || fresh == nil || fresh.HQ == nil || fresh.HQ.ChangeDescription == nil {
		return false, "", false
	}
	kept := *fresh.HQ.ChangeDescription
	if kept.Change != 0 && kept.Change != number {
		forgetChangeDescription(stateDir, m, kept.Text)
		return false, "", false
	}
	if moved {
		forgetChangeDescription(stateDir, m, kept.Text)
		return false, "", true
	}
	err = putChangeDescription(ctx, hqc, stateDir, m, number, kept.Text, kept.Title)
	var gone *picturesGoneError
	if errors.As(err, &gone) || errors.Is(err, errCannotAttach) {
		forgetChangeDescription(stateDir, m, kept.Text)
		return false, "The description you wrote is dropped, not on it: " + err.Error() + ".", false
	}
	return err == nil, "", false
}

// forgetChangeDescription drops the words the pair keeps, in memory and on
// disk — only while they are still text: a describe that kept newer words
// after these were read keeps them.
func forgetChangeDescription(stateDir string, m *workflow.ServiceMeta, text string) {
	if kept := m.HQ.ChangeDescription; kept != nil && kept.Text == text {
		m.HQ.ChangeDescription = nil
	}
	_ = workflow.UpsertServiceMeta(stateDir, m.Hostname, func(meta *workflow.ServiceMeta, existed bool) error {
		if !existed || meta.HQ == nil || meta.HQ.ChangeDescription == nil || meta.HQ.ChangeDescription.Text != text {
			return workflow.ErrSkipWrite
		}
		meta.HQ.ChangeDescription = nil
		return nil
	})
}

// nextDeliveryOf names when a pair's next change opens: its next stage
// deploy, which delivers.
func nextDeliveryOf(m *workflow.ServiceMeta) string {
	if m.StageHostname != "" {
		return fmt.Sprintf("%s's next delivery (a deploy of %s)", m.Hostname, m.StageHostname)
	}
	return m.Hostname + "'s next delivery"
}

// describeLine is what a push or a delivery that leaves a change open says
// of its description: the person reviews the change by it, and is asked to
// only once the Mate describes it at what it holds — so a change this call
// moved is a draft until then — or, when the words it kept went on with this
// call, that they did.
func describeLine(ref *changeRef, hostname string) string {
	if ref.DescriptionNote != "" {
		return fmt.Sprintf(`%s Describe it again with zerops_workflow action="describe-change" service=%q, without that picture or with one this Mate keeps.`,
			ref.DescriptionNote, hostname)
	}
	if ref.Described {
		return fmt.Sprintf(`It carries the description you wrote, and asks for the person's review. A push that moves it makes it a draft again: describe it again then with zerops_workflow action="describe-change" service=%q title="…" description="…".`,
			hostname)
	}
	ask := fmt.Sprintf(`zerops_workflow action="describe-change" service=%q title="…" description="…" — as its title, what this repository's change does in a few words; then what it does and why, how you checked it, and the zerops_browser screenshots that show it as ![what it shows](shot-N)`,
		hostname)
	if ref.Draft {
		stale := ""
		if ref.staleDescription {
			stale = "The description you wrote for it before this push is not on it: it describes the change as it was. "
		}
		return fmt.Sprintf("%sChange #%d is a draft until you describe it as it is now: the person is not asked to review it before. Once its work is done — not while you still change it — describe it with %s. Every push that moves it makes it a draft again, until you describe it again.",
			stale, ref.Number, ask)
	}
	return fmt.Sprintf("Describe it for the person's review with %s — and again after every push that moves it.", ask)
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
