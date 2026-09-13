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
			// Source "GIT" (finding E4: the platform's real value, uppercase —
			// a CLI push's Build alone must NOT fail this row; only an
			// independent GIT-sourced build does).
			WithAppVersionEvents([]platform.AppVersionEvent{
				{ID: "av1", ProjectID: "prod-1", ServiceStackID: "svc-app1", Source: "GIT", Status: "ACTIVE", Created: "2026-09-10T00:00:00Z", Build: &platform.BuildInfo{}},
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

// TestLaunchShape_CliBuild_NoBuildFromGitPasses pins finding E4: O7 notes a
// CLI push also populates Build, so a CLI-sourced appVersion carrying a
// Build must not fail no_build_from_git — only Source=="GIT" or a non-nil
// PublicGitSource does.
func TestLaunchShape_CliBuild_NoBuildFromGitPasses(t *testing.T) {
	t.Parallel()
	cfg := &LaunchShapeConfig{ProdProject: "zcp-farm-r1-prod"}
	client := platform.NewMock().
		WithUserInfo(&platform.UserInfo{ID: "client-1"}).
		WithProjects([]platform.Project{{ID: "prod-1", Name: "zcp-farm-r1-prod"}}).
		WithServicesDirect([]platform.ServiceStack{
			{ID: "svc-app1", ProjectID: "prod-1", Name: "app1", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av1", ProjectID: "prod-1", ServiceStackID: "svc-app1", Source: "CLI", Status: "ACTIVE", Created: "2026-09-10T00:00:00Z", Build: &platform.BuildInfo{}},
		})

	rows := evaluateLaunchShapeRows(context.Background(), cfg, client, "", nil, "")
	row := findRow(t, rows, "launch_shape/no_build_from_git")
	if row.Result != CheckPassed {
		t.Fatalf("expected no_build_from_git to pass for a CLI-sourced appVersion carrying a Build, got %+v", row)
	}
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

// TestLaunchToken_LeakInsideJSONString_Detected pins finding E3: the
// transcript is JSONL, so a leaked token typically appears as
// `"ZCP_LAUNCH_TOKEN=abc…"` (a quoted JSON string value, immediately after
// `=`) or `"abc…",` — a whitespace-only split never isolates the token from
// its surrounding quotes/`=`/`,`, so its sha256 never matched and the leak
// silently passed.
func TestLaunchToken_LeakInsideJSONString_Detected(t *testing.T) {
	t.Parallel()
	const token = "abc123XYZsecret"
	tokenSHA := sha256Hex(token)

	t.Run("token embedded in a JSON string value", func(t *testing.T) {
		t.Parallel()
		transcript := `{"type":"tool_result","content":"value is \"` + token + `\""}`
		row := evaluateTokenNotInTranscriptRow(transcript, nil, tokenSHA)
		if row.Result != CheckFailed {
			t.Fatalf("expected failed when the token is embedded inside a JSON string, got %+v", row)
		}
	})

	t.Run("token immediately after an equals sign", func(t *testing.T) {
		t.Parallel()
		transcript := `{"type":"tool_use","input":{"env":"ZCP_LAUNCH_TOKEN=` + token + `"}}`
		row := evaluateTokenNotInTranscriptRow(transcript, nil, tokenSHA)
		if row.Result != CheckFailed {
			t.Fatalf("expected failed when the token follows '=' with no surrounding whitespace, got %+v", row)
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

// TestLaunchShape_ProdProjectIDEnv_ResolvesByID pins docs/spec-eval-farm.md
// §4.4 O6/§4.1: launchShape.prodProjectIdEnv resolves the prod project by
// id (client.GetProject), not by name — and the project_exists row's Scope
// carries the env NAME, never the id value.
func TestLaunchShape_ProdProjectIDEnv_ResolvesByID(t *testing.T) {
	const envName = "ZCP_E2E_EXISTING_PROJECT_ID"
	t.Setenv(envName, "prod-secret-id-1")
	cfg := &LaunchShapeConfig{ProdProjectIDEnv: envName}
	client := platform.NewMock().
		WithProject(&platform.Project{ID: "prod-secret-id-1", Name: "some-prod-name"}).
		WithServicesDirect([]platform.ServiceStack{
			{ID: "svc-app1", ProjectID: "prod-secret-id-1", Name: "app1", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av1", ProjectID: "prod-secret-id-1", ServiceStackID: "svc-app1", Source: "NONE", Status: "ACTIVE", Created: "2026-09-10T00:00:00Z"},
		})

	rows := evaluateLaunchShapeRows(context.Background(), cfg, client, "", nil, "")
	row := findRow(t, rows, "launch_shape/project_exists")
	if row.Result != CheckPassed {
		t.Fatalf("expected project_exists to pass, got %+v", row)
	}
	if row.Scope != envName {
		t.Errorf("Scope = %q, want the env NAME %q (never the id value)", row.Scope, envName)
	}
	if strings.Contains(row.Message, "prod-secret-id-1") {
		t.Errorf("Message leaks the project id value: %q", row.Message)
	}
}

// TestLaunchShape_ProdProjectIDEnv_Unset_Blocked pins the defensive path:
// even though the runner's requiredEnvVars gate normally prevents the
// oracle from ever being reached with the env unset, the oracle itself
// grades project_exists blocked with a "resource … missing" message rather
// than panicking or silently resolving an empty id.
func TestLaunchShape_ProdProjectIDEnv_Unset_Blocked(t *testing.T) {
	const envName = "ZCP_E2E_EXISTING_PROJECT_ID_UNSET"
	t.Setenv(envName, "")
	cfg := &LaunchShapeConfig{ProdProjectIDEnv: envName}
	client := platform.NewMock()

	rows := evaluateLaunchShapeRows(context.Background(), cfg, client, "", nil, "")
	row := findRow(t, rows, "launch_shape/project_exists")
	if row.Result != CheckBlocked {
		t.Fatalf("expected project_exists blocked when env is unset, got %+v", row)
	}
	if !strings.Contains(row.Message, "resource") || !strings.Contains(row.Message, envName) || !strings.Contains(row.Message, "missing") {
		t.Errorf("Message should read as a resource-missing block, got %q", row.Message)
	}
	if row.Scope != envName {
		t.Errorf("Scope = %q, want the env NAME %q", row.Scope, envName)
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
