// Tests for: the origin's CI on a Git environment's newest push, as the agent
// reads it — zerops_observe action="ci" (hq_observe.go) and the merged
// change's note (hq_change.go).
package tools

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/workflow"
)

const (
	ciSha  = "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b"
	ciUnit = "https://github.com/acme/app/actions/runs/7/job/9"
)

// ciPush is HQ's gitPushes entry for git-stage's push of ciSha, with ci as
// HQ's raw JSON record ("" for none).
func ciPush(ci string) string {
	push := `{"environment":"git-stage","tier":"stage","branch":"main","repo":"appdev","sha":"` + ciSha + `"`
	if ci != "" {
		push += `,"ci":{"sha":"` + ciSha + `",` + ci + `,"readAt":"2026-10-17T10:00:10.000Z","until":"2026-10-17T11:00:00.000Z"}`
	}
	return push + "}"
}

// TestObserve_CI_AnswersEachGitEnvironmentsPushPerVerdict: each push's CI,
// with what the agent does about a running, failed or unreadable one — said
// only to a Mate, which alone reads CI through HQ.
func TestObserve_CI_AnswersEachGitEnvironmentsPushPerVerdict(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, gitPushes, want string
	}{
		{"running while HQ watches", `[` + ciPush(`"verdict":"running","done":1,"total":3,"failed":[],"required":null,"reason":null,"watch":"watching"`) + `]`,
			"appdev: CI on 1a2b3c4d5e in git-stage: running — 1 of 3 checks done; HQ reads it until 2026-10-17T11:00:00.000Z; tell the person CI is still running and read it again a few minutes later, never in a loop and not past then"},
		{"running after HQ stopped watching", `[` + ciPush(`"verdict":"running","done":1,"total":3,"failed":[],"required":null,"reason":"Mate stopped watching CI 60 minutes after the push.","watch":"stopped"`) + `]`,
			"appdev: CI on 1a2b3c4d5e in git-stage: running — 1 of 3 checks done; HQ stopped reading: Mate stopped watching CI 60 minutes after the push."},
		{"passed", `[` + ciPush(`"verdict":"passed","done":3,"total":3,"failed":[],"required":null,"reason":null,"watch":"finished"`) + `]`,
			"appdev: CI on 1a2b3c4d5e in git-stage: passed — 3 of 3 checks"},
		{"failed names each check with its URL", `[` + ciPush(`"verdict":"failed","done":2,"total":2,"failed":[{"name":"check_types","url":"`+ciUnit+`"},{"name":"lint","url":null}],"required":null,"reason":null,"watch":"finished"`) + `]`,
			"appdev: CI on 1a2b3c4d5e in git-stage: failed — check_types " + ciUnit + ", lint; you hold no token for the origin, so run the repository's own test command for each on the dev half, fix it and deliver the fix as a new change"},
		{"unreadable says why", `[` + ciPush(`"verdict":"unreadable","done":0,"total":0,"failed":[],"required":null,"reason":"GitHub rejected this token (expired, revoked or mistyped). Replace the token to show CI.","watch":"stopped"`) + `]`,
			"appdev: CI on 1a2b3c4d5e in git-stage: unreadable — GitHub rejected this token (expired, revoked or mistyped). Replace the token to show CI. Tell the person this reason: only they can replace the Git environment's token."},
		{"none while HQ watches", `[` + ciPush(`"verdict":"none","done":0,"total":0,"failed":[],"required":null,"reason":null,"watch":"watching"`) + `]`,
			"appdev: CI on 1a2b3c4d5e in git-stage: none — no check reported; HQ reads it until 2026-10-17T11:00:00.000Z"},
		{"a push HQ read no CI for", `[` + ciPush("") + `]`,
			"appdev: CI on 1a2b3c4d5e in git-stage: not read — HQ holds no CI record for this push"},
		{"no Git environment push", `[]`,
			"No Git environment of this Mate's application has taken a push yet."},
		{"an older HQ", "",
			"This HQ does not report its Git environments' pushes, so there is no CI to read through it."},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body := `{"projectId":"P_MATE","name":"Ada","face":"f","standupRequestedBy":null,"appId":"a1","changes":[]`
			if tt.gitPushes != "" {
				body += `,"gitPushes":` + tt.gitPushes
			}
			body += "}"
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/api/mate/self" || r.Header.Get("Authorization") != "Mate good" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL)
				}
				_, _ = w.Write([]byte(body))
			}))
			defer fake.Close()
			path := filepath.Join(t.TempDir(), "enrollment.json")
			if err := hq.SaveEnrollment(path, hq.Enrollment{HQ: fake.URL, ProjectID: "P_MATE", Credential: "good"}); err != nil {
				t.Fatal(err)
			}
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
			registerObserve(srv, fake.Client(), path, "P_MATE")

			result := callTool(t, srv, "zerops_observe", map[string]any{"action": "ci"})

			if text := result.Content[0].(*mcp.TextContent).Text; result.IsError || text != tt.want {
				t.Errorf("answer = %q (error %v)\nwant     %q", text, result.IsError, tt.want)
			}
		})
	}
}

// TestHQLearnLanding_MergedChange_CarriesTheCIOfItsPush: the note that the
// pair's change merged carries the origin's CI on a Git environment's push of
// that very commit, and nothing of a push of another commit or repository.
func TestHQLearnLanding_MergedChange_CarriesTheCIOfItsPush(t *testing.T) {
	t.Parallel()
	failed := "failed"
	url := ciUnit
	pushes := []hq.GitPush{
		{Environment: "git-stage", Repo: "appdev", Sha: ciSha, CI: &hq.PushCI{Verdict: failed, Done: 1, Total: 1,
			Failed: []hq.PushCIFailure{{Name: "check_types", URL: &url}}, Watch: "finished"}},
		{Environment: "git-prod", Repo: "appdev", Sha: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"},
		{Environment: "git-stage", Repo: "apidev", Sha: ciSha},
	}
	stateDir := t.TempDir()
	m := &workflow.ServiceMeta{Hostname: "appdev", BootstrapSession: "test", BootstrappedAt: "2026-10-02",
		HQ: &workflow.HQRepoRef{AppID: labApp, Repo: "appdev", Change: 3}}
	if err := workflow.WriteServiceMeta(stateDir, m); err != nil {
		t.Fatal(err)
	}
	head := "2222222222222222222222222222222222222222"
	merged := ciSha
	state := hq.MateState{GitPushes: pushes, Changes: []hq.MateChange{
		{Repo: "appdev", Number: 3, State: hq.ChangeMerged, Head: &head, MergedSha: &merged, LandedHead: &head},
	}}

	note := hqLearnLanding(stateDir, m, state)

	want := `change #3 is merged — this Mate's work is on "main" now; CI on 1a2b3c4d5e in git-stage: failed — check_types ` + ciUnit + "; you hold no token for the origin, so run the repository's own test command for each on the dev half, fix it and deliver the fix as a new change; start the next work from \"main\""
	if note != want {
		t.Errorf("note = %q\nwant   %q", note, want)
	}
}
