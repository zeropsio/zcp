// Tests for: tools/hq_change_description.go — the Mate's description of its
// change, the body of the change in HQ, run against a fake HQ that serves
// real git (hq_lab_test.go).
package tools

import (
	"context"
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
	res, typed, err := handleDescribeChange(context.Background(), l.hq.srv.Client(), l.stateDir, WorkflowInput{Service: service, Description: words})
	if err != nil {
		l.t.Fatalf("handleDescribeChange: %v", err)
	}
	if typed != nil {
		l.t.Error("a handler's typed output must stay nil — machine state rides in the text")
	}
	return resultText(l.t, res), res.IsError
}

// deliveredLab is a lab whose pair delivered once: change #1 is open.
func deliveredLab(t *testing.T) *hqLab {
	t.Helper()
	lab := newHQLab(t)
	lab.wire()
	lab.write(map[string]string{"index.js": "the app\n"})
	if d := lab.deliver(); d == nil || d.Change == nil {
		t.Fatalf("delivery: %+v", d)
	}
	return lab
}

func TestDescribeChange(t *testing.T) {
	tests := []struct {
		name string
		// lab builds the state the words meet.
		lab      func(t *testing.T) *hqLab
		service  string
		wantBody string
		wantKept *workflow.ChangeDescription
		wantText []string
		// wantChange is the change the pair records after; wantLanded whether
		// a landing is.
		wantChange int
		wantLanded bool
	}{
		{
			name: "the open change takes the words at once", lab: deliveredLab,
			wantBody: describedWords, wantChange: 1,
			wantText: []string{`"described":true`, `"pullRequest":1`, "/changes/app-1/appdev/1", "whenever the change grows"},
		},
		{
			name: "the stage half names the pair too", lab: deliveredLab, service: "appstage",
			wantBody: describedWords, wantChange: 1, wantText: []string{`"described":true`},
		},
		{
			name: "no change yet: kept for the one the next delivery opens",
			lab: func(t *testing.T) *hqLab {
				t.Helper()
				lab := newHQLab(t)
				lab.wire()
				return lab
			},
			wantKept: &workflow.ChangeDescription{Text: describedWords},
			wantText: []string{`"described":false`, `"kept":true`, "next delivery", "appstage"},
		},
		{
			name: "an open change missing from the record is found and described",
			lab: func(t *testing.T) *hqLab {
				t.Helper()
				lab := deliveredLab(t)
				clearChange(lab.stateDir, lab.meta(), 1)
				return lab
			},
			wantBody: describedWords, wantChange: 1, wantText: []string{`"described":true`, `"pullRequest":1`},
		},
		{
			name: "the change merged before the words reached it",
			lab: func(t *testing.T) *hqLab {
				t.Helper()
				lab := deliveredLab(t)
				lab.hq.merge()
				return lab
			},
			wantLanded: true,
			wantText:   []string{`"described":false`, "#1 is merged", "not written", `action=\"describe-change\"`},
		},
		{
			name: "the change was closed without merging",
			lab: func(t *testing.T) *hqLab {
				t.Helper()
				lab := deliveredLab(t)
				lab.hq.close(1)
				return lab
			},
			wantText: []string{`"described":false`, "closed without merging", "not written"},
		},
		{
			name: "HQ does not answer: the words stay kept for that change",
			lab: func(t *testing.T) *hqLab {
				t.Helper()
				lab := deliveredLab(t)
				lab.hq.setDown(true)
				return lab
			},
			wantKept: &workflow.ChangeDescription{Text: describedWords, Change: 1}, wantChange: 1,
			wantText: []string{`"described":false`, `"kept":true`, "did not take", "next delivery"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lab := tt.lab(t)
			text, isError := lab.describe(tt.service, "\n"+describedWords+"\n\n")
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
			meta := lab.meta()
			if got := meta.HQ.ChangeDescription; (got == nil) != (tt.wantKept == nil) || got != nil && *got != *tt.wantKept {
				t.Errorf("kept = %+v, want %+v", got, tt.wantKept)
			}
			if meta.HQ.Change != tt.wantChange || (meta.HQ.Landed != nil) != tt.wantLanded {
				t.Errorf("the pair records change #%d, landed %+v; want #%d, landed=%v", meta.HQ.Change, meta.HQ.Landed, tt.wantChange, tt.wantLanded)
			}
			if lab.hq.change(2) != nil {
				t.Errorf("describing a change opened one; it must never open one")
			}
		})
	}
}

