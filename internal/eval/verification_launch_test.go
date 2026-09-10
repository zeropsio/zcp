package eval

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// TestVerification_LaunchShape_AllRowsFromSnapshot pins O6's four
// platform-read rows (docs/spec-eval-farm.md §4.4 O6): a healthy prod
// project with two startWithoutCode runtimes and no buildFromGit produces
// four passed rows; a runtime that carries a build fails no_build_from_git
// with the hostname named in Observed.
func TestVerification_LaunchShape_AllRowsFromSnapshot(t *testing.T) {
	t.Parallel()
	cfg := &LaunchShapeConfig{ProdProject: "zcp-farm-r1-prod"}

	t.Run("clean launch, all rows pass", func(t *testing.T) {
		t.Parallel()
		client := platform.NewMock().
			WithUserInfo(&platform.UserInfo{ID: "client-1"}).
			WithProjects([]platform.Project{{ID: "prod-1", Name: "zcp-farm-r1-prod"}}).
			WithServicesDirect([]platform.ServiceStack{
				{ID: "svc-app1", ProjectID: "prod-1", Name: "app1", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}},
				{ID: "svc-app2", ProjectID: "prod-1", Name: "app2", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "go@1", ServiceStackTypeCategoryName: "USER"}},
			}).
			WithAppVersionEvents([]platform.AppVersionEvent{
				{ID: "av1", ProjectID: "prod-1", ServiceStackID: "svc-app1", Source: "NONE", Status: "ACTIVE", Created: "2026-09-10T00:00:00Z"},
				{ID: "av2", ProjectID: "prod-1", ServiceStackID: "svc-app2", Source: "NONE", Status: "ACTIVE", Created: "2026-09-10T00:00:00Z"},
			})

		rows := evaluateLaunchShapeRows(context.Background(), cfg, client, "", nil, "")
		want := map[string]CheckResult{
			"launch_shape/project_exists":               CheckPassed,
			"launch_shape/runtimes_start_without_code":  CheckPassed,
			"launch_shape/no_build_from_git":            CheckPassed,
			"launch_shape/first_release_is_first_build": CheckPassed,
		}
		assertRowResults(t, rows, want)
	})

	t.Run("one runtime carries a build, no_build_from_git fails with hostname", func(t *testing.T) {
		t.Parallel()
		client := platform.NewMock().
			WithUserInfo(&platform.UserInfo{ID: "client-1"}).
			WithProjects([]platform.Project{{ID: "prod-1", Name: "zcp-farm-r1-prod"}}).
			WithServicesDirect([]platform.ServiceStack{
				{ID: "svc-app1", ProjectID: "prod-1", Name: "app1", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}},
			}).
			WithAppVersionEvents([]platform.AppVersionEvent{
				{ID: "av1", ProjectID: "prod-1", ServiceStackID: "svc-app1", Source: "git", Status: "ACTIVE", Created: "2026-09-10T00:00:00Z", Build: &platform.BuildInfo{}},
			})

		rows := evaluateLaunchShapeRows(context.Background(), cfg, client, "", nil, "")
		row := findRow(t, rows, "launch_shape/no_build_from_git")
		if row.Result != CheckFailed {
			t.Fatalf("expected no_build_from_git to fail, got %+v", row)
		}
		if !strings.Contains(row.Observed, "app1") {
			t.Errorf("expected Observed to name the offending hostname app1, got %q", row.Observed)
		}
	})
}

// TestVerification_LaunchShape_TokenInTranscript_Fails pins the
// token_not_in_transcript row (docs/spec-workflows.md §10.2b): the row
// never handles the token value, only its sha256.
func TestVerification_LaunchShape_TokenInTranscript_Fails(t *testing.T) {
	t.Parallel()
	tokenSHA := sha256Hex("super-secret-launch-token")

	t.Run("token present in transcript fails", func(t *testing.T) {
		t.Parallel()
		row := evaluateTokenNotInTranscriptRow("the agent printed super-secret-launch-token to the user", nil, tokenSHA)
		if row.Result != CheckFailed {
			t.Fatalf("expected failed, got %+v", row)
		}
	})

	t.Run("token absent from transcript passes", func(t *testing.T) {
		t.Parallel()
		row := evaluateTokenNotInTranscriptRow("the agent launched the project successfully", nil, tokenSHA)
		if row.Result != CheckPassed {
			t.Fatalf("expected passed, got %+v", row)
		}
	})

	// The assertion never places the token value in a test name or log —
	// only the row's boolean Result is asserted above.
}

// TestVerification_LaunchShape_ListNotPermitted_Blocked pins the case
// where the run-project-scoped key can't list account projects: a 403 from
// ListProjects blocks project_exists, and the three runtime-shape rows
// are not-run rather than failed (docs/spec-eval-farm.md §4.4 O6).
func TestVerification_LaunchShape_ListNotPermitted_Blocked(t *testing.T) {
	t.Parallel()
	cfg := &LaunchShapeConfig{ProdProject: "zcp-farm-r1-prod"}
	client := platform.NewMock().
		WithUserInfo(&platform.UserInfo{ID: "client-1"}).
		WithError("ListProjects", errors.New("403 forbidden"))

	rows := evaluateLaunchShapeRows(context.Background(), cfg, client, "", nil, "")
	projectRow := findRow(t, rows, "launch_shape/project_exists")
	if projectRow.Result != CheckBlocked {
		t.Fatalf("expected project_exists blocked, got %+v", projectRow)
	}
	for _, field := range []string{"runtimes_start_without_code", "no_build_from_git", "first_release_is_first_build"} {
		row := findRow(t, rows, "launch_shape/"+field)
		if row.Result != CheckNotRun {
			t.Errorf("expected %s not-run when project list is blocked, got %+v", field, row)
		}
	}
}

func assertRowResults(t *testing.T, rows []RequiredCheck, want map[string]CheckResult) {
	t.Helper()
	for id, expected := range want {
		row := findRow(t, rows, id)
		if row.Result != expected {
			t.Errorf("row %s: expected %s, got %s (%+v)", id, expected, row.Result, row)
		}
	}
}

func findRow(t *testing.T, rows []RequiredCheck, id string) RequiredCheck {
	t.Helper()
	for _, row := range rows {
		if row.ID == id {
			return row
		}
	}
	t.Fatalf("row %s not found in %+v", id, rows)
	return RequiredCheck{}
}
