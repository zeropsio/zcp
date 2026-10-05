// Tests for: tools/hq_change.go — a bare change's title in HQ, run against
// a fake HQ that serves real git (hq_lab_test.go).
package tools

import (
	"strings"
	"testing"
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

// TestABareChangeFollowsItsWork: a change no description has reached is
// retitled by each push that moves it, as it holds more; once the Mate has
// described it, its title is the Mate's, and a push leaves it.
func TestABareChangeFollowsItsWork(t *testing.T) {
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

	if text, isError := lab.describeTitled("", "A footer on every page", describedWords); isError || !strings.Contains(text, `"described":true`) {
		t.Fatalf("describe: %s", text)
	}
	lab.write(map[string]string{"footer.js": "the footer, fixed\n"})
	lab.commit("Fix the footer's links")
	lab.deliver()
	if change := lab.hq.change(1); change == nil || change.Title != "A footer on every page" {
		t.Errorf("change #1 = %+v, want the title the Mate gave it", change)
	}
}
