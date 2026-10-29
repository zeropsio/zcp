package content

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// agentGitSources are the Go sources whose string literals carry the git
// commands zcp gives a Mate's agent, relative to this package.
var agentGitSources = []string{
	"../tools/hq_*.go", "../tools/origin*.go", "../tools/deploy_git_push.go", "../tools/workflow_pool_migrate.go",
	"../ops/hq_*.go", "../hq/*.go", "../../cmd/zcp/*.go",
}

// agentGitSourceAllowed are the literals that run git on purpose where
// AgentGitViolations would refuse it, by source file: each of a declaration
// ("*" for every one of the file) holding piece ("" for any), with why. A
// declaration that also gives agent-facing words names its piece, so the
// allow never hides those words.
var agentGitSourceAllowed = map[string][]agentGitAllow{
	"internal/ops/hq_base_branch.go": {
		{"baseBranchCommand", "", "zcp's own builder, executed in the dev service over ssh — no agent runs it"},
		{"KeepOwnHistory", "", "the remedy's form for a project without a Mate, whose git stays on its mounted checkout (owner decision Q1, docs/spec-mate.md §6.7)"},
	},
	"internal/ops/hq_main_sync.go": {{"BuildMainSyncCommand", "", "zcp's own builder, executed in the dev service over ssh — no agent runs it"}},
	"internal/ops/hq_project.go":   {{"*", "", "the plain-project door's builders, executed in the dev service over ssh — no agent runs them"}},
	"internal/hq/gitcredential.go": {
		{"GitHubHelperConfig", "", "the persisted github.com helper's writer, and the mount-fallback refusal that names it, kept for sessions on the old guidance; no new guidance names it"},
	},
	"internal/tools/hq_origin_kept.go": {{"hqPairOrigin", "git remote get-url", "zcp's own command pointing the checkout's origin, executed in the dev service over ssh"}},
	"internal/tools/hq_recipe_edit.go": {{"*", "", "a zcp-side fresh clone of the group recipe repository — no mount, no dev repository, no code the repository controls"}},
	"internal/tools/hq_project.go":     {{"*", "", "a project without a Mate keeps `zcp hq git` on its mounted checkout (owner decision Q1, docs/spec-mate.md §6.7)"}},
	"internal/tools/hq_repo_host.go":   {{"repoRoute.git", "`ssh %s", "the lease form: ssh to the lease's alias, its checkout /var/www"}},
	"internal/tools/deploy_git_push.go": {
		{"gitPushMetaPreflight", "over ssh to the lease's alias", "a lease pushes over ssh to its alias"},
		{"leaseGitPushRefusal", "over ssh to %s yourself", "a lease pushes over ssh to its alias"},
		{"committedCodeCheckCmd", "", "zcp's own probe, executed in the dev service over ssh"},
		{"handleGitPush", "git add -A && git commit", "a git-push pair's own remote and token: its commit runs over ssh in its container, never on the mount"},
	},
}

// agentGitAllow is one entry of agentGitSourceAllowed.
type agentGitAllow struct{ decl, piece, why string }

// agentGitSurfaces are the embedded texts that tell a Mate's agent how to
// run git for its dev repositories, each from the marker its Mate's part
// starts at ("" for the whole text): every git command there runs in the
// dev service. Elsewhere a text may name a git command in passing — what a
// status reads, a local dev machine's git — and only the phrases and
// commands that run git on the mount are refused. pool-migrate-move.md is no
// surface: it renders for a project without a Mate too, whose table of git
// commands stays as it was (owner decision Q1), its Mate's form a paragraph
// of its own (agentGitOutsideAMate).
var agentGitSurfaces = map[string]string{
	"templates/agents_git_host.md":                "",
	"atoms/develop-git-push-start-from-remote.md": "Wired to this Mate's HQ",
	"atoms/develop-git-push-delivery.md":          "",
}

// agentGitContentAllowed are the lines of embedded texts that run git where
// AgentGitViolations would refuse it, by file and a piece of the line, each
// with why.
var agentGitContentAllowed = map[string]map[string]string{
	"atoms/develop-git-push-delivery.md": {
		"`git -C <workingDir> add -A && git commit -m '<msg>'` on a dev machine": "local mode: git in the dev machine's own working directory, no mount",
		"never run `git push --force`":                                           "names the force-push it forbids",
	},
}

