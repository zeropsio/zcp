// Tests for: `zcp --version` and `zcp help` (cmd/zcp/main.go) — an agent that
// asks either gets an answer, never MCP serve mode waiting on stdin.
package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestCLIDispatch_VersionAndHelpAreVerbs: the usual spellings of version and
// help are CLI verbs, so run() answers them instead of falling through to
// serve mode.
func TestCLIDispatch_VersionAndHelpAreVerbs(t *testing.T) {
	t.Parallel()
	for _, verb := range []string{"--version", "-v", "help", "--help", "-h"} {
		if _, ok := cliDispatch()[verb]; !ok {
			t.Errorf("%q is not a CLI verb: run() would fall through to serve mode and wait on stdin", verb)
		}
	}
}

// TestUsage_NamesEveryVerb: help names serve and every first-level verb.
func TestUsage_NamesEveryVerb(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	printUsage(&out)
	text := out.String()
	if !strings.Contains(text, "serve") {
		t.Errorf("help does not name serve:\n%s", text)
	}
	for verb := range cliDispatch() {
		if strings.HasPrefix(verb, "-") || verb == "help" {
			continue
		}
		if !strings.Contains(text, "  "+verb+"\n") {
			t.Errorf("help does not name %q:\n%s", verb, text)
		}
	}
}
