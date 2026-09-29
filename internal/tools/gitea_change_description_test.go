// Tests for: tools/gitea_change_description.go — the description a Mate
// writes for its change. The person reviews a change by it, in the app and in
// Gitea, so it travels as the change's pull request's body: onto the request
// open from the pair's branch, or kept until one opens, and never onto a
// request it was not written for.
package tools

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

const describedWords = "## What it does\n\nShows how many todos are still open, above the list.\n\n" +
	"## How I checked it\n\n- the count reads \"3 open\" on appstage, and \"0 open\" once all are done"

// openPullRequestFromTheMatesBranch is Gitea's open list holding the pair's
// own request, #9 — the shape a request takes when a person opened it from
// the app's Git tab, where nothing recorded it on the pair.
const openPullRequestFromTheMatesBranch = `[{"number":9,"state":"open","head":{"ref":"mate/mate-p1",` +
	`"repo":{"full_name":"acme/appdev"}},"base":{"ref":"main"}}]`

// writeDescribablePair seeds a pair wired to the group's Gitea, with the
// pull request it has on record (0 for none).
func writeDescribablePair(t *testing.T, stateDir, dev, stage string, pullRequest int) {
	t.Helper()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         dev,
		Mode:             topology.PlanModeStandard,
		StageHostname:    stage,
		BootstrapSession: "test",
		BootstrappedAt:   "2026-09-29",
		GitPushState:     topology.GitPushConfigured,
		Gitea: &workflow.GiteaRepoRef{
			FullName:      "acme/" + dev,
			Branch:        "mate/mate-p1",
			DefaultBranch: "main",
			PullRequest:   pullRequest,
		},
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
}

// giteaWiredEnvFile is the live env store of a Mate whose three Gitea
// variables have landed.
func giteaWiredEnvFile(t *testing.T, giteaURL string) string {
	t.Helper()
	return writeLiveEnvFile(t, map[string]string{
		"GITEA_URL": giteaURL, "MATE_BROKER_URL": giteaURL, "GITEA_TOKEN": giteaBotToken,
	})
}

// keptDescription reads what the pair keeps on disk.
func keptDescription(t *testing.T, stateDir, hostname string) *workflow.ChangeDescription {
	t.Helper()
	meta, err := workflow.FindServiceMeta(stateDir, hostname)
	if err != nil || meta == nil || meta.Gitea == nil {
		t.Fatalf("the pair's meta is gone: %v", err)
	}
	return meta.Gitea.ChangeDescription
}

