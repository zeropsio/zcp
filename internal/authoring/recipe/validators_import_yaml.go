package recipe

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Run-15 F.5 — env import.yaml comment checks that require parsing the
// yaml AST.
//
// Two failure modes from run-14 motivated this file:
//
//  1. **Fabricated yaml field names.** Tier import.yaml preamble
//     comments referenced `project_env_vars` (snake_case). The actual
//     schema field is `project.envVariables` (camelCase, nested). A
//     porter searching the yaml for `project_env_vars` finds nothing —
//     the fabrication is structurally invisible (looks like a normal
//     field name but doesn't exist). Detection: parse the yaml AST to
//     collect every real key path, then scan comments for tokens that
//     LOOK like field paths (lowercase + underscore OR dot-separated)
//     and refuse any whose path doesn't exist in the AST.
//
//  2. **Authoring-voice leak in env yaml comments.** Run-14 tier-1 +
//     tier-5 import.yaml mentioned "recipe author" / "during scaffold"
//     in comment prose. The yaml comments speak about the porter's
//     deployed runtime; the agent that wrote them is not the audience.
//     `validators_source_comments.go` patrols apps-repo source comments
//     (TS/JS/Svelte/etc.), but env import.yaml comments live on a
//     different surface and weren't covered.
//
// Both checks run as part of validateEnvImportComments — the surface's
// single registered validator. Helpers live here for cohesion with the
// yaml-AST cross-check.

// envYAMLAudienceLeakPhrases — case-insensitive substrings inside
// import.yaml comment lines that signal authoring-voice. Notice
// severity: the issue is voice (porter-facing → no agent-self-reference)
// not factuality. Mirrors the apps-repo scanner phrases at
// validators_source_comments.go::sourceForbiddenPhrases but tuned for
// env yaml comment voice specifically.
var envYAMLAudienceLeakPhrases = []string{
	"recipe author",
	"during scaffold",
	"the scaffold ",
	"scaffolded by",
	"we chose",
	"we added",
	"we decided",
	"for the recipe",
	"the agent",
	"sub-agent",
}

// envYAMLFieldTokenRE matches tokens inside comments that LOOK like
// yaml field paths. Two shapes:
//
//   - underscore-separated lowercase: `project_env_vars`,
//     `vertical_autoscaling`, `min_free_ram_gb`. The shape signals "I
//     thought this was a yaml field" but yaml uses camelCase.
//   - dot-separated path: `project.envVariables`, `services.api.mode`.
//     The shape names a real key path; cross-check it against the AST.
//
// Tokens must contain only `[a-z0-9_.]` (no spaces, no uppercase) and
// either a `_` or a `.` to qualify as field-shaped — single-word
// English prose ("ports", "recipe") fails the heuristic and is never
// flagged.
var envYAMLFieldTokenRE = regexp.MustCompile(`\b[a-z][a-z0-9]*(?:[_.][a-z0-9]+)+\b`)

// validateEnvYAMLImportCommentsExtra runs the F.5 yaml-AST + voice
// checks against the env import.yaml body. Returns blocking violations
// for fabricated field names + notice violations for audience-voice
// leaks. Called from validateEnvImportComments alongside the existing
// per-tier comment checks.
func validateEnvYAMLImportCommentsExtra(path string, body []byte) []Violation {
	var vs []Violation
	bodyStr := string(body)

	// Audience-voice leak — runs first, doesn't need a successful parse.
	vs = append(vs, scanEnvYAMLAudienceLeaks(path, bodyStr)...)

	// Fabricated-field check — needs the parsed AST to know what paths
	// actually exist. Parse failure short-circuits the AST-driven check
	// (the yaml-syntax validator surfaces the parse error separately).
	var root yaml.Node
	if err := yaml.Unmarshal(body, &root); err != nil {
		return vs
	}
	knownPaths := collectYAMLPaths(&root)
	vs = append(vs, scanFabricatedYAMLFieldNames(path, bodyStr, knownPaths)...)
	return vs
}

