// Tests for: tools/hq_github_mirror.go — the repository pass says once that
// HQ mirrors a repository to GitHub, and once that the two diverged, run
// against a fake HQ that serves real git (hq_lab_test.go).
package tools

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
)

// flipOnGitHub is flip.studio's GitHub repository, connected through the
// organisation's GitHub app.
var flipOnGitHub = hq.RepoOrigin{URL: "https://github.com/lukaspitter-x/flip", Mode: hq.OriginOnGitHub}

// TestHQPass_GitHubMirror_SaidOnceWhenHQMirrorsTheRepository is Filip on
// flip.studio: after every merged change it cherry-picked the change onto
// GitHub's master in a clone of its own and pushed it, as its memory said to.
// Once the organisation's GitHub app connects the repository, HQ mirrors it —
// `main` to GitHub's default branch, each change as its pull request — and a
// sync by hand fights HQ's follow. The pass says so once, when the
// repository's remote becomes the app's; a remote reached with a person's
// token is no mirror and says nothing.
func TestHQPass_GitHubMirror_SaidOnceWhenHQMirrorsTheRepository(t *testing.T) {
	t.Parallel()
	const said = "HQ mirrors it to GitHub now (lukaspitter-x/flip)"
	tests := []struct {
		name   string
		origin hq.RepoOrigin
		want   bool
	}{
		{name: "connected through the organisation's GitHub app", origin: flipOnGitHub, want: true},
		{name: "followed with a person's token", origin: hq.RepoOrigin{URL: flipOnGitHub.URL, Mode: hq.OriginFollowed}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lab := newHQLab(t)
			lab.wire()
			if report := lab.passReport(); strings.Contains(report, "mirrors it to GitHub") {
				t.Fatalf("a repository HQ alone keeps is said mirrored:\n%s", report)
			}
			lab.hq.keepOn(tt.origin)

			report := lab.passReport()

			if got := strings.Contains(report, said); got != tt.want {
				t.Fatalf("the pass says %q = %v, want %v:\n%s", said, got, tt.want, report)
			}
			if !tt.want {
				return
			}
			for _, want := range []string{
				`repository "appdev": `,
				"stop pushing to GitHub's default branch yourself",
				"drop any note of your own",
				"a merged change reaches GitHub as its pull request",
			} {
				if !strings.Contains(report, want) {
					t.Errorf("the pass misses %q:\n%s", want, report)
				}
			}
			if again := lab.passReport(); strings.Contains(again, "mirrors it to GitHub") {
				t.Errorf("the next pass says it again:\n%s", again)
			}
		})
	}
}

// TestHQPass_GitHubMirror_DivergenceSaidOnceWithTheRealignment: while
// neither GitHub's default branch nor HQ's `main` holds the other, HQ's
// follow moves neither and the repository's remote reads divergedSince. The
// pass says so once, with the one change that brings them back in step — a
// branch holding both, merged, squashed on GitHub, onto which HQ then moves
// `main` (mate-hq protocol, GitHub "Realignment") — in git commands, never a
// force or a rebase; in step again it says nothing, and a later divergence
// is said anew.
func TestHQPass_GitHubMirror_DivergenceSaidOnceWithTheRealignment(t *testing.T) {
	t.Parallel()
	lab := newHQLab(t)
	lab.wire()
	lab.hq.keepOn(flipOnGitHub)
	lab.passReport()
	lab.hq.diverge("appdev", "2026-10-28T10:00:00.000Z")

	report := lab.passReport()

	for _, want := range []string{
		`repository "appdev": `,
		"have diverged",
		"since 2026-10-28T10:00:00.000Z",
		"git remote set-head origin --auto",
		"+main:refs/remotes/hq/main",
		"git checkout -b realign-github origin/HEAD",
		"git merge refs/remotes/hq/main",
		"git push " + lab.hq.srv.URL + "/git/" + labOrg + "/" + labApp + "/appdev.git realign-github",
		`action="open-change" branch="realign-github"`,
		"squash",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("the pass misses %q:\n%s", want, report)
		}
	}
	for _, never := range []string{"--force", "rebase", "allow-unrelated-histories"} {
		if strings.Contains(report, never) {
			t.Errorf("the pass names %q:\n%s", never, report)
		}
	}
	if again := lab.passReport(); strings.Contains(again, "have diverged") {
		t.Errorf("the next pass says it again:\n%s", again)
	}
	lab.hq.diverge("appdev", "")
	if inStep := lab.passReport(); strings.Contains(inStep, "have diverged") {
		t.Errorf("a pass in step says they diverged:\n%s", inStep)
	}
	lab.hq.diverge("appdev", "2026-10-29T08:00:00.000Z")
	if later := lab.passReport(); !strings.Contains(later, "since 2026-10-29T08:00:00.000Z") {
		t.Errorf("a later divergence is not said:\n%s", later)
	}
}
