// Tests for: tools/hq_change_description.go — the Mate's description of its
// change, the body of the change in HQ, run against a fake HQ that serves
// real git (hq_lab_test.go).
package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

const describedWords = "## What it does\n\nShows how many todos are still open, above the list.\n\n" +
	"## How I checked it\n\nOpened the stage and counted."

// describe is zerops_workflow action="describe-change" in the lab.
func (l *hqLab) describe(service, words string) (text string, isError bool) {
	l.t.Helper()
	return l.describeTitled(service, "", words)
}

// describeTitled is describe-change with a title for the change.
func (l *hqLab) describeTitled(service, title, words string) (text string, isError bool) {
	l.t.Helper()
	res, typed, err := handleDescribeChange(context.Background(), l.hq.srv.Client(), l.stateDir, WorkflowInput{Service: service, Title: title, Description: words})
	if err != nil {
		l.t.Fatalf("handleDescribeChange: %v", err)
	}
	if typed != nil {
		l.t.Error("a handler's typed output must stay nil — machine state rides in the text")
	}
	return resultText(l.t, res), res.IsError
}

// describeOn is describe-change naming the change by its branch, with
// describedWords.
func (l *hqLab) describeOn(branch string) (text string, isError bool) {
	l.t.Helper()
	res, typed, err := handleDescribeChange(context.Background(), l.hq.srv.Client(), l.stateDir, WorkflowInput{Branch: branch, Description: describedWords})
	if err != nil {
		l.t.Fatalf("handleDescribeChange: %v", err)
	}
	if typed != nil {
		l.t.Error("a handler's typed output must stay nil — machine state rides in the text")
	}
	return resultText(l.t, res), res.IsError
}

// openedLab is a lab whose pair's work is up as change #1, open, and in the
// ledger as a describe that found it leaves it.
func openedLab(t *testing.T) *hqLab {
	t.Helper()
	lab := newHQLab(t)
	lab.wire()
	lab.write(map[string]string{"index.js": "the app\n"})
	lab.openRecorded()
	return lab
}

