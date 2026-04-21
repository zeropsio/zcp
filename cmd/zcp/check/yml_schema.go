package check

import (
	"context"
	"fmt"
	"io"
	"os"

	opschecks "github.com/zeropsio/zcp/internal/ops/checks"
	"github.com/zeropsio/zcp/internal/schema"
)

// yml-schema validates zerops.yaml field names against the live platform
// JSON schema. Requires --schema-json=<path> to hydrate ValidFields;
// when absent, prints a skip note on stderr and exits 0 (avoids making
// shims flaky on agent containers that don't have network access to
// fetch the schema at shim-invocation time).
//
// Expected --schema-json payload: the raw JSON fetched from
// https://api.app-prg1.zerops.io/api/rest/public/settings/zerops-yml-json-schema.json
// (same schema the `internal/schema` package caches at runtime).
func init() {
	registerCheck("yml-schema", runYmlSchema)
}

func runYmlSchema(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("yml-schema", stderr)
	cf := addCommonFlags(fs)
	hostname := fs.String("hostname", "", "codebase hostname (required)")
	schemaJSON := fs.String("schema-json", "", "path to a zerops.yaml JSON schema dump (optional; absent => skip)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *hostname == "" {
		fmt.Fprintln(stderr, "yml-schema: --hostname is required")
		return 1
	}
	if *schemaJSON == "" {
		fmt.Fprintln(stderr, "yml-schema: SKIP — --schema-json not provided (no network fetch in shim)")
		return 0
	}
	data, err := os.ReadFile(*schemaJSON)
	if err != nil {
		fmt.Fprintf(stderr, "yml-schema: reading %s: %v\n", *schemaJSON, err)
		return 1
	}
	validFields, err := parseValidFields(data)
	if err != nil {
		fmt.Fprintf(stderr, "yml-schema: parsing %s: %v\n", *schemaJSON, err)
		return 1
	}
	ymlDir := resolveHostnameDir(cf.path, *hostname)
	checks := opschecks.CheckZeropsYmlFields(ctx, ymlDir, validFields)
	return emitResults(stdout, cf.json, checks)
}

// parseValidFields converts the raw JSON schema into a schema.ValidFields
// the predicate consumes. Routes through schema.ParseZeropsYmlSchema +
// schema.ExtractValidFields to keep the shim aligned with the runtime
// parser the tool-layer uses.
func parseValidFields(data []byte) (*schema.ValidFields, error) {
	parsed, err := schema.ParseZeropsYmlSchema(data)
	if err != nil {
		return nil, fmt.Errorf("parse schema: %w", err)
	}
	vf := schema.ExtractValidFields(parsed)
	if vf == nil {
		return nil, fmt.Errorf("extract valid fields: schema shape did not yield any")
	}
	return vf, nil
}
