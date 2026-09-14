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

func TestAdoptBaseline_NotARepo_InitsCommitsAndTags(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{
		{err: errTest}, // test -d .git -> not a repo
		{},             // git init -b main
		{},             // seed exclude
		{},             // git add -A && commit -m "baseline: adopted appVersion <id>"
		{},             // git tag -f zcp/baseline/<id> HEAD
	}}
	alreadyRepo, err := AdoptBaseline(context.Background(), r, "/var/www", "av-1", topology.RuntimeDynamic)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if alreadyRepo {
		t.Error("alreadyRepo = true, want false (repo was created fresh)")
	}
	if len(r.calls) != 5 {
		t.Fatalf("calls = %d, want 5: %+v", len(r.calls), r.calls)
	}
	if !strings.Contains(r.calls[3].script, `commit -q -m 'baseline: adopted appVersion av-1'`) {
		t.Errorf("call[3] = %q, want the baseline commit message", r.calls[3].script)
	}
	if !strings.Contains(r.calls[4].script, "tag -f 'zcp/baseline/av-1' HEAD") {
		t.Errorf("call[4] = %q, want the baseline tag", r.calls[4].script)
	}
}

func TestAdoptBaseline_ExistingRepo_OnlyTagsHEAD(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{
		{},                         // test -d .git -> is a repo
		{stdout: "sha-existing\n"}, // git tag -f zcp/baseline/<id> HEAD (returns nothing meaningful; recorded call)
	}}
	alreadyRepo, err := AdoptBaseline(context.Background(), r, "/var/www", "av-2", topology.RuntimeDynamic)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !alreadyRepo {
		t.Error("alreadyRepo = false, want true (repo pre-existed)")
	}
	if len(r.calls) != 2 {
		t.Fatalf("calls = %d, want 2 (probe + tag only, no init/commit): %+v", len(r.calls), r.calls)
	}
	if !strings.Contains(r.calls[1].script, "tag -f 'zcp/baseline/av-2' HEAD") {
		t.Errorf("call[1] = %q, want the baseline tag", r.calls[1].script)
	}
}
