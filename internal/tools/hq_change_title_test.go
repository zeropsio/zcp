// Tests for: tools/hq_change.go — a bare change's title in HQ, run against
// a fake HQ that serves real git (hq_lab_test.go).
package tools

import (
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/workflow"
)

// sessionTask is a task that names more than one repository: the title of
// neither change.
const sessionTask = "Build the whole shop: a backend with the product API and a storefront that lists the products"

// TestABareChangeIsTitledByWhatItHolds (R12-15): two repositories' changes
// read the same long task for minutes until the Mate described them. A change
// opens titled by what its own repository holds.
func TestABareChangeIsTitledByWhatItHolds(t *testing.T) {
	tests := []struct {
		name string
		// work is what the pair holds when its stage is deployed.
		work func(lab *hqLab)
		want string
	}{
		{
			name: "only the deployed tree: the files it holds",
			work: func(lab *hqLab) { lab.write(map[string]string{"index.js": "the app\n"}) },
			want: "Add index.js",
		},
		{
			name: "the Mate's own commit",
			work: func(lab *hqLab) {
				lab.write(map[string]string{"api.js": "the api\n"})
				lab.commit("Add the product API")
				lab.write(map[string]string{"README.md": "notes\n"})
			},
			want: "Add the product API",
		},
		{
			// R12-15 again: the briefing asks every repository for a
			// baseline commit first.
			name: "the baseline commit the briefing asks for",
			work: func(lab *hqLab) {
				lab.write(map[string]string{"index.js": "the app\n"})
				lab.git("add", "-A")
				lab.git("-c", "user.name=mate", "-c", "user.email=mate@example.invalid", "commit", "-q", "-m", "baseline commit", "-m", "Zcp-Commit: baseline")
				lab.write(map[string]string{"list.js": "the list\n"})
			},
			want: "Add index.js and list.js",
		},
		{
			name: "a baseline commit made before the briefing marked it",
			work: func(lab *hqLab) {
				lab.write(map[string]string{"index.js": "the app\n"})
				lab.commit("baseline commit")
			},
			want: "Add index.js",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lab := newHQLab(t)
			lab.wire()
			startSession(t, lab.stateDir, sessionTask)
			tt.work(lab)
			if d := lab.deliver(); d == nil || d.Change == nil {
				t.Fatalf("delivery: %+v", d)
			}
			if change := lab.hq.change(1); change == nil || change.Title != tt.want {
				t.Errorf("change #1 = %+v, want it titled %q", change, tt.want)
			}
		})
	}
}

// TestABareChangeFollowsItsWork: while a change's title is the one zcp gave
// it, each push that finds the change holding something new retitles it —
// a description without a title leaves the title zcp's, as it is what a
// squash lands on main under. Once the Mate names the change, its title is
// the Mate's, and a push leaves it.
func TestABareChangeFollowsItsWork(t *testing.T) {
	tests := []struct {
		name string
		// title is the title the describe between the pushes gives, "" for
		// none.
		title string
		want  string
	}{
		{name: "described without a title: the title stays zcp's and follows the work", want: "Fix the footer's links"},
		{name: "named by the Mate: the title is the Mate's", title: "A footer on every page", want: "A footer on every page"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lab := newHQLab(t)
			lab.wire()
			startSession(t, lab.stateDir, sessionTask)
			lab.write(map[string]string{"index.js": "the app\n"})
			lab.deliver()

			lab.write(map[string]string{"footer.js": "the footer\n"})
			lab.commit("Add a footer")
			lab.deliver()
			if change := lab.hq.change(1); change == nil || change.Title != "Add a footer" {
				t.Fatalf("change #1 = %+v, want it retitled by the Mate's commit", change)
			}

			if text, isError := lab.describeTitled("", tt.title, describedWords); isError || !strings.Contains(text, `"described":true`) {
				t.Fatalf("describe: %s", text)
			}
			lab.write(map[string]string{"footer.js": "the footer, fixed\n"})
			lab.commit("Fix the footer's links")
			lab.deliver()
			if change := lab.hq.change(1); change == nil || change.Title != tt.want {
				t.Errorf("change #1 = %+v, want it titled %q", change, tt.want)
			}
		})
	}
}

// TestDescribeChange_WaitsForTheDeliveryOnItsPair: a describe from one chat
// and a delivery from another never interleave — a delivery reads whether
// the change's title is still zcp's and then retitles it, and a describe
// landing in between would be written over. A describe holds the pair as a
// delivery does, waits for one in flight, and writes nothing when the pair
// stays held.
func TestDescribeChange_WaitsForTheDeliveryOnItsPair(t *testing.T) {
	lab := deliveredLab(t)
	prev := hqPairLockWait
	hqPairLockWait = 50 * time.Millisecond
	t.Cleanup(func() { hqPairLockWait = prev })
	release, err := workflow.LockPair(t.Context(), lab.stateDir, "appdev", 0)
	if err != nil {
		t.Fatal(err)
	}
	text, _ := lab.describeTitled("", "A footer on every page", describedWords)
	for _, want := range []string{"Nothing was written", "held its checkout", "describe-change"} {
		if !strings.Contains(text, want) {
			t.Errorf("the answer misses %q:\n%s", want, text)
		}
	}
	if change := lab.hq.change(1); change.Body != "" || change.Title == "A footer on every page" {
		t.Errorf("a describe wrote onto change #1 while its pair was held: %+v", change)
	}
	release()
	if text, isError := lab.describeTitled("", "A footer on every page", describedWords); isError || !strings.Contains(text, `"described":true`) {
		t.Errorf("once the pair is let go, the describe lands:\n%s", text)
	}
}
