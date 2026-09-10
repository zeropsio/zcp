package tools

import "testing"

// TestMutatingToolNames_MatchesKnownMutatingSet pins MutatingToolNames'
// output against the same "Mutating tools" set documented in
// annotations_test.go (tools_test package) — derived independently here via
// the in-memory registry connect, not a shared hand-list, so drift between
// the two would mean one of them is wrong.
func TestMutatingToolNames_MatchesKnownMutatingSet(t *testing.T) {
	t.Parallel()
	got := MutatingToolNames()

	want := []string{
		"zerops_process", "zerops_manage", "zerops_scale", "zerops_delete",
		"zerops_subdomain", "zerops_deploy", "zerops_env", "zerops_import",
		"zerops_mount", "zerops_dev_server", "zerops_deploy_batch",
	}
	if len(got) != len(want) {
		t.Fatalf("MutatingToolNames() = %v (%d), want exactly %v (%d)", got, len(got), want, len(want))
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("MutatingToolNames() missing %q", name)
		}
	}

	readOnly := []string{"zerops_discover", "zerops_knowledge", "zerops_logs", "zerops_events", "zerops_verify"}
	for _, name := range readOnly {
		if got[name] {
			t.Errorf("MutatingToolNames() must not include read-only tool %q (not registered by this snapshot anyway)", name)
		}
	}
}
