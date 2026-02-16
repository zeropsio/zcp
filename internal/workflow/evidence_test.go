// Tests for: workflow evidence CRUD — atomic writes, load, list, archive.
package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveEvidence_WritesFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ev := &Evidence{
		SessionID:        "sess-1",
		Timestamp:        "2025-01-01T00:00:00Z",
		VerificationType: "attestation",
		Type:             "recipe_review",
		Attestation:      "reviewed",
		Passed:           3,
		Failed:           0,
	}
	if err := SaveEvidence(dir, "sess-1", ev); err != nil {
		t.Fatalf("SaveEvidence: %v", err)
	}
	path := filepath.Join(dir, "sess-1", "recipe_review.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file at %s: %v", path, err)
	}
}

func TestSaveEvidence_AtomicOverwrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ev1 := &Evidence{
		SessionID: "sess-1", Type: "discovery", Attestation: "first",
		VerificationType: "attestation",
	}
	ev2 := &Evidence{
		SessionID: "sess-1", Type: "discovery", Attestation: "second",
		VerificationType: "attestation",
	}
	if err := SaveEvidence(dir, "sess-1", ev1); err != nil {
		t.Fatalf("SaveEvidence first: %v", err)
	}
	if err := SaveEvidence(dir, "sess-1", ev2); err != nil {
		t.Fatalf("SaveEvidence second: %v", err)
	}
	loaded, err := LoadEvidence(dir, "sess-1", "discovery")
	if err != nil {
		t.Fatalf("LoadEvidence: %v", err)
	}
	if loaded.Attestation != "second" {
		t.Errorf("expected attestation 'second', got %q", loaded.Attestation)
	}
}

func TestLoadEvidence_NotFound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := LoadEvidence(dir, "sess-1", "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing evidence")
	}
}

func TestLoadEvidence_Success(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ev := &Evidence{
		SessionID:        "sess-2",
		Timestamp:        "2025-01-02T00:00:00Z",
		VerificationType: "attestation",
		Type:             "dev_verify",
		Service:          "app",
		Attestation:      "tests pass",
		Passed:           5,
		Failed:           1,
	}
	if err := SaveEvidence(dir, "sess-2", ev); err != nil {
		t.Fatalf("SaveEvidence: %v", err)
	}
	loaded, err := LoadEvidence(dir, "sess-2", "dev_verify")
	if err != nil {
		t.Fatalf("LoadEvidence: %v", err)
	}
	if loaded.SessionID != "sess-2" {
		t.Errorf("session_id: want sess-2, got %s", loaded.SessionID)
	}
	if loaded.Service != "app" {
		t.Errorf("service: want app, got %s", loaded.Service)
	}
	if loaded.Passed != 5 || loaded.Failed != 1 {
		t.Errorf("passed/failed: want 5/1, got %d/%d", loaded.Passed, loaded.Failed)
	}
}

func TestListEvidence_Empty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	evs, err := ListEvidence(dir, "sess-1")
	if err != nil {
		t.Fatalf("ListEvidence: %v", err)
	}
	if len(evs) != 0 {
		t.Errorf("expected 0 evidence files, got %d", len(evs))
	}
}

func TestListEvidence_MultipleFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	types := []string{"recipe_review", "discovery", "dev_verify"}
	for _, typ := range types {
		ev := &Evidence{
			SessionID: "sess-3", Type: typ, VerificationType: "attestation",
		}
		if err := SaveEvidence(dir, "sess-3", ev); err != nil {
			t.Fatalf("SaveEvidence(%s): %v", typ, err)
		}
	}
	evs, err := ListEvidence(dir, "sess-3")
	if err != nil {
		t.Fatalf("ListEvidence: %v", err)
	}
	if len(evs) != 3 {
		t.Errorf("expected 3 evidence files, got %d", len(evs))
	}
}

func TestArchiveEvidence_MovesFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ev := &Evidence{
		SessionID: "sess-4", Type: "discovery", VerificationType: "attestation",
	}
	if err := SaveEvidence(dir, "sess-4", ev); err != nil {
		t.Fatalf("SaveEvidence: %v", err)
	}
	if err := ArchiveEvidence(dir, "sess-4", 1); err != nil {
		t.Fatalf("ArchiveEvidence: %v", err)
	}

	// Original should be gone.
	origPath := filepath.Join(dir, "sess-4", "discovery.json")
	if _, err := os.Stat(origPath); !os.IsNotExist(err) {
		t.Error("expected original file to be removed after archive")
	}

	// Archived copy should exist.
	archivePath := filepath.Join(dir, "sess-4", "iterations", "1", "discovery.json")
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("expected archived file at %s: %v", archivePath, err)
	}
}

func TestArchiveEvidence_EmptySession(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Archive with no evidence should not error.
	if err := ArchiveEvidence(dir, "sess-empty", 1); err != nil {
		t.Fatalf("ArchiveEvidence on empty session: %v", err)
	}
}
