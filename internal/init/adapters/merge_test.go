package adapters

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestUpsertPath_SetsValueAtLeaf(t *testing.T) {
	t.Parallel()
	data := map[string]any{}
	UpsertPath(data, "value", "a", "b", "c")
	if data["a"].(map[string]any)["b"].(map[string]any)["c"] != "value" {
		t.Errorf("UpsertPath did not set leaf: %v", data)
	}
}

func TestUpsertPath_PreservesSiblingKeysAtEveryLevel(t *testing.T) {
	t.Parallel()
	data := map[string]any{
		"mcpServers": map[string]any{
			"puppeteer": map[string]any{"command": "puppeteer-mcp"},
			"gmail":     map[string]any{"command": "gmail-mcp"},
		},
		"customField": "user-value",
	}
	UpsertPath(data, map[string]any{"command": "zcp", "args": []string{"serve"}}, "mcpServers", "zerops")
	servers := data["mcpServers"].(map[string]any)
	if _, ok := servers["puppeteer"]; !ok {
		t.Error("puppeteer key clobbered by upsert")
	}
	if _, ok := servers["gmail"]; !ok {
		t.Error("gmail key clobbered by upsert")
	}
	if _, ok := servers["zerops"]; !ok {
		t.Error("zerops key not added by upsert")
	}
	if data["customField"] != "user-value" {
		t.Error("top-level user field clobbered by upsert")
	}
}

func TestUpsertPath_OverwritesExistingValueAtLeaf(t *testing.T) {
	t.Parallel()
	data := map[string]any{
		"mcpServers": map[string]any{
			"zerops": map[string]any{"command": "old-command"},
		},
	}
	UpsertPath(data, map[string]any{"command": "new-command"}, "mcpServers", "zerops")
	cmd := data["mcpServers"].(map[string]any)["zerops"].(map[string]any)["command"]
	if cmd != "new-command" {
		t.Errorf("UpsertPath did not overwrite existing leaf: got %v", cmd)
	}
}

func TestUpsertPath_IdempotentOnRerun(t *testing.T) {
	t.Parallel()
	data := map[string]any{}
	UpsertPath(data, "value", "a", "b")
	first, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal first: %v", err)
	}
	UpsertPath(data, "value", "a", "b")
	second, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal second: %v", err)
	}
	if string(first) != string(second) {
		t.Errorf("UpsertPath not idempotent:\n  first:  %s\n  second: %s", first, second)
	}
}

func TestUpsertPath_CreatesIntermediateMapsWhenAbsent(t *testing.T) {
	t.Parallel()
	data := map[string]any{"unrelated": "value"}
	UpsertPath(data, "leaf", "x", "y", "z")
	if got := data["x"].(map[string]any)["y"].(map[string]any)["z"]; got != "leaf" {
		t.Errorf("intermediate maps not created: got %v", got)
	}
	if data["unrelated"] != "value" {
		t.Error("unrelated top-level key clobbered")
	}
}

func TestHasPath_TrueForExistingNestedKey(t *testing.T) {
	t.Parallel()
	data := map[string]any{"a": map[string]any{"b": "leaf"}}
	if !HasPath(data, "a", "b") {
		t.Error("HasPath false for present nested key")
	}
}

func TestHasPath_FalseForMissingKeyOrNonMapIntermediate(t *testing.T) {
	t.Parallel()
	data := map[string]any{"a": "leaf"}
	if HasPath(data, "a", "b") {
		t.Error("HasPath true for path into a non-map leaf")
	}
	if HasPath(data, "missing", "anything") {
		t.Error("HasPath true for missing top-level key")
	}
}

func TestLoadJSONFile_MissingFileReturnsEmptyMap(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "nonexistent.json")
	data, err := LoadJSONFile(path)
	if err != nil {
		t.Fatalf("LoadJSONFile on missing file: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("expected empty map for missing file, got %v", data)
	}
}

func TestLoadJSONFile_EmptyFileReturnsEmptyMap(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "empty.json")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := LoadJSONFile(path)
	if err != nil {
		t.Fatalf("LoadJSONFile on empty file: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("expected empty map for empty file, got %v", data)
	}
}

