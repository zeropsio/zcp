package recipe

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Run-46 Item 7 — self-referential-naming linter for codebase KB
// fragments.
//
// Run-45 forensic: three apidev KB bullets violated spec
// §"Self-referential decoration prohibition" but 0/3 were caught by
// run-45 refinement-2. The principle ("every published item must make
// sense to its reader without them having read the rest of the
// recipe's code") was in the audit substrate; the audit didn't fire on
// the recipe-internal symbols.
//
// Item 7 closes the loop with a structural validator at record-fragment
// time, scoped to fragmentId `codebase/<host>/knowledge-base`. The
// validator detects backticked tokens in bullet bodies that resolve to:
//
//  1. File paths under the codebase's `src/` glob
//  2. Class/export names from the codebase's `src/` exports
//  3. Module file names matching the NestJS recipe-internal shape
//     (`<x>.module.ts`, `<x>.service.ts`, `<x>.controller.ts`)
//
// Refusal redirects the agent to rewrite at the principle level or
// move to code comments, citing spec §S5 self-referential decoration
// prohibition. Same shape as the named-constant-drift gate.

// nestModuleSuffixes — NestJS recipe-internal file patterns. Any
// backticked token ending in one of these is a recipe-internal symbol
// regardless of on-disk match (NestJS-shaped recipes consistently
// generate these; the pattern itself is the signal).
var nestModuleSuffixes = []string{
	".module.ts",
	".service.ts",
	".controller.ts",
	".module.js",
	".service.js",
	".controller.js",
	".tokens.ts",
	".tokens.js",
	".guard.ts",
	".interceptor.ts",
	".dto.ts",
	".entity.ts",
}

// genericHTTPHeaderTokens — tokens that LOOK recipe-internal (capital
// pre-hyphen / backtick-styled) but are actually platform-spec content
// (HTTP headers, well-known env vars). Allow-list to suppress false
// positives.
var genericHTTPHeaderTokens = map[string]bool{
	"x-cache":            true,
	"x-cache-elapsed-ms": true,
	"x-cache-key":        true,
	"x-frame-options":    true,
	"x-content-type":     true,
	"x-forwarded-for":    true,
	"x-forwarded-proto":  true,
	"x-request-id":       true,
}

// backtickedTokenRE captures every `<token>` backticked span. The
// linter scans these against the codebase's recipe-internal symbol
// set.
var backtickedTokenRE = regexp.MustCompile("`([^`]+)`")

// lintSelfReferentialKB scans a KB fragment body for backticked tokens
// that resolve to recipe-internal symbols under codebaseDir. Returns a
// slice of human-readable refusal messages; empty when the body passes
// the self-referential check.
//
// Missing codebaseDir (in-memory test fixtures, no dev mount) is a
// silent no-op — the linter can't see the recipe-internal symbols
// without a filesystem to walk, and refusing every backtick would
// create false positives the agent can't recover from.
func lintSelfReferentialKB(body, codebaseDir string) ([]string, error) {
	kb := extractBetweenMarkers(body, "knowledge-base")
	if kb == "" {
		return nil, nil
	}
	symbols, err := collectCodebaseSymbols(codebaseDir)
	if err != nil {
		return nil, err
	}
	// Walk each bullet separately so the refusal message can name the
	// offending bullet text.
	bulletStarts := boldBulletRE.FindAllStringIndex(kb, -1)
	if len(bulletStarts) == 0 {
		return nil, nil
	}
	var refusals []string
	for i, m := range bulletStarts {
		var end int
		if i+1 < len(bulletStarts) {
			end = bulletStarts[i+1][0]
		} else {
			end = len(kb)
		}
		bullet := kb[m[0]:end]
		offenders := scanBulletForRecipeInternalSymbols(bullet, symbols)
		if len(offenders) == 0 {
			continue
		}
		refusals = append(refusals, formatSelfReferentialRefusal(bullet, offenders))
	}
	return refusals, nil
}

