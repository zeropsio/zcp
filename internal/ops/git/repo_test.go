// Tests for: ops/git/repo.go — the repo-always guarantee run at adopt
// (baseline tag), driven through the Runner abstraction so it works
// identically over SSH (container) and locally (docs/spec-workflows.md's
// Git Lifecycle section, GLC-7).
package git

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

func TestExcludePatterns_Dynamic_IncludesNodeModulesAndBuildOutputs(t *testing.T) {
	got := ExcludePatterns(topology.RuntimeDynamic)
	for _, want := range []string{"node_modules/", "dist/", "build/", ".env", "*.log", ".zcp/"} {
		if !slices.Contains(got, want) {
			t.Errorf("ExcludePatterns(dynamic) = %v, want it to contain %q", got, want)
		}
	}
}

func TestExcludePatterns_Managed_OmitsBuildOutputs(t *testing.T) {
	got := ExcludePatterns(topology.RuntimeManaged)
	for _, unwanted := range []string{"node_modules/", "dist/", "build/"} {
		if slices.Contains(got, unwanted) {
			t.Errorf("ExcludePatterns(managed) = %v, want it NOT to contain %q", got, unwanted)
		}
	}
}

func TestSeedExclude_WritesInfoExcludeWithPatterns(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{{}}}
	if err := SeedExclude(context.Background(), r, "/repo", topology.RuntimeDynamic); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(r.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(r.calls))
	}
	script := r.calls[0].script
	if !strings.Contains(script, ".git/info/exclude") {
		t.Errorf("script = %q, want it to write .git/info/exclude", script)
	}
	if !strings.Contains(script, "node_modules/") {
		t.Errorf("script = %q, want node_modules/ pattern", script)
	}
}

// TestAdoptBaseline_NoRepo_InitsSnapshotsAndTags pins the probe=1 case (no
// repo at all): git init runs (probe=1 skips the tree check entirely —
// exists is already false), then seed/add/commit/tag.
func TestAdoptBaseline_NoRepo_InitsSnapshotsAndTags(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{
		{err: errTest}, // test -d .git -> not a repo
		{},             // git init -b main
		{},             // seed exclude
		{},             // git add -A
		{},             // git -c ... commit -q -m "zcp: snapshot ..."
		{},             // git tag -f zcp/baseline/<id> HEAD
	}}
	result, err := AdoptBaseline(context.Background(), r, "/var/www", "av-1", topology.RuntimeDynamic)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Case != AdoptCaseSnapshot {
		t.Errorf("Case = %q, want %q", result.Case, AdoptCaseSnapshot)
	}
	if result.EmptyCommit {
		t.Error("EmptyCommit = true, want false (the normal commit succeeded)")
	}
	if len(r.calls) != 6 {
		t.Fatalf("calls = %d, want 6: %+v", len(r.calls), r.calls)
	}
	if r.calls[1].script != "git init -q -b main" {
		t.Errorf("call[1] = %q, want git init", r.calls[1].script)
	}
	if r.calls[3].script != "git add -A" {
		t.Errorf("call[3] = %q, want git add -A", r.calls[3].script)
	}
	wantCommit := `git -c user.name='Zerops Agent' -c user.email='agent@zerops.io' commit -q -m 'zcp: snapshot of /var/www as found at adopt (appVersion av-1)'`
	if r.calls[4].script != wantCommit {
		t.Errorf("call[4] = %q, want %q", r.calls[4].script, wantCommit)
	}
	if !strings.Contains(r.calls[5].script, "tag -f 'zcp/baseline/av-1' HEAD") {
		t.Errorf("call[5] = %q, want the baseline tag", r.calls[5].script)
	}
}

