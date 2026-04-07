// Package catalog provides version catalog management from public Zerops JSON schemas.
// Used to generate offline snapshots of valid platform versions for test validation.
package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/zeropsio/zcp/internal/schema"
)

// Snapshot represents a point-in-time capture of valid platform versions.
type Snapshot struct {
	Generated string   `json:"generated"`
	Versions  []string `json:"versions"`
}

// Sync fetches the public zerops.yaml and import.yaml JSON schemas, extracts
// all valid version strings (build bases, run bases, service types), deduplicates
// and sorts them, then writes the snapshot to outPath.
// No authentication required — schemas are public endpoints.
func Sync(ctx context.Context, outPath string) (*Snapshot, error) {
	schemas, err := schema.FetchSchemas(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch schemas: %w", err)
	}

	versions := mergeVersions(schemas)
	sort.Strings(versions)

	snap := &Snapshot{
		Generated: time.Now().UTC().Format(time.RFC3339),
		Versions:  versions,
	}

	if err := writeSnapshot(snap, outPath); err != nil {
		return nil, err
	}

	return snap, nil
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

func writeSnapshot(snap *Snapshot, path string) error {
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
