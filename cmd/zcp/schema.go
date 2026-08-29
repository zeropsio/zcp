package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/catalog"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/schema"
)

// subcmdSync is the "sync" subcommand verb, shared by `zcp schema sync` and the
// `zcp catalog sync` alias.
const subcmdSync = "sync"

// runSchema handles `zcp schema sync` and `zcp schema check`.
//
//	sync  — fetch the live public schemas, write the committed embedded copies,
//	        and derive active_versions.json from the SAME fetch (one pass, so
//	        the embedded schema and the version catalog cannot drift).
//	check — fetch the live schemas and report drift vs the committed copies;
//	        exits 0 = no drift, 2 = drift. Default mode preserves local
//	        SKIP-on-inconclusive behavior; --strict exits 1 on auth/fetch errors.
func runSchema(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: zcp schema {sync|check}")
		return 1
	}
	switch args[0] {
	case subcmdSync:
		return runSchemaSync()
	case "check":
		strict := false
		switch {
		case len(args) == 1:
		case len(args) == 2 && args[1] == "--strict":
			strict = true
		default:
			fmt.Fprintln(os.Stderr, "Usage: zcp schema check [--strict]")
			return 1
		}
		return runSchemaCheck(strict)
	default:
		fmt.Fprintf(os.Stderr, "unknown schema subcommand: %s\n", args[0])
		return 1
	}
}

// runSchemaSync is the thin return-code wrapper; schemaSyncCore holds the
// testable logic. Both pin the CANONICAL host (NOT auth/ZCP_API_HOST): the
// embedded copy + active_versions.json are SHARED committed artifacts that must
// not vary by whoever's ZCP_API_HOST ran the sync.
func runSchemaSync() int {
	activeVersions, err := schemaActiveVersionsProvider(schema.CanonicalAPIHost)
	if err != nil {
		log.Printf("schema sync: active service type versions: %v", err)
		return 1
	}
	if err := schemaSyncCore(schema.CanonicalAPIHost, defaultSnapshotPath, activeVersions); err != nil {
		log.Printf("schema sync: %v", err)
		return 1
	}
	return 0
}

func schemaSyncCore(host, snapshotPath string, activeVersions schema.ActiveVersionsProvider) error {
	if activeVersions == nil {
		return fmt.Errorf("active service type version provider is required")
	}
	raw, err := schema.FetchRawSchemas(context.Background(), host)
	if err != nil {
		return err
	}
	activeForms, err := activeVersions(context.Background())
	if err != nil {
		return fmt.Errorf("fetch active service type versions: %w", err)
	}
	if err := raw.ApplyActiveFilter(activeForms); err != nil {
		return err
	}
	if err := raw.WriteEmbedded(); err != nil {
		return err
	}
	snap := catalog.SnapshotFromSchemas(raw.Parsed)
	if err := catalog.WriteSnapshot(snap, snapshotPath); err != nil {
		return err
	}
	zeropsPath, importPath := schema.EmbeddedTestdataPaths()
	fmt.Fprintf(os.Stderr, "schema sync: wrote %s, %s, and %d versions to %s\n",
		zeropsPath, importPath, len(snap.Versions), snapshotPath)
	return nil
}

// runSchemaCheck is the thin wrapper; schemaCheckCore holds the testable logic.
// Canonical-pinned (see runSchemaSync) so the CI sentinel's live-vs-committed
// comparison stays apples-to-apples regardless of ZCP_API_HOST.
func runSchemaCheck(strict bool) int {
	return runSchemaCheckWith(strict, os.Stderr, schemaActiveVersionsProvider, schemaCheckCore)
}

func runSchemaCheckWith(
	strict bool,
	out io.Writer,
	provider func(string) (schema.ActiveVersionsProvider, error),
	checkFn func(string, schema.ActiveVersionsProvider) (schema.DriftReport, error),
) int {
	activeVersions, activeErr := provider(schema.CanonicalAPIHost)
	if activeErr != nil {
		if strict {
			fmt.Fprintf(out, "schema check: ERROR — could not construct active-version client: %v\n", activeErr)
			return 1
		}
		fmt.Fprintf(out, "schema check: SKIP — could not construct active-version client: %v\n", activeErr)
		return 0
	}
	report, fetchErr := checkFn(schema.CanonicalAPIHost, activeVersions)
	if fetchErr != nil {
		if strict {
			fmt.Fprintf(out, "schema check: ERROR — could not fetch a valid live schema: %v\n", fetchErr)
			return 1
		}
		fmt.Fprintf(out, "schema check: SKIP — could not fetch a valid live schema: %v\n", fetchErr)
		return 0
	}
	if !report.HasDrift() {
		fmt.Fprintln(out, "schema check: OK — committed schema matches live")
		return 0
	}
	var which []string
	if report.ZeropsDrift {
		which = append(which, "zerops.yaml")
	}
	if report.ImportDrift {
		which = append(which, "import.yaml")
	}
	fmt.Fprintf(out, "schema check: DRIFT in %v — run `make schema-sync` and commit\n", which)
	return 2
}

// schemaCheckCore fetches from host and reports drift vs the committed copy. A
// non-nil error means "could not fetch a valid schema" (the wrapper either
// self-skips or fails in strict mode); the host is a parameter, never read from
// the environment, so dev tooling cannot accidentally drift-check against a
// non-canonical instance.
func schemaCheckCore(host string, activeVersions schema.ActiveVersionsProvider) (schema.DriftReport, error) {
	if activeVersions == nil {
		return schema.DriftReport{}, fmt.Errorf("active service type version provider is required")
	}
	raw, err := schema.FetchRawSchemas(context.Background(), host)
	if err != nil {
		return schema.DriftReport{}, err
	}
	activeForms, err := activeVersions(context.Background())
	if err != nil {
		return schema.DriftReport{}, fmt.Errorf("fetch active service type versions: %w", err)
	}
	if err := raw.ApplyActiveFilter(activeForms); err != nil {
		return schema.DriftReport{}, err
	}
	return raw.CheckDrift()
}

func schemaActiveVersionsProvider(host string) (schema.ActiveVersionsProvider, error) {
	creds, err := auth.ResolveCredentials()
	if err != nil {
		return nil, err
	}
	client, err := platform.NewZeropsClient(creds.Token, host)
	if err != nil {
		return nil, fmt.Errorf("create platform client: %w", err)
	}
	return client.ActiveServiceTypeVersions, nil
}
