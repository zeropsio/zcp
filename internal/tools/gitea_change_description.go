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

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A change's description is what the person reviews a Mate's change by — in
// the app's review, right after the verdict, and on Gitea's own pull-request
// page. It travels as the body of the pull request that carries the pair's
// branch, and only the Mate can write it (zerops_workflow
// action="describe-change"): zcp opens the request with the task as its title
// and nothing more, because the task is all zcp knows. The Mate keeps working
// on top of an open change, so it rewrites the description as the change
// grows; a link to try the change can go stale, and the description is where
// what it does and how it was checked stays true.
//
// The words are kept on the pair first and put on the request second, so a
// Gitea that did not answer, or a request not open yet, loses nothing:
//
//   - a request open from the pair's branch takes them at once — the one on
//     record, or one a person opened from the app's Git tab;
//   - with none open they wait for the one the pair opens next
//     (openGiteaPairPullRequest puts them on it);
//   - words never reach a request they were not written for: when the
//     request merged or closed before they reached it, the Mate is told and
//     nothing is kept, and words kept for a request that is gone are dropped
//     rather than put on the next change's.
//
// describe-change never opens a request. After one is closed without merging
// the next change opens the next (readGiteaPairPullRequestOutcome), and
// describing is not a change.

// changeDescriptionMaxRunes bounds a description. The app reads every open
// pull request, body included, once a minute in every open tab (the project
// flow), so a description stays a page, not a log.
const changeDescriptionMaxRunes = 20000

// changeDescriptionResult is describe-change's answer.
type changeDescriptionResult struct {
	Service        string `json:"service"`
	PullRequest    int    `json:"pullRequest,omitempty"`
	PullRequestURL string `json:"pullRequestUrl,omitempty"`
	Described      bool   `json:"described"`
	Kept           bool   `json:"kept,omitempty"`
	Message        string `json:"message"`
}

