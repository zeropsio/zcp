package hq

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveEnrollment_ThenLoad_OwnerOnly(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "hq", "enrollment.json")
	if _, found, err := LoadEnrollment(path); err != nil || found {
		t.Fatalf("LoadEnrollment before any save = found %v, err %v; want none", found, err)
	}

	want := Enrollment{HQ: "https://hq.example", HQProjectID: "hq1", ProjectID: "p1", Credential: "secret"}
	if err := SaveEnrollment(path, want); err != nil {
		t.Fatalf("SaveEnrollment: %v", err)
	}
	got, found, err := LoadEnrollment(path)
	if err != nil || !found || got != want {
		t.Fatalf("LoadEnrollment = %+v, found %v, err %v; want %+v", got, found, err, want)
	}

	for p, mode := range map[string]os.FileMode{path: 0o600, filepath.Dir(path): 0o700} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %s: %v", p, err)
		}
		if info.Mode().Perm() != mode {
			t.Errorf("%s mode = %v, want %v", p, info.Mode().Perm(), mode)
		}
	}

	// A second save replaces the first whole, leaving nothing else behind.
	next := Enrollment{HQ: "https://hq.example", HQProjectID: "hq1", ProjectID: "p1", Credential: "rotated"}
	if err := SaveEnrollment(path, next); err != nil {
		t.Fatalf("SaveEnrollment again: %v", err)
	}
	if got, _, _ := LoadEnrollment(path); got != next {
		t.Errorf("after rotation = %+v, want %+v", got, next)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("state dir holds %d entries, want only the enrollment", len(entries))
	}
}