// TestDescribeChange_KeptWordsGoOntoTheNextChange: words kept while no change
// could carry them go onto the one the next delivery opens, and are no longer
// kept then.
func TestDescribeChange_KeptWordsGoOntoTheNextChange(t *testing.T) {
	lab := newHQLab(t)
	lab.wire()
	if text, isError := lab.describe("", describedWords); isError || !strings.Contains(text, `"kept":true`) {
		t.Fatalf("want the words kept:\n%s", text)
	}
	lab.write(map[string]string{"index.js": "the app\n"})
	delivery := lab.deliver()
	if delivery == nil || delivery.Change == nil || !delivery.Change.Described {
		t.Fatalf("the delivery must put the kept words on its change: %+v", delivery)
	}
	if change := lab.hq.change(1); change == nil || change.Body != describedWords {
		t.Errorf("change #1 = %+v, want the kept words as its body", change)
	}
	if !strings.Contains(delivery.Line, "carries the description you wrote") {
		t.Errorf("the line must say the description went on:\n%s", delivery.Line)
	}
	if kept := lab.meta().HQ.ChangeDescription; kept != nil {
		t.Errorf("the words are on the change, yet still kept: %+v", kept)
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
		description string
		wantText    []string
	}{
		{name: "a Mate not enrolled with an HQ", notEnrolled: true, pairs: []string{"appdev"}, description: describedWords,
			wantText: []string{"PREREQUISITE_MISSING", "not enrolled"}},
		{name: "no words", pairs: []string{"appdev"}, description: " \n\t ", wantText: []string{"INVALID_PARAMETER", "description"}},
		{name: "a NUL no description keeps", pairs: []string{"appdev"}, description: "words\x00more", wantText: []string{"INVALID_PARAMETER", "NUL"}},
		{name: "more than a page", pairs: []string{"appdev"}, description: strings.Repeat("a", changeDescriptionMaxRunes+1),
			wantText: []string{"INVALID_PARAMETER", "20000"}},
		{name: "a picture this Mate does not keep", pairs: []string{"appdev"}, description: "![gone](shot-7)",
			wantText: []string{"INVALID_PARAMETER", "shot-7", "zerops_browser"}},
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
					Hostname: dev, Mode: topology.PlanModeStandard, StageHostname: strings.TrimSuffix(dev, "dev") + "stage",
					BootstrapSession: "test", BootstrappedAt: "2026-10-02", GitPushState: topology.GitPushConfigured,
					HQ: &workflow.HQRepoRef{AppID: labApp, Repo: dev, Branch: "mate/" + labMate, Change: 1},
				}); err != nil {
					t.Fatal(err)
				}
			}
			if tt.plainPair != "" {
				if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
					Hostname: tt.plainPair, Mode: topology.PlanModeStandard, StageHostname: "webstage",
					BootstrapSession: "test", BootstrappedAt: "2026-10-02",
				}); err != nil {
					t.Fatal(err)
				}
			}

			res, _, err := handleDescribeChange(context.Background(), fake.srv.Client(), stateDir,
				WorkflowInput{Service: tt.service, Description: tt.description})
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
		})
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
		lab := deliveredLab(t)
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

	t.Run("a picture HQ will not keep publishes nothing and keeps the words", func(t *testing.T) {
		lab := deliveredLab(t)
		id := keptScreenshot(t, lab.stateDir, []byte("not a png at all"), 800, 600)
		words := "![The count](" + id + ")"
		text, isError := lab.describe("", words)
		for _, want := range []string{"PREREQUISITE_MISSING", "Nothing was written onto change #1", "not_png", "without that picture"} {
			if !isError || !strings.Contains(text, want) {
				t.Errorf("want a refusal saying %q, got:\n%s", want, text)
			}
		}
		if body := lab.hq.change(1).Body; body != "" {
			t.Errorf("a body went onto the change with its picture refused: %q", body)
		}
		if kept := lab.meta().HQ.ChangeDescription; kept == nil || kept.Text != words {
			t.Errorf("kept = %+v, want the words kept", kept)
		}
	})
}
