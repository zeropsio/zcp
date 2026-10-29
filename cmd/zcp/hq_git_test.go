package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestIsHQGit_RecognizesOnlyTheGitPassthrough(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		args []string
		want bool
	}{
		{[]string{"hq", "git"}, true},
		{[]string{"hq", "git", "-C", "/var/www/web", "fetch"}, true},
		{[]string{"hq", "git-credential", "get"}, false},
		{[]string{"hq", "status"}, false},
		{[]string{"git"}, false},
		{nil, false},
	} {
		if got := isHQGit(tt.args); got != tt.want {
			t.Errorf("isHQGit(%v) = %v, want %v", tt.args, got, tt.want)
		}
	}
}

func TestRunHQGit_MateUsesItsOwnGitCredential(t *testing.T) {
	t.Parallel()
	if os.Getenv("ZCP_TEST_HQ_GIT_CHILD") == "1" {
		os.Exit(runHQGit([]string{"--version"}))
	}
	for _, tc := range []struct {
		value   string
		refused bool
	}{{"1", true}, {" TRUE ", true}, {"0", false}, {"false", false}, {"", false}} {
		t.Run(tc.value, func(t *testing.T) {
			t.Parallel()
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestRunHQGit_MateUsesItsOwnGitCredential$") //nolint:gosec // Runs this test binary with a fixed test name.
			cmd.Env = append(os.Environ(), "ZCP_TEST_HQ_GIT_CHILD=1", "ZCP_MATE_ENABLED="+tc.value, "T3CODE_ZEROPS_HQ_ORIGIN=https://hq.example", "ZCP_API_KEY=test-key", "projectId=p")
			out, err := cmd.CombinedOutput()
			if tc.refused {
				if err == nil || !strings.Contains(string(out), "Mate's own Git credential") || !strings.Contains(string(out), "zcp git <host> <git args>") || strings.Contains(string(out), "git version") {
					t.Fatalf("Mate wrapper result = %q, %v", out, err)
				}
			} else if err != nil || !strings.Contains(string(out), "git version") {
				t.Fatalf("plain Git result = %q, %v", out, err)
			}
		})
	}
}