func TestSaveJSONFile_AtomicAndIdempotent(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "out.json")
	want := map[string]any{"key": "value"}
	if err := SaveJSONFile(path, want); err != nil {
		t.Fatalf("SaveJSONFile: %v", err)
	}
	first, _ := os.ReadFile(path)
	if err := SaveJSONFile(path, want); err != nil {
		t.Fatalf("SaveJSONFile re-run: %v", err)
	}
	second, _ := os.ReadFile(path)
	if string(first) != string(second) {
		t.Errorf("SaveJSONFile not byte-identical on rerun:\n  first:  %s\n  second: %s", first, second)
	}
	got, err := LoadJSONFile(path)
	if err != nil {
		t.Fatalf("LoadJSONFile round-trip: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round-trip mismatch:\n  got:  %v\n  want: %v", got, want)
	}
}

func TestSaveJSONFileIndented_HumanReadableAndStable(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "out.json")
	want := map[string]any{"mcpServers": map[string]any{"zerops": map[string]any{"command": "zcp"}}}
	if err := SaveJSONFileIndented(path, want); err != nil {
		t.Fatalf("SaveJSONFileIndented: %v", err)
	}
	first, _ := os.ReadFile(path)
	text := string(first)
	if !strings.Contains(text, "\n  \"mcpServers\"") {
		t.Errorf("output not two-space indented:\n%s", text)
	}
	if !strings.HasSuffix(text, "\n") {
		t.Errorf("output must end with a trailing newline:\n%q", text)
	}
	if err := SaveJSONFileIndented(path, want); err != nil {
		t.Fatalf("SaveJSONFileIndented re-run: %v", err)
	}
	second, _ := os.ReadFile(path)
	if string(first) != string(second) {
		t.Errorf("not byte-identical on rerun:\n  first:  %s\n  second: %s", first, second)
	}
	got, err := LoadJSONFile(path)
	if err != nil {
		t.Fatalf("LoadJSONFile round-trip: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round-trip mismatch:\n  got:  %v\n  want: %v", got, want)
	}
}

func TestEnsureArrayAt_AbsentKey_CreatesEmptyArray(t *testing.T) {
	t.Parallel()
	data := map[string]any{"permissions": map[string]any{"allow": []any{"Mcp(zerops:*)"}}}
	EnsureArrayAt(data, "permissions", "deny")
	deny, ok := data["permissions"].(map[string]any)["deny"].([]any)
	if !ok || len(deny) != 0 {
		t.Errorf("deny = %v (ok=%v), want empty []any", deny, ok)
	}
}

func TestEnsureArrayAt_ExistingArray_Untouched(t *testing.T) {
	t.Parallel()
	data := map[string]any{"permissions": map[string]any{"deny": []any{"Shell(rm)"}}}
	EnsureArrayAt(data, "permissions", "deny")
	deny, _ := data["permissions"].(map[string]any)["deny"].([]any)
	if len(deny) != 1 || deny[0] != "Shell(rm)" {
		t.Errorf("existing deny entries lost: %v", deny)
	}
}

func TestEnsureArrayAt_ScalarExisting_WrappedNotDropped(t *testing.T) {
	t.Parallel()
	data := map[string]any{"permissions": map[string]any{"deny": "Shell(rm)"}}
	EnsureArrayAt(data, "permissions", "deny")
	deny, _ := data["permissions"].(map[string]any)["deny"].([]any)
	if len(deny) != 1 || deny[0] != "Shell(rm)" {
		t.Errorf("hand-set scalar deny not preserved as array: %v", deny)
	}
}

func TestEnsureArrayAt_CreatesIntermediateNodes(t *testing.T) {
	t.Parallel()
	data := map[string]any{}
	EnsureArrayAt(data, "permissions", "deny")
	deny, ok := data["permissions"].(map[string]any)["deny"].([]any)
	if !ok || len(deny) != 0 {
		t.Errorf("deny = %v (ok=%v), want empty []any under created intermediates", deny, ok)
	}
}

