// Tests for: the project variable sharing step of `zcp init` in a Mate's
// container (docs/spec-mate.md C-14) — a step that must never cost any
// container its start.
//
// NOT parallel — every test redirects HOME, the command runner and the
// unit-file path, all package-level (newMateRig).
package init_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	zcpinit "github.com/zeropsio/zcp/internal/init"
	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/ops"
)

// witness: internal/init/init.go | "a service whose variables do not reach this container answers no_credentials in the data console"} → ""}
func TestRun_MateContainer_AdmissionRefused_InitCompletes(t *testing.T) {
	rig := newMateRig(t)
	rig.installBundle(t)
	info := containerInfo()
	info.ServiceName = "zcp"
	refuse := zcpinit.WithEnvAdmission(func(context.Context, string) (ops.EnvIsolationAdmission, error) {
		return ops.EnvIsolationAdmission{}, errors.New("set the project's envIsolation: forbidden")
	})

	var runErr error
	stderr := captureStderr(t, func() { runErr = zcpinit.Run(rig.baseDir, info, refuse) })
	if runErr != nil {
		t.Fatalf("Run() = %v, want init to complete past a refused admission", runErr)
	}
	if strings.Count(stderr, "set the project's envIsolation: forbidden") != 1 {
		t.Errorf("stderr = %q, want the refusal said once", stderr)
	}
	if _, err := os.Stat(filepath.Join(rig.baseDir, filepath.FromSlash(mate.InitMarkerRelPath))); err != nil {
		t.Errorf("the init-complete marker is missing after a refused admission: %v", err)
	}
}

// witness: internal/init/init.go | if rt.InContainer && rt.MateEnabled { → if rt.InContainer {
func TestRun_MateDisabled_NoAdmission(t *testing.T) {
	rig := newMateRig(t)
	info := containerInfo()
	info.ServiceName = "zcp"
	info.MateEnabled = false
	asked := false
	admit := zcpinit.WithEnvAdmission(func(context.Context, string) (ops.EnvIsolationAdmission, error) {
		asked = true
		return ops.EnvIsolationAdmission{}, nil
	})

	if err := zcpinit.Run(rig.baseDir, info, admit); err != nil {
		t.Fatalf("Run(): %v", err)
	}
	if asked {
		t.Error("a container that is no Mate asked its project's envIsolation: it must never write its project's settings")
	}
}
