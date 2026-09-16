// Tests for: ops/git/preflight.go — the self-deploy GF-12 preflight
// (docs/spec-workflows.md §8 DM, §12.6): one combined script, parsed into
// SelfDeployPreflight.
package git

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestReadSelfDeployPreflight_CleanRepo_ParsesShaDirtyState(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{{stdout: "ZCP:GITFILE:0\nZCP:SUBMODULES:0\nZCP:HASREPO:1\nZCP:SHA:abc123\nZCP:DIRTY:0\nZCP:REPOSTATE:clean\n"}}}
	got, err := ReadSelfDeployPreflight(context.Background(), r, "/var/www")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.HasRepo || got.SHA != "abc123" || got.Dirty || got.RepoState != "clean" {
		t.Errorf("got = %+v, want HasRepo=true SHA=abc123 Dirty=false RepoState=clean", got)
	}
	if len(r.calls) != 1 {
		t.Errorf("calls = %d, want 1 (single combined round trip)", len(r.calls))
	}
}

func TestReadSelfDeployPreflight_NoRepo_ReturnsHasRepoFalse(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{{stdout: "ZCP:GITFILE:0\nZCP:SUBMODULES:0\nZCP:HASREPO:0\n"}}}
	got, err := ReadSelfDeployPreflight(context.Background(), r, "/var/www")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.HasRepo {
		t.Error("HasRepo = true, want false")
	}
}

func TestReadSelfDeployPreflight_GitFile_ParsesRegardlessOfRepo(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{{stdout: "ZCP:GITFILE:1\nZCP:SUBMODULES:0\nZCP:HASREPO:0\n"}}}
	got, err := ReadSelfDeployPreflight(context.Background(), r, "/var/www")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.GitIsFile {
		t.Error("GitIsFile = false, want true")
	}
}

func TestReadSelfDeployPreflight_Submodules_Parsed(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{{stdout: "ZCP:GITFILE:0\nZCP:SUBMODULES:1\nZCP:HASREPO:1\nZCP:SHA:abc123\nZCP:DIRTY:0\nZCP:REPOSTATE:clean\n"}}}
	got, err := ReadSelfDeployPreflight(context.Background(), r, "/var/www")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.HasSubmodules {
		t.Error("HasSubmodules = false, want true")
	}
}

func TestReadSelfDeployPreflight_IgnoredPaths_CountsBytesAndSample(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{{stdout: "" +
		"ZCP:GITFILE:0\nZCP:SUBMODULES:0\nZCP:HASREPO:1\nZCP:SHA:abc123\nZCP:DIRTY:1\nZCP:REPOSTATE:dirty\n" +
		"ZCP:IGNORED:10:a.log\n" +
		"ZCP:IGNORED:20:b/c.tmp\n",
	}}}
	got, err := ReadSelfDeployPreflight(context.Background(), r, "/var/www")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.NotCarried.Count != 2 {
		t.Errorf("NotCarried.Count = %d, want 2", got.NotCarried.Count)
	}
	if got.NotCarried.Bytes != 30 {
		t.Errorf("NotCarried.Bytes = %d, want 30", got.NotCarried.Bytes)
	}
	if len(got.NotCarried.Sample) != 2 || got.NotCarried.Sample[0] != "a.log" || got.NotCarried.Sample[1] != "b/c.tmp" {
		t.Errorf("NotCarried.Sample = %v, want [a.log b/c.tmp]", got.NotCarried.Sample)
	}
}

// TestReadSelfDeployPreflight_IgnoredPaths_SampleCapsAtTen pins the
// spec-mate.md §1 reducer rule: the sample stays small even when many
// paths are ignored — Count/Bytes still cover the full set.
func TestReadSelfDeployPreflight_IgnoredPaths_SampleCapsAtTen(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("ZCP:GITFILE:0\nZCP:SUBMODULES:0\nZCP:HASREPO:1\nZCP:SHA:abc123\nZCP:DIRTY:1\nZCP:REPOSTATE:dirty\n")
	for i := range 15 {
		sb.WriteString("ZCP:IGNORED:1:file" + string(rune('a'+i)) + "\n")
	}
	r := &fakeRunner{results: []fakeResult{{stdout: sb.String()}}}
	got, err := ReadSelfDeployPreflight(context.Background(), r, "/var/www")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.NotCarried.Count != 15 {
		t.Errorf("NotCarried.Count = %d, want 15", got.NotCarried.Count)
	}
	if got.NotCarried.Bytes != 15 {
		t.Errorf("NotCarried.Bytes = %d, want 15", got.NotCarried.Bytes)
	}
	if len(got.NotCarried.Sample) != notCarriedSampleLimit {
		t.Errorf("len(Sample) = %d, want %d", len(got.NotCarried.Sample), notCarriedSampleLimit)
	}
}

func TestReadSelfDeployPreflight_EnvFiles_Parsed(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{{stdout: "" +
		"ZCP:GITFILE:0\nZCP:SUBMODULES:0\nZCP:HASREPO:1\nZCP:SHA:abc123\nZCP:DIRTY:0\nZCP:REPOSTATE:clean\n" +
		"ZCP:ENVFILE:.env\n" +
		"ZCP:ENVFILE:.env.local\n",
	}}}
	got, err := ReadSelfDeployPreflight(context.Background(), r, "/var/www")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.EnvFiles) != 2 || got.EnvFiles[0] != ".env" || got.EnvFiles[1] != ".env.local" {
		t.Errorf("EnvFiles = %v, want [.env .env.local]", got.EnvFiles)
	}
}

// TestReadSelfDeployPreflight_TransportError_NeverFails pins "facts,
// never a gate": a transport error (or a source with no repo yet) comes
// back as a zero-value read, err=nil — the same non-fatal-absence
// contract as HeadStatus. A real push failure still surfaces separately.
func TestReadSelfDeployPreflight_TransportError_NeverFails(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{{err: errors.New("ssh connection refused")}}}
	got, err := ReadSelfDeployPreflight(context.Background(), r, "/var/www")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.HasRepo || got.GitIsFile || got.HasSubmodules {
		t.Errorf("got = %+v, want zero-value", got)
	}
}

func TestReadHeadAndState_ResolvesShaAndState(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{{stdout: "abc123\nSTATE:clean\n"}}}
	sha, state, ok, err := ReadHeadAndState(context.Background(), r, "/var/www")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok || sha != "abc123" || state != "clean" {
		t.Errorf("got sha=%q state=%q ok=%v, want abc123/clean/true", sha, state, ok)
	}
	if len(r.calls) != 1 {
		t.Errorf("calls = %d, want 1 (single combined round trip)", len(r.calls))
	}
}

func TestReadHeadAndState_NoRepo_ReturnsNotOkNoError(t *testing.T) {
	r := &fakeRunner{results: []fakeResult{{err: errors.New("exit 128: not a git repository")}}}
	sha, state, ok, err := ReadHeadAndState(context.Background(), r, "/var/www")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok || sha != "" || state != "" {
		t.Errorf("got sha=%q state=%q ok=%v, want empty/empty/false", sha, state, ok)
	}
}