// codebaseSymbols holds the recipe-internal symbol set the linter
// compares backticked tokens against. Populated from filesystem walk;
// empty when codebaseDir doesn't exist (silent no-op upstream).
type codebaseSymbols struct {
	// Filenames is the set of bare filenames under `<codebaseDir>/src/`
	// (no directory prefix), so a backticked `cache.module.ts` matches
	// regardless of where in src/ it lives.
	Filenames map[string]bool
	// PackageDeps is the union of `dependencies` + `devDependencies`
	// keys in `<codebaseDir>/package.json`. Used to skip false positives
	// on backticked dependency names (e.g. `redis` the npm package).
	PackageDeps map[string]bool
	// ExportedSymbols is the set of exported identifiers parsed from
	// `<codebaseDir>/src/**/*.{ts,js,tsx,jsx,go,py}` — TypeScript/JS
	// `export {const,class,function,interface,type} <X>` declarations,
	// Go package-level `func`/`type`/`var`/`const` with uppercase first
	// letter, Python module-level `class`/`def`. Run-46 Item 7
	// G3-followup — without this set, leaf-module symbols like
	// `REDIS_CLIENT` exported from `cache.tokens.ts` would slip past
	// the filename-prefix cross-match in isLikelyClassExport.
	ExportedSymbols map[string]bool
}

// exportParseExtensions are the file extensions the export parser walks.
// Each maps to the patterns ParseExportedSymbols uses. Extensions not
// in this set are silently skipped — adding new languages means
// extending this slice + the regex table in extractExportedSymbols.
var exportParseExtensions = []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".go", ".py"}

// exportParseSkipDirs are directories the export-parser walk skips for
// performance + correctness — these don't carry recipe-internal symbol
// declarations, only third-party / build artifacts.
var exportParseSkipDirs = map[string]bool{
	"node_modules": true,
	"__pycache__":  true,
	"dist":         true,
	"build":        true,
	".git":         true,
	"vendor":       true,
}

// exportTSConstClassFuncRE matches TypeScript / JavaScript export
// declarations: `export const X`, `export class X`, `export function X`,
// `export interface X`, `export type X`, `export default class X`,
// `export default function X`, `export abstract class X`. Captures the
// identifier (group 1).
var exportTSConstClassFuncRE = regexp.MustCompile(
	`(?m)^\s*export\s+(?:default\s+)?(?:abstract\s+)?(?:async\s+)?(?:const|let|var|class|function|interface|type|enum)\s+([A-Za-z_][A-Za-z0-9_]*)`,
)

// exportTSNamedListRE matches `export { Foo, Bar as Baz }` block
// declarations. Captures the whole identifier list (group 1) — split +
// trim per token afterwards.
var exportTSNamedListRE = regexp.MustCompile(`(?m)^\s*export\s*\{([^}]+)\}`)

// goPackageLevelDeclRE matches Go package-level `func`/`type`/`var`/
// `const` declarations whose identifier starts with an uppercase letter
// (Go's export rule). Captures the identifier (group 2). Skips the
// `var (` / `const (` block-open lines — block contents need a separate
// pass.
var goPackageLevelDeclRE = regexp.MustCompile(
	`(?m)^(?:func|type|var|const)\s+(?:\([^)]*\)\s+)?([A-Z][A-Za-z0-9_]*)`,
)

// pythonModuleLevelDeclRE matches Python module-level `class X` /
// `def X` declarations (no indent). Captures the identifier (group 2).
var pythonModuleLevelDeclRE = regexp.MustCompile(`(?m)^(class|def)\s+([A-Za-z_][A-Za-z0-9_]*)`)

