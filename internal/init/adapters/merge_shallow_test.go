package adapters

import (
	"reflect"
	"strings"
	"testing"
)

// ShallowMergeAtPath was added to address Codex review finding (d):
// the Claude project entry under projects[VSCodeWorkDir] must not
// fully clobber any keys Claude adds during interactive sessions —
// shallow merge preserves user/tool-added keys while overwriting
// ZCP-owned keys with their template values.

func TestShallowMergeAtPath_OverwritesZCPKeys_PreservesUserKeys(t *testing.T) {
	t.Parallel()
	data := map[string]any{
		"projects": map[string]any{
			"/var/www": map[string]any{
				// ZCP-owned keys that should be overwritten:
				"hasTrustDialogAccepted":        false, // user toggled — ZCP forces true
				"hasCompletedProjectOnboarding": false,
				// User-added keys that should survive:
				"customUserSetting":   "preserved",
				"recentlyOpenedFiles": []any{"file1.go", "file2.go"},
				"perProjectTokens":    map[string]any{"key": "secret"},
			},
		},
	}

	zcpEntry := map[string]any{
		"hasTrustDialogAccepted":        true,
		"hasCompletedProjectOnboarding": true,
		"allowedTools":                  []string{},
	}

	ShallowMergeAtPath(data, zcpEntry, "projects", "/var/www")

	entry := data["projects"].(map[string]any)["/var/www"].(map[string]any)

	// ZCP keys overwritten.
	if entry["hasTrustDialogAccepted"] != true {
		t.Errorf("hasTrustDialogAccepted = %v, want true (ZCP-owned, must reset)", entry["hasTrustDialogAccepted"])
	}
	if entry["hasCompletedProjectOnboarding"] != true {
		t.Errorf("hasCompletedProjectOnboarding = %v, want true", entry["hasCompletedProjectOnboarding"])
	}
	// User keys preserved.
	if entry["customUserSetting"] != "preserved" {
		t.Errorf("customUserSetting clobbered: %v", entry["customUserSetting"])
	}
	if files, ok := entry["recentlyOpenedFiles"].([]any); !ok || len(files) != 2 {
		t.Errorf("recentlyOpenedFiles clobbered: %v", entry["recentlyOpenedFiles"])
	}
	tokens, ok := entry["perProjectTokens"].(map[string]any)
	if !ok || tokens["key"] != "secret" {
		t.Errorf("perProjectTokens clobbered: %v", entry["perProjectTokens"])
	}
}

func TestShallowMergeAtPath_MissingPath_BehavesLikeUpsert(t *testing.T) {
	t.Parallel()
	data := map[string]any{}
	zcpEntry := map[string]any{"key": "value"}
	ShallowMergeAtPath(data, zcpEntry, "projects", "/var/www")

	got := data["projects"].(map[string]any)["/var/www"]
	if !reflect.DeepEqual(got, zcpEntry) {
		t.Errorf("missing-path merge result = %v, want %v", got, zcpEntry)
	}
}

func TestShallowMergeAtPath_NonMapAtPath_Replaced(t *testing.T) {
	t.Parallel()
	data := map[string]any{
		"projects": map[string]any{
			"/var/www": "this is a string, not a map",
		},
	}
	zcpEntry := map[string]any{"hasTrustDialogAccepted": true}
	ShallowMergeAtPath(data, zcpEntry, "projects", "/var/www")

	got := data["projects"].(map[string]any)["/var/www"]
	if !reflect.DeepEqual(got, zcpEntry) {
		t.Errorf("non-map-at-path replacement = %v, want %v", got, zcpEntry)
	}
}

func TestShallowMergeAtPath_Idempotent(t *testing.T) {
	t.Parallel()
	data := map[string]any{
		"projects": map[string]any{
			"/var/www": map[string]any{
				"hasTrustDialogAccepted": true,
				"customUserKey":          "preserved",
			},
		},
	}
	zcpEntry := map[string]any{"hasTrustDialogAccepted": true}

	ShallowMergeAtPath(data, zcpEntry, "projects", "/var/www")
	first := snapshot(data)
	ShallowMergeAtPath(data, zcpEntry, "projects", "/var/www")
	second := snapshot(data)

	if first != second {
		t.Errorf("ShallowMergeAtPath not idempotent:\n  first:  %s\n  second: %s", first, second)
	}
}

func snapshot(data map[string]any) string {
	// Cheap stable string representation for diff comparison.
	return reflectDeepString(data)
}

func reflectDeepString(v any) string {
	return reflect.TypeOf(v).String() + ":" + formatValue(v)
}

func formatValue(v any) string {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		// Sort for stable output.
		for i := 1; i < len(keys); i++ {
			for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
				keys[j-1], keys[j] = keys[j], keys[j-1]
			}
		}
		var b strings.Builder
		b.WriteString("{")
		for _, k := range keys {
			b.WriteString(k)
			b.WriteString(":")
			b.WriteString(formatValue(t[k]))
			b.WriteString(",")
		}
		b.WriteString("}")
		return b.String()
	default:
		return reflect.ValueOf(v).String()
	}
}