// TestNoMountGitInAgentContent: no source literal zcp gives a Mate's agent,
// and no embedded atom or template, tells it to run git on the sshfs mount,
// in its own shell, in a zcp-side worktree or a substitution its shell works
// out; a worktree is the dev service's below /tmp/wt-, never forced or
// pruned; index.lock is never deleted; a substitution of `zcp git` is an
// assignment chained with && (docs/spec-mate.md §6.8). What runs git so on
// purpose is allowed by name, with why.
func TestNoMountGitInAgentContent(t *testing.T) {
	t.Parallel()
	used := map[string]bool{}
	var found []string
	for _, pattern := range agentGitSources {
		files, err := filepath.Glob(pattern)
		if err != nil || len(files) == 0 {
			t.Fatalf("no source matches %s: %v", pattern, err)
		}
		for _, file := range files {
			if !strings.HasSuffix(file, "_test.go") {
				found = append(found, agentGitLiteralViolations(t, file, used)...)
			}
		}
	}
	found = append(found, agentGitContentViolations(t, used)...)
	// An entry that allows nothing any more goes: the allow-lists only shrink.
	for file, allows := range agentGitSourceAllowed {
		for _, allow := range allows {
			if !used[file+"#"+allow.decl+"#"+allow.piece] {
				t.Errorf("agentGitSourceAllowed[%q] %s %q allows nothing any more: remove it", file, allow.decl, allow.piece)
			}
		}
	}
	for file, pieces := range agentGitContentAllowed {
		for piece := range pieces {
			if !used[file+"#"+piece] {
				t.Errorf("agentGitContentAllowed[%q][%q] allows nothing any more: remove it", file, piece)
			}
		}
	}
	for _, f := range found {
		t.Error(f)
	}
	if len(found) > 0 {
		t.Logf("%d agent-facing git texts run git outside the dev service", len(found))
	}
}

// agentGitLiteralViolations is each string literal of file AgentGitViolations
// refuses, outside the declarations agentGitSourceAllowed names; used marks
// each entry that allowed one.
func agentGitLiteralViolations(t *testing.T, file string, used map[string]bool) []string {
	t.Helper()
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	name := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(file)), "../")
	name = strings.TrimPrefix(name, "../")
	if !strings.HasPrefix(name, "cmd/") {
		name = "internal/" + name
	}
	var out []string
	for _, decl := range parsed.Decls {
		declName := declarationName(decl)
		ast.Inspect(decl, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			violations := AgentGitViolations(value)
			if len(violations) == 0 {
				return true
			}
			for _, allow := range agentGitSourceAllowed[name] {
				if (allow.decl == "*" || allow.decl == declName) && strings.Contains(value, allow.piece) {
					used[name+"#"+allow.decl+"#"+allow.piece] = true
					return true
				}
			}
			for _, v := range violations {
				out = append(out, name+":"+strconv.Itoa(fset.Position(lit.Pos()).Line)+" ("+declName+"): "+v)
			}
			return true
		})
	}
	return out
}

// declarationName is a top-level declaration's name: a function's (a
// method's as Type.Method), else its first value or type spec's.
func declarationName(decl ast.Decl) string {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if d.Recv != nil && len(d.Recv.List) > 0 {
			recv := d.Recv.List[0].Type
			if star, ok := recv.(*ast.StarExpr); ok {
				recv = star.X
			}
			if id, ok := recv.(*ast.Ident); ok {
				return id.Name + "." + d.Name.Name
			}
		}
		return d.Name.Name
	case *ast.GenDecl:
		for _, spec := range d.Specs {
			switch s := spec.(type) {
			case *ast.ValueSpec:
				if len(s.Names) > 0 {
					return s.Names[0].Name
				}
			case *ast.TypeSpec:
				return s.Name.Name
			}
		}
	}
	return ""
}

