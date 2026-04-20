// Package check implements the `zcp check <name>` CLI shim tree. Each
// subcommand is a thin adapter around a reusable predicate function in
// internal/ops/checks. Gate (server-side tool-layer) and shim (this
// package) share exactly one predicate implementation per check — they
// cannot diverge because there is only one Go function that computes
// the result.
//
// Output shape: one line per StepCheck row, either text or ndjson.
// Exit code: 0 if every row is StatusPass, 1 otherwise (including
// usage errors).
package check

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// Run is the CLI entry point for `zcp check`. Invoked from cmd/zcp/main
// with os.Args[2:]. Calls os.Exit with 0 (all pass) or 1 (any fail,
// usage error, I/O failure).
func Run(args []string) {
	os.Exit(run(context.Background(), args, os.Stdout, os.Stderr))
}

// run is the testable core. Returns the process exit code.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 1
	}
	name := args[0]
	rest := args[1:]
	handler, ok := registry[name]
	if !ok {
		fmt.Fprintf(stderr, "unknown check: %s\n\n", name)
		printUsage(stderr)
		return 1
	}
	return handler(ctx, rest, stdout, stderr)
}

// handler is the signature every shim implements. Returns the subcommand
// exit code (0 pass / 1 fail / non-zero usage error).
type handler func(ctx context.Context, args []string, stdout, stderr io.Writer) int

// registry maps subcommand name → handler. Populated by each shim file's
// init() — keeping the wiring co-located with the shim so adding a new
// check is a one-file change.
var registry = map[string]handler{}

// registerCheck wires a subcommand into the registry. Panics on duplicate
// registration — a programming error surfaced at startup, not in
// production, because main.go imports this package eagerly.
func registerCheck(name string, h handler) {
	if _, exists := registry[name]; exists {
		panic("check: duplicate registration for " + name)
	}
	registry[name] = h
}

// writeJSONLine marshals v to w with a trailing newline. Marshal errors
// on the StepCheck struct / string-keyed map are impossible in practice
// (plain data types, no channels / funcs / unsupported values), so the
// error is written to the same stream as a diagnostic line rather than
// swallowed — preserves the ndjson stream shape without dropping rows.
func writeJSONLine(w io.Writer, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		fmt.Fprintf(w, `{"status":"marshal_error","detail":%q}`+"\n", err.Error())
		return
	}
	_, _ = w.Write(data)
	_, _ = w.Write([]byte{'\n'})
}

// printUsage lists every registered subcommand to stderr. Called on
// empty-args and unknown-subcommand paths.
func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: zcp check <name> [flags]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Available checks:")
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintln(w, "  "+n)
	}
}

// emitResults prints every StepCheck row and returns an exit code.
// Text mode: `PASS <name>` / `FAIL <name>: <detail>`, one per line.
// JSON mode (--json): one JSON object per row (ndjson).
// Empty `checks` means the predicate declined to emit (graceful skip
// per predicate contract) — emit a single skip line and exit 0.
func emitResults(w io.Writer, asJSON bool, checks []workflow.StepCheck) int {
	if len(checks) == 0 {
		if asJSON {
			writeJSONLine(w, map[string]string{"status": "skip", "detail": "no rows emitted"})
		} else {
			fmt.Fprintln(w, "SKIP no rows emitted (predicate declined — upstream surface concern)")
		}
		return 0
	}
	exit := 0
	for _, c := range checks {
		if strings.EqualFold(c.Status, "fail") {
			exit = 1
		}
		if asJSON {
			writeJSONLine(w, c)
			continue
		}
		if strings.EqualFold(c.Status, "pass") {
			if c.Detail != "" {
				fmt.Fprintf(w, "PASS %s — %s\n", c.Name, c.Detail)
			} else {
				fmt.Fprintf(w, "PASS %s\n", c.Name)
			}
			continue
		}
		// Treat anything non-pass as failure-shaped output.
		if c.Detail != "" {
			fmt.Fprintf(w, "FAIL %s: %s\n", c.Name, c.Detail)
		} else {
			fmt.Fprintf(w, "FAIL %s\n", c.Name)
		}
	}
	return exit
}

// commonFlags holds flags shared across most subcommands.
type commonFlags struct {
	path string // project/mount root
	json bool   // ndjson output
}

// addCommonFlags registers --path / --json on the given FlagSet. Returns
// a pointer the caller reads after fs.Parse.
func addCommonFlags(fs *flag.FlagSet) *commonFlags {
	cf := &commonFlags{}
	fs.StringVar(&cf.path, "path", ".", "project / mount root (default: current directory)")
	fs.BoolVar(&cf.json, "json", false, "emit ndjson instead of plain text")
	return cf
}

// newFlagSet builds a FlagSet that routes output to the caller's stderr
// writer instead of os.Stderr — important for testability.
func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet("zcp check "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

// resolveHostnameDir picks the directory under projectRoot that holds
// a given hostname's files. Tries `{host}dev` first (canonical scaffold
// convention), falls back to `{host}`, then the project root itself.
// Returns the first path that exists as a directory; falls through to
// projectRoot when nothing matches so downstream reads produce the same
// "file not found" errors they would against the canonical layout.
func resolveHostnameDir(projectRoot, hostname string) string {
	for _, cand := range []string{hostname + "dev", hostname} {
		full := filepath.Join(projectRoot, cand)
		if info, err := os.Stat(full); err == nil && info.IsDir() {
			return full
		}
	}
	return projectRoot
}

// readHostnameReadme returns the README.md body for a hostname. Returns
// the path that was tried + an error when the file is missing; callers
// surface the error as a fail row with the path in the detail.
func readHostnameReadme(projectRoot, hostname string) (string, string, error) {
	dir := resolveHostnameDir(projectRoot, hostname)
	path := filepath.Join(dir, "README.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return path, "", err
	}
	return path, string(data), nil
}

// extractFragmentBody returns the content between a ZEROPS_EXTRACT_START
// and matching END marker, identical to the tool-layer extractor used by
// the gate. Declared here so the shims can do the same fragment slicing
// without importing tool-internal helpers.
func extractFragmentBody(content, name string) string {
	startMarker := "#ZEROPS_EXTRACT_START:" + name + "#"
	endMarker := "#ZEROPS_EXTRACT_END:" + name + "#"
	startIdx := strings.Index(content, startMarker)
	if startIdx < 0 {
		return ""
	}
	afterStart := startIdx + len(startMarker)
	lineEnd := strings.Index(content[afterStart:], "\n")
	if lineEnd < 0 {
		return ""
	}
	contentStart := afterStart + lineEnd + 1
	endIdx := strings.Index(content[contentStart:], endMarker)
	if endIdx < 0 {
		return ""
	}
	extractEnd := contentStart + endIdx
	for extractEnd > contentStart && content[extractEnd-1] != '\n' {
		extractEnd--
	}
	return strings.TrimSpace(content[contentStart:extractEnd])
}

// extractYAMLBlock returns the body of the first ```yaml (or ```yml)
// fenced block in content. Mirrors the tool-layer helper used by the
// comment-specificity checker.
func extractYAMLBlock(content string) string {
	start := strings.Index(content, "```yaml")
	if start < 0 {
		start = strings.Index(content, "```yml")
	}
	if start < 0 {
		return ""
	}
	lineEnd := strings.Index(content[start:], "\n")
	if lineEnd < 0 {
		return ""
	}
	blockStart := start + lineEnd + 1
	end := strings.Index(content[blockStart:], "```")
	if end < 0 {
		return ""
	}
	return content[blockStart : blockStart+end]
}
