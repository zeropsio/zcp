package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// checkContentReality validates that file paths and declared code symbols
// named in README knowledge-base/integration-guide fragments and in the
// CLAUDE.md repo-local guide actually exist in the codebase — or that the
// surrounding prose is framed as advisory ("Pattern to add if…",
// "Consider adding…", etc.) so a reader knows the artifact is not
// shipped.
//
// Two failure modes from the v20 retrospective drive this check:
//
//  1. **File-path drift** — appdev gotcha cited a `_nginx.json` proxy
//     fix when the codebase never shipped `_nginx.json`. A reader trying
//     to apply the fix finds nothing to edit.
//  2. **Declared-symbol drift** — workerdev gotcha imperatively said
//     "Implement an internal watchdog" with a full `setInterval` code
//     block declaring a `lastActivity` watchdog, while no watchdog
//     symbol existed in src/. The gotcha read as documentation but was
//     a feature request.
//
// Both classes are caught here by walking each bullet, collecting the
// concrete claims (file paths from inline `code` and fenced blocks;
// top-level identifier declarations from TS/JS code blocks), and
// verifying each against the codebase. Claims that don't reflect reality
// must be reframed as advisory.
//
// codebaseDir empty no-ops (e.g. test contexts without a fixture mount).
func checkContentReality(codebaseDir, hostname, readmeContent, claudeContent string) []workflow.StepCheck {
	if codebaseDir == "" {
		return nil
	}
	if readmeContent == "" && claudeContent == "" {
		return nil
	}

	var bullets []realityBullet
	if readmeContent != "" {
		// Both KB and IG fragments use bullet/heading shapes that ship
		// concrete file/symbol claims. The intro fragment is prose-only —
		// scan it for inline file refs but don't expect bullets.
		for _, frag := range []string{"knowledge-base", "integration-guide", "intro"} {
			content := extractFragmentContent(readmeContent, frag)
			if content == "" {
				continue
			}
			bullets = append(bullets, splitIntoRealityBullets(content, frag)...)
		}
	}
	if claudeContent != "" {
		// CLAUDE.md is a single-document scan — no bullet split needed.
		// Treat the whole document as one "bullet" for advisory-marker
		// matching. Per-section bullet split would over-fail because
		// CLAUDE.md often shows a procedure that uses files declared in
		// adjacent sections, so claim/advisory pairing is whole-doc.
		bullets = append(bullets, realityBullet{
			source: "CLAUDE.md",
			body:   claudeContent,
		})
	}

	if len(bullets) == 0 {
		return nil
	}

	var failures []string
	for _, b := range bullets {
		failures = append(failures, b.checkPaths(codebaseDir)...)
		failures = append(failures, b.checkSymbols(codebaseDir)...)
	}

	checkName := hostname + "_content_reality"
	if len(failures) == 0 {
		return []workflow.StepCheck{{Name: checkName, Status: statusPass}}
	}
	// Keep failure detail focused — too many findings overwhelm the
	// agent and split attention. Cap at the first 4 with a tail count.
	tail := ""
	if len(failures) > 4 {
		tail = fmt.Sprintf("; and %d more", len(failures)-4)
		failures = failures[:4]
	}
	return []workflow.StepCheck{{
		Name:   checkName,
		Status: statusFail,
		Detail: fmt.Sprintf(
			"%s content makes claims that do not match what ships in the codebase — every named file path or top-level code symbol in a gotcha/IG bullet must either (a) exist in the codebase so the reader can inspect/apply it, or (b) be framed as advisory in the surrounding prose so the reader knows it is a pattern to consider rather than shipped behavior. Use phrases like 'Pattern to add if…', 'Consider adding…', 'If your … needs …', 'One approach…', or simply restate the bullet as a problem statement without prescribing a fix. Findings: %s%s",
			hostname, strings.Join(failures, "; "), tail,
		),
	}}
}

// realityBullet is one logical chunk that may carry concrete claims about
// the codebase. For READMEs, each Markdown bullet becomes one. For
// CLAUDE.md, the whole document is one bullet (sections aren't
// bullet-isolated and a CLAUDE.md procedure often spans multiple
// sections).
type realityBullet struct {
	source string // "knowledge-base", "integration-guide", "intro", "CLAUDE.md"
	body   string // raw text of the bullet, including any fenced code blocks
}