func TestDescribeChange(t *testing.T) {
	tests := []struct {
		name string
		// lab builds the state the words meet.
		lab      func(t *testing.T) *hqLab
		service  string
		title    string
		wantBody string
		// wantTitle is change #1's title after, "" for the one it opened with.
		wantTitle string
		wantKept  *workflow.ChangeDescription
		wantText  []string
		// wantChange is the change the ledger keeps after, 0 for none.
		wantChange int
	}{
		{
			name: "the open change takes the words at once", lab: openedLab,
			wantBody: describedWords, wantChange: 1,
			wantText: []string{`"described":true`, `"pullRequest":1`, "/changes/app-1/appdev/1", "draft again"},
		},
		{
			name: "the stage half names the pair too", lab: openedLab, service: "appstage",
			wantBody: describedWords, wantChange: 1, wantText: []string{`"described":true`},
		},
		{
			name: "the words name this repository's change by a title of their own", lab: openedLab,
			title: "  Count the open todos above the list  ", wantTitle: "Count the open todos above the list",
			wantBody: describedWords, wantChange: 1, wantText: []string{`"described":true`},
		},
		{
			// Words describe the work a change carries; a change opened later carries
			// whatever the pair holds then, so nothing is kept for it.
			name: "no change yet: nothing to describe until one is open",
			lab: func(t *testing.T) *hqLab {
				t.Helper()
				lab := newHQLab(t)
				lab.wire()
				return lab
			},
			wantText: []string{`"described":false`, "No change of this Mate is open", "nothing to describe yet"},
		},
		{
			name: "an open change missing from the record is found and described",
			lab: func(t *testing.T) *hqLab {
				t.Helper()
				lab := openedLab(t)
				lab.forget(1)
				return lab
			},
			wantBody: describedWords, wantChange: 1, wantText: []string{`"described":true`, `"pullRequest":1`},
		},
		{
			name: "the change merged before the words reached it",
			lab: func(t *testing.T) *hqLab {
				t.Helper()
				lab := openedLab(t)
				lab.hq.merge()
				return lab
			},
			wantText: []string{`"described":false`, "#1 is merged", "not written"},
		},
		{
			name: "the change was closed without merging",
			lab: func(t *testing.T) *hqLab {
				t.Helper()
				lab := openedLab(t)
				lab.hq.close(1)
				return lab
			},
			wantText: []string{`"described":false`, "closed without merging", "not written"},
		},
		{
			name: "HQ does not answer: the words stay kept for that change",
			lab: func(t *testing.T) *hqLab {
				t.Helper()
				lab := openedLab(t)
				lab.hq.setDown(true)
				return lab
			},
			wantKept: &workflow.ChangeDescription{Text: describedWords, Change: 1}, wantChange: 1,
			wantText: []string{`"described":false`, `"kept":true`, "may not have landed", "may still be a draft"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lab := tt.lab(t)
			text, isError := lab.describeTitled(tt.service, tt.title, "\n"+describedWords+"\n\n")
			if isError {
				t.Fatalf("want an answer, got an error:\n%s", text)
			}
			for _, want := range tt.wantText {
				if !strings.Contains(text, want) {
					t.Errorf("the answer misses %q:\n%s", want, text)
				}
			}
			if change := lab.hq.change(1); change != nil && change.Body != tt.wantBody {
				t.Errorf("change #1's body = %q, want %q", change.Body, tt.wantBody)
			}
			if change := lab.hq.change(1); change != nil && tt.wantTitle != "" && change.Title != tt.wantTitle {
				t.Errorf("change #1's title = %q, want %q", change.Title, tt.wantTitle)
			}
			if strings.Contains(text, `"kept":true`) != (tt.wantKept != nil) {
				t.Errorf("the answer says kept=%v, want %v:\n%s", tt.wantKept == nil, tt.wantKept != nil, text)
			}
			ledger := lab.ledger()
			var got *workflow.ChangeDescription
			if len(ledger) == 1 {
				got = ledger[0].Description
			}
			if (got == nil) != (tt.wantKept == nil) || got != nil && *got != *tt.wantKept {
				t.Errorf("kept = %+v, want %+v", got, tt.wantKept)
			}
			switch {
			case tt.wantChange == 0 && len(ledger) != 0:
				t.Errorf("the ledger keeps %+v, want no change", ledger)
			case tt.wantChange != 0 && (len(ledger) != 1 || ledger[0].Number != tt.wantChange):
				t.Errorf("the ledger keeps %+v, want change #%d", ledger, tt.wantChange)
			}
			if lab.hq.change(2) != nil {
				t.Errorf("describing a change opened one; it must never open one")
			}
		})
	}
}