// TestAdoptBaseline_EmptyTreeHEAD_SkipsInitButStillSnapshots pins the
// probe=2 case (docs/spec-workflows.md's Git Lifecycle section, GLC-7): a
// repo that already exists (ops.InitServiceGit's GLC-1 marker commit ran
// first) but whose HEAD is over the empty tree must NOT be trusted as
// content — no git init (the repo already exists), but the same
// seed/add/commit/tag sequence as the no-repo case.
func TestAdoptBaseline_EmptyTreeHEAD_SkipsInitButStillSnapshots(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{
		{}, // test -d .git -> is a repo
		{stdout: "4b825dc642cb6eb9a060e54bf8d69288fbee4904\n"}, // rev-parse HEAD^{tree} -> empty tree
		{}, // seed exclude
		{}, // git add -A
		{}, // git -c ... commit -q -m "zcp: snapshot ..."
		{}, // git tag -f zcp/baseline/<id> HEAD
	}}
	result, err := AdoptBaseline(context.Background(), r, "/var/www", "av-2", topology.RuntimeDynamic)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Case != AdoptCaseSnapshot {
		t.Errorf("Case = %q, want %q", result.Case, AdoptCaseSnapshot)
	}
	if len(r.calls) != 6 {
		t.Fatalf("calls = %d, want 6 (no git init call): %+v", len(r.calls), r.calls)
	}
	for _, c := range r.calls {
		if c.script == "git init -q -b main" {
			t.Errorf("calls = %+v, must not init — the repo already exists", r.calls)
		}
	}
	if r.calls[1].script != `git rev-parse --verify 'HEAD^{tree}'` {
		t.Errorf("call[1] = %q, want the tree probe", r.calls[1].script)
	}
	wantCommit := `git -c user.name='Zerops Agent' -c user.email='agent@zerops.io' commit -q -m 'zcp: snapshot of /var/www as found at adopt (appVersion av-2)'`
	if r.calls[4].script != wantCommit {
		t.Errorf("call[4] = %q, want %q", r.calls[4].script, wantCommit)
	}
	if !strings.Contains(r.calls[5].script, "tag -f 'zcp/baseline/av-2' HEAD") {
		t.Errorf("call[5] = %q, want the baseline tag", r.calls[5].script)
	}
}

// TestAdoptBaseline_ContentHEAD_OnlyTagsHEAD pins the probe=3 case: a HEAD
// whose tree already carries content is trusted as-is — only the tag
// moves, no commit, no history change.
func TestAdoptBaseline_ContentHEAD_OnlyTagsHEAD(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{
		{},                          // test -d .git -> is a repo
		{stdout: "tree-sha-real\n"}, // rev-parse HEAD^{tree} -> real content
		{},                          // git tag -f zcp/baseline/<id> HEAD
	}}
	result, err := AdoptBaseline(context.Background(), r, "/var/www", "av-3", topology.RuntimeDynamic)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Case != AdoptCaseExisting {
		t.Errorf("Case = %q, want %q", result.Case, AdoptCaseExisting)
	}
	if len(r.calls) != 3 {
		t.Fatalf("calls = %d, want 3 (probe + tree probe + tag only, no init/seed/commit): %+v", len(r.calls), r.calls)
	}
	if !strings.Contains(r.calls[2].script, "tag -f 'zcp/baseline/av-3' HEAD") {
		t.Errorf("call[2] = %q, want the baseline tag", r.calls[2].script)
	}
}

// TestAdoptBaseline_NothingStaged_CommitsWithAllowEmpty pins the empty-
// working-tree edge case: `git add -A` stages nothing (a genuinely empty
// directory), so the normal commit fails with "nothing to commit" and
// AdoptBaseline falls back to --allow-empty so the tag still lands and the
// case is recorded via AdoptResult.EmptyCommit.
func TestAdoptBaseline_NothingStaged_CommitsWithAllowEmpty(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{
		{err: errTest}, // test -d .git -> not a repo
		{},             // git init -b main
		{},             // seed exclude
		{},             // git add -A (nothing to stage)
		{err: errTest}, // git -c ... commit -q -m "..." -> fails, nothing staged
		{},             // git -c ... commit -q --allow-empty -m "..."
		{},             // git tag -f zcp/baseline/<id> HEAD
	}}
	result, err := AdoptBaseline(context.Background(), r, "/var/www", "av-4", topology.RuntimeDynamic)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Case != AdoptCaseSnapshot {
		t.Errorf("Case = %q, want %q", result.Case, AdoptCaseSnapshot)
	}
	if !result.EmptyCommit {
		t.Error("EmptyCommit = false, want true (the normal commit failed, fell back to --allow-empty)")
	}
	if len(r.calls) != 7 {
		t.Fatalf("calls = %d, want 7: %+v", len(r.calls), r.calls)
	}
	wantEmptyCommit := `git -c user.name='Zerops Agent' -c user.email='agent@zerops.io' commit -q --allow-empty -m 'zcp: snapshot of /var/www as found at adopt (appVersion av-4)'`
	if r.calls[5].script != wantEmptyCommit {
		t.Errorf("call[5] = %q, want %q", r.calls[5].script, wantEmptyCommit)
	}
}
