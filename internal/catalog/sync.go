// Package catalog provides version catalog management from public Zerops JSON schemas.
// Used to generate offline snapshots of valid platform versions for test validation.
package catalog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/zeropsio/zcp/internal/schema"
)

// Snapshot represents a capture of valid platform versions. Deliberately
// has no timestamp: the snapshot is content-addressed (versions list
// only), so two regenerations against the same upstream schemas produce
// byte-identical files. Eliminates the rebase-conflict-on-every-release
// pattern that the prior `Generated: time.Now()` field caused. Freshness
// is observable via `git log -1 -- testdata/active_versions.json`.
type Snapshot struct {
	Versions []string `json:"versions"`
}

// SnapshotFromSchemas builds the version snapshot from already-fetched schemas
// (no I/O). `zcp schema sync` calls this with the SAME parsed schemas it writes
// to the embedded testdata, so active_versions.json is a pure projection of the
// embedded copy and the two cannot drift. There is intentionally NO standalone
// catalog-fetch orchestrator: a second independent fetch was exactly the path
// that let active_versions.json drift from the embedded schemas, so the only
// refresh entry point is `zcp schema sync` (one fetch → both artifacts).
func SnapshotFromSchemas(schemas *schema.Schemas) *Snapshot {
	versions := mergeVersions(schemas)
	sort.Strings(versions)
	return &Snapshot{Versions: versions}
}

// mergeVersions combines build bases, run bases, and import service types
// into a single deduplicated list.
func mergeVersions(schemas *schema.Schemas) []string {
	seen := make(map[string]bool)
	var versions []string

	add := func(values []string) {
		for _, v := range values {
			if v != "" && !seen[v] {
				seen[v] = true
				versions = append(versions, v)
			}
		}
	}

	if schemas.ZeropsYml != nil {
		add(schemas.ZeropsYml.BuildBases)
		add(schemas.ZeropsYml.RunBases)
	}
	if schemas.ImportYml != nil {
		add(schemas.ImportYml.ServiceTypes)
	}

	return versions
}

// WriteSnapshot writes the snapshot as deterministic, content-addressed JSON
// (sorted versions, no timestamp) so regenerations are byte-identical.
func WriteSnapshot(snap *Snapshot, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}

	return nil
}