// scanEnvYAMLAudienceLeaks emits one notice per audience-voice phrase
// hit inside a yaml comment line.
func scanEnvYAMLAudienceLeaks(path, body string) []Violation {
	var vs []Violation
	for i, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		comment := strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
		lower := strings.ToLower(comment)
		for _, phrase := range envYAMLAudienceLeakPhrases {
			if strings.Contains(lower, phrase) {
				vs = append(vs, notice("env-yaml-audience-voice-leak",
					fmt.Sprintf("%s:%d", path, i+1),
					fmt.Sprintf("env import.yaml comment carries authoring-phase phrase %q — comments are porter-facing; the agent that wrote them is not the audience: %q",
						phrase, trimForMessage(comment))))
			}
		}
	}
	return vs
}

// scanFabricatedYAMLFieldNames flags comment tokens that look like yaml
// field paths but don't appear in the parsed yaml. One blocking
// violation per fabricated path. Tokens that DO appear in the yaml pass
// silently (correct cross-references). Tokens whose shape doesn't match
// envYAMLFieldTokenRE (English prose words) are never inspected.
//
// Run-21 §A4 — three context-based escapes that suppress validator
// over-fire on legitimate non-yaml-field tokens:
//
//   - Backtick-wrapped (`tasks.created`, `vite.config.js`) — agents
//     deliberately mark non-yaml strings as code; respect the marker.
//   - `${...}` interpolation prose (`${cache_hostname}`) — managed-
//     service aliases the brief teaches; regex sees `cache_hostname`
//     after stripping the `${}`.
//   - File-extension tail (`.json`, `.yaml`, `.ts`, `.js`, ...) —
//     filename references (`config.json`, `vite.config.js`,
//     `tsconfig.json`) shaped like dotted paths.
func scanFabricatedYAMLFieldNames(path, body string, knownPaths map[string]bool) []Violation {
	var vs []Violation
	seen := map[string]bool{}
	for i, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		comment := strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
		for _, tok := range envYAMLFieldTokenRE.FindAllString(comment, -1) {
			if knownPaths[tok] {
				continue
			}
			// Suffix-match: a comment naming `envVariables` is fine if
			// the yaml has `project.envVariables` somewhere.
			if anyPathSuffixMatches(tok, knownPaths) {
				continue
			}
			// English-prose escape: tokens whose every segment is a
			// common English word (e.g. "back.to.basics") still match
			// the regex. Filter via a tiny stoplist to avoid the
			// obvious false positives. The regex's underscore/dot
			// requirement already filters most prose.
			if isLikelyProseToken(tok) {
				continue
			}
			// Run-21 §A4 — three context-based escapes.
			if tokenIsBacktickWrapped(comment, tok) {
				continue
			}
			if tokenIsAliasInterpolation(comment, tok) {
				continue
			}
			if tokenHasFileExtension(tok) {
				continue
			}
			key := fmt.Sprintf("%d:%s", i+1, tok)
			if seen[key] {
				continue
			}
			seen[key] = true
			vs = append(vs, violation("env-yaml-fabricated-field-name",
				fmt.Sprintf("%s:%d", path, i+1),
				fmt.Sprintf(
					"comment names yaml field %q but no such path exists in the yaml below. If you reference a yaml field in a comment, that path must exist as a key in the yaml AST. Common cause: snake_case (`project_env_vars`) when the schema uses camelCase (`project.envVariables`).",
					tok,
				)))
		}
	}
	return vs
}

// tokenIsBacktickWrapped reports whether `tok` appears inside a
// backtick-quoted span on `comment`. Agents mark strings (NATS subjects,
// filenames, code identifiers) with backticks; the validator must
// respect that marker. Walks the comment looking for an opening backtick
// that precedes `tok`'s first occurrence with no closing backtick
// between them.
func tokenIsBacktickWrapped(comment, tok string) bool {
	idx := strings.Index(comment, tok)
	if idx < 0 {
		return false
	}
	open := strings.LastIndex(comment[:idx], "`")
	if open < 0 {
		return false
	}
	// A `closing` backtick between `open` and `idx` would mean the open
	// already terminated; tok is outside the quoted span.
	if strings.Contains(comment[open+1:idx], "`") {
		return false
	}
	// Closing backtick must exist after tok.
	tail := comment[idx+len(tok):]
	return strings.Contains(tail, "`")
}

