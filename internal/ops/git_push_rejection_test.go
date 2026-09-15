package ops

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestIsNonFastForwardRejection_MatchesGitPhrasings(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		output string
		want   bool
	}{
		{"rejected-bracket", " ! [rejected]        main -> main (fetch first)", true},
		{"fetch-first", "hint: Updates were rejected because the remote contains work\nfailed to push some refs (fetch first)", true},
		{"updates-were-rejected", "error: failed to push some refs to 'https://github.com/foo/bar'\nhint: Updates were rejected because the remote contains work that you do not\nhint: have locally.", true},
		{"non-fast-forward-word", "! [remote rejected] main -> main (non-fast-forward)", true},
		{"auth-failure-not-matched", "fatal: Authentication failed for 'https://github.com/example/repo'", false},
		{"unrelated-text", "Everything up-to-date", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IsNonFastForwardRejection(tc.output); got != tc.want {
				t.Errorf("IsNonFastForwardRejection(%q) = %v, want %v", tc.output, got, tc.want)
			}
		})
	}
}

// fakeGitRunner builds a GitRunner from a canned response table keyed by
// the joined args string.
func fakeGitRunner(responses map[string]struct {
	out string
	err error
}) GitRunner {
	return func(args ...string) (string, error) {
		key := fmt.Sprint(args)
		r, ok := responses[key]
		if !ok {
			return "", fmt.Errorf("fakeGitRunner: no stub for %v", args)
		}
		return r.out, r.err
	}
}

func TestProbeGitDivergence_AheadRemote(t *testing.T) {
	t.Parallel()
	run := fakeGitRunner(map[string]struct {
		out string
		err error
	}{
		fmt.Sprint([]string{"fetch", "origin", "main"}):                 {"", nil},
		fmt.Sprint([]string{"rev-list", "--count", "HEAD..FETCH_HEAD"}): {"3\n", nil},
		fmt.Sprint([]string{"rev-list", "--count", "FETCH_HEAD..HEAD"}): {"0\n", nil},
		fmt.Sprint([]string{"merge-base", "HEAD", "FETCH_HEAD"}):        {"abc123\n", nil},
	})
	probe, refExists, err := ProbeGitDivergence(run, "main")
	if err != nil {
		t.Fatalf("ProbeGitDivergence: %v", err)
	}
	if !refExists {
		t.Fatal("expected refExists=true")
	}
	if probe.RemoteAhead != 3 || probe.LocalAhead != 0 || probe.Unrelated {
		t.Errorf("unexpected probe: %+v", probe)
	}
}

func TestProbeGitDivergence_UnrelatedHistories(t *testing.T) {
	t.Parallel()
	run := fakeGitRunner(map[string]struct {
		out string
		err error
	}{
		fmt.Sprint([]string{"fetch", "origin", "main"}):                 {"", nil},
		fmt.Sprint([]string{"rev-list", "--count", "HEAD..FETCH_HEAD"}): {"5\n", nil},
		fmt.Sprint([]string{"rev-list", "--count", "FETCH_HEAD..HEAD"}): {"2\n", nil},
		fmt.Sprint([]string{"merge-base", "HEAD", "FETCH_HEAD"}):        {"", errors.New("exit status 1")},
	})
	probe, refExists, err := ProbeGitDivergence(run, "main")
	if err != nil {
		t.Fatalf("ProbeGitDivergence: %v", err)
	}
	if !refExists {
		t.Fatal("expected refExists=true")
	}
	if !probe.Unrelated {
		t.Error("expected Unrelated=true when merge-base finds no common ancestor")
	}
}

func TestProbeGitDivergence_RefNotFound_ReturnsEmptyNoError(t *testing.T) {
	t.Parallel()
	run := fakeGitRunner(map[string]struct {
		out string
		err error
	}{
		fmt.Sprint([]string{"fetch", "origin", "main"}): {"", errors.New("fatal: couldn't find remote ref main")},
	})
	probe, refExists, err := ProbeGitDivergence(run, "main")
	if err != nil {
		t.Fatalf("expected nil error for ref-not-found, got: %v", err)
	}
	if refExists {
		t.Fatal("expected refExists=false")
	}
	if probe == nil {
		t.Fatal("expected a non-nil probe")
	}
}

func TestProbeGitDivergence_FetchAuthFailure_ReturnsError(t *testing.T) {
	t.Parallel()
	run := fakeGitRunner(map[string]struct {
		out string
		err error
	}{
		fmt.Sprint([]string{"fetch", "origin", "main"}): {"", errors.New("fatal: Authentication failed")},
	})
	_, _, err := ProbeGitDivergence(run, "main")
	if err == nil {
		t.Fatal("expected an error for a non-ref-not-found fetch failure")
	}
}

func TestClassifyRemoteRefState(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		refExists   bool
		remoteAhead int
		localAhead  int
		unrelated   bool
		want        RemoteRefState
	}{
		{"empty", false, 0, 0, false, RemoteRefEmpty},
		{"in-sync", true, 0, 0, false, RemoteRefInSync},
		{"ahead", true, 2, 0, false, RemoteRefAhead},
		{"behind", true, 0, 3, false, RemoteRefBehind},
		{"diverged", true, 1, 1, false, RemoteRefDiverged},
		{"unrelated", true, 4, 4, true, RemoteRefUnrelated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ClassifyRemoteRefState(tc.refExists, tc.remoteAhead, tc.localAhead, tc.unrelated)
			if got != tc.want {
				t.Errorf("ClassifyRemoteRefState(%v,%d,%d,%v) = %q, want %q", tc.refExists, tc.remoteAhead, tc.localAhead, tc.unrelated, got, tc.want)
			}
		})
	}
}

func TestRemoteRefState_NeedsPushDecision(t *testing.T) {
	t.Parallel()
	for _, s := range []RemoteRefState{RemoteRefAhead, RemoteRefDiverged, RemoteRefUnrelated} {
		if !s.NeedsPushDecision() {
			t.Errorf("%q should need a push decision", s)
		}
	}
	for _, s := range []RemoteRefState{RemoteRefEmpty, RemoteRefInSync, RemoteRefBehind} {
		if s.NeedsPushDecision() {
			t.Errorf("%q should NOT need a push decision", s)
		}
	}
}

func TestBuildGitDivergenceStepCommand_FetchIsAuthenticated(t *testing.T) {
	t.Parallel()
	cmd := BuildGitDivergenceStepCommand("/var/www", []string{"fetch", "origin", "main"})
	if !strings.Contains(cmd, "credential.helper") {
		t.Errorf("fetch step must carry the credential helper: %s", cmd)
	}
	if !strings.Contains(cmd, "GIT_TERMINAL_PROMPT=0") {
		t.Errorf("fetch step must disable terminal prompts: %s", cmd)
	}
}

func TestBuildGitDivergenceStepCommand_RevListIsUnauthenticated(t *testing.T) {
	t.Parallel()
	cmd := BuildGitDivergenceStepCommand("/var/www", []string{"rev-list", "--count", "HEAD..FETCH_HEAD"})
	if strings.Contains(cmd, "credential.helper") {
		t.Errorf("a local graph read must not carry the credential helper: %s", cmd)
	}
}
