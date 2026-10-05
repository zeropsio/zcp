// Tests for: tools/hq_change_description.go — the pictures a description
// shows, as the store keeps them (workflow.KeepPicture), run in the HQ lab.
package tools

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/workflow"
)

// prunePictureFile takes a picture's file away the way a prune does once the
// picture is past the store's bounds — its entry stays only for its address.
func prunePictureFile(t *testing.T, stateDir, id string) {
	t.Helper()
	if err := os.Remove(filepath.Join(stateDir, "pictures", id+".png")); err != nil {
		t.Fatal(err)
	}
}

// TestDescribeChange_PicturePrunedAfterItWentOn: a change already carries
// the picture, HQ still serves it, and a push made the change a draft. The
// Mate describes it again with the same picture after its file was pruned:
// on that change the address it was attached at is enough. Another change
// needs the file, and the refusal says the picture is gone.
func TestDescribeChange_PicturePrunedAfterItWentOn(t *testing.T) {
	tests := []struct {
		name string
		// attachedTo is the change the picture went onto before its file was
		// pruned, "" for the change being described.
		attachedTo string
		wantText   []string
		wantError  bool
	}{
		{name: "the same change shows it from HQ", wantText: []string{`"described":true`}},
		{name: "another change needs the file", attachedTo: labApp + "/appdev#7", wantError: true,
			wantText: []string{"INVALID_PARAMETER", "shot-1", "no longer kept", "zerops_browser"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lab := deliveredLab(t)
			id := keptScreenshot(t, lab.stateDir, []byte("\x89PNG\r\n\x1a\n-a-before"), 1280, 720)
			words := "## Before\n\n![The old home page](" + id + ")\n"
			if tt.attachedTo == "" {
				if text, isError := lab.describeTitled("", "", words); isError {
					t.Fatalf("not described:\n%s", text)
				}
			} else if err := workflow.RecordPictureUpload(lab.stateDir, id, tt.attachedTo, "https://hq.example.invalid/att-7"); err != nil {
				t.Fatal(err)
			}
			prunePictureFile(t, lab.stateDir, id)

			text, isError := lab.describeTitled("", "", words)
			if isError != tt.wantError {
				t.Fatalf("isError = %v, want %v:\n%s", isError, tt.wantError, text)
			}
			for _, want := range tt.wantText {
				if !strings.Contains(text, want) {
					t.Errorf("the answer misses %q:\n%s", want, text)
				}
			}
			attached := len(lab.hq.attachments)
			if tt.attachedTo == "" {
				body := lab.hq.change(1).Body
				if attached != 1 || !strings.Contains(body, "/attachments/att-1") {
					t.Errorf("attached %d times, body %q: want the first attachment's address reused", attached, body)
				}
				return
			}
			if attached != 0 || lab.hq.change(1).Body != "" {
				t.Errorf("a refused description attached %d pictures or wrote %q", attached, lab.hq.change(1).Body)
			}
		})
	}
}

// TestDescribeChange_PicturePrunedWhileDescribing: the check passed, and a
// helper's screenshot pruned the picture before the words went on. The Mate
// is told plainly which picture is gone, nothing is written, and the words
// are not kept — kept, they would protect their other pictures and fail on
// every later delivery.
func TestDescribeChange_PicturePrunedWhileDescribing(t *testing.T) {
	lab := deliveredLab(t)
	kept := keptScreenshot(t, lab.stateDir, []byte("\x89PNG\r\n\x1a\n-kept"), 800, 600)
	gone := keptScreenshot(t, lab.stateDir, []byte("\x89PNG\r\n\x1a\n-pruned"), 800, 600)
	// The fake HQ's failOn sees every request: here it only prunes, at the
	// moment the describe reads the Mate's state, after its check.
	lab.hq.mu.Lock()
	lab.hq.failing, lab.hq.failOn = 1, func(r *http.Request) bool {
		if r.URL.Path == "/api/mate/self" {
			_ = os.Remove(filepath.Join(lab.stateDir, "pictures", gone+".png"))
		}
		return false
	}
	lab.hq.mu.Unlock()

	text, isError := lab.describeTitled("", "", "![after]("+kept+")\n![before]("+gone+")")
	if !isError {
		t.Fatalf("want a refusal, got:\n%s", text)
	}
	for _, want := range []string{"INVALID_PARAMETER", gone, "no longer kept", "Nothing was written"} {
		if !strings.Contains(text, want) {
			t.Errorf("the refusal misses %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "goes onto #") {
		t.Errorf("the refusal promises the words go on later:\n%s", text)
	}
	if words := lab.meta().HQ.ChangeDescription; words != nil {
		t.Errorf("the words are kept: %+v", words)
	}
	if len(lab.hq.attachments) != 0 || lab.hq.change(1).Body != "" {
		t.Errorf("attached %d pictures, body %q: want nothing written", len(lab.hq.attachments), lab.hq.change(1).Body)
	}
}

// TestDescribeChange_KeptWordsMeetAGonePicture: words kept while HQ did not
// answer, whose picture's file is lost before the delivery that would put
// them on, are dropped with a line naming the picture — never kept to fail
// again on every delivery.
func TestDescribeChange_KeptWordsMeetAGonePicture(t *testing.T) {
	lab := deliveredLab(t)
	id := keptScreenshot(t, lab.stateDir, []byte("\x89PNG\r\n\x1a\n-a-picture"), 800, 600)
	lab.hq.setDown(true)
	if text, isError := lab.describeTitled("", "", "![The count]("+id+")"); isError || !strings.Contains(text, `"kept":true`) {
		t.Fatalf("want the words kept while HQ does not answer:\n%s", text)
	}
	lab.hq.setDown(false)
	prunePictureFile(t, lab.stateDir, id)

	delivery := lab.deliver()
	if delivery == nil || delivery.Change == nil {
		t.Fatalf("delivery: %+v", delivery)
	}
	if !strings.Contains(delivery.Line, id) || !strings.Contains(delivery.Line, "no longer kept") {
		t.Errorf("the line does not say %s is gone:\n%s", id, delivery.Line)
	}
	if words := lab.meta().HQ.ChangeDescription; words != nil {
		t.Errorf("the words are still kept: %+v", words)
	}
}