// TestDescribeChange_Refusals: every refusal is said before HQ is asked
// anything, and keeps nothing.
func TestDescribeChange_Refusals(t *testing.T) {
	tests := []struct {
		name        string
		notEnrolled bool
		pairs       []string
		plainPair   string
		service     string
		title       string
		description string
		wantText    []string
	}{
		{name: "a Mate not enrolled with an HQ", notEnrolled: true, pairs: []string{"appdev"}, description: describedWords,
			wantText: []string{"PREREQUISITE_MISSING", "not enrolled"}},
		{name: "no words", pairs: []string{"appdev"}, description: " \n\t ", wantText: []string{"INVALID_PARAMETER", "description"}},
		{name: "a NUL no description keeps", pairs: []string{"appdev"}, description: "words\x00more", wantText: []string{"INVALID_PARAMETER", "NUL"}},
		{name: "a title longer than HQ keeps", pairs: []string{"appdev"}, title: strings.Repeat("ä", changeTitleRunes+1), description: describedWords,
			wantText: []string{"INVALID_PARAMETER", "title", "120"}},
		{name: "a title of more than one line", pairs: []string{"appdev"}, title: "Count the todos\nand more", description: describedWords,
			wantText: []string{"INVALID_PARAMETER", "title", "one line"}},
		{name: "a title holding a NUL", pairs: []string{"appdev"}, title: "Count\x00", description: describedWords,
			wantText: []string{"INVALID_PARAMETER", "title", "NUL"}},
		{name: "more than a page", pairs: []string{"appdev"}, description: strings.Repeat("a", changeDescriptionMaxRunes+1),
			wantText: []string{"INVALID_PARAMETER", "20000"}},
		{name: "a picture this Mate does not keep", pairs: []string{"appdev"}, description: "![gone](shot-7)",
			wantText: []string{"INVALID_PARAMETER", "shot-7", "keeps no picture", "zerops_browser"}},
		{name: "two pairs and no service", pairs: []string{"appdev", "apidev"}, description: describedWords,
			wantText: []string{"SERVICE_REQUIRED", `service=\"apidev\"`, `service=\"appdev\"`}},
		{name: "a service that is no pair", pairs: []string{"appdev"}, service: "db", description: describedWords,
			wantText: []string{"SERVICE_NOT_FOUND", `\"db\"`}},
		{name: "a pair with no repository in HQ", pairs: []string{"appdev"}, plainPair: "webdev", service: "webdev",
			description: describedWords, wantText: []string{"PREREQUISITE_MISSING", "webdev"}},
		{name: "no pair with a repository at all", plainPair: "webdev", description: describedWords,
			wantText: []string{"PREREQUISITE_MISSING", "no change to describe"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeHQ(t)
			t.Setenv("HOME", t.TempDir())
			if !tt.notEnrolled {
				if err := hq.SaveEnrollment(hq.EnrollmentPath(), hq.Enrollment{HQ: fake.srv.URL, ProjectID: labMate, Credential: labCredential}); err != nil {
					t.Fatal(err)
				}
			}
			stateDir := t.TempDir()
			for _, dev := range tt.pairs {
				if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
					Hostname: dev, Mode: topology.PlanModeStandard, Stages: []topology.StageRef{{Hostname: strings.TrimSuffix(dev, "dev") + "stage"}},
					BootstrapSession: "test", BootstrappedAt: "2026-10-02", GitPushState: topology.GitPushConfigured,
					HQ: &workflow.HQRepoRef{AppID: labApp, Repo: dev, Change: 1},
				}); err != nil {
					t.Fatal(err)
				}
			}
			if tt.plainPair != "" {
				if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
					Hostname: tt.plainPair, Mode: topology.PlanModeStandard, Stages: []topology.StageRef{{Hostname: "webstage"}},
					BootstrapSession: "test", BootstrappedAt: "2026-10-02",
				}); err != nil {
					t.Fatal(err)
				}
			}

			res, _, err := handleDescribeChange(context.Background(), fake.srv.Client(), stateDir,
				WorkflowInput{Service: tt.service, Title: tt.title, Description: tt.description})
			if err != nil {
				t.Fatalf("handleDescribeChange: %v", err)
			}
			text := resultText(t, res)
			if !res.IsError {
				t.Fatalf("want a refusal, got:\n%s", text)
			}
			for _, want := range tt.wantText {
				if !strings.Contains(text, want) {
					t.Errorf("the refusal misses %q:\n%s", want, text)
				}
			}
			if len(fake.calls) != 0 {
				t.Errorf("a refusal asked HQ %v; it must ask nothing", fake.calls)
			}
			for _, dev := range tt.pairs {
				if m, _ := workflow.FindServiceMeta(stateDir, dev); m.HQ.ChangeDescription != nil {
					t.Errorf("%s keeps %+v after a refusal", dev, m.HQ.ChangeDescription)
				}
			}
			kept, err := workflow.ListHQChanges(stateDir)
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range kept {
				if c.Description != nil {
					t.Errorf("change #%d of %s keeps %+v after a refusal", c.Number, c.Repo, c.Description)
				}
			}
		})
	}
}

// TestDescribeChange_MissingPictureNamesWhatIsKept: a refusal for a picture
// this Mate does not keep says which it does keep — the newest by id, the
// runs of them in one — and how long one is kept, so the Mate picks a kept
// one or retakes the screenshot instead of guessing.
func TestDescribeChange_MissingPictureNamesWhatIsKept(t *testing.T) {
	tests := []struct {
		name     string
		takes    int
		named    []string // ids the description shows
		wantText []string
	}{
		{name: "a run of kept pictures", takes: 3, named: []string{"shot-2", "shot-9"},
			wantText: []string{"shot-9", "keeps shot-1 to shot-3", "7 days", "zerops_browser screenshot=true"}},
		{name: "one kept picture", takes: 1, named: []string{"shot-4"},
			wantText: []string{"shot-4", "keeps shot-1,", "7 days"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lab := openedLab(t)
			for i := range tt.takes {
				keptScreenshot(t, lab.stateDir, fmt.Appendf(nil, "\x89PNG-%d", i), 800, 600)
			}
			var words strings.Builder
			for _, id := range tt.named {
				words.WriteString("![a page](" + id + ")\n")
			}
			text, isError := lab.describe("", words.String())
			if !isError {
				t.Fatalf("want a refusal, got:\n%s", text)
			}
			for _, want := range tt.wantText {
				if !strings.Contains(text, want) {
					t.Errorf("the refusal misses %q:\n%s", want, text)
				}
			}
		})
	}
}