// TestDescribeChange is the action's table: the words reach the request open
// from the pair's branch at once; with none open they are kept for the one the
// next delivery opens; and a request that merged or closed before they
// reached it is told, never written over and never inherited by the next.
func TestDescribeChange(t *testing.T) {
	tests := []struct {
		name     string
		recorded int
		// pullState and pullMerged are what Gitea says of the recorded
		// request; "" is open.
		pullState  string
		pullMerged bool
		pullsOpen  string
		editStatus int
		service    string

		wantPaths    []string
		wantKept     *workflow.ChangeDescription
		wantRecorded int
		wantLanded   bool
		wantText     []string
	}{
		{
			name:         "an open request on record carries the words at once",
			recorded:     9,
			wantPaths:    []string{"/api/v1/repos/acme/appdev/pulls/9"},
			wantRecorded: 9,
			wantText:     []string{`"described":true`, `"pullRequest":9`, "/acme/appdev/pulls/9", "whenever the change grows"},
		},
		{
			name:         "the stage half names the pair too",
			recorded:     9,
			service:      "appstage",
			wantPaths:    []string{"/api/v1/repos/acme/appdev/pulls/9"},
			wantRecorded: 9,
			wantText:     []string{`"described":true`},
		},
		{
			name:     "no request yet: kept for the one the next delivery opens",
			wantKept: &workflow.ChangeDescription{Text: describedWords},
			wantText: []string{`"described":false`, `"kept":true`, "next delivery", "appstage"},
		},
		{
			name:         "a request a person opened is found, recorded and described",
			pullsOpen:    openPullRequestFromTheMatesBranch,
			wantPaths:    []string{"/api/v1/repos/acme/appdev/pulls/9"},
			wantRecorded: 9,
			wantText:     []string{`"described":true`, `"pullRequest":9`},
		},
		{
			name: "another Mate's open request is never described",
			pullsOpen: `[{"number":4,"state":"open","head":{"ref":"mate/other",` +
				`"repo":{"full_name":"acme/appdev"}},"base":{"ref":"main"}}]`,
			wantKept: &workflow.ChangeDescription{Text: describedWords},
			wantText: []string{`"kept":true`},
		},
		{
			name:       "the request on record merged before the words reached it",
			recorded:   9,
			pullState:  "closed",
			pullMerged: true,
			wantLanded: true,
			wantText:   []string{`"described":false`, "#9 is merged", "not written", `action=\"describe-change\"`},
		},
		{
			name:      "the request on record was closed without merging",
			recorded:  9,
			pullState: "closed",
			wantText:  []string{`"described":false`, "closed without merging", "not written"},
		},
		{
			name:         "Gitea refuses the edit: the words stay kept for that request",
			recorded:     9,
			editStatus:   500,
			wantKept:     &workflow.ChangeDescription{Text: describedWords, PullRequest: 9},
			wantRecorded: 9,
			wantText:     []string{`"described":false`, `"kept":true`, "status 500", "next delivery"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeGitea()
			fake.pullState = tt.pullState
			fake.pullMerged = tt.pullMerged
			if tt.pullMerged {
				fake.pullMergeCommit, fake.pullMergeHead = "squash-sha", "branch-tip-sha"
			}
			if tt.pullsOpen != "" {
				fake.pullsOpen = tt.pullsOpen
			}
			fake.pullEditStatus = tt.editStatus
			gitea := fake.start(t)
			stateDir := t.TempDir()
			writeDescribablePair(t, stateDir, "appdev", "appstage", tt.recorded)

			res, typed, err := handleDescribeChange(context.Background(), gitea.Client(), stateDir,
				giteaWiredEnvFile(t, gitea.URL), WorkflowInput{Service: tt.service, Description: "\n" + describedWords + "\n\n"})
			if err != nil {
				t.Fatalf("handleDescribeChange: %v", err)
			}
			if typed != nil {
				t.Error("a handler's typed output must stay nil — machine state rides in the text")
			}
			text := resultText(t, res)
			if res.IsError {
				t.Fatalf("want an answer, got an error:\n%s", text)
			}
			for _, want := range tt.wantText {
				if !strings.Contains(text, want) {
					t.Errorf("the answer misses %q:\n%s", want, text)
				}
			}
			if !slices.Equal(fake.pullBodyPaths, tt.wantPaths) {
				t.Errorf("descriptions went to %v, want %v", fake.pullBodyPaths, tt.wantPaths)
			}
			for _, body := range fake.pullBodies {
				if body != describedWords {
					t.Errorf("Gitea was given %q, want the words trimmed and otherwise as written", body)
				}
			}
			if got := keptDescription(t, stateDir, "appdev"); !equalChangeDescription(got, tt.wantKept) {
				t.Errorf("kept = %+v, want %+v", got, tt.wantKept)
			}
			meta, _ := workflow.FindServiceMeta(stateDir, "appdev")
			if meta.Gitea.PullRequest != tt.wantRecorded {
				t.Errorf("recorded pull request = %d, want %d", meta.Gitea.PullRequest, tt.wantRecorded)
			}
			if (meta.Gitea.Landed != nil) != tt.wantLanded {
				t.Errorf("landing recorded = %+v, want recorded=%v", meta.Gitea.Landed, tt.wantLanded)
			}
			if fake.pullCreates != 0 {
				t.Errorf("describing a change opened %d pull requests; it must never open one", fake.pullCreates)
			}
		})
	}
}

// TestDescribeChange_Refusals: every refusal is said before Gitea is asked
// anything, and keeps nothing.
func TestDescribeChange_Refusals(t *testing.T) {
	tests := []struct {
		name        string
		unwired     bool
		pairs       []string
		plainPair   string
		service     string
		description string
		wantText    []string
	}{
		{
			name: "a Mate with no Gitea", unwired: true, pairs: []string{"appdev"}, description: describedWords,
			wantText: []string{"PREREQUISITE_MISSING", "group's Gitea"},
		},
		{
			name: "no words", pairs: []string{"appdev"}, description: " \n\t ",
			wantText: []string{"INVALID_PARAMETER", "description"},
		},
		{
			name: "more than a page", pairs: []string{"appdev"}, description: strings.Repeat("a", changeDescriptionMaxRunes+1),
			wantText: []string{"INVALID_PARAMETER", "20000"},
		},
		{
			name: "two pairs and no service", pairs: []string{"appdev", "apidev"}, description: describedWords,
			wantText: []string{"SERVICE_REQUIRED", `service=\"apidev\"`, `service=\"appdev\"`},
		},
		{
			name: "a service that is no pair", pairs: []string{"appdev"}, service: "db", description: describedWords,
			wantText: []string{"SERVICE_NOT_FOUND", `\"db\"`},
		},
		{
			name: "a pair with no Gitea repository", pairs: []string{"appdev"}, plainPair: "webdev", service: "webdev",
			description: describedWords,
			wantText:    []string{"PREREQUISITE_MISSING", "webdev"},
		},
		{
			name: "no pair wired at all", plainPair: "webdev", description: describedWords,
			wantText: []string{"PREREQUISITE_MISSING", "no pull request to describe"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeGitea()
			gitea := fake.start(t)
			stateDir := t.TempDir()
			for _, dev := range tt.pairs {
				writeDescribablePair(t, stateDir, dev, strings.TrimSuffix(dev, "dev")+"stage", 9)
			}
			if tt.plainPair != "" {
				if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
					Hostname: tt.plainPair, Mode: topology.PlanModeStandard, StageHostname: "webstage",
					BootstrapSession: "test", BootstrappedAt: "2026-09-29",
				}); err != nil {
					t.Fatalf("WriteServiceMeta: %v", err)
				}
			}
			env := giteaWiredEnvFile(t, gitea.URL)
			if tt.unwired {
				env = writeLiveEnvFile(t, map[string]string{})
				t.Setenv("GITEA_URL", "")
				t.Setenv("MATE_BROKER_URL", "")
				t.Setenv("GITEA_TOKEN", "")
			}

			res, _, err := handleDescribeChange(context.Background(), gitea.Client(), stateDir, env,
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
			if fake.requests != 0 {
				t.Errorf("a refusal asked Gitea %d times; it must ask nothing", fake.requests)
			}
			for _, dev := range tt.pairs {
				if got := keptDescription(t, stateDir, dev); got != nil {
					t.Errorf("%s keeps %+v after a refusal", dev, got)
				}
			}
		})
	}
}