// collectCodebaseSymbols walks the codebase dir and collects file
// names + package deps + exported identifiers. Returns an empty symbol
// set on missing dir (silent no-op).
func collectCodebaseSymbols(codebaseDir string) (codebaseSymbols, error) {
	syms := codebaseSymbols{
		Filenames:       map[string]bool{},
		PackageDeps:     map[string]bool{},
		ExportedSymbols: map[string]bool{},
	}
	if codebaseDir == "" {
		return syms, nil
	}
	if _, err := os.Stat(codebaseDir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return syms, nil
		}
		return syms, err
	}
	srcDir := filepath.Join(codebaseDir, "src")
	if _, err := os.Stat(srcDir); err == nil {
		// WalkDir errors propagate; the linter is best-effort and the
		// outer caller can surface transient FS hiccups as needed.
		walkErr := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if exportParseSkipDirs[d.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			syms.Filenames[d.Name()] = true
			ext := strings.ToLower(filepath.Ext(d.Name()))
			if !exportParseExtIsCovered(ext) {
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				// Don't fail the walk on a single unreadable file — best-
				// effort symbol collection. The error is intentionally
				// swallowed so a transient FS hiccup doesn't kill the
				// entire walk; lint-quiet via the nolint pragma.
				return nil //nolint:nilerr // best-effort walk
			}
			for _, sym := range extractExportedSymbols(string(data), ext) {
				syms.ExportedSymbols[sym] = true
			}
			return nil
		})
		if walkErr != nil {
			return syms, walkErr
		}
	}
	pkgPath := filepath.Join(codebaseDir, "package.json")
	if data, err := os.ReadFile(pkgPath); err == nil {
		var pkg struct {
			Dependencies    map[string]string `json:"dependencies"`
			DevDependencies map[string]string `json:"devDependencies"`
		}
		if jErr := json.Unmarshal(data, &pkg); jErr == nil {
			for k := range pkg.Dependencies {
				syms.PackageDeps[k] = true
			}
			for k := range pkg.DevDependencies {
				syms.PackageDeps[k] = true
			}
		}
	}
	return syms, nil
}

// exportParseExtIsCovered reports whether the file extension belongs to
// the export-parser's supported language set.
func exportParseExtIsCovered(ext string) bool {
	return slices.Contains(exportParseExtensions, ext)
}

// extractExportedSymbols runs the per-language regex set against the
// file content + returns the identifiers exported from the file. The
// parser is intentionally regex-based — the linter classifies "is this
// backticked token a recipe-internal symbol" and false positives are
// preferable to false negatives (recipe-internal symbols leaking past
// the linter is the regression class Item 7 closes).
//
// Run-46 Item 7 G3-followup.
func extractExportedSymbols(content, ext string) []string {
	switch ext {
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs":
		return extractTSExportedSymbols(content)
	case ".go":
		return extractGoPackageLevelDecls(content)
	case ".py":
		return extractPythonModuleLevelDecls(content)
	}
	return nil
}

func extractTSExportedSymbols(content string) []string {
	directMatches := exportTSConstClassFuncRE.FindAllStringSubmatch(content, -1)
	namedListMatches := exportTSNamedListRE.FindAllStringSubmatch(content, -1)
	out := make([]string, 0, len(directMatches)+len(namedListMatches))
	for _, m := range directMatches {
		out = append(out, m[1])
	}
	for _, m := range namedListMatches {
		// Split `Foo, Bar as Baz, Default as default` into tokens, take
		// the exported identifier.
		for raw := range strings.SplitSeq(m[1], ",") {
			tok := strings.TrimSpace(raw)
			if tok == "" {
				continue
			}
			// `Foo as Bar` → take Bar (the exported name).
			if idx := strings.Index(tok, " as "); idx >= 0 {
				tok = strings.TrimSpace(tok[idx+4:])
			}
			// `default as Foo` covered by `as` branch; bare `default`
			// keyword we skip.
			if tok == "" || tok == "default" {
				continue
			}
			out = append(out, tok)
		}
	}
	return out
}

