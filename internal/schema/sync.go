package schema

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
)

// Dev/CI-time schema refresh + drift detection.
//
// These are the ONLY paths that fetch the live public schema for committing —
// the runtime never writes the embedded copy. `make schema-sync` refreshes the
// committed embedded schemas (and, via the catalog package, derives
// active_versions.json from the SAME fetch so the two can't drift). The drift
// sentinel (`zcp schema check`) compares the live schema against the committed
// copy and is reachability-gated (it self-skips on an unreachable or poisoned
// endpoint rather than failing).

// RawSchemas holds the canonicalised JSON bytes of both schemas plus their
// parsed form. Raw is what gets written to the embedded testdata files; Parsed
// feeds the catalog projection so both artifacts come from one fetch.
type RawSchemas struct {
	ZeropsRaw []byte
	ImportRaw []byte
	Parsed    *Schemas
}

// FetchRawSchemas fetches both public schemas, canonicalises them (stable,
// key-sorted, indented JSON), parses them, and applies the poison guard. It is
// the single source for `schema sync` and `schema check`.
func FetchRawSchemas(ctx context.Context, apiHost string) (*RawSchemas, error) {
	zeropsURL, importURL := URLs(apiHost)
	zeropsData, err := fetchURL(ctx, zeropsURL)
	if err != nil {
		return nil, fmt.Errorf("fetch zerops.yaml schema: %w", err)
	}
	importData, err := fetchURL(ctx, importURL)
	if err != nil {
		return nil, fmt.Errorf("fetch import.yaml schema: %w", err)
	}

	zeropsCanon, err := canonicalJSON(zeropsData)
	if err != nil {
		return nil, fmt.Errorf("canonicalize zerops schema: %w", err)
	}
	importCanon, err := canonicalJSON(importData)
	if err != nil {
		return nil, fmt.Errorf("canonicalize import schema: %w", err)
	}

	zeropsYml, err := ParseZeropsYmlSchema(zeropsCanon)
	if err != nil {
		return nil, err
	}
	importYml, err := ParseImportYmlSchema(importCanon)
	if err != nil {
		return nil, err
	}
	if err := rejectEmptyEnums(zeropsYml, importYml); err != nil {
		return nil, err
	}

	return &RawSchemas{
		ZeropsRaw: zeropsCanon,
		ImportRaw: importCanon,
		Parsed:    &Schemas{ZeropsYml: zeropsYml, ImportYml: importYml},
	}, nil
}

// EmbeddedTestdataPaths returns the committed embedded schema file locations,
// relative to the repo root (where the sync command runs).
func EmbeddedTestdataPaths() (zerops string, importYml string) {
	return "internal/schema/testdata/zerops_yml_schema.json",
		"internal/schema/testdata/import_yml_schema.json"
}

// WriteEmbedded writes the canonicalised schema bytes to the committed embedded
// testdata files. A trailing newline keeps the files git-clean.
func (r *RawSchemas) WriteEmbedded() error {
	zeropsPath, importPath := EmbeddedTestdataPaths()
	if err := os.WriteFile(zeropsPath, withNewline(r.ZeropsRaw), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", zeropsPath, err)
	}
	if err := os.WriteFile(importPath, withNewline(r.ImportRaw), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", importPath, err)
	}
	return nil
}

// DriftReport describes how the live schema differs from the committed copy.
type DriftReport struct {
	ZeropsDrift bool
	ImportDrift bool
}

func (d DriftReport) HasDrift() bool { return d.ZeropsDrift || d.ImportDrift }

// CheckDrift compares freshly-fetched canonical schemas against the committed
// embedded files. Byte comparison is meaningful because both sides are
// canonicalised identically.
func (r *RawSchemas) CheckDrift() (DriftReport, error) {
	zeropsPath, importPath := EmbeddedTestdataPaths()
	committedZerops, err := os.ReadFile(zeropsPath)
	if err != nil {
		return DriftReport{}, fmt.Errorf("read %s: %w", zeropsPath, err)
	}
	committedImport, err := os.ReadFile(importPath)
	if err != nil {
		return DriftReport{}, fmt.Errorf("read %s: %w", importPath, err)
	}
	return DriftReport{
		ZeropsDrift: !bytes.Equal(withNewline(r.ZeropsRaw), committedZerops),
		ImportDrift: !bytes.Equal(withNewline(r.ImportRaw), committedImport),
	}, nil
}

// canonicalJSON re-encodes arbitrary JSON into a stable, key-sorted, 2-space
// indented form so committed schema files and drift comparisons are
// deterministic across fetches and machines.
func canonicalJSON(raw []byte) ([]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return json.MarshalIndent(v, "", "  ")
}

func withNewline(b []byte) []byte {
	if len(b) > 0 && b[len(b)-1] == '\n' {
		return b
	}
	return append(append([]byte{}, b...), '\n')
}
