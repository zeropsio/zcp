package recipe

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Run-40 B1 — source-grep for environment-variable reads.
//
// Populates plan.ObservedFacts.EnvReads at feature complete-phase by
// walking each codebase's source root and matching the language's
// idiomatic env-read shapes. Used by the env-reads-derivable gate to
// refuse close when a codebase's zerops.yaml declares envs the source
// can't reach (run-39's S0-6 dead-env class: SEARCH_PUBLIC_HOST,
// SEARCH_SEARCH_KEY, NATS_QUEUE_GROUP).
//
// Patterns covered:
//
//   - process.env.<KEY>           (Node.js — TypeScript, JavaScript)
//   - process.env["<KEY>"]        (Node.js — bracket form)
//   - process.env['<KEY>']
//   - import.meta.env.<KEY>       (Vite, Astro, modern bundlers)
//   - import.meta.env["<KEY>"]
//
// Extensions covered: .ts, .tsx, .js, .jsx, .mjs, .cjs, .svelte, .vue
// — current recipe surfaces use these exclusively. Adding new
// languages (Go's os.Getenv, Python's os.environ, etc.) is one regex
// per pattern.
//
// File-tree pruning skips node_modules, dist, build, .next, .nuxt,
// .svelte-kit, vendor, .git. The walk reads up to a per-file size cap
// (2 MB) so unintended binary assets in the tree don't OOM the walk.
//
// Diagnosed in plans/run-40-evidence-grounded-plan.md §"B1".