// TestOpenGiteaPairPullRequest_PutsTheKeptDescription: words kept while no
// request could carry them go onto the one a delivery, a push or a reconcile
// pass opens or finds — read fresh from disk, never from the caller's copy
// of the pair — unless they were written for a request that is gone.
func TestOpenGiteaPairPullRequest_PutsTheKeptDescription(t *testing.T) {
	tests := []struct {
		name       string
		kept       *workflow.ChangeDescription
		pullsOpen  string
		editStatus int
		// newerDuringEdit is kept by a concurrent describe while the edit is
		// on its way.
		newerDuringEdit string

		wantNumber    int
		wantDescribed bool
		wantPaths     []string
		wantKept      *workflow.ChangeDescription
	}{
		{
			name:          "kept for the next request: it goes onto the one opened",
			kept:          &workflow.ChangeDescription{Text: describedWords},
			wantNumber:    3,
			wantDescribed: true,
			wantPaths:     []string{"/api/v1/repos/acme/appdev/pulls/3"},
		},
		{
			name:          "kept for the request found open: it goes onto it",
			kept:          &workflow.ChangeDescription{Text: describedWords, PullRequest: 9},
			pullsOpen:     openPullRequestFromTheMatesBranch,
			wantNumber:    9,
			wantDescribed: true,
			wantPaths:     []string{"/api/v1/repos/acme/appdev/pulls/9"},
		},
		{
			name:       "kept for a request that is gone: dropped, never put on the next",
			kept:       &workflow.ChangeDescription{Text: describedWords, PullRequest: 9},
			wantNumber: 3,
		},
		{
			name:       "nothing kept, nothing written",
			wantNumber: 3,
		},
		{
			name:       "Gitea refuses: the words stay kept",
			kept:       &workflow.ChangeDescription{Text: describedWords},
			editStatus: 500,
			wantNumber: 3,
			wantKept:   &workflow.ChangeDescription{Text: describedWords},
		},
		{
			name:            "newer words kept while the edit was on its way stay kept",
			kept:            &workflow.ChangeDescription{Text: describedWords},
			newerDuringEdit: "## What it does\n\nAlso sorts them by due date.",
			wantNumber:      3,
			wantDescribed:   true,
			wantPaths:       []string{"/api/v1/repos/acme/appdev/pulls/3"},
			wantKept:        &workflow.ChangeDescription{Text: "## What it does\n\nAlso sorts them by due date."},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeGitea()
			if tt.pullsOpen != "" {
				fake.pullsOpen = tt.pullsOpen
			}
			fake.pullEditStatus = tt.editStatus
			gitea := fake.start(t)
			stateDir := t.TempDir()
			writeDescribablePair(t, stateDir, "appdev", "appstage", 0)
			if tt.newerDuringEdit != "" {
				fake.onPullEdit = func() { keepForTest(t, stateDir, &workflow.ChangeDescription{Text: tt.newerDuringEdit}) }
			}

			// The caller's copy of the pair predates the kept words: the
			// delivery read it before a describe landed.
			meta, _ := workflow.FindServiceMeta(stateDir, "appdev")
			if tt.kept != nil {
				keepForTest(t, stateDir, tt.kept)
			}
			wiring := ops.GiteaWiring{GiteaURL: gitea.URL, BrokerURL: gitea.URL, Token: giteaBotToken}
			ref := openGiteaPairPullRequest(context.Background(), gitea.Client(), wiring, stateDir, meta)
			if ref == nil {
				t.Fatal("want a pull request")
			}
			if ref.Number != tt.wantNumber || ref.Described != tt.wantDescribed {
				t.Errorf("ref = #%d described=%v, want #%d described=%v", ref.Number, ref.Described, tt.wantNumber, tt.wantDescribed)
			}
			if !slices.Equal(fake.pullBodyPaths, tt.wantPaths) {
				t.Errorf("descriptions went to %v, want %v", fake.pullBodyPaths, tt.wantPaths)
			}
			if got := keptDescription(t, stateDir, "appdev"); !equalChangeDescription(got, tt.wantKept) {
				t.Errorf("kept = %+v, want %+v", got, tt.wantKept)
			}
		})
	}
}