// handleDescribeChange is zerops_workflow action="describe-change": the Mate's
// description of its change, set as the description of the pull request that
// carries the pair's branch, or kept until one is open.
func handleDescribeChange(
	ctx context.Context,
	httpClient ops.HTTPDoer,
	stateDir, liveEnvPath string,
	input WorkflowInput,
) (*mcp.CallToolResult, any, error) {
	wiring := ops.ReadGiteaWiring(giteaEnvLookup(liveEnvPath))
	if !wiring.Ready() {
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			"describe-change writes a pull request's description on this Mate's group's Gitea, and this container has no Gitea wiring ("+
				strings.Join(wiring.MissingKeys(), ", ")+" not set).",
			"Without a pull request there is nothing to describe: say what the change does in your answer instead.",
		), WithRecoveryStatus()), nil, nil
	}
	text := strings.TrimSpace(input.Description)
	if text == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			`describe-change needs the words: pass description="…" in markdown.`,
			"Say what the change does and why, and how you checked it.",
		), WithRecoveryStatus()), nil, nil
	}
	if n := utf8.RuneCountInString(text); n > changeDescriptionMaxRunes {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("The description is %d characters long; a change's description holds at most %d.", n, changeDescriptionMaxRunes),
			"Keep it to what the change does and why, and how you checked it — logs and long output belong in the conversation, not in the review.",
		), WithRecoveryStatus()), nil, nil
	}
	if missing := giteaMissingPictures(stateDir, text); len(missing) > 0 {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("The description shows %s, and this Mate keeps no such picture — the newest %d screenshots are kept, and every one a kept description shows.",
				strings.Join(missing, ", "), workflow.PictureKeep),
			"Take the screenshot again with zerops_browser screenshot=true and use the picture its result names, or leave the picture out.",
		), WithRecoveryStatus()), nil, nil
	}
	meta, refusal := giteaPairToDescribe(stateDir, input.Service)
	if refusal != nil {
		return refusal, nil, nil
	}

	// The request on record is only as fresh as the last pass that asked
	// Gitea about it, and words are never put on one that merged or closed
	// in between: they were written for it, and the next change is another.
	if learned := giteaLearnLanding(ctx, httpClient, stateDir, wiring, meta); learned != "" {
		return jsonResult(changeDescriptionResult{
			Service: meta.Hostname,
			Message: sentenceOf(learned) + " The description was not written: the request it was for is no longer open, " +
				`and the next change is another one. If these words are about work not delivered yet, call zerops_workflow action="describe-change" ` +
				"again — they are then kept for the pull request " + giteaNextDeliveryOf(meta) + " opens.",
		}), nil, nil
	}

	number := meta.Gitea.PullRequest
	if number == 0 {
		// Nothing on record — a person may have opened it from the app's Git
		// tab. A lookup Gitea does not answer reads as none: the words are
		// kept, and the next delivery finds the request itself.
		if found, err := ops.FindGiteaPullRequest(ctx, httpClient, wiring.GiteaURL, wiring.Token,
			meta.Gitea.FullName, "", meta.Gitea.Branch, giteaBaseOf(meta)); err == nil && found != 0 {
			recordGiteaPullRequest(stateDir, meta, found)
			number = found
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
			Message: fmt.Sprintf("No pull request is open from %s yet, so the description is kept: it goes onto the pull request %s opens.",
				meta.Gitea.Branch, giteaNextDeliveryOf(meta)),
		}), nil, nil
	}

	url := giteaPullRequestURL(wiring.GiteaURL, meta.Gitea.FullName, number)
	if err := putChangeDescription(ctx, httpClient, wiring, stateDir, meta, number, text); err != nil {
		if errors.Is(err, ops.ErrGiteaCannotAttach) {
			return convertError(platform.NewPlatformError(
				platform.ErrPrerequisiteMissing,
				fmt.Sprintf("Nothing was written onto pull request #%d: %v. The description is kept, and goes onto #%d, pictures and all, with %s once this Mate's token can attach them.",
					number, err, number, giteaNextDeliveryOf(meta)),
				"Describe the change again without the pictures to put the words on it now. This Mate's token is the broker's to deliver — never ask for one.",
			), WithRecoveryStatus()), nil, nil
		}
		if keepErr != nil {
			return convertError(fmt.Errorf("gitea did not take the description of pull request #%d (%w), and it could not be kept either: %w",
				number, err, keepErr), WithRecoveryStatus()), nil, nil
		}
		return jsonResult(changeDescriptionResult{
			Service:        meta.Hostname,
			PullRequest:    number,
			PullRequestURL: url,
			Kept:           true,
			Message: fmt.Sprintf("Gitea did not take the description of pull request #%d (%v). It is kept, and goes onto #%d with %s; call describe-change again to try now.",
				number, err, number, giteaNextDeliveryOf(meta)),
		}), nil, nil
	}
	return jsonResult(changeDescriptionResult{
		Service:        meta.Hostname,
		PullRequest:    number,
		PullRequestURL: url,
		Described:      true,
		Message: fmt.Sprintf("Pull request #%d carries this description now — the person reviews the change by it, in the app and in Gitea. Describe it again whenever the change grows.",
			number),
	}), nil, nil
}

// giteaPairToDescribe is the pair a description is for: the one service
// names, either half, or the only pair on the group's Gitea. Otherwise the
// refusal that says which pairs there are.
func giteaPairToDescribe(stateDir, service string) (*workflow.ServiceMeta, *mcp.CallToolResult) {
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
		if !giteaPairDescribable(meta) {
			return nil, convertError(platform.NewPlatformError(
				platform.ErrPrerequisiteMissing,
				fmt.Sprintf("%s has no repository on the group's Gitea yet, so it has no pull request to describe.", meta.Hostname),
				"A pair gets its repository once the Mate's Gitea variables have landed; its first stage deploy opens the request.",
			), WithRecoveryStatus())
		}
		return meta, nil
	}
	metas, _ := workflow.ListServiceMetas(stateDir)
	var wired []*workflow.ServiceMeta
	for _, m := range metas {
		if giteaPairDescribable(m) {
			wired = append(wired, m)
		}
	}
	switch len(wired) {
	case 1:
		return wired[0], nil
	case 0:
		return nil, convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			"No pair of this Mate has its repository on the group's Gitea yet, so there is no pull request to describe.",
			"A pair gets its repository once the Mate's Gitea variables have landed; its first stage deploy opens the request.",
		), WithRecoveryStatus())
	}
	sort.Slice(wired, func(i, j int) bool { return wired[i].Hostname < wired[j].Hostname })
	names := make([]string, 0, len(wired))
	for _, m := range wired {
		names = append(names, fmt.Sprintf("service=%q", m.Hostname))
	}
	return nil, convertError(platform.NewPlatformError(
		platform.ErrServiceRequired,
		"This Mate has several pairs on its group's Gitea, each with its own pull request: say which change this describes.",
		"Pass "+strings.Join(names, " or ")+".",
	), WithRecoveryStatus())
}

