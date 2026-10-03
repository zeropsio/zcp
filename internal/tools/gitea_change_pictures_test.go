// Tests for: the pictures of a change's description — a screenshot
// zerops_browser kept, named in the description as ![what it shows](shot-N),
// attached to the pull request once and published as an <img> with its size.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/workflow"
)

// keptScreenshot keeps a picture the way zerops_browser does, and returns its
// id and bytes.
func keptScreenshot(t *testing.T, stateDir string, width, height int) (string, []byte) {
	t.Helper()
	png := []byte("\x89PNG\r\n\x1a\n-a-screenshot-" + stateDir)
	pic, err := workflow.KeepPicture(stateDir, png, width, height)
	if err != nil {
		t.Fatalf("KeepPicture: %v", err)
	}
	return pic.ID, png
}

// TestDescribeChange_Pictures: a picture the description names goes onto the
// request as its attachment — once per request, however often the change is
// described — and into the published body as an <img> whose width and height
// say its shape at the width a description column shows it, so a reader
// reserves its box before the bytes arrive and Gitea's own page never
// stretches it. A picture the Mate does not keep is refused before anything
// is written, and a token that cannot attach one publishes nothing: never a
// body with a broken picture.
func TestDescribeChange_Pictures(t *testing.T) {
	t.Run("attached once, published as an img with its size", func(t *testing.T) {
		fake := newFakeGitea()
		gitea := fake.start(t)
		stateDir := t.TempDir()
		writeDescribablePair(t, stateDir, "appdev", "appstage", 9)
		id, png := keptScreenshot(t, stateDir, 1280, 720)
		env := giteaWiredEnvFile(t, gitea.URL)
		words := "## How I checked it\n\n![The count reads \"3 open\" & more](" + id + ")\n"

		for range 2 {
			res, _, err := handleDescribeChange(context.Background(), gitea.Client(), stateDir, env, WorkflowInput{Description: words})
			if err != nil || res.IsError {
				t.Fatalf("describe: %v %s", err, resultText(t, res))
			}
			if text := resultText(t, res); !strings.Contains(text, `"described":true`) {
				t.Fatalf("not described:\n%s", text)
			}
		}
		if len(fake.attachments) != 1 {
			t.Fatalf("attached %d times, want once per request", len(fake.attachments))
		}
		att := fake.attachments[0]
		if att.path != "/api/v1/repos/acme/appdev/issues/9/assets" || att.name != id+".png" || !bytes.Equal(att.content, png) {
			t.Errorf("attachment = %s %s (%d bytes), want the picture on #9", att.path, att.name, len(att.content))
		}
		want := `<img alt="The count reads &#34;3 open&#34; &amp; more" width="720" height="405" src="https://gitea.example.invalid/attachments/00000000-0000-4000-8000-000000000001">`
		for _, body := range fake.pullBodies {
			if !strings.Contains(body, want) || strings.Contains(body, "]("+id+")") {
				t.Errorf("published body = %q, want the picture as %s", body, want)
			}
		}
		if got := keptDescription(t, stateDir, "appdev"); got != nil {
			t.Errorf("the words are on the request, yet still kept: %+v", got)
		}
	})

	t.Run("a picture this Mate does not keep is refused before anything", func(t *testing.T) {
		fake := newFakeGitea()
		gitea := fake.start(t)
		stateDir := t.TempDir()
		writeDescribablePair(t, stateDir, "appdev", "appstage", 9)
		res, _, err := handleDescribeChange(context.Background(), gitea.Client(), stateDir, giteaWiredEnvFile(t, gitea.URL),
			WorkflowInput{Description: "![gone](shot-7)"})
		if err != nil {
			t.Fatal(err)
		}
		text := resultText(t, res)
		for _, want := range []string{"INVALID_PARAMETER", "shot-7", "zerops_browser"} {
			if !res.IsError || !strings.Contains(text, want) {
				t.Errorf("want a refusal naming %q, got:\n%s", want, text)
			}
		}
		if fake.requests != 0 || keptDescription(t, stateDir, "appdev") != nil {
			t.Errorf("a refusal asked Gitea %d times or kept words", fake.requests)
		}
	})

	t.Run("a token that cannot attach publishes nothing and keeps the words", func(t *testing.T) {
		fake := newFakeGitea()
		fake.attachStatus = http.StatusForbidden
		gitea := fake.start(t)
		stateDir := t.TempDir()
		writeDescribablePair(t, stateDir, "appdev", "appstage", 9)
		id, _ := keptScreenshot(t, stateDir, 800, 600)
		words := "![The count](" + id + ")"
		res, _, err := handleDescribeChange(context.Background(), gitea.Client(), stateDir, giteaWiredEnvFile(t, gitea.URL),
			WorkflowInput{Description: words})
		if err != nil {
			t.Fatal(err)
		}
		text := resultText(t, res)
		for _, want := range []string{"PREREQUISITE_MISSING", "cannot attach pictures yet", "required=[write:issue]", "kept", "without the pictures"} {
			if !res.IsError || !strings.Contains(text, want) {
				t.Errorf("want a refusal saying %q, got:\n%s", want, text)
			}
		}
		if len(fake.pullBodies) != 0 {
			t.Errorf("a body went onto the request with its picture refused: %q", fake.pullBodies)
		}
		if got := keptDescription(t, stateDir, "appdev"); got == nil || got.Text != words {
			t.Errorf("kept = %+v, want the words kept for when pictures can be attached", got)
		}
	})
}