// agentGitContentViolations is each line of an embedded atom or template
// AgentGitViolations refuses — outside a Mate's git surfaces only the phrases
// and commands that run git on the mount — but the lines
// agentGitContentAllowed names; used marks each entry that allowed one.
func agentGitContentViolations(t *testing.T, used map[string]bool) []string {
	t.Helper()
	texts := map[string]string{}
	atoms, err := ReadAllAtoms()
	if err != nil {
		t.Fatalf("ReadAllAtoms: %v", err)
	}
	for _, a := range atoms {
		texts["atoms/"+a.Name] = a.Content
	}
	if err := fs.WalkDir(templateFS, "templates", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, err := fs.ReadFile(templateFS, path)
		texts[path] = string(body)
		return err
	}); err != nil {
		t.Fatalf("read templates: %v", err)
	}
	for file := range agentGitSurfaces {
		if _, ok := texts[file]; !ok {
			t.Errorf("agentGitSurfaces names %s, which no atom or template is", file)
		}
	}
	var out []string
	for _, file := range slices.Sorted(maps.Keys(texts)) {
		body := texts[file]
		marker, surface := agentGitSurfaces[file]
		for i, line := range strings.Split(body, "\n") {
			for _, v := range AgentGitViolations(line) {
				if strings.HasPrefix(v, "git run where no dev service is named") && (!surface || (marker != "" && !strings.Contains(body[:lineOffset(body, i)], marker))) {
					continue
				}
				if piece, ok := contentAllowed(file, line); ok {
					used[file+"#"+piece] = true
					continue
				}
				out = append(out, file+":"+strconv.Itoa(i+1)+": "+v)
			}
		}
	}
	return out
}

// contentAllowed is the piece of line by which agentGitContentAllowed names
// it in file, if it does.
func contentAllowed(file, line string) (string, bool) {
	for piece := range agentGitContentAllowed[file] {
		if strings.Contains(line, piece) {
			return piece, true
		}
	}
	return "", false
}

// lineOffset is where line i of body ends.
func lineOffset(body string, i int) int {
	offset := 0
	for range i + 1 {
		next := strings.IndexByte(body[offset:], '\n')
		if next < 0 {
			return len(body)
		}
		offset += next + 1
	}
	return offset
}

// TestAgentGitViolations_KnownTexts: the rule refuses each way a text runs git
// outside a dev service and passes each that runs it there — so a lint that
// finds nothing has looked.
func TestAgentGitViolations_KnownTexts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		text    string
		refused bool
	}{
		{"run it in your own shell on the mounted checkout", true},
		{"fetch it in the mounted checkout", true},
		{"never on the mounted checkout `/var/www/appdev`", false},
		{"in your own shell, never over ssh: `git fetch origin`", true},
		{"`cd /var/www/appdev && git fetch origin`", true},
		{"`git push origin fix-login`", true},
		{"`zcp git appdev fetch origin && zcp git appdev push origin fix-login`", false},
		{"`ssh webpool-3 \"cd /var/www && git fetch origin && git merge origin/main\"`", false},
		{"`zcp git appdev worktree add /tmp/fix-login fix-login`", true},
		{"`zcp git appdev worktree add --detach /tmp/wt-fix-login origin/fix-login`", false},
		{"`zcp git appdev worktree add -f /tmp/wt-x origin/x`", true},
		{"`zcp git appdev worktree prune`", true},
		{"delete .git/index.lock and try again", true},
		{"its index.lock clears when its git ends", false},
		{"`git branch -f \"own-history-$(git rev-parse --short=12 HEAD)\"`", true},
		{"`if [ -z \"$(zcp git appdev ls-tree HEAD)\" ]; then zcp git appdev reset --keep FETCH_HEAD; fi`", true},
		{"`t=$(zcp git appdev ls-tree HEAD); if [ -z \"$t\" ]; then :; fi`", true},
		{"`t=$(zcp git appdev ls-tree HEAD) && if [ -z \"$t\" ]; then zcp git appdev reset --keep FETCH_HEAD; fi`", false},
		{"git in appdev's sessions cannot reach HQ", false},
	}
	for _, tt := range tests {
		if got := AgentGitViolations(tt.text); (len(got) > 0) != tt.refused {
			t.Errorf("AgentGitViolations(%q) = %q, want refused %v", tt.text, got, tt.refused)
		}
	}
}