// giteaPairDescribable reports whether a pair has what a pull request needs:
// its repository and the Mate's branch on it.
func giteaPairDescribable(m *workflow.ServiceMeta) bool {
	return m != nil && m.Gitea != nil && m.Gitea.FullName != "" && m.Gitea.Branch != ""
}

// keepChangeDescription records the words on the pair, in memory and on disk,
// for the request they were written for (0: the one the pair opens next).
func keepChangeDescription(stateDir string, m *workflow.ServiceMeta, text string, number int) error {
	kept := &workflow.ChangeDescription{Text: text, PullRequest: number}
	if err := workflow.UpsertServiceMeta(stateDir, m.Hostname, func(meta *workflow.ServiceMeta, existed bool) error {
		if !existed || meta.Gitea == nil {
			return fmt.Errorf("%s is no longer wired to the group's Gitea", m.Hostname)
		}
		meta.Gitea.ChangeDescription = kept
		return nil
	}); err != nil {
		return fmt.Errorf("keep the description of %s's change: %w", m.Hostname, err)
	}
	m.Gitea.ChangeDescription = kept
	return nil
}

// putChangeDescription sets text as request number's description — its
// pictures attached to the request first (giteaChangeBody) — and, once Gitea
// took it, forgets the words the pair kept, unless newer ones were kept
// meanwhile, which stay for the next put. Nothing is written when a picture
// could not be attached: never a body with a broken picture.
func putChangeDescription(
	ctx context.Context,
	httpClient ops.HTTPDoer,
	wiring ops.GiteaWiring,
	stateDir string,
	m *workflow.ServiceMeta,
	number int,
	text string,
) error {
	body, err := giteaChangeBody(ctx, httpClient, wiring, stateDir, m, number, text)
	if err != nil {
		return err
	}
	if err := ops.EditGiteaPullRequestBody(ctx, httpClient, wiring.GiteaURL, wiring.Token, m.Gitea.FullName, number, body); err != nil {
		return err
	}
	forgetChangeDescription(stateDir, m, text)
	return nil
}

// giteaChangeBody is text as it is published on request number: every
// picture it shows attached to the request — once per request, the address
// Gitea gave remembered with the picture — and written as an <img> with its
// size (giteaPictureTag).
func giteaChangeBody(
	ctx context.Context,
	httpClient ops.HTTPDoer,
	wiring ops.GiteaWiring,
	stateDir string,
	m *workflow.ServiceMeta,
	number int,
	text string,
) (string, error) {
	ids := workflow.PictureRefs(text)
	if len(ids) == 0 {
		return text, nil
	}
	target := fmt.Sprintf("%s#%d", m.Gitea.FullName, number)
	tags := make(map[string]func(alt string) string, len(ids))
	for _, id := range ids {
		pic, png, err := workflow.KeptPicture(stateDir, id)
		if err != nil {
			return "", fmt.Errorf("%s is no longer kept, so it cannot be shown: take the screenshot again", id)
		}
		url := pic.Uploads[target]
		if url == "" {
			url, err = ops.AttachGiteaPicture(ctx, httpClient, wiring.GiteaURL, wiring.Token, m.Gitea.FullName, number, id+".png", png)
			if err != nil {
				return "", err
			}
			_ = workflow.RecordPictureUpload(stateDir, id, target, url)
		}
		width, height := pic.Width, pic.Height
		tags[id] = func(alt string) string { return giteaPictureTag(alt, url, width, height) }
	}
	return workflow.ReplacePictureRefs(text, func(alt, id string) string { return tags[id](alt) }), nil
}

// pictureDisplayWidth is the widest a picture is drawn in a description: about
// the width of Gitea's pull-request column. Gitea's own stylesheet caps an
// image at the column (max-width: 100%) but never sets height: auto, so a
// height attribute taller than the capped width allows stretches the picture;
// sized to the column, it is drawn true, and the app reserves the same shape.
const pictureDisplayWidth = 720

