// Tests for: build_agents.go BuildClaudeWrapper + refresh_agents.go — in a
// zcp container every session opens in the workspace (/var/www), where zcp
// mounts each pair's dev half at /var/www/<host>, so the workspace CLAUDE.md
// imports each repository's own CLAUDE.md and Claude Code reads it at session
// start, not only once the agent reads a file in that repository
// (docs/spec-workflows.md §5.1).
package content

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/runtime"
)

// TestBuildClaudeWrapper_ImportsEachRepositorysCLAUDEMD: after zcp's own
// @AGENTS.md, one @<dir>/CLAUDE.md line per repository, sorted, each once —
// and nothing else, so the wrapper restates none of zcp's guidance. A
// directory that is not a plain relative path (empty, absolute, escaping the
// workspace, carrying a space, a quote, a `#` or an `@` that would end or
// change the import) gets no line: its CLAUDE.md loads on demand, as it did
// before.
func TestBuildClaudeWrapper_ImportsEachRepositorysCLAUDEMD(t *testing.T) {
	t.Parallel()
	for _, rt := range []runtime.Info{
		{InContainer: true, ServiceName: "zcp"},
		{InContainer: true, ServiceName: "zcp", MateEnabled: true},
	} {
		got := BuildClaudeWrapper(rt, []string{
			"botdev", "apidev", "botdev", "webdev/ws",
			"", "/etc", "../etc", "a/../../etc", "./botdev", "a b", `"q"`, "x#y", "@x", "x/", `a\b`,
		})
		want := "@AGENTS.md\n@apidev/CLAUDE.md\n@botdev/CLAUDE.md\n@webdev/ws/CLAUDE.md\n"
		if got != want {
			t.Errorf("BuildClaudeWrapper(%+v) =\n%s\nwant\n%s", rt, got, want)
		}
	}
}

// TestBuildClaudeWrapper_NoImportsOutsideAUserContainer: the imports render
// only where zcp mounts repositories under the session's working directory —
// a container — and never where the corpus keeps the single-project user
// surface out: an authoring context (there a codebase's CLAUDE.md is a
// content surface the run authors for porters, never the authoring agent's
// instructions — spec-content-surfaces Surface 6) and org mode (the Chief's
// project work is each project's Mate's — spec-mate §2.11). Locally the
// working directory is the repository itself.
func TestBuildClaudeWrapper_NoImportsOutsideAUserContainer(t *testing.T) {
	t.Parallel()
	repos := []string{"appdev", "botdev"}
	tests := []struct {
		name string
		rt   runtime.Info
	}{
		{name: "local", rt: runtime.Info{}},
		{name: "local authoring", rt: runtime.Info{Authoring: true}},
		{name: "container authoring", rt: runtime.Info{InContainer: true, ServiceName: "zcp", Authoring: true}},
		{name: "org mode", rt: runtime.Info{InContainer: true, ServiceName: "zcp", MateEnabled: true, Chief: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if WorkspaceImports(tt.rt) {
				t.Errorf("WorkspaceImports(%+v) = true, want false", tt.rt)
			}
			if got := BuildClaudeWrapper(tt.rt, repos); got != "@AGENTS.md\n" {
				t.Errorf("BuildClaudeWrapper = %q, want only zcp's own @AGENTS.md", got)
			}
		})
	}
	if got := BuildClaudeWrapper(runtime.Info{InContainer: true, ServiceName: "zcp"}, nil); got != "@AGENTS.md\n" {
		t.Errorf("a container with no pair: BuildClaudeWrapper = %q, want only @AGENTS.md", got)
	}
}

// TestRefreshAgentContext_ClaudeImports_FollowTheRepositories: the refresh
// rewrites CLAUDE.md's managed block to the repositories it is handed — a
// repository added gains its line, one removed loses it, the same list
// writes nothing — and every byte outside the block stays as the user wrote
// it.
func TestRefreshAgentContext_ClaudeImports_FollowTheRepositories(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	agentsPath := filepath.Join(dir, "AGENTS.md")
	claudePath := filepath.Join(dir, "CLAUDE.md")
	rt := runtime.Info{InContainer: true, ServiceName: "zcp", MateEnabled: true}

	body, err := BuildAgentsMD(rt, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agentsPath, []byte(wrapManagedBlock(body)), 0o644); err != nil {
		t.Fatal(err)
	}
	above := "# Team notes\n\nAlways answer in Czech.\n\n"
	below := "\n## Mine\n\n- keep @this line verbatim\n"
	if err := os.WriteFile(claudePath, []byte(above+wrapManagedBlock("@AGENTS.md\n")+below), 0o644); err != nil {
		t.Fatal(err)
	}

	steps := []struct {
		name        string
		repos       []string
		wantChanged bool
		wantBlock   string
	}{
		{name: "a pair", repos: []string{"botdev"}, wantChanged: true, wantBlock: "@AGENTS.md\n@botdev/CLAUDE.md\n"},
		{name: "a pair added", repos: []string{"botdev", "apidev"}, wantChanged: true, wantBlock: "@AGENTS.md\n@apidev/CLAUDE.md\n@botdev/CLAUDE.md\n"},
		{name: "a pair removed", repos: []string{"apidev"}, wantChanged: true, wantBlock: "@AGENTS.md\n@apidev/CLAUDE.md\n"},
		{name: "nothing moved", repos: []string{"apidev"}, wantBlock: "@AGENTS.md\n@apidev/CLAUDE.md\n"},
		{name: "the last pair removed", repos: nil, wantChanged: true, wantBlock: "@AGENTS.md\n"},
	}
	for _, step := range steps {
		agentsChanged, claudeChanged, err := RefreshAgentContext(agentsPath, claudePath, rt, false, step.repos)
		if err != nil {
			t.Fatalf("%s: RefreshAgentContext: %v", step.name, err)
		}
		if agentsChanged {
			t.Errorf("%s: AGENTS.md was rewritten; the repositories are CLAUDE.md's alone", step.name)
		}
		if claudeChanged != step.wantChanged {
			t.Errorf("%s: CLAUDE.md changed = %v, want %v", step.name, claudeChanged, step.wantChanged)
		}
		raw, err := os.ReadFile(claudePath)
		if err != nil {
			t.Fatal(err)
		}
		got := string(raw)
		if want := above + wrapManagedBlock(step.wantBlock) + below; got != want {
			t.Errorf("%s: CLAUDE.md =\n%s\nwant\n%s", step.name, got, want)
		}
	}
	agents, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(agents), "/CLAUDE.md\n") {
		t.Errorf("AGENTS.md carries a repository import; only CLAUDE.md may:\n%s", agents)
	}
}
