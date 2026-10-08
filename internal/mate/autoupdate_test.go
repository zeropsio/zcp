package mate_test

import (
	"errors"
	"os"
	"testing"

	"github.com/zeropsio/zcp/internal/mate"
)

// non-parallel: HOME controls the install and update state paths.
func TestAutomaticUpdate_CandidateDecision_Result(t *testing.T) {
	cases := []struct {
		name, installed, failed, candidate string
		compatible, allowed                bool
	}{
		{"new compatible", "0.14.0", "", "0.14.2", true, true},
		{"confirmation required", "0.14.0", "", "0.14.2", false, false},
		{"failed release", "0.14.0", "0.14.2", "0.14.2", true, false},
		{"newer than failed", "0.14.0", "0.14.1", "0.14.2", true, true},
		{"downgrade", "0.14.2", "", "0.14.0", true, false},
		{"same", "0.14.2", "", "0.14.2", true, false},
		{"dev preserved", "0.14.0-dev.abc", "", "0.14.2", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mate.AutomaticUpdateAllowed(tc.installed, tc.failed, mate.Manifest{Version: tc.candidate, RollbackCompatible: tc.compatible})
			if got != tc.allowed {
				t.Fatalf("allowed=%v, want %v", got, tc.allowed)
			}
		})
	}
}

func TestSwitchUpdate_Readiness_Result(t *testing.T) {
	for _, healthy := range []bool{true, false} {
		t.Run(map[bool]string{true: "ready", false: "rollback"}[healthy], func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			seedInstalledVersion(t, "0.14.0")
			writeFakePackage(t, mate.VersionDir("0.14.2"), "0.14.2")
			var restarts []string
			err := mate.SwitchUpdate("0.14.2", "old-boot", mate.SwitchHooks{
				Restart: func() error { v, _ := mate.InstalledVersion(); restarts = append(restarts, v); return nil },
				Ready: func(version, boot string) error {
					if healthy || version == "0.14.0" {
						return nil
					}
					return errors.New("candidate failed")
				},
				Commit: func() error { return nil }, Cancel: func() error { return nil },
			})
			if err != nil {
				t.Fatalf("switch: %v", err)
			}
			want := "0.14.2"
			if !healthy {
				want = "0.14.0"
			}
			got, _ := mate.InstalledVersion()
			if got != want {
				t.Fatalf("current=%s want=%s", got, want)
			}
			state, e := mate.ReadUpdateState()
			if e != nil {
				t.Fatal(e)
			}
			if !healthy && state.FailedVersion != "0.14.2" {
				t.Fatalf("failed=%q", state.FailedVersion)
			}
			if len(restarts) != map[bool]int{true: 1, false: 2}[healthy] {
				t.Fatalf("restarts=%v", restarts)
			}
			if _, e := os.Stat(mate.LastGoodLink()); e != nil {
				t.Fatal(e)
			}
		})
	}
}