func TestUpsertPath_JSONRoundTrip_PreservesUserFieldsAcrossRerun(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "claude.json")
	existing := map[string]any{
		"mcpServers": map[string]any{
			"puppeteer": map[string]any{"command": "puppeteer-mcp"},
		},
		"theme":           "dark",
		"customUserField": float64(42),
	}
	if err := SaveJSONFile(path, existing); err != nil {
		t.Fatal(err)
	}

	// First adapter run: upsert ZCP entries.
	data, _ := LoadJSONFile(path)
	UpsertPath(data, map[string]any{"command": "zcp", "args": []any{"serve"}}, "mcpServers", "zerops")
	UpsertPath(data, map[string]any{"hasTrustDialogAccepted": true}, "projects", "/var/www")
	if err := SaveJSONFile(path, data); err != nil {
		t.Fatal(err)
	}

	// Second adapter run: upsert the SAME entries (idempotent contract).
	data2, _ := LoadJSONFile(path)
	UpsertPath(data2, map[string]any{"command": "zcp", "args": []any{"serve"}}, "mcpServers", "zerops")
	UpsertPath(data2, map[string]any{"hasTrustDialogAccepted": true}, "projects", "/var/www")
	if err := SaveJSONFile(path, data2); err != nil {
		t.Fatal(err)
	}

	final, _ := LoadJSONFile(path)
	servers := final["mcpServers"].(map[string]any)
	if _, ok := servers["puppeteer"]; !ok {
		t.Error("user's puppeteer MCP server lost across upsert")
	}
	if _, ok := servers["zerops"]; !ok {
		t.Error("ZCP server entry not added")
	}
	if final["theme"] != "dark" {
		t.Error("user's theme setting clobbered")
	}
	if final["customUserField"] != float64(42) {
		t.Error("user's custom top-level field clobbered")
	}
}

func TestUpsertPath_TOMLRoundTrip_PreservesUserSections(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	// Simulate user having other MCP servers + a global setting.
	existing := []byte(`
model = "gpt-5.5"

[mcp_servers.github]
command = "github-mcp"

[projects."/Users/me/work"]
trust_level = "trusted"
`)
	if err := os.WriteFile(path, existing, 0o644); err != nil {
		t.Fatal(err)
	}

	data, err := LoadTOMLFile(path)
	if err != nil {
		t.Fatalf("LoadTOMLFile: %v", err)
	}

	// Upsert ZCP entries — must not touch github server or user project.
	UpsertPath(data, map[string]any{
		"command": "zcp",
		"args":    []any{"serve"},
	}, "mcp_servers", "zerops")
	UpsertPath(data, map[string]any{
		"trust_level": "trusted",
	}, "projects", "/var/www")

	if err := SaveTOMLFile(path, data); err != nil {
		t.Fatalf("SaveTOMLFile: %v", err)
	}

	reloaded, err := LoadTOMLFile(path)
	if err != nil {
		t.Fatalf("LoadTOMLFile reloaded: %v", err)
	}

	if reloaded["model"] != "gpt-5.5" {
		t.Errorf("top-level model setting lost: got %v", reloaded["model"])
	}
	servers, ok := reloaded["mcp_servers"].(map[string]any)
	if !ok {
		t.Fatalf("mcp_servers section missing")
	}
	if _, ok := servers["github"]; !ok {
		t.Error("user's github MCP server lost")
	}
	if _, ok := servers["zerops"]; !ok {
		t.Error("ZCP server entry not added")
	}
	projects, ok := reloaded["projects"].(map[string]any)
	if !ok {
		t.Fatalf("projects section missing")
	}
	if _, ok := projects["/Users/me/work"]; !ok {
		t.Error("user's project trust entry lost")
	}
	if _, ok := projects["/var/www"]; !ok {
		t.Error("ZCP project trust entry not added")
	}
}

