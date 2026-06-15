package recipe

import (
	"strings"
	"testing"
)

// briefs_scaffold_atom_run28_test.go — Run-28 fix #5 atom assertions.
//
// Pins the scaffold preship_contract atom additions that close the
// run-27 `start-dev.sh` invention trap: env-roll timing teaching
// (wait for the new container after `zerops_deploy` rather than
// SSH-probing the old one) and an explicit env-wrapper-script ban
// when `run.envVariables` already maps the same keys.

// TestScaffoldPreshipContract_EnvRollTimingSection_Present — Run-28
// fix #5. Teaches porter to wait for the new container after
// `zerops_deploy` rather than SSH-probing the old one and seeing
// stale env. Closes the run-27 `start-dev.sh` invention trap.
func TestScaffoldPreshipContract_EnvRollTimingSection_Present(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/scaffold/preship_contract.md")
	if err != nil {
		t.Fatalf("read preship_contract.md: %v", err)
	}
	for _, want := range []string{
		"Env-roll timing",
		"AFTER the container restarts",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("preship_contract.md missing env-roll teaching anchor %q", want)
		}
	}
}

// TestScaffoldPreshipContract_EnvWrapperScriptBan_Present — Run-28
// fix #5. The atom must explicitly forbid env-wrapper scripts when
// `run.envVariables` already maps the same keys, with a worked
// example showing the dead-code shape.
func TestScaffoldPreshipContract_EnvWrapperScriptBan_Present(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/scaffold/preship_contract.md")
	if err != nil {
		t.Fatalf("read preship_contract.md: %v", err)
	}
	for _, want := range []string{
		"Env-wrapper-script ban",
		"start-dev.sh",
		"run.envVariables",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("preship_contract.md missing env-wrapper ban anchor %q", want)
		}
	}
}