// TestOpenGiteaPairPullRequest_PutsTheKeptPictures: words kept with a picture
// go onto the request a delivery opens with the picture attached to it; while
// the token cannot attach, they stay kept and the reference says why.
func TestOpenGiteaPairPullRequest_PutsTheKeptPictures(t *testing.T) {
	for _, tc := range []struct {
		name          string
		attachStatus  int
		wantDescribed bool
		wantNote      string
	}{
		{name: "attached and put", wantDescribed: true},
		{name: "the token cannot attach yet", attachStatus: http.StatusForbidden, wantNote: "cannot attach pictures yet"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeGitea()
			fake.attachStatus = tc.attachStatus
			gitea := fake.start(t)
			stateDir := t.TempDir()
			writeDescribablePair(t, stateDir, "appdev", "appstage", 0)
			id, _ := keptScreenshot(t, stateDir, 640, 480)
			keepForTest(t, stateDir, &workflow.ChangeDescription{Text: "![The list](" + id + ")"})
			meta, _ := workflow.FindServiceMeta(stateDir, "appdev")

			ref, _ := openGiteaPairPullRequest(context.Background(), gitea.Client(),
				ops.GiteaWiring{GiteaURL: gitea.URL, BrokerURL: gitea.URL, Token: giteaBotToken}, stateDir, meta)
			if ref == nil || ref.Number != 3 {
				t.Fatalf("ref = %+v, want #3", ref)
			}
			if ref.Described != tc.wantDescribed || !strings.Contains(ref.DescriptionNote, tc.wantNote) {
				t.Errorf("described=%v note=%q, want %v and a note saying %q", ref.Described, ref.DescriptionNote, tc.wantDescribed, tc.wantNote)
			}
			if tc.wantDescribed {
				if !slices.ContainsFunc(fake.pullBodies, func(b string) bool {
					return strings.Contains(b, `<img alt="The list" width="640" height="480" src="https://gitea.example.invalid/attachments/`)
				}) {
					t.Errorf("published bodies %q, want the picture as an img at its own size", fake.pullBodies)
				}
				return
			}
			if len(fake.pullBodies) != 0 || keptDescription(t, stateDir, "appdev") == nil {
				t.Errorf("bodies %q kept %+v: nothing goes on while the picture cannot", fake.pullBodies, keptDescription(t, stateDir, "appdev"))
			}
			if line := giteaDescribeLine(ref, "appdev"); !strings.Contains(line, "cannot attach pictures yet") || !strings.Contains(line, "without the pictures") {
				t.Errorf("the line must say why the kept words are not on it:\n%s", line)
			}
		})
	}
}

// TestKeepBrowserPicture: a screenshot zerops_browser takes is kept, and its
// result names it — the only way the Mate can put it in a description.
func TestKeepBrowserPicture(t *testing.T) {
	stateDir := t.TempDir()
	result := &ops.BrowserBatchResult{
		URL:        "https://appstage.example.invalid",
		Screenshot: &ops.BrowserScreenshotResult{PNG: []byte("\x89PNG-browser"), Width: 1280, Height: 720},
	}
	keepBrowserPicture(stateDir, result)
	if result.Screenshot.Picture != "shot-1" {
		t.Fatalf("picture = %q, want shot-1", result.Screenshot.Picture)
	}
	var shown struct {
		Screenshot struct {
			Picture string `json:"picture"`
		} `json:"screenshot"`
	}
	if err := json.Unmarshal([]byte(resultText(t, browserToolResult(result))), &shown); err != nil || shown.Screenshot.Picture != "shot-1" {
		t.Errorf("the result must name the picture: %+v (%v)", shown, err)
	}
	if _, png, err := workflow.KeptPicture(stateDir, "shot-1"); err != nil || !bytes.Equal(png, []byte("\x89PNG-browser")) {
		t.Errorf("the picture is not kept: %v", err)
	}

	for _, none := range []*ops.BrowserBatchResult{
		{URL: "https://appstage.example.invalid"},
		{URL: "https://appstage.example.invalid", Screenshot: &ops.BrowserScreenshotResult{}},
	} {
		keepBrowserPicture(stateDir, none)
	}
	keepBrowserPicture("", result)
	if _, _, err := workflow.KeptPicture(stateDir, "shot-2"); err == nil {
		t.Error("a result with no picture kept one")
	}
}