// tokenIsAliasInterpolation reports whether `tok` appears inside a
// `${...}` interpolation span on `comment` — e.g. `${cache_hostname}`,
// `${db_dbName}`, `${broker_user}`. The regex strips `${}` and matches
// the inner identifier; without this escape every alias mention
// triggers a false fabrication.
func tokenIsAliasInterpolation(comment, tok string) bool {
	idx := strings.Index(comment, tok)
	if idx < 0 {
		return false
	}
	if idx < 2 {
		return false
	}
	if comment[idx-2:idx] != "${" {
		return false
	}
	tail := comment[idx+len(tok):]
	return strings.HasPrefix(tail, "}")
}

// tokenHasFileExtension reports whether `tok` ends in a recognized
// source / config file extension OR domain TLD. Filenames
// (`config.json`, `vite.config.js`, `tsconfig.json`) match the dotted-
// path regex; so do bare domain references (`zerops.app`, `zerops.io`,
// `app.zerops.io`) — neither is a yaml-field claim.
func tokenHasFileExtension(tok string) bool {
	exts := []string{
		// Source / config file extensions.
		".json", ".yaml", ".yml", ".toml", ".env",
		".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs",
		".svelte", ".vue",
		".go", ".py", ".rb", ".php", ".rs", ".java", ".cs",
		".md", ".txt", ".sql", ".lock", ".sh",
		// Domain TLDs that commonly appear in Zerops recipe content
		// (`zerops.app`, `zerops.io`, `*.dev`, `*.com`, `*.net`).
		".app", ".io", ".com", ".net", ".dev", ".org", ".sh",
	}
	for _, ext := range exts {
		if strings.HasSuffix(tok, ext) {
			return true
		}
	}
	return false
}

// collectYAMLPaths walks a parsed yaml.Node and returns a set of every
// reachable key path in dot-notation form (`project.envVariables`,
// `services.api.mode`) plus every leaf key on its own
// (`envVariables`, `mode`) so a comment can reference either form.
func collectYAMLPaths(n *yaml.Node) map[string]bool {
	out := map[string]bool{}
	walkYAMLNode(n, "", out)
	return out
}

func walkYAMLNode(n *yaml.Node, prefix string, out map[string]bool) {
	if n == nil {
		return
	}
	switch n.Kind {
	case yaml.DocumentNode:
		for _, c := range n.Content {
			walkYAMLNode(c, prefix, out)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			keyNode := n.Content[i]
			valNode := n.Content[i+1]
			key := keyNode.Value
			if key == "" {
				continue
			}
			out[key] = true
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			out[path] = true
			walkYAMLNode(valNode, path, out)
		}
	case yaml.SequenceNode:
		for _, c := range n.Content {
			walkYAMLNode(c, prefix, out)
		}
	case yaml.ScalarNode, yaml.AliasNode:
		// Leaf nodes — already accounted for by the parent's recursion;
		// no further keys to collect.
	}
}

// anyPathSuffixMatches reports whether tok is the suffix of any known
// yaml path. Allows comments to reference a leaf key by its bare name
// (`minRam`) when the full path is `services.0.verticalAutoscaling.minRam`.
func anyPathSuffixMatches(tok string, knownPaths map[string]bool) bool {
	for path := range knownPaths {
		if path == tok {
			return true
		}
		if strings.HasSuffix(path, "."+tok) {
			return true
		}
	}
	return false
}

// proseTokenStopList is a small set of dot/underscore-joined tokens
// that look like field paths but appear in genuine English prose. Add
// entries when a false-positive ships; bias toward surfacing fabricated
// names over silencing prose.
var proseTokenStopList = map[string]bool{}

func isLikelyProseToken(tok string) bool {
	return proseTokenStopList[tok]
}