func TestSaveTOMLFile_IdempotentOnRerun(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	data := map[string]any{
		"mcp_servers": map[string]any{
			"zerops": map[string]any{
				"command": "zcp",
				"args":    []any{"serve"},
			},
		},
	}
	if err := SaveTOMLFile(path, data); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(path)
	if err := SaveTOMLFile(path, data); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(path)
	if string(first) != string(second) {
		t.Errorf("SaveTOMLFile not byte-identical on rerun:\n  first:  %q\n  second: %q", first, second)
	}
}

func TestAppendIfMissingString_AbsentValue_CreatesArray(t *testing.T) {
	t.Parallel()
	got := AppendIfMissingString(nil, "want")
	if len(got) != 1 || got[0] != "want" {
		t.Errorf("AppendIfMissingString(nil, %q) = %v, want [%q]", "want", got, "want")
	}
}

func TestAppendIfMissingString_ExistingArray_AppendsPreservingOrder(t *testing.T) {
	t.Parallel()
	existing := []any{"a", "b"}
	got := AppendIfMissingString(existing, "c")
	want := []any{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AppendIfMissingString = %v, want %v", got, want)
	}
	// Original slice must not be mutated (defensive copy).
	if len(existing) != 2 {
		t.Errorf("input slice mutated: %v", existing)
	}
}

func TestAppendIfMissingString_AlreadyPresent_NoDuplicate(t *testing.T) {
	t.Parallel()
	existing := []any{"a", "b"}
	got := AppendIfMissingString(existing, "a")
	want := []any{"a", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AppendIfMissingString = %v, want %v (no duplicate)", got, want)
	}
}

func TestAppendIfMissingString_ScalarExisting_WrappedToArray(t *testing.T) {
	t.Parallel()
	got := AppendIfMissingString("solo", "want")
	want := []any{"solo", "want"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AppendIfMissingString(scalar) = %v, want %v", got, want)
	}
}

func TestEnsureArrayContains_CreatesIntermediateNodesAndArray(t *testing.T) {
	t.Parallel()
	data := map[string]any{}
	EnsureArrayContains(data, "Mcp(zerops:*)", "permissions", "allow")
	allow, _ := data["permissions"].(map[string]any)["allow"].([]any)
	if len(allow) != 1 || allow[0] != "Mcp(zerops:*)" {
		t.Errorf("permissions.allow = %v, want [%q]", allow, "Mcp(zerops:*)")
	}
}

func TestEnsureArrayContains_PreservesExistingEntriesAndSiblingFields(t *testing.T) {
	t.Parallel()
	data := map[string]any{
		"permissions": map[string]any{
			"allow": []any{"Shell(ls)"},
			"deny":  []any{"Shell(rm)"},
		},
		"customTopLevel": true,
	}
	EnsureArrayContains(data, "Mcp(zerops:*)", "permissions", "allow")
	allow, _ := data["permissions"].(map[string]any)["allow"].([]any)
	want := []any{"Shell(ls)", "Mcp(zerops:*)"}
	if !reflect.DeepEqual(allow, want) {
		t.Errorf("permissions.allow = %v, want %v", allow, want)
	}
	deny, _ := data["permissions"].(map[string]any)["deny"].([]any)
	if len(deny) != 1 || deny[0] != "Shell(rm)" {
		t.Errorf("permissions.deny lost: %v", deny)
	}
	if data["customTopLevel"] != true {
		t.Error("sibling top-level field lost")
	}
}

func TestEnsureArrayContains_Idempotent(t *testing.T) {
	t.Parallel()
	data := map[string]any{}
	EnsureArrayContains(data, "zerops:*", "mcpAllowlist")
	EnsureArrayContains(data, "zerops:*", "mcpAllowlist")
	allow, _ := data["mcpAllowlist"].([]any)
	if len(allow) != 1 {
		t.Errorf("mcpAllowlist = %v, want exactly one entry after two calls", allow)
	}
}

func TestLoadTOMLFile_MalformedReturnsError(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "bad.toml")
	if err := os.WriteFile(path, []byte("not = valid = toml = at = all"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTOMLFile(path); err == nil {
		t.Error("expected error on malformed TOML, got nil")
	}
}