// parseDeclaredRunEnvKeys returns the sorted, de-duplicated set of
// run.envVariables key names declared anywhere in the yaml body.
// Reuses collectRunEnvVariables to handle the three valid yaml
// shapes (list-of-setup-blocks, single-setup-block, bare-setup
// shorthand). Returns empty slice when the yaml is unparseable or
// declares no env vars. Used by gateEnvReadsDerivable to compare
// declared keys against source-derived reads. Run-40 B1.
func parseDeclaredRunEnvKeys(yamlBody string) []string {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(yamlBody), &doc); err != nil {
		return nil
	}
	nodes := collectRunEnvVariables(&doc)
	if len(nodes) == 0 {
		return []string{}
	}
	hits := map[string]struct{}{}
	for _, node := range nodes {
		for i := 0; i+1 < len(node.Content); i += 2 {
			keyNode := node.Content[i]
			if keyNode == nil || keyNode.Kind != yaml.ScalarNode {
				continue
			}
			if keyNode.Value != "" {
				hits[keyNode.Value] = struct{}{}
			}
		}
	}
	if len(hits) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(hits))
	for k := range hits {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// envReadFileExtensions enumerates the source file suffixes the
// source-grep walks. Pattern table below assumes JS/TS-family syntax.
var envReadFileExtensions = map[string]struct{}{
	".ts":     {},
	".tsx":    {},
	".js":     {},
	".jsx":    {},
	".mjs":    {},
	".cjs":    {},
	".svelte": {},
	".vue":    {},
}

// envReadSkipDirs are directories the source-grep prunes. Generated
// output trees (dist, build, .next, .nuxt, .svelte-kit), dependency
// trees (node_modules, vendor), and version-control state (.git).
var envReadSkipDirs = map[string]struct{}{
	"node_modules": {},
	"dist":         {},
	"build":        {},
	".next":        {},
	".nuxt":        {},
	".svelte-kit":  {},
	".turbo":       {},
	".cache":       {},
	"vendor":       {},
	".git":         {},
}

// envReadPatterns lists the regexes the source-grep applies to each
// source file. Each pattern captures the env key as submatch 1. Add a
// pattern when a new language joins the recipe surface.
//
// Run-40 fix-up #10 — destructuring + dynamic-key patterns extended.
// Codex code review flagged that the original 6 patterns missed
// idiomatic JavaScript shapes the recipe agent writes:
//
//	const { DB_HOST, DB_PORT } = process.env
//	const { VITE_API_URL } = import.meta.env
//
// The destructuring patterns capture each name inside the braces.
// Multi-name destructures still produce one violation per orphan
// name because the gate iterates declared envs against the read
// set; the regex just needs to populate the set.
//
// Dynamic-key shape `process.env[key]` (variable subscript) is NOT
// statically resolvable — the gate's caller-side carve-out
// (Vite import.meta.env idiomatic-use detector) handles the
// dynamic-import.meta.env case; for runtime dynamic process.env
// reads the agent can record a negation fact or remove the
// declaration. No regex for it on purpose.
var envReadPatterns = []*regexp.Regexp{
	regexp.MustCompile(`process\.env\.([A-Za-z_][A-Za-z0-9_]*)`),
	regexp.MustCompile(`process\.env\["([A-Za-z_][A-Za-z0-9_]*)"\]`),
	regexp.MustCompile(`process\.env\['([A-Za-z_][A-Za-z0-9_]*)'\]`),
	regexp.MustCompile(`import\.meta\.env\.([A-Za-z_][A-Za-z0-9_]*)`),
	regexp.MustCompile(`import\.meta\.env\["([A-Za-z_][A-Za-z0-9_]*)"\]`),
	regexp.MustCompile(`import\.meta\.env\['([A-Za-z_][A-Za-z0-9_]*)'\]`),
}

// envReadDestructurePatterns capture `{ A, B } = process.env` /
// `{ A } = import.meta.env` shapes. Each match's submatch 1 is the
// comma-separated brace body; the inner regex splits and returns
// one KEY per name. Aliasing (`{ DB_HOST: host }`) keeps the
// LEFT side (the env name).
//
// Run-40 fix-up second pass (codex finding #10 PARTIAL closure):
// removed the bogus `=\s*process\.env[^{]*\{...\}` pattern that
// matched `process.env BEFORE the destructure braces`. No such
// JS shape exists — destructure always has braces on the LEFT of
// the assignment. The pattern was producing false positives on
//
//	const env = process.env; const config = { DB_HOST: "x" }
//
// where `DB_HOST` is a config-object key, not an env read.
// Codex flagged this in the second-pass review as worth closing
// before any future env-reads grep expansion.
var envReadDestructurePatterns = []*regexp.Regexp{
	regexp.MustCompile(`\{([^}]+)\}\s*=\s*process\.env`),
	regexp.MustCompile(`\{([^}]+)\}\s*=\s*import\.meta\.env`),
}

// envReadDestructureKeyRe matches one entry inside a destructure
// brace body. Captures the env name (left side of `:` aliasing).
var envReadDestructureKeyRe = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)`)

// envReadFileSizeCap is the maximum per-file body the source-grep
// reads. Source files this large are almost certainly bundled
// vendor blobs that escaped the dir-prune; skip them rather than
// risk slowing the walk.
const envReadFileSizeCap = 2 * 1024 * 1024 // 2 MB

// sourceGrepEnvReads walks sourceRoot for env-var read sites and
// returns the sorted, de-duplicated KEY set. Errors propagate so the
// caller can decide whether to refuse the phase close (missing
// sourceRoot) or skip the codebase silently (sim path).
func sourceGrepEnvReads(sourceRoot string) ([]string, error) {
	if sourceRoot == "" {
		return nil, errors.New("sourceGrepEnvReads: empty sourceRoot")
	}
	if _, err := os.Stat(sourceRoot); err != nil {
		return nil, fmt.Errorf("sourceGrepEnvReads: stat sourceRoot %s: %w", sourceRoot, err)
	}
	hits := map[string]struct{}{}
	walkErr := filepath.WalkDir(sourceRoot, func(path string, d fs.DirEntry, walkErr error) error {
		// A single unreadable entry shouldn't abort the walk —
		// continue past errors on individual files; the
		// stat-then-read failure modes below mirror this contract.
		if walkErr != nil {
			return nil //nolint:nilerr // permissive walk: keep scanning siblings
		}
		if d.IsDir() {
			name := d.Name()
			if _, skip := envReadSkipDirs[name]; skip {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if _, ok := envReadFileExtensions[ext]; !ok {
			return nil
		}
		fi, statErr := d.Info()
		if statErr != nil {
			return nil //nolint:nilerr // permissive walk: skip un-statable file
		}
		if fi.Size() > envReadFileSizeCap {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil //nolint:nilerr // permissive walk: skip unreadable file
		}
		for _, re := range envReadPatterns {
			for _, m := range re.FindAllStringSubmatch(string(body), -1) {
				if len(m) < 2 || m[1] == "" {
					continue
				}
				hits[m[1]] = struct{}{}
			}
		}
		// Run-40 fix-up #10 — destructuring patterns. Captures the
		// brace body, then the inner regex pulls each KEY out. Handles
		// aliasing (`{ DB_HOST: host } = process.env`) by capturing
		// the left side of the colon.
		for _, re := range envReadDestructurePatterns {
			for _, m := range re.FindAllStringSubmatch(string(body), -1) {
				if len(m) < 2 || m[1] == "" {
					continue
				}
				for entry := range strings.SplitSeq(m[1], ",") {
					// `KEY` / `KEY: alias` / `KEY = default`. The
					// inner regex picks the leading identifier which
					// is the env name in every shape.
					if sub := envReadDestructureKeyRe.FindString(entry); sub != "" {
						hits[sub] = struct{}{}
					}
				}
			}
		}
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("sourceGrepEnvReads: walk %s: %w", sourceRoot, walkErr)
	}
	if len(hits) == 0 {
		return []string{}, nil
	}
	out := make([]string, 0, len(hits))
	for k := range hits {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

// populateEnvReadsFromSource walks each codebase in the session's
// plan and populates Plan.ObservedFacts.EnvReads[hostname]. Codebases
// without a SourceRoot or with an unreadable tree are skipped (their
// existing EnvReads entry is preserved if present). The refreshed
// plan persists to plan.json so re-renders see the same state.
//
// Concurrency: snapshot codebases under sess.mu, walk unlocked, then
// re-lock to write the field. Walks can take tens of ms across a
// recipe's worth of code; the lock-release prevents handler
// starvation.
func populateEnvReadsFromSource(sess *Session) error {
	sess.mu.Lock()
	if sess.Plan == nil {
		sess.mu.Unlock()
		return nil
	}
	outputRoot := sess.OutputRoot
	codebases := make([]Codebase, len(sess.Plan.Codebases))
	copy(codebases, sess.Plan.Codebases)
	sess.mu.Unlock()

	derived := map[string][]string{}
	for _, cb := range codebases {
		if cb.SourceRoot == "" {
			continue
		}
		reads, err := sourceGrepEnvReads(cb.SourceRoot)
		if err != nil {
			// Conservative: skip codebases the engine couldn't walk
			// rather than refusing close on infrastructure failure.
			// The agent will see the missing entry surface as a gate
			// pass-through if no envs declared; if envs ARE declared,
			// the gate's "no entry" branch is documented as a soft
			// notice (caller decides whether to block).
			continue
		}
		derived[cb.Hostname] = reads
	}

	sess.mu.Lock()
	if sess.Plan == nil {
		sess.mu.Unlock()
		return nil
	}
	if sess.Plan.ObservedFacts.EnvReads == nil {
		sess.Plan.ObservedFacts.EnvReads = map[string][]string{}
	}
	maps.Copy(sess.Plan.ObservedFacts.EnvReads, derived)
	snapshot := *sess.Plan
	sess.mu.Unlock()

	if err := WritePlan(outputRoot, &snapshot); err != nil {
		return fmt.Errorf("persist plan after env-reads populate: %w", err)
	}
	return nil
}
