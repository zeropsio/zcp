package content

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// legacyTypeFormExemptDirs are agentFacingMarkdownDirs subtrees that quote
// upstream recipe/theme text VERBATIM as an authoring reference rather than
// composing ZCP's own agent-facing prose — the composite-identifier rewrite
// (this test's gate) does not apply to them.
var legacyTypeFormExemptDirs = []string{
	"internal/authoring/recipe/content/",
	"internal/knowledge/themes/refinement-references/",
}

// legacyOSFieldRe matches the deprecated `os:` sibling field zerops.yaml
// still accepts alongside `build.base`/`run.base` (the schema marks it
// deprecated — "use base attribute").
var legacyOSFieldRe = regexp.MustCompile(`^\s*os:\s*(alpine|ubuntu)\b`)

// legacyModeFieldRe matches the deprecated `mode:` sibling field import.yaml
// still accepts alongside a managed service's `:single`/`:ha` type variant.
var legacyModeFieldRe = regexp.MustCompile(`\bmode:\s*(HA|NON_HA)\b`)

// bareTypeKeyRe finds a yaml-ish `base:`/`type:` key and captures the token
// that follows (through an optional opening backtick, stopping before
// whitespace or a closing backtick).
var bareTypeKeyRe = regexp.MustCompile("\\b(?:base|type)\"?:\\s*[`\"]?([^\\s`\"]+)")

// runtimeTechAtRe matches a runtime tech immediately followed by `@` at the
// start of a captured token, optionally preceded by a `alpine/`/`ubuntu/` OS
// prefix. Go's RE2 has no lookbehind, so the OS prefix is captured (group 1)
// instead — an empty group 1 means the token had no OS prefix, i.e. it is the
// deprecated bare form.
var runtimeTechAtRe = regexp.MustCompile(`^(alpine/|ubuntu/)?(nodejs|bun|deno|go|golang|python|php|php-nginx|php-apache|nginx|rust|java|dotnet|elixir|gleam|ruby)@`)

// TestNoLegacyTypeFormInAgentContent pins the OS-axis migration
// (bootstrap-provision-rules.md "OS variant" / "Legacy forms" paragraphs):
// agent-facing markdown must present a runtime identifier in composite form
// (`<os>/<tech>@<ver>`) and a managed identifier as `<tech>:single|ha@<ver>`,
// never the deprecated `os:`/`mode:` sibling fields or a bare runtime
// base/type — EXCEPT inside a block labelled "Legacy", the one place old YAML
// forms are documented as an equivalence table rather than authored live.
func TestNoLegacyTypeFormInAgentContent(t *testing.T) {
	t.Parallel()

	repoRoot := findRepoRoot(t)
	files := gitTrackedFiles(t, repoRoot)

	type violation struct {
		File    string
		Line    int
		Snippet string
	}
	var violations []violation

	for _, rel := range files {
		if !strings.HasSuffix(rel, ".md") || !underAnyDir(rel, agentFacingMarkdownDirs) {
			continue
		}
		if underAnyDir(rel, legacyTypeFormExemptDirs) {
			continue
		}
		body, err := os.ReadFile(filepath.Join(repoRoot, rel))
		if err != nil || !looksLikeText(body) {
			continue
		}
		lines := strings.Split(string(body), "\n")
		for i, raw := range lines {
			if !lineIsLegacyTypeForm(raw) {
				continue
			}
			if legacyExemptByContext(lines, i) {
				continue
			}
			violations = append(violations, violation{rel, i + 1, trim(strings.TrimSpace(raw), 140)})
		}
	}

	if len(violations) == 0 {
		return
	}
	sort.Slice(violations, func(i, j int) bool {
		if violations[i].File != violations[j].File {
			return violations[i].File < violations[j].File
		}
		return violations[i].Line < violations[j].Line
	})
	var msg strings.Builder
	msg.WriteString("agent-facing content must use the composite identifier forms (`<os>/<tech>@<ver>`, `<tech>:single|ha@<ver>`); deprecated `os:` / `mode:` and bare runtime bases may appear only inside a block labelled Legacy:\n")
	for _, v := range violations {
		msg.WriteString("  " + v.File + ":" + itoa(v.Line) + "\n      " + v.Snippet + "\n")
	}
	t.Error(msg.String())
}

// lineIsLegacyTypeForm reports whether a single line carries a deprecated
// `os:`/`mode:` sibling field or a bare (non-OS-prefixed) runtime base/type.
func lineIsLegacyTypeForm(line string) bool {
	if legacyOSFieldRe.MatchString(line) {
		return true
	}
	if legacyModeFieldRe.MatchString(line) {
		return true
	}
	for _, m := range bareTypeKeyRe.FindAllStringSubmatch(line, -1) {
		if isBareRuntimeToken(m[1]) {
			return true
		}
	}
	return false
}

// isBareRuntimeToken reports whether token is a runtime base/type value
// (`nodejs@22`) with no `alpine/`/`ubuntu/` OS prefix — the deprecated form.
// A token that isn't a recognized runtime tech at all (a placeholder like
// `<build-base>`, a managed type like `mariadb:single@10.11`, a hostname)
// returns false — this gate only fires on an actual bare runtime identifier.
func isBareRuntimeToken(token string) bool {
	m := runtimeTechAtRe.FindStringSubmatch(token)
	if m == nil {
		return false
	}
	return m[1] == ""
}

// legacyExemptByContext reports whether the line at index i, or any of the 6
// lines before it, contains the word "legacy" (case-insensitive) — the one
// labelled place agent-facing content may quote a deprecated field/form as a
// documented equivalence rather than a live example to copy.
func legacyExemptByContext(lines []string, i int) bool {
	start := max(i-6, 0)
	for j := start; j <= i; j++ {
		if strings.Contains(strings.ToLower(lines[j]), "legacy") {
			return true
		}
	}
	return false
}