// keepForTest writes what a pair keeps, the way a describe would.
func keepForTest(t *testing.T, stateDir string, kept *workflow.ChangeDescription) {
	t.Helper()
	if err := workflow.UpsertServiceMeta(stateDir, "appdev", func(meta *workflow.ServiceMeta, _ bool) error {
		meta.Gitea.ChangeDescription = kept
		return nil
	}); err != nil {
		t.Fatalf("keep: %v", err)
	}
}

func equalChangeDescription(a, b *workflow.ChangeDescription) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// TestGiteaPushNextActions_AsksForTheDescription: a push that leaves a request
// open asks the Mate to describe it — and to describe it again as it grows,
// since the Mate keeps working on top of an open change — or says the words it
// kept are on it now.
func TestGiteaPushNextActions_AsksForTheDescription(t *testing.T) {
	tests := []struct {
		name     string
		pr       *giteaPullRequestRef
		want     []string
		wantNone bool
	}{
		{name: "no request open", wantNone: true},
		{
			name: "a request open",
			pr:   &giteaPullRequestRef{Repo: "acme/appdev", Branch: "mate/mate-p1", Base: "main", Number: 3, URL: "https://gitea.example.invalid/acme/appdev/pulls/3"},
			want: []string{`zerops_workflow action="describe-change" service="appdev"`, "what it does and why", "how you checked it", "whenever it grows"},
		},
		{
			name: "the words it kept are on it now",
			pr: &giteaPullRequestRef{Repo: "acme/appdev", Branch: "mate/mate-p1", Base: "main", Number: 3,
				URL: "https://gitea.example.invalid/acme/appdev/pulls/3", Described: true},
			want: []string{"carries the description you wrote", `action="describe-change" service="appdev"`, "whenever the change grows"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := giteaPushNextActions(tt.pr, "appdev")
			if tt.wantNone {
				if strings.Contains(got, "describe-change") {
					t.Errorf("with no request open there is nothing to describe yet:\n%s", got)
				}
				return
			}
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("next actions miss %q:\n%s", want, got)
				}
			}
		})
	}
}

// TestADeliveryPutsTheKeptDescriptionOnItsRequest: a Mate that described its
// change before delivering it finds its words on the request the stage
// deploy opens, and is told so.
func TestADeliveryPutsTheKeptDescriptionOnItsRequest(t *testing.T) {
	fake := newFakeGitea()
	gitea := fake.start(t)
	stateDir := t.TempDir()
	writeWiredGiteaPairMeta(t, stateDir, gitea.URL+"/acme/appdev")
	keepForTest(t, stateDir, &workflow.ChangeDescription{Text: describedWords})
	t.Setenv("GITEA_URL", gitea.URL)
	t.Setenv("MATE_BROKER_URL", gitea.URL)
	t.Setenv("GITEA_TOKEN", giteaBotToken)

	var hosts []string
	delivery := deliverGiteaPair(context.Background(), platform.NewMock(), gitea.Client(), &hostRecordingSSH{output: "ok", hosts: &hosts},
		runtime.Info{InContainer: true, ProjectID: "proj-1"}, stateDir, "appstage")
	if delivery == nil || delivery.PullRequest == nil {
		t.Fatalf("want a delivery with its pull request, got %+v", delivery)
	}
	if !slices.Equal(fake.pullBodies, []string{describedWords}) {
		t.Errorf("descriptions given = %q, want the kept words once", fake.pullBodies)
	}
	if !strings.Contains(delivery.Line, "carries the description you wrote") {
		t.Errorf("the line must say the kept words are on the request:\n%s", delivery.Line)
	}
	if got := keptDescription(t, stateDir, "appdev"); got != nil {
		t.Errorf("the words are on the request, yet still kept: %+v", got)
	}
}