func (b realityBullet) checkPaths(codebaseDir string) []string {
	paths := extractCitedFilePaths(b.body)
	if len(paths) == 0 {
		return nil
	}
	advisory := containsAdvisoryMarker(b.body)
	var missing []string
	for _, p := range paths {
		if pathExistsInCodebase(codebaseDir, p) {
			continue
		}
		missing = append(missing, p)
	}
	if len(missing) == 0 {
		return nil
	}
	if advisory {
		return nil
	}
	stem := bulletStem(b.body)
	var out []string
	for _, p := range missing {
		out = append(out, fmt.Sprintf("%s bullet %q cites file `%s` but it does not exist in the codebase", b.source, stem, p))
	}
	return out
}

func (b realityBullet) checkSymbols(codebaseDir string) []string {
	if !isImperativeBullet(b.body) {
		return nil
	}
	symbols := extractDeclaredSymbols(b.body)
	if len(symbols) == 0 {
		return nil
	}
	if containsAdvisoryMarker(b.body) {
		return nil
	}
	for _, sym := range symbols {
		if symbolFoundInCodebase(codebaseDir, sym) {
			return nil
		}
	}
	stem := bulletStem(b.body)
	return []string{fmt.Sprintf(
		"%s bullet %q reads as declarative ('Implement…' / 'Add…') but its code block declares %s and no such symbol appears in the codebase — either ship the symbol in src/ or reframe the bullet as advisory ('Pattern to add if…')",
		b.source, stem, strings.Join(symbols, "/"),
	)}
}

// splitIntoRealityBullets walks fragment content and yields one bullet per
// top-level "- " list item, including any indented continuation prose and
// fenced code blocks that belong to that item. Headings (### N. Title)
// inside the integration-guide also count as bullets — each H3 block
// stands as one logical claim chunk.
func splitIntoRealityBullets(content, source string) []realityBullet {
	lines := strings.Split(content, "\n")
	var out []realityBullet
	var cur strings.Builder
	flush := func() {
		s := strings.TrimSpace(cur.String())
		if s != "" {
			out = append(out, realityBullet{source: source, body: s})
		}
		cur.Reset()
	}
	inFence := false
	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		// Fenced code blocks: do not split on a `- ` that lives inside a fence.
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			cur.WriteString(line)
			cur.WriteByte('\n')
			continue
		}
		if !inFence {
			isTopBullet := strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ")
			isHeading := strings.HasPrefix(trimmed, "### ")
			if isTopBullet || isHeading {
				flush()
			}
		}
		cur.WriteString(line)
		cur.WriteByte('\n')
	}
	flush()
	return out
}

// bulletStem returns a short label for the bullet — the bolded stem if
// present, otherwise the first heading or the first 60 chars of prose.
// Used in failure messages so the agent can locate the bullet.
func bulletStem(body string) string {
	// Look for the **bold stem** pattern that gotcha bullets always use.
	re := regexp.MustCompile(`\*\*([^*]+)\*\*`)
	if m := re.FindStringSubmatch(body); m != nil {
		s := strings.TrimSpace(m[1])
		if len(s) > 80 {
			s = s[:77] + "..."
		}
		return s
	}
	// Fall back to first non-empty line.
	for line := range strings.SplitSeq(body, "\n") {
		s := strings.TrimSpace(line)
		s = strings.TrimPrefix(s, "- ")
		s = strings.TrimPrefix(s, "* ")
		s = strings.TrimPrefix(s, "### ")
		if s != "" {
			if len(s) > 80 {
				s = s[:77] + "..."
			}
			return s
		}
	}
	return "(empty)"
}

// filePathRe matches a "looks like a file path" — extension-anchored to
// avoid matching every backticked identifier. The extension list covers
// the languages the recipe ecosystem actually ships; add to it as new
// stacks land. The leading boundary `(?:^|[^A-Za-z0-9_/.-])` requires the
// path to start at a token boundary so we don't match the tail of a URL
// or a longer identifier.
var filePathRe = regexp.MustCompile(`(?:^|[^A-Za-z0-9_/.-])([A-Za-z0-9_][A-Za-z0-9_./-]*\.(?:ts|tsx|js|jsx|mjs|cjs|json|yaml|yml|svelte|vue|py|rb|php|go|sql|html|css|scss|conf|toml|sh|bash|env|md))(?:$|[^A-Za-z0-9_/.-])`)

// pathsToIgnore are reference-only filenames that don't represent claims
// about the codebase — `zerops.yaml` is the workflow's own config file
// (always present, the recipe.md brief tells the agent to write it). All
// other paths the agent cites are checked.
var pathsToIgnore = map[string]bool{
	"zerops.yaml": true,
	"zerops.yml":  true,
}

