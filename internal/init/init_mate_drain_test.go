package init_test

import (
	"testing"

	zcpinit "github.com/zeropsio/zcp/internal/init"
	"github.com/zeropsio/zcp/internal/mate"
)

func TestRun_Mate_ExistingSwitch_PostponesInit(t *testing.T) {
	// non-parallel: the rig redirects HOME, installer and command seams.
	rig := newMateRig(t)
	rig.installBundle(t)
	if err := mate.WriteUpdateState(mate.WorkerUpdateState(mate.UpdateState{Phase: "switching", Candidate: pinnedTestVersion})); err != nil {
		t.Fatal(err)
	}
	if err := zcpinit.Run(rig.baseDir, containerInfo()); err != nil {
		t.Fatal(err)
	}
	if rig.installs != 0 || len(rig.restartCalls()) != 0 {
		t.Errorf("init interfered with an unfinished switch: installs=%d restarts=%v", rig.installs, rig.restartCalls())
	}
}