// TestKeptPicturesWords: the kept ids read as runs, oldest first, and a long
// list keeps its newest runs.
func TestKeptPicturesWords(t *testing.T) {
	t.Parallel()
	many := []string{}
	for i := 1; i <= 40; i += 2 {
		many = append(many, fmt.Sprintf("shot-%d", i))
	}
	tests := []struct {
		ids  []string
		want string
	}{
		{nil, ""},
		{[]string{"shot-5"}, "shot-5"},
		{[]string{"shot-4", "shot-5"}, "shot-4, shot-5"},
		{[]string{"shot-4", "shot-50", "shot-51", "shot-52", "shot-69"}, "shot-4, shot-50 to shot-52, shot-69"},
		{many, "12 older pictures, shot-25, shot-27, shot-29, shot-31, shot-33, shot-35, shot-37, shot-39"},
		{many[:9], "1 older picture, shot-3, shot-5, shot-7, shot-9, shot-11, shot-13, shot-15, shot-17"},
	}
	for _, tt := range tests {
		if got := keptPicturesWords(tt.ids); got != tt.want {
			t.Errorf("keptPicturesWords(%v) = %q, want %q", tt.ids, got, tt.want)
		}
	}
}

// keptScreenshot keeps a picture the way zerops_browser does, and returns its
// id.
func keptScreenshot(t *testing.T, stateDir string, png []byte, width, height int) string {
	t.Helper()
	pic, err := workflow.KeepPicture(stateDir, png, width, height)
	if err != nil {
		t.Fatalf("KeepPicture: %v", err)
	}
	return pic.ID
}

// TestDescribeChange_Pictures: a picture the description names goes onto the
// change as its attachment — once per change, however often the change is
// described — and into the published body as an <img> whose width and height
// say its shape at the width the review shows it, at HQ's address. A picture
// HQ will not keep publishes nothing: never a body with a broken picture.
func TestDescribeChange_Pictures(t *testing.T) {
	t.Run("attached once, published as an img with its size", func(t *testing.T) {
		lab := openedLab(t)
		id := keptScreenshot(t, lab.stateDir, []byte("\x89PNG\r\n\x1a\n-a-screenshot"), 1280, 720)
		words := "## How I checked it\n\n![The count reads \"3 open\" & more](" + id + ")\n"
		for range 2 {
			if text, isError := lab.describe("", words); isError || !strings.Contains(text, `"described":true`) {
				t.Fatalf("not described:\n%s", text)
			}
		}
		if len(lab.hq.attachments) != 1 {
			t.Fatalf("attached %d times, want once per change", len(lab.hq.attachments))
		}
		want := `<img alt="The count reads &#34;3 open&#34; &amp; more" width="720" height="405" src="` +
			lab.hq.srv.URL + `/api/apps/app-1/changes/appdev/1/attachments/att-1">`
		if body := lab.hq.change(1).Body; !strings.Contains(body, want) || strings.Contains(body, "]("+id+")") {
			t.Errorf("published body = %q, want the picture as %s", body, want)
		}
	})

	t.Run("a picture HQ will not keep publishes nothing and drops the words", func(t *testing.T) {
		lab := openedLab(t)
		id := keptScreenshot(t, lab.stateDir, []byte("not a png at all"), 800, 600)
		words := "![The count](" + id + ")"
		text, isError := lab.describe("", words)
		for _, want := range []string{"PREREQUISITE_MISSING", "Nothing was written onto change #1", "not_png", "dropped", "without that picture"} {
			if !isError || !strings.Contains(text, want) {
				t.Errorf("want a refusal saying %q, got:\n%s", want, text)
			}
		}
		if strings.Contains(text, "goes onto") {
			t.Errorf("HQ refuses that picture every time, yet the refusal promises the words go on:\n%s", text)
		}
		if body := lab.hq.change(1).Body; body != "" {
			t.Errorf("a body went onto the change with its picture refused: %q", body)
		}
		if kept := lab.kept(1); kept == nil || kept.Description != nil {
			t.Errorf("kept = %+v, want change #1 with the words dropped: they could never go on", kept)
		}
	})
}
