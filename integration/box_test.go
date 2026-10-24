// Tests for: integration — a Zerops Box through the full server: env changes,
// lifecycle and verify (spec-workflows.md §8 PS-8, PS-9).

package integration_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// TestIntegration_Box_KeyRotationLifecycleAndVerify — rotating a Box's
// workspace API key restarts nothing of the Box and names the reload that
// applies it; a restart of the Box then says its running sandboxes came back
// PARKED without their memory; verify, run outside the project, says the
// workspace API answers only inside it.
func TestIntegration_Box_KeyRotationLifecycleAndVerify(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myproject", Status: "ACTIVE"}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-box", Name: "box", ProjectID: "proj-1", Status: "ACTIVE",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "ubuntu/zbox@1", ServiceStackTypeCategoryName: "STANDARD"}},
		}).
		WithServiceEnv("svc-box", []platform.ServiceEnvVar{{ID: "u-key", Key: "ZBOX_WORKSPACE_API_KEY", Content: "old", Sensitive: true}}).
		WithProcess(&platform.Process{ID: "proc-envset-svc-box", Status: "FINISHED"}).
		WithProcess(&platform.Process{ID: "proc-restart-svc-box", Status: "FINISHED"})
	session, cleanup := setupTestServer(t, mock, defaultLogFetcher())
	defer cleanup()

	text := callAndGetText(t, session, "zerops_env", map[string]any{
		"action": "set", "serviceHostname": "box", "variables": []any{"ZBOX_WORKSPACE_API_KEY=rotated-secret"},
	})
	var env struct {
		RestartedServices []string `json:"restartedServices"`
		NextActions       string   `json:"nextActions"`
	}
	if err := json.Unmarshal([]byte(text), &env); err != nil {
		t.Fatalf("parse env result: %v\n%s", err, text)
	}
	if len(env.RestartedServices) != 0 {
		t.Errorf("a key rotation restarted %v — a Box is never restarted for an env change", env.RestartedServices)
	}
	if !strings.Contains(env.NextActions, `zerops_manage action=reload serviceHostname="box"`) {
		t.Errorf("nextActions %q should name the reload that applies the key", env.NextActions)
	}
	if strings.Contains(text, "rotated-secret") {
		t.Errorf("the key is echoed: %s", text)
	}

	text = callAndGetText(t, session, "zerops_manage", map[string]any{"action": "restart", "serviceHostname": "box"})
	var manage struct {
		NextActions string `json:"nextActions"`
	}
	if err := json.Unmarshal([]byte(text), &manage); err != nil {
		t.Fatalf("parse manage result: %v\n%s", err, text)
	}
	for _, want := range []string{"PARKED", "without their memory", "reload"} {
		if !strings.Contains(manage.NextActions, want) {
			t.Errorf("restart nextActions miss %q: %s", want, manage.NextActions)
		}
	}

	text = callAndGetText(t, session, "zerops_verify", map[string]any{"serviceHostname": "box"})
	var vr ops.VerifyResult
	if err := json.Unmarshal([]byte(text), &vr); err != nil {
		t.Fatalf("parse verify result: %v\n%s", err, text)
	}
	found := false
	for _, c := range vr.Checks {
		if c.Name == "orchestrator" && c.Status == ops.CheckSkip && strings.Contains(c.Detail, "only inside the project") {
			found = true
		}
	}
	if !found {
		t.Errorf("verify checks = %+v, want the orchestrator check saying the API answers only inside the project", vr.Checks)
	}
}