// giteaPictureTag is a picture as a published description carries it: an
// <img> whose width and height give its shape at the width it is drawn, so
// whoever reads the description reserves its box before the bytes arrive.
// The picture itself keeps every pixel.
func giteaPictureTag(alt, url string, width, height int) string {
	if width <= 0 || height <= 0 {
		return fmt.Sprintf(`<img alt="%s" src="%s">`, html.EscapeString(alt), html.EscapeString(url))
	}
	if width > pictureDisplayWidth {
		height = int(math.Round(float64(height) * pictureDisplayWidth / float64(width)))
		width = pictureDisplayWidth
	}
	return fmt.Sprintf(`<img alt="%s" width="%d" height="%d" src="%s">`, html.EscapeString(alt), width, height, html.EscapeString(url))
}

// giteaMissingPictures is the pictures text shows that this Mate does not
// keep.
func giteaMissingPictures(stateDir, text string) []string {
	var missing []string
	for _, id := range workflow.PictureRefs(text) {
		if _, _, err := workflow.KeptPicture(stateDir, id); err != nil {
			missing = append(missing, id)
		}
	}
	return missing
}

// putKeptChangeDescription puts what the pair keeps onto request number — the
// one a delivery, a push or a pass has just opened or found — and reports
// whether it did, or, when a picture could not go with it, why not (the words
// stay kept). Read fresh from disk: the caller's copy of the pair can predate
// a describe that landed while it worked. Words kept for another request are
// dropped: that request is gone, and this one carries another change.
func putKeptChangeDescription(
	ctx context.Context,
	httpClient ops.HTTPDoer,
	wiring ops.GiteaWiring,
	stateDir string,
	m *workflow.ServiceMeta,
	number int,
) (described bool, note string) {
	fresh, err := workflow.FindServiceMeta(stateDir, m.Hostname)
	if err != nil || fresh == nil || fresh.Gitea == nil || fresh.Gitea.ChangeDescription == nil {
		return false, ""
	}
	kept := *fresh.Gitea.ChangeDescription
	if kept.PullRequest != 0 && kept.PullRequest != number {
		forgetChangeDescription(stateDir, m, kept.Text)
		return false, ""
	}
	err = putChangeDescription(ctx, httpClient, wiring, stateDir, m, number, kept.Text)
	if errors.Is(err, ops.ErrGiteaCannotAttach) {
		return false, err.Error()
	}
	return err == nil, ""
}

// forgetChangeDescription drops the words the pair keeps, in memory and on
// disk — only while they are still text: a describe that kept newer words
// after these were read keeps them.
func forgetChangeDescription(stateDir string, m *workflow.ServiceMeta, text string) {
	if kept := m.Gitea.ChangeDescription; kept != nil && kept.Text == text {
		m.Gitea.ChangeDescription = nil
	}
	_ = workflow.UpsertServiceMeta(stateDir, m.Hostname, func(meta *workflow.ServiceMeta, existed bool) error {
		if !existed || meta.Gitea == nil || meta.Gitea.ChangeDescription == nil || meta.Gitea.ChangeDescription.Text != text {
			return workflow.ErrSkipWrite
		}
		meta.Gitea.ChangeDescription = nil
		return nil
	})
}

// giteaNextDeliveryOf names when a pair's next pull request opens: its next
// stage deploy, which delivers.
func giteaNextDeliveryOf(m *workflow.ServiceMeta) string {
	if m.StageHostname != "" {
		return fmt.Sprintf("%s's next delivery (a deploy of %s)", m.Hostname, m.StageHostname)
	}
	return m.Hostname + "'s next delivery"
}

// giteaDescribeLine is what a push or a delivery that leaves a request open
// says of its description: the person reviews the change by it, and the Mate
// keeps working on top of an open change, so it describes the change as it
// grows — or, when the words it kept went on with this call, that they did.
func giteaDescribeLine(pr *giteaPullRequestRef, hostname string) string {
	if pr.DescriptionNote != "" {
		return fmt.Sprintf(`The description you wrote is kept, not on it yet: %s. It goes on with the next delivery once the token can attach its pictures — or describe it again without the pictures with zerops_workflow action="describe-change" service=%q to put the words on now.`,
			pr.DescriptionNote, hostname)
	}
	if pr.Described {
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
