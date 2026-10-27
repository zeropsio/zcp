package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// defaultSnapshotPath is the committed snapshot location for test validation.
var defaultSnapshotPath = filepath.Join("internal", "knowledge", "testdata", "schema_versions.json")

func runCatalog(args []string) int {
	const usage = "Usage: zcp catalog sync"
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, usage)
		return 1
	}
	if isHelpArg(args[0]) {
		fmt.Fprintln(os.Stdout, usage)
		return 0
	}

	switch args[0] {
	case subcmdSync:
		if _, code, done := commandFlags(usage, args[1:]); done {
			return code
		}
		// `zcp catalog sync` is a thin alias for `zcp schema sync`. A standalone
		// catalog fetch (the old behavior) was a SECOND independent live fetch
		// that wrote only schema_versions.json — exactly the path that let the
		// catalog drift from the embedded schemas. Delegating keeps one refresh
		// path: one fetch refreshes the embedded schemas AND derives the catalog.
		fmt.Fprintln(os.Stderr, "note: `catalog sync` delegates to `schema sync` (refreshes embedded schemas + version catalog from one public fetch)")
		return runSchemaSync()
	default:
		fmt.Fprintf(os.Stderr, "unknown catalog subcommand: %s\n", args[0])
		return 1
	}
}
