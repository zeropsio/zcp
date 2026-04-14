package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteDevelopMarker_CreatesFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		projectID string
		intent    string
	}{
		{"basic", "proj-1", "test"},
		{"empty_intent", "proj-2", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := t.TempDir()

			if err := WriteDevelopMarker(stateDir, tt.projectID, tt.intent); err != nil {
				t.Fatalf("WriteDevelopMarker: %v", err)
			}

			path := filepath.Join(stateDir, developMarkerDir, fmt.Sprintf("%d.json", os.Getpid()))
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read marker file: %v", err)
			}

			var marker DevelopMarker
			if err := json.Unmarshal(data, &marker); err != nil {
				t.Fatalf("unmarshal marker: %v", err)
			}
			if marker.ProjectID != tt.projectID {
				t.Errorf("ProjectID = %s, want %s", marker.ProjectID, tt.projectID)
			}
			if marker.Intent != tt.intent {
				t.Errorf("Intent = %s, want %s", marker.Intent, tt.intent)
			}
			if marker.CreatedAt == "" {
				t.Error("CreatedAt should be set")
			}
		})
	}
}

func TestWriteDevelopMarker_EmptyStateDir(t *testing.T) {
	t.Parallel()

	if err := WriteDevelopMarker("", "proj-1", "test"); err == nil {
		t.Error("expected error for empty stateDir")
	}
}

func TestHasDevelopMarker_CurrentPID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		write    bool
		expected bool
	}{
		{"exists", true, true},
		{"missing", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := t.TempDir()

			if tt.write {
				if err := WriteDevelopMarker(stateDir, "proj-1", "test"); err != nil {
					t.Fatalf("WriteDevelopMarker: %v", err)
				}
			}

			got := HasDevelopMarker(stateDir)
			if got != tt.expected {
				t.Errorf("HasDevelopMarker = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestHasDevelopMarker_EmptyStateDir(t *testing.T) {
	t.Parallel()

	if HasDevelopMarker("") {
		t.Error("HasDevelopMarker should return false for empty stateDir")
	}
}

func TestCleanStaleDevelopMarkers_RemovesDead(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	dir := filepath.Join(stateDir, developMarkerDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Write a marker for a PID that doesn't exist.
	deadPID := 999999
	marker := DevelopMarker{ProjectID: "proj-1", CreatedAt: "2026-01-01T00:00:00Z"}
	data, err := json.Marshal(marker)
	if err != nil {
		t.Fatalf("marshal marker: %v", err)
	}
	deadPath := filepath.Join(dir, fmt.Sprintf("%d.json", deadPID))
	if err := os.WriteFile(deadPath, data, 0o644); err != nil {
		t.Fatalf("write dead marker: %v", err)
	}

	// Write a marker for our own PID (alive).
	if err := WriteDevelopMarker(stateDir, "proj-1", "test"); err != nil {
		t.Fatalf("WriteDevelopMarker: %v", err)
	}

	if err := CleanStaleDevelopMarkers(stateDir); err != nil {
		t.Fatalf("CleanStaleDevelopMarkers: %v", err)
	}

	// Dead marker should be removed.
	if _, err := os.Stat(deadPath); err == nil {
		t.Error("dead marker should be removed")
	}

	// Our marker should remain.
	if !HasDevelopMarker(stateDir) {
		t.Error("our marker should remain after cleanup")
	}
}

func TestCleanStaleDevelopMarkers_NoDir(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	// No develop/ dir — should not error.
	if err := CleanStaleDevelopMarkers(stateDir); err != nil {
		t.Errorf("should not error when dir missing: %v", err)
	}
}
