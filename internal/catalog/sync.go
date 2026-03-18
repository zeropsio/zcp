// Package catalog provides API-driven version catalog management.
// Used to generate offline snapshots of active platform versions for test validation.
package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// Snapshot represents a point-in-time capture of active platform versions.
type Snapshot struct {
	Generated string   `json:"generated"`
	Versions  []string `json:"versions"`
}

// specialTypes are service types not returned by the API catalog.
var specialTypes = []string{"object-storage", "shared-storage", "static"}

// internalPrefixes are version name prefixes for internal/system service types
// that should be excluded from the recipe validation catalog.
var internalPrefixes = []string{"prepare-", "prepare_"}

// internalNames are exact version names for internal/system service types.
var internalNames = map[string]bool{
	"build_runtime":    true,
	"core":             true,
	"l7-http-balancer": true,
	"runtime":          true,
}

// Sync fetches all ACTIVE service stack type versions from the platform API
// and writes a sorted snapshot to the given path.
func Sync(ctx context.Context, client platform.Client, outPath string) (*Snapshot, error) {
	types, err := client.ListServiceStackTypes(ctx)
	if err != nil {
		return nil, fmt.Errorf("list service stack types: %w", err)
	}

	versions := activeVersions(types)

	// Add special types not in API (deduplicated).
	seen := make(map[string]bool, len(versions))
	for _, v := range versions {
		seen[v] = true
	}
	for _, st := range specialTypes {
		if !seen[st] {
			versions = append(versions, st)
			seen[st] = true
		}
	}

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

// activeVersions extracts all ACTIVE version names from the platform catalog,
// excluding internal/system types.
func activeVersions(types []platform.ServiceStackType) []string {
	seen := make(map[string]bool)
	var versions []string
	for _, st := range types {
		for _, v := range st.Versions {
			if v.Status != "ACTIVE" || seen[v.Name] || isInternal(v.Name) {
				continue
			}
			seen[v.Name] = true
			versions = append(versions, v.Name)
		}
	}
	return versions
}

// isInternal returns true if the version name is an internal/system type.
func isInternal(name string) bool {
	if internalNames[name] {
		return true
	}
	for _, prefix := range internalPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
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