// extractCitedFilePaths returns the de-duplicated list of file paths
// mentioned in the bullet body, scanning both inline `backticks` and
// the inside of fenced code blocks. Paths in the ignore list and bare
// extensions like `.env` are filtered.
func extractCitedFilePaths(body string) []string {
	matches := filePathRe.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range matches {
		p := strings.TrimSpace(m[1])
		if p == "" || pathsToIgnore[p] {
			continue
		}
		// Skip URLs (anything starting with a scheme).
		if strings.Contains(p, "://") {
			continue
		}
		// Skip bare extension references like ".env" or "*.svelte" — these
		// are pattern citations, not file claims.
		if strings.HasPrefix(p, ".") || strings.HasPrefix(p, "*") {
			continue
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// pathExistsInCodebase reports whether the cited path resolves to a real
// file/dir under codebaseDir. Tries the path as given (relative to root),
// then under common source roots (`src/`, `lib/`, `app/`) so a citation of
// `migrate.ts` matches `src/migrate.ts`. Also tries basename-anywhere
// match as a last resort — recipes don't normalize source roots, and a
// citation of `_nginx.json` should match a file at any depth.
func pathExistsInCodebase(codebaseDir, path string) bool {
	clean := filepath.Clean(path)
	if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		// Defensive — never escape codebaseDir. Treat as existing so we
		// don't surface false positives on path traversal patterns.
		return true
	}
	// Direct join.
	if _, err := os.Stat(filepath.Join(codebaseDir, clean)); err == nil {
		return true
	}
	// Common source-root fallbacks.
	for _, root := range []string{"src", "lib", "app", "source"} {
		if _, err := os.Stat(filepath.Join(codebaseDir, root, clean)); err == nil {
			return true
		}
	}
	// Basename-anywhere fallback. Walk the codebase looking for the
	// basename. This handles deep nesting (`src/foo/bar/baz.ts`) when the
	// citation is just `baz.ts`. Capped depth to avoid `node_modules`
	// scans.
	base := filepath.Base(clean)
	found := false
	_ = filepath.WalkDir(codebaseDir, func(_ string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			// Transient I/O on this entry — continue walking the rest
			// of the tree. The reality check is best-effort; one
			// unreadable file should not abort the codebase scan.
			return nil //nolint:nilerr // intentional: continue past unreadable entries
		}
		if d.IsDir() {
			if isHeavySkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == base {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// isHeavySkipDir names directories the reality check should skip when
// walking the codebase — package caches, build outputs, and VCS state
// are bulk and irrelevant to "did the agent ship this file".
func isHeavySkipDir(name string) bool {
	switch name {
	case "node_modules", "vendor", "dist", "build", ".git":
		return true
	}
	return false
}

// advisoryMarkers are phrases that signal the bullet is teaching a
// pattern the reader could adopt rather than documenting shipped
// behavior. When any marker appears anywhere in the bullet body the
// reality check passes — the reader is informed that the artifact is
// optional. Markers are case-insensitive substring matches; the list
// covers the common framings without being exhaustive.
var advisoryMarkers = []string{
	"pattern to add",
	"if you implement",
	"if you need",
	"if your ",
	"if the ",
	"consider adding",
	"consider using",
	"consider switching",
	"one approach",
	"one pattern",
	"add this if",
	"you can add",
	"add it if",
	"to add this",
	"to enable this",
	"alternative:",
	"as an alternative",
	"optional pattern",
	"if needed",
	"if required",
	"could be added",
}

func containsAdvisoryMarker(body string) bool {
	low := strings.ToLower(body)
	for _, m := range advisoryMarkers {
		if strings.Contains(low, m) {
			return true
		}
	}
	return false
}

// imperativeVerbs at the start of the bullet body's first prose sentence
// signal a declarative shape: "Implement X.", "Add X.", "Configure X.".
// When the bullet has imperative shape AND ships a code block AND the
// declared symbols don't appear in the codebase, the bullet is decorative.
var imperativeVerbs = []string{
	"implement ",
	"add ",
	"configure ",
	"set up ",
	"setup ",
	"install ",
	"create ",
	"register ",
	"declare ",
	"wire ",
	"enable ",
	"build ",
	"ship ",
}

// boldStemRe captures `**bold stem**` non-greedily so a stem with an
// internal em-dash ("Workers have no health check — a hung process")
// is consumed as a single span. The body separator that follows is the
// FIRST em-dash AFTER the closing `**`, not the first one in the line.
var boldStemRe = regexp.MustCompile(`\*\*[^*]+\*\*`)

func isImperativeBullet(body string) bool {
	prose := body
	if loc := boldStemRe.FindStringIndex(body); loc != nil {
		prose = body[loc[1]:]
	}
	// Skip the leading ` — ` / ` -- ` separator if present.
	prose = strings.TrimLeft(prose, " \t")
	prose = strings.TrimPrefix(prose, "—")
	prose = strings.TrimPrefix(prose, "--")
	prose = strings.TrimSpace(prose)
	low := strings.ToLower(prose)
	for _, v := range imperativeVerbs {
		if strings.HasPrefix(low, v) {
			return true
		}
	}
	return false
}

// declaredSymbolRe matches top-level identifier declarations in TS/JS
// code blocks: function/const/let/var/class/interface/type/enum names.
// Not perfect — block-internal nested declarations are also matched —
// but that's tolerable for the reality check: a nested symbol that
// appears in real code somewhere will pass; a nested symbol that
// appears only in the gotcha's example will fail, which is the
// intended outcome.
var declaredSymbolRe = regexp.MustCompile(`(?m)^\s*(?:export\s+)?(?:async\s+)?(?:function|const|let|var|class|interface|type|enum)\s+([A-Za-z_$][A-Za-z0-9_$]*)`)

// extractDeclaredSymbols walks fenced code blocks (any language tag) and
// returns the de-duplicated set of top-level identifier names declared
// inside them. Identifiers shorter than 4 chars or matching common
// generic names are skipped — they're too noisy to anchor a reality
// claim.
var genericSymbolNames = map[string]bool{
	"app": true, "ctx": true, "req": true, "res": true, "err": true,
	"i": true, "j": true, "k": true, "x": true, "y": true, "z": true,
	"a": true, "b": true, "fn": true, "cb": true, "id": true, "val": true,
	"tmp": true, "buf": true, "key": true, "msg": true, "sub": true,
	"data": true, "result": true, "error": true, "value": true,
	"main": true, "init": true, "start": true, "stop": true, "run": true,
	"foo": true, "bar": true, "baz": true,
}

func extractDeclaredSymbols(body string) []string {
	// Walk fenced blocks only — declarations in prose are not claims.
	lines := strings.Split(body, "\n")
	var blockBuf strings.Builder
	inFence := false
	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			if inFence {
				blockBuf.Reset()
			}
			continue
		}
		if inFence {
			blockBuf.WriteString(line)
			blockBuf.WriteByte('\n')
		}
	}
	all := blockBuf.String()
	matches := declaredSymbolRe.FindAllStringSubmatch(all, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range matches {
		s := m[1]
		if len(s) < 4 {
			continue
		}
		if genericSymbolNames[strings.ToLower(s)] {
			continue
		}
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// symbolFoundInCodebase scans codebase source files for the symbol as a
// word match. Skips heavy directories. Used as a positive test — any
// file that mentions the symbol counts as "this is a real claim about
// shipped behavior".
func symbolFoundInCodebase(codebaseDir, symbol string) bool {
	pat := regexp.MustCompile(`\b` + regexp.QuoteMeta(symbol) + `\b`)
	found := false
	_ = filepath.WalkDir(codebaseDir, func(p string, d os.DirEntry, walkErr error) error {
		if found {
			return filepath.SkipAll
		}
		if walkErr != nil {
			// Transient I/O on this entry — continue walking. Best-
			// effort scan; one unreadable file shouldn't abort.
			return nil //nolint:nilerr // intentional: continue past unreadable entries
		}
		if d.IsDir() {
			if isHeavySkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		// Source-extension filter — avoid binary scans.
		ext := strings.ToLower(filepath.Ext(d.Name()))
		switch ext {
		case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".svelte", ".vue",
			".py", ".rb", ".php", ".go", ".rs", ".java", ".kt", ".cs":
		default:
			return nil
		}
		content, readErr := os.ReadFile(p)
		if readErr != nil {
			// Unreadable file (permissions, race) — skip and continue
			// the walk. Symbol existence is a positive test; we only
			// need ONE file to contain the symbol to pass.
			return nil //nolint:nilerr // intentional: skip past unreadable file
		}
		if pat.Match(content) {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}
