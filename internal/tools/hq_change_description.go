package tools

import (
	"context"
	"errors"
	"fmt"
	"html"
	"math"
	"sort"
	"strings"
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

// errCannotAttach is a picture HQ would not keep for a change: nothing is
// written then, never a description with a broken picture.
var errCannotAttach = errors.New("a picture could not be attached")

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
	if refusal := describeTextRefusal(stateDir, text); refusal != nil {
		return convertError(refusal, WithRecoveryStatus()), nil, nil
	}
	meta, refusal := pairToDescribe(stateDir, input.Service)
	if refusal != nil {
		return refusal, nil, nil
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
					`and the next change is another one. If these words are about work not delivered yet, call zerops_workflow action="describe-change" ` +
					"again — they are then kept for the change " + nextDeliveryOf(meta) + " opens.",
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

	keepErr := keepChangeDescription(stateDir, meta, text, number)
	if keepErr != nil && number == 0 {
		return convertError(keepErr, WithRecoveryStatus()), nil, nil
	}
	if number == 0 {
		return jsonResult(changeDescriptionResult{
			Service: meta.Hostname,
			Kept:    true,
			Message: fmt.Sprintf("No change is open from %s yet, so the description is kept: it goes onto the change %s opens.",
				meta.Hostname, nextDeliveryOf(meta)),
		}), nil, nil
	}

	url := hqc.ChangeURL(meta.HQ.AppID, meta.HQ.Repo, number)
	if err := putChangeDescription(ctx, hqc, stateDir, meta, number, text); err != nil {
		if errors.Is(err, errCannotAttach) {
			return convertError(platform.NewPlatformError(
				platform.ErrPrerequisiteMissing,
				fmt.Sprintf("Nothing was written onto change #%d: %v. The description is kept, and goes onto #%d with %s.",
					number, err, number, nextDeliveryOf(meta)),
				"Describe the change again without that picture to put the words on it now.",
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
			Message: fmt.Sprintf("HQ did not take the description of change #%d (%v). It is kept, and goes onto #%d with %s; call describe-change again to try now.",
				number, err, number, nextDeliveryOf(meta)),
		}), nil, nil
	}
	return jsonResult(changeDescriptionResult{
		Service:        meta.Hostname,
		PullRequest:    number,
		PullRequestURL: url,
		Described:      true,
		Message: fmt.Sprintf("Change #%d carries this description now — the person reviews the change by it in the app. Describe it again whenever the change grows.",
			number),
	}), nil, nil
}

// describeTextRefusal refuses words no description can carry: none, longer
// than HQ keeps, a NUL — which no text in HQ keeps — or a picture this Mate
// does not keep. Nil for words that can go on.
func describeTextRefusal(stateDir, text string) *platform.PlatformError {
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
	if missing := missingPictures(stateDir, text); len(missing) > 0 {
		return platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("The description shows %s, and this Mate keeps no such picture — the newest %d screenshots are kept, and every one a kept description shows.",
				strings.Join(missing, ", "), workflow.PictureKeep),
			"Take the screenshot again with zerops_browser screenshot=true and use the picture its result names, or leave the picture out.",
		)
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

// keepChangeDescription records the words on the pair, in memory and on
// disk, for the change they were written for (0: the one the pair opens
// next).
func keepChangeDescription(stateDir string, m *workflow.ServiceMeta, text string, number int) error {
	kept := &workflow.ChangeDescription{Text: text, Change: number}
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
// pictures attached to the change first (changeBody) — and, once HQ took it,
// forgets the words the pair kept, unless newer ones were kept meanwhile,
// which stay for the next put. Nothing is written when a picture could not
// be attached: never a description with a broken picture.
func putChangeDescription(ctx context.Context, hqc hq.Client, stateDir string, m *workflow.ServiceMeta, number int, text string) error {
	body, err := changeBody(ctx, hqc, stateDir, m, number, text)
	if err != nil {
		return err
	}
	callCtx, cancel := context.WithTimeout(ctx, hqCallTimeout)
	defer cancel()
	if _, err := hqc.EditChange(callCtx, m.HQ.Repo, number, hq.ChangeEdit{Body: &body}); err != nil {
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
	target := fmt.Sprintf("%s/%s#%d", m.HQ.AppID, m.HQ.Repo, number)
	tags := make(map[string]func(alt string) string, len(ids))
	for _, id := range ids {
		pic, png, err := workflow.KeptPicture(stateDir, id)
		if err != nil {
			return "", fmt.Errorf("%s is no longer kept, so it cannot be shown: take the screenshot again", id)
		}
		url := pic.Uploads[target]
		if url == "" {
			if len(png) > changePictureMaxBytes {
				return "", fmt.Errorf("%w: %s is larger than the 20 MiB a picture of a change may be", errCannotAttach, id)
			}
			callCtx, cancel := context.WithTimeout(ctx, hqCallTimeout)
			kept, err := hqc.Attach(callCtx, m.HQ.Repo, number, png)
			cancel()
			var refused *hq.RefusedError
			if errors.As(err, &refused) {
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

// missingPictures is the pictures text shows that this Mate does not keep.
func missingPictures(stateDir, text string) []string {
	var missing []string
	for _, id := range workflow.PictureRefs(text) {
		if _, _, err := workflow.KeptPicture(stateDir, id); err != nil {
			missing = append(missing, id)
		}
	}
	return missing
}

// putKeptChangeDescription puts what the pair keeps onto change number — the
// one a delivery or a push has just opened or found — and reports whether it
// did, or, when a picture could not go with it, why not (the words stay
// kept). Read fresh from disk: the caller's copy of the pair can predate a
// describe that landed while it worked. Words kept for another change are
// dropped: that change is gone, and this one carries another.
func putKeptChangeDescription(ctx context.Context, hqc hq.Client, stateDir string, m *workflow.ServiceMeta, number int) (described bool, note string) {
	fresh, err := workflow.FindServiceMeta(stateDir, m.Hostname)
	if err != nil || fresh == nil || fresh.HQ == nil || fresh.HQ.ChangeDescription == nil {
		return false, ""
	}
	kept := *fresh.HQ.ChangeDescription
	if kept.Change != 0 && kept.Change != number {
		forgetChangeDescription(stateDir, m, kept.Text)
		return false, ""
	}
	err = putChangeDescription(ctx, hqc, stateDir, m, number, kept.Text)
	if errors.Is(err, errCannotAttach) {
		return false, err.Error()
	}
	return err == nil, ""
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
// of its description: the person reviews the change by it, and the Mate keeps
// working on top of an open change, so it describes the change as it grows —
// or, when the words it kept went on with this call, that they did.
func describeLine(ref *changeRef, hostname string) string {
	if ref.DescriptionNote != "" {
		return fmt.Sprintf(`The description you wrote is kept, not on it yet: %s. Describe it again without that picture with zerops_workflow action="describe-change" service=%q to put the words on now.`,
			ref.DescriptionNote, hostname)
	}
	if ref.Described {
		return fmt.Sprintf(`It carries the description you wrote; rewrite it with zerops_workflow action="describe-change" service=%q description="…" whenever the change grows.`,
			hostname)
	}
	return fmt.Sprintf(`Describe it for the person's review with zerops_workflow action="describe-change" service=%q description="…" — what it does and why, how you checked it, and the zerops_browser screenshots that show it as ![what it shows](shot-N) — and again whenever it grows.`,
		hostname)
}

// sentenceOf makes one of the outcome lines a sentence of its own.
func sentenceOf(line string) string {
	if line == "" {
		return ""
	}
	return strings.ToUpper(line[:1]) + line[1:] + "."
}