// agentGitOutsideAMate are the embedded texts that also render for a project
// without a Mate, whose git stays on its mounted checkout where `zcp git`
// cannot authenticate (owner decision Q1, docs/spec-mate.md §6.7): each keeps
// the commands it gave before `zcp git` byte for byte (plain), and gives a
// Mate its `zcp git` form in a paragraph of its own that says it is a Mate's
// (mate) — the atoms have no axis that renders a part for a Mate alone. A
// text with a Mate's part ("Wired to this Mate's HQ") is checked before it.
var agentGitOutsideAMate = []struct {
	file  string
	plain []string
	mate  []string
}{
	{
		file:  "atoms/develop-git-push-delivery.md",
		plain: []string{"The call refuses an empty tree or uncommitted changes — commit first (`ssh <host> \"cd <repo dir> && git add -A && git commit -m '<msg>'\"` for the runtime container (`/var/www`, or the code volume's `mountPath`) `git -C <workingDir> add -A && git commit -m '<msg>'` on a dev machine). It pushes HEAD to the configured remote and then follows the integration build on the build target until it settles."},
		mate:  []string{"`zcp git <host> add -A && zcp git <host> commit -m '<msg>'`"},
	},
	{
		file: "atoms/pool-migrate-move.md",
		plain: []string{
			"Each item the census names, and how to keep it (you commit and push; the\ncall commits nothing):\n\n| census |",
			"| `trees[].detachedCommits` | commits only a detached HEAD holds | `git switch -c <branch>` in that checkout, then push it |",
			"| `branches[]` | a local branch's commits on no remote branch | `git push -u origin <branch>` |",
			"| `stashes[]` | stash entries | `git stash branch <branch> <ref>` with the entry's `ref`, then commit and push; drop the ones you do not keep |",
			"| `reflogOnly` | commits only a reflog still reaches: reset, amended or rebased away | the retire's bundle keeps each one, so they never hold it; `git branch <branch> <commit>` and push the ones you want on the origin |",
		},
		mate: []string{"`zcp git <dev> <git args>`", "-C <its path>", "never on its mount", "`zcp git <dev> branch <branch> <head>`"},
	},
	{
		file: "atoms/develop-git-push-start-from-remote.md",
		plain: []string{
			"ssh <push-source-host> \"cd /var/www && git fetch origin && git checkout <default-branch> && git reset --hard origin/<default-branch>\"",
			"Read the default branch from the remote rather than assuming `main` — `git remote show origin` names it, and a repo created before that convention may still be on `master`. Uncommitted changes in the working copy are the one thing this destroys, so look before resetting (`git status --short`).",
		},
		mate: []string{"`zcp git <push-source-host> remote show origin`", "`zcp git <push-source-host> status --short`"},
	},
}

// TestAgentGitContent_OutsideAMateKeepsItsCommands: a text that renders for a
// project without a Mate too keeps the commands that project runs, as they
// were before `zcp git`, and names `zcp git` only in a paragraph that says it
// is a Mate's.
func TestAgentGitContent_OutsideAMateKeepsItsCommands(t *testing.T) {
	t.Parallel()
	atoms, err := ReadAllAtoms()
	if err != nil {
		t.Fatalf("ReadAllAtoms: %v", err)
	}
	bodies := map[string]string{}
	for _, a := range atoms {
		bodies["atoms/"+a.Name] = a.Content
	}
	for _, tt := range agentGitOutsideAMate {
		body, ok := bodies[tt.file]
		if !ok {
			t.Errorf("no atom %s", tt.file)
			continue
		}
		body, _, _ = strings.Cut(body, "Wired to this Mate's HQ")
		for _, want := range tt.plain {
			if !strings.Contains(body, want) {
				t.Errorf("%s no longer gives a project without a Mate %q", tt.file, want)
			}
		}
		for _, want := range tt.mate {
			if !strings.Contains(body, want) {
				t.Errorf("%s gives a Mate no %q", tt.file, want)
			}
		}
		for paragraph := range strings.SplitSeq(body, "\n\n") {
			if strings.Contains(paragraph, "zcp git") && !strings.Contains(paragraph, "In a Mate") {
				t.Errorf("%s names zcp git where a project without a Mate reads it as its own:\n%s", tt.file, paragraph)
			}
		}
	}
}
