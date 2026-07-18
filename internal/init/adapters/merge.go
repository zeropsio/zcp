// Package adapters provides per-agent container-init adapters and the
// shared merge primitives they use to safely upsert ZCP-owned keys into
// pre-existing user config files without clobbering user content.
//
// The merge helpers are the load-bearing backward-compat surface:
// existing users may have hand-edited ~/.claude.json, added other MCP
// servers to ~/.codex/config.toml, or set custom fields the adapter
// doesn't know about — Upsert* preserves all of that.
package adapters

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// LoadJSONFile reads a JSON object from path into a map[string]any.
// Missing file → empty map (not an error: first init is the common case).
// Empty file → empty map. Malformed JSON returns an error so the caller
// can decide whether to bail or overwrite.
func LoadJSONFile(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if data == nil {
		data = map[string]any{}
	}
	return data, nil
}

// SaveJSONFile marshals data as JSON and writes it atomically (temp +
// rename) so a crash mid-write can't corrupt the user's config. Parent
// dirs are created with 0o755; the file is written with 0o644 — same
// permissions as the pre-merge writer.
func SaveJSONFile(path string, data map[string]any) error {
	out, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	return atomicWrite(path, out)
}

// SaveJSONFileIndented is SaveJSONFile with two-space indentation and a
// trailing newline — for project-scope files users read, diff, and commit
// (e.g. .mcp.json), where a compact single-line rewrite would churn git
// diffs. Same atomic-write guarantees.
func SaveJSONFileIndented(path string, data map[string]any) error {
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	return atomicWrite(path, append(out, '\n'))
}

// LoadTOMLFile reads a TOML document from path into a map[string]any.
// Same missing-file / empty-file semantics as LoadJSONFile.
func LoadTOMLFile(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var data map[string]any
	if err := toml.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if data == nil {
		data = map[string]any{}
	}
	return data, nil
}

// SaveTOMLFile marshals data as TOML and writes it atomically. The
// BurntSushi/toml encoder produces deterministic output for sorted map
// keys (it walks reflect-discovered fields alphabetically) so re-saving
// an unchanged map round-trips byte-for-byte — a precondition for
// idempotent ContainerInit.
func SaveTOMLFile(path string, data map[string]any) error {
	out, err := toml.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	return atomicWrite(path, out)
}

// UpsertPath sets data[path[0]][path[1]]…[path[n-1]] = value, creating
// intermediate map[string]any nodes as needed. Returns the (possibly
// mutated) root map. Keys at every level not on the path are preserved.
//
// Panics on empty path — that's a programmer error, not a runtime
// condition.
//
// Example: UpsertPath(cfg, entry, "mcpServers", "zerops") sets only the
// "zerops" key under "mcpServers"; any other "mcpServers" entries the
// user added (puppeteer, gmail, custom) survive untouched.
func UpsertPath(data map[string]any, value any, path ...string) map[string]any {
	if len(path) == 0 {
		panic("adapters.UpsertPath: empty path")
	}
	if data == nil {
		data = map[string]any{}
	}
	cursor := data
	for i, key := range path {
		if i == len(path)-1 {
			cursor[key] = value
			return data
		}
		next, ok := cursor[key].(map[string]any)
		if !ok {
			next = map[string]any{}
			cursor[key] = next
		}
		cursor = next
	}
	return data
}

// ShallowMergeAtPath sets data[path[0]]…[path[n-1]] to a map that
// merges keys from `value` over keys already present at that path.
// Existing keys not in `value` are preserved. Useful when ZCP owns
// SOME keys of a nested object but the user (or another tool) may add
// additional keys to the same object — replacing the whole object via
// UpsertPath would drop those.
//
// Behavior:
//
//   - Target path missing or not a map → behaves like UpsertPath(value).
//   - Target path is a map → for each key in value, overwrite. Keys in
//     existing target but not in value are preserved.
//
// Shallow only — nested maps in `value` replace nested maps in target
// (no recursive merge). Sufficient for the current consumer (Claude's
// projects[VSCodeWorkDir] entry where ZCP owns a flat set of booleans
// + default arrays).
func ShallowMergeAtPath(data map[string]any, value map[string]any, path ...string) map[string]any {
	if len(path) == 0 {
		panic("adapters.ShallowMergeAtPath: empty path")
	}
	if data == nil {
		data = map[string]any{}
	}
	cursor := data
	for i, key := range path {
		if i == len(path)-1 {
			existing, ok := cursor[key].(map[string]any)
			if !ok {
				cursor[key] = value
				return data
			}
			maps.Copy(existing, value)
			return data
		}
		next, ok := cursor[key].(map[string]any)
		if !ok {
			next = map[string]any{}
			cursor[key] = next
		}
		cursor = next
	}
	return data
}