func extractGoPackageLevelDecls(content string) []string {
	matches := goPackageLevelDeclRE.FindAllStringSubmatch(content, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m[1])
	}
	return out
}

func extractPythonModuleLevelDecls(content string) []string {
	matches := pythonModuleLevelDeclRE.FindAllStringSubmatch(content, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m[2])
	}
	return out
}

// scanBulletForRecipeInternalSymbols returns the set of backticked
// tokens in the bullet body that match the recipe-internal symbol set.
// Skips tokens that match the generic HTTP-header allow-list +
// package-dependency names.
func scanBulletForRecipeInternalSymbols(bullet string, syms codebaseSymbols) []string {
	matches := backtickedTokenRE.FindAllStringSubmatch(bullet, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var offenders []string
	for _, m := range matches {
		token := m[1]
		if seen[token] {
			continue
		}
		if isRecipeInternalSymbol(token, syms) {
			offenders = append(offenders, token)
			seen[token] = true
		}
	}
	return offenders
}

// isRecipeInternalSymbol classifies a backticked token as a recipe-
// internal symbol per Item 7's design:
//
//  1. NestJS module-pattern suffix match — recipe-internal regardless
//     of on-disk presence.
//  2. Bare filename present in the codebase's `src/` glob.
//  3. Token resembles a TypeScript/JavaScript class export — UpperCamelCase
//     + a kebab/dot variant present in the codebase's src/ filenames.
//  4. Token matches an exported identifier parsed from the codebase's
//     src/ files (TS/JS export {const,class,function,interface,type},
//     Go uppercase package-level declarations, Python module-level
//     class/def). Run-46 Item 7 G3-followup — closes the leaf-module
//     hole (e.g. `REDIS_CLIENT` exported from `cache.tokens.ts`).
func isRecipeInternalSymbol(token string, syms codebaseSymbols) bool {
	lower := strings.ToLower(token)
	if genericHTTPHeaderTokens[lower] {
		return false
	}
	if syms.PackageDeps[token] {
		// Backticking a dependency name (e.g. `redis`, `meilisearch`) is
		// not self-referential.
		return false
	}
	// (1) NestJS module-pattern.
	for _, suf := range nestModuleSuffixes {
		if strings.HasSuffix(token, suf) {
			return true
		}
	}
	// (2) Bare filename in src/.
	if syms.Filenames[token] {
		return true
	}
	// (4) Exported identifier — handles leaf modules where the token
	// resembles a constant (`REDIS_CLIENT`) or a class exported from a
	// non-eponymous file (`CacheService` in `cache.service.ts` →
	// matches the filename rule above; but `CacheService` exported from
	// `cache.module.ts` does NOT match filename, only this rule).
	// Also catches the bare-symbol form of class names directly.
	if syms.ExportedSymbols[token] {
		return true
	}
	// (3) UpperCamelCase class-name. Heuristic: starts with uppercase
	// letter, contains another uppercase letter further in (so `App` /
	// `URL` alone don't trip), no whitespace, no punctuation other than
	// `.` (e.g. `Class.method` is still class-named).
	if isLikelyClassExport(token) {
		// Cross-check against src/ filenames — a class named CacheService
		// likely has cache.service.ts or CacheService.ts somewhere.
		base := strings.ToLower(token)
		// Map `CacheService` → `cache.service` for kebab-form match.
		kebab := classNameToKebab(token)
		for fname := range syms.Filenames {
			lname := strings.ToLower(fname)
			if strings.HasPrefix(lname, base+".") {
				return true
			}
			if strings.HasPrefix(lname, kebab+".") {
				return true
			}
		}
	}
	return false
}

// isLikelyClassExport reports whether token looks like a TypeScript/
// JavaScript class export. Two-or-more-capitals heuristic so generic
// English nouns (`App`, `User`) don't trip.
func isLikelyClassExport(token string) bool {
	if token == "" {
		return false
	}
	first := token[0]
	if first < 'A' || first > 'Z' {
		return false
	}
	// At least one more uppercase letter after position 0.
	capitalCount := 1
	for i := 1; i < len(token); i++ {
		c := token[i]
		if c >= 'A' && c <= 'Z' {
			capitalCount++
		}
		// Reject obvious non-symbol punctuation that would mean this isn't
		// a class name — whitespace, slash, parens. `.` is allowed
		// (`Class.method`).
		if c == ' ' || c == '/' || c == '(' || c == ')' {
			return false
		}
	}
	return capitalCount >= 2
}

// classNameToKebab transforms `CacheService` → `cache.service`. Used to
// cross-check class names against the src/ kebab-form filenames the
// NestJS-style scaffolds emit.
func classNameToKebab(token string) string {
	var b strings.Builder
	for i, c := range token {
		if c >= 'A' && c <= 'Z' {
			if i > 0 {
				b.WriteByte('.')
			}
			b.WriteRune(c + ('a' - 'A'))
			continue
		}
		b.WriteRune(c)
	}
	return b.String()
}

// formatSelfReferentialRefusal renders the per-bullet refusal message
// the agent sees on a record-fragment refusal. Names the offending
// token(s), redirects to the principle level + code-comments, cites
// the spec section.
func formatSelfReferentialRefusal(bullet string, offenders []string) string {
	stem := bullet
	if _, after, ok := strings.Cut(stem, "**"); ok {
		if inner, _, ok2 := strings.Cut(after, "**"); ok2 {
			stem = inner
		}
	}
	stem = strings.TrimSpace(stem)
	tokens := strings.Join(offenders, ", ")
	return "self-referential KB bullet — backticks " + tokens + " on bullet " +
		"\"" + truncateForMessage(stem) + "\"" +
		". Rewrite at the principle level (what the porter learns from this " +
		"that ISN'T tied to your recipe's helper-file names) or move the symbol " +
		"to code comments. Spec §Self-referential decoration prohibition: published " +
		"items must make sense without the reader having read the rest of the " +
		"recipe's code; recipe-internal helper/class/module names are recipe scaffold " +
		"decoration, not platform-trap teaching."
}

// truncateForMessage caps a string length for inclusion in a refusal
// message so multi-bullet errors don't blow out the response envelope.
func truncateForMessage(s string) string {
	const maxLen = 80
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…"
}

// selfReferentialKBRefusal is the record-fragment integration point.
// Returns a non-empty refusal message when fragmentID is a codebase KB
// AND the body carries one or more self-referential symbols; empty
// otherwise.
//
// Codebase dir resolution: prefer the codebase's declared SourceRoot
// (engine-populated at scaffold entry); fall back to
// `<outputRoot>/<host>dev` for in-memory test fixtures that stage a
// codebase tree under outputRoot. Missing dir = silent no-op via
// lintSelfReferentialKB's missing-dir behavior.
func selfReferentialKBRefusal(sess *Session, fragmentID, body string) string {
	if sess == nil || sess.Plan == nil {
		return ""
	}
	host := codebaseHostFromFragmentID(fragmentID)
	if host == "" {
		return ""
	}
	if !strings.HasSuffix(fragmentID, "/knowledge-base") {
		return ""
	}
	codebaseDir := ""
	for _, cb := range sess.Plan.Codebases {
		if cb.Hostname == host {
			codebaseDir = cb.SourceRoot
			break
		}
	}
	if codebaseDir == "" && sess.OutputRoot != "" {
		codebaseDir = filepath.Join(sess.OutputRoot, host+"dev")
	}
	refusals, err := lintSelfReferentialKB(body, codebaseDir)
	if err != nil || len(refusals) == 0 {
		return ""
	}
	if len(refusals) == 1 {
		return "record-fragment: " + refusals[0]
	}
	return fmt.Sprintf("record-fragment: %d self-referential bullets\n  - %s",
		len(refusals), strings.Join(refusals, "\n  - "))
}
