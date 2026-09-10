package platform

import (
	"encoding/json"
	"testing"
)

// TestAppVersionEvent_DecodesPublicGitSource pins the JSON shape
// docs/spec-eval-farm.md §4.4 O7 relies on: a GIT-sourced appVersion carries
// a non-nil publicGitSource with gitUrl/branchName, while a CLI-sourced (or
// NONE-sourced) appVersion carries no publicGitSource field at all.
func TestAppVersionEvent_DecodesPublicGitSource(t *testing.T) {
	t.Parallel()

	t.Run("GIT source carries publicGitSource", func(t *testing.T) {
		t.Parallel()
		raw := `{
			"id": "av-1",
			"serviceStackId": "svc-1",
			"source": "GIT",
			"status": "ACTIVE",
			"publicGitSource": {"gitUrl": "https://github.com/example/repo", "branchName": "main"},
			"created": "2026-09-10T00:00:00Z"
		}`
		var ev AppVersionEvent
		if err := json.Unmarshal([]byte(raw), &ev); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if ev.PublicGitSource == nil {
			t.Fatalf("expected PublicGitSource to be set, got nil")
		}
		if ev.PublicGitSource.GitURL != "https://github.com/example/repo" {
			t.Errorf("GitURL = %q", ev.PublicGitSource.GitURL)
		}
		if ev.PublicGitSource.BranchName != "main" {
			t.Errorf("BranchName = %q", ev.PublicGitSource.BranchName)
		}
	})

	t.Run("CLI source carries no publicGitSource", func(t *testing.T) {
		t.Parallel()
		raw := `{
			"id": "av-2",
			"serviceStackId": "svc-2",
			"source": "CLI",
			"status": "ACTIVE",
			"publicGitSource": null,
			"created": "2026-09-10T01:00:00Z"
		}`
		var ev AppVersionEvent
		if err := json.Unmarshal([]byte(raw), &ev); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if ev.PublicGitSource != nil {
			t.Errorf("expected PublicGitSource to be nil, got %+v", ev.PublicGitSource)
		}
	})
}