// AppendIfMissingString returns a []any containing every existing entry
// plus `want` if not already present, preserving order (existing
// entries first, `want` appended only when new). Normalizes non-array
// existing values so hand-edited config isn't clobbered:
//
//   - nil / absent          → fresh array containing only `want`
//   - []any (canonical)     → preserved entry-for-entry
//   - scalar (string)       → wrapped to []any{scalar} so a hand-set
//     scalar value survives normalization to array form
//   - other type            → wrapped defensively so unknown future
//     schema shapes are preserved instead of dropped
func AppendIfMissingString(existing any, want string) []any {
	var out []any
	switch v := existing.(type) {
	case nil:
		out = nil
	case []any:
		out = append([]any(nil), v...)
	default:
		out = []any{v}
	}
	for _, v := range out {
		if s, ok := v.(string); ok && s == want {
			return out
		}
	}
	return append(out, want)
}

// EnsureArrayContains upserts a string into the array at
// data[path[0]]…[path[n-1]], creating intermediate map[string]any
// nodes (and the array itself) as needed. Existing entries and their
// order are preserved — see AppendIfMissingString. UpsertPath's
// counterpart for "add to a set" rather than "replace a value".
//
// Panics on empty path — programmer error, not a runtime condition.
func EnsureArrayContains(data map[string]any, value string, path ...string) map[string]any {
	if len(path) == 0 {
		panic("adapters.EnsureArrayContains: empty path")
	}
	if data == nil {
		data = map[string]any{}
	}
	cursor := data
	for i, key := range path {
		if i == len(path)-1 {
			cursor[key] = AppendIfMissingString(cursor[key], value)
			return data
		}
		next, ok := cursor[key].(map[string]any)
		if !ok {
			next = map[string]any{}
			cursor[key] = next
		}
		cursor = next
	}
	return data
}

// EnsureArrayAt guarantees data[path[0]]…[path[n-1]] exists as an array
// WITHOUT appending anything, creating intermediate map[string]any nodes
// as needed: absent/nil → []any{}; existing array → untouched; scalar →
// wrapped to []any{scalar} (preserved, not dropped). For schema-required
// array keys (e.g. Cursor cli.json's permissions.deny, whose absence
// fails Cursor's schema validation) where ZCP must materialize the key
// but owns none of its entries.
//
// Panics on empty path — programmer error, not a runtime condition.
func EnsureArrayAt(data map[string]any, path ...string) map[string]any {
	if len(path) == 0 {
		panic("adapters.EnsureArrayAt: empty path")
	}
	if data == nil {
		data = map[string]any{}
	}
	cursor := data
	for i, key := range path {
		if i == len(path)-1 {
			switch v := cursor[key].(type) {
			case nil:
				cursor[key] = []any{}
			case []any:
				// already an array — untouched
			default:
				cursor[key] = []any{v}
			}
			return data
		}
		next, ok := cursor[key].(map[string]any)
		if !ok {
			next = map[string]any{}
			cursor[key] = next
		}
		cursor = next
	}
	return data
}

// HasPath reports whether data has a value at the given nested path.
// Returns false if any intermediate node is missing or not a map.
// Useful for "should we upsert?" checks where the adapter wants to skip
// when the user already has a custom entry.
func HasPath(data map[string]any, path ...string) bool {
	if len(path) == 0 {
		return false
	}
	cursor := any(data)
	for _, key := range path {
		m, ok := cursor.(map[string]any)
		if !ok {
			return false
		}
		v, present := m[key]
		if !present {
			return false
		}
		cursor = v
	}
	return true
}

// atomicWrite writes data to path via a temp file in the same directory
// + rename, so a crash mid-write leaves either the old content or the
// new content — never a half-written file. Mirrors the pattern in
// init.upsertManagedSection so behavior is consistent across the codebase.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".adapter-*")
	if err != nil {
		return fmt.Errorf("create temp in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename %s: %w", path, err)
	}
	return nil
}
