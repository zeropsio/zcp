package recipe

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Run-9-readiness Workstream I — source-code comment scanner.
//
// Authoring-phase leaks into committed source-code comments ("the
// scaffold ships…", "pre-ship contract item N", "showcase default")
// are meaningless to a porter cloning the apps repo. The porter-voice
// rule in content_authoring.md teaches sub-agents to avoid them; this
// validator is the engine-side teeth that catches leaks the brief
// missed.
//
// The validator walks a codebase's SourceRoot directory, scans every
// source file for comment lines containing a forbidden phrase, and
// emits one violation per hit. Missing SourceRoot skips silently
// (covers chain-parent codebases + not-yet-scaffold-complete states).

// sourceCommentExts enumerates the source-file extensions worth
// scanning. Limited to typical framework languages — anything else
// gets a pass. Order intentionally broad so new frameworks slot in.
var sourceCommentExts = map[string]bool{
	".ts": true, ".tsx": true,
	".js": true, ".jsx": true, ".mjs": true, ".cjs": true,
	".svelte": true, ".vue": true,
	".go":  true,
	".php": true,
	".py":  true,
	".rb":  true,
}

// sourceCommentSkipDirs are path segments that signal vendored or
// build-output code. The scan skips them entirely — a porter reading
// the apps repo won't browse node_modules either.
var sourceCommentSkipDirs = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	".venv":        true,
	"venv":         true,
	"dist":         true,
	"build":        true,
	"target":       true,
	".next":        true,
	".svelte-kit":  true,
	".git":         true,
}

// sourceForbiddenPhrases — case-insensitive substrings that signal an
// authoring-phase reference inside a comment. Tuned tight: phrases,
// not bare words where possible, so legit uses of `scaffold` (e.g.
// "next.js scaffolding") don't false-positive.
var sourceForbiddenPhrases = []string{
	"pre-ship contract",
	"preship contract",
	"the scaffold",
	"scaffold smoke test",
	"scaffold default",
	"scaffolded by",
	"feature phase",
	"feature-phase",
	"showcase default",
	"showcase tier",
	"showcase tradeoff",
	"showcase-tier",
	"for the recipe",
	"we chose",
	"we added",
	"we decided",
	"grew from",
}

// scanSourceCommentsAt walks one codebase SourceRoot (directory) and
// emits one Violation per forbidden-phrase hit inside a comment line.
// Missing root → empty result, no error (the caller gates on stat).
func scanSourceCommentsAt(root string) ([]Violation, error) {
	var vs []Violation
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		// Don't fail whole walk on one unreadable entry — skip by
		// returning nil. nolint:nilerr — the whole-walk tolerance is
		// intentional for this scan.
		if err != nil {
			return nil //nolint:nilerr
		}
		if d.IsDir() {
			if sourceCommentSkipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if !sourceCommentExts[ext] {
			return nil
		}
		body, rerr := readFileCapped(path, 512*1024)
		if rerr != nil {
			return nil //nolint:nilerr
		}
		for i, line := range strings.Split(body, "\n") {
			if !looksLikeCommentLine(line) {
				continue
			}
			lower := strings.ToLower(line)
			for _, phrase := range sourceForbiddenPhrases {
				if strings.Contains(lower, phrase) {
					vs = append(vs, Violation{
						Code: "source-comment-authoring-voice-leak",
						Path: fmt.Sprintf("%s:%d", path, i+1),
						Message: fmt.Sprintf("comment line contains authoring-phase reference %q: %s",
							phrase, trimForMessage(strings.TrimSpace(line))),
						Severity: SeverityNotice,
					})
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return vs, nil
}

// readFileCapped reads up to maxBytes of a file so the scanner doesn't
// blow memory on a minified bundle that slipped past the dir-skip list.
func readFileCapped(path string, maxBytes int) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(body) > maxBytes {
		body = body[:maxBytes]
	}
	return string(body), nil
}

// looksLikeCommentLine returns true for lines that start with one of
// the common comment prefixes (`//`, `#`, `/*`, `*`, `<!--`). Kept
// conservative — a code line containing a matching substring is never
// flagged because it doesn't start with a comment prefix.
func looksLikeCommentLine(line string) bool {
	trimmed := strings.TrimLeft(line, " \t")
	switch {
	case strings.HasPrefix(trimmed, "//"):
		return true
	case strings.HasPrefix(trimmed, "/*"):
		return true
	case strings.HasPrefix(trimmed, "*"):
		return true
	case strings.HasPrefix(trimmed, "#"):
		return true
	case strings.HasPrefix(trimmed, "<!--"):
		return true
	}
	return false
}
