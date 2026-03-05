package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteServiceMeta_Success(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	meta := &ServiceMeta{
		Hostname:         "appdev",
		Type:             "nodejs@22",
		Mode:             "standard",
		StageHostname:    "appstage",
		DeployFlow:       "ssh",
		Dependencies:     []string{"db", "cache"},
		BootstrapSession: "abc123",
		BootstrappedAt:   "2026-03-04T12:00:00Z",
	}

	if err := WriteServiceMeta(dir, meta); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	path := filepath.Join(dir, "services", "appdev.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	var got ServiceMeta
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Hostname != "appdev" {
		t.Errorf("hostname: want appdev, got %s", got.Hostname)
	}
	if got.StageHostname != "appstage" {
		t.Errorf("stageHostname: want appstage, got %s", got.StageHostname)
	}
}

func TestWriteServiceMeta_CreatesDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	meta := &ServiceMeta{
		Hostname:         "db",
		Type:             "postgresql@16",
		BootstrapSession: "abc123",
		BootstrappedAt:   "2026-03-04T12:00:00Z",
	}

	if err := WriteServiceMeta(dir, meta); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "services"))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if !info.IsDir() {
		t.Error("services should be a directory")
	}
}

func TestReadServiceMeta_Success(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	original := &ServiceMeta{
		Hostname:         "appdev",
		Type:             "bun@1.2",
		Mode:             "standard",
		StageHostname:    "appstage",
		DeployFlow:       "ssh",
		Dependencies:     []string{"db"},
		BootstrapSession: "sess1",
		BootstrappedAt:   "2026-03-04T12:00:00Z",
		Decisions:        map[string]string{"framework": "hono"},
	}

	if err := WriteServiceMeta(dir, original); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	got, err := ReadServiceMeta(dir, "appdev")
	if err != nil {
		t.Fatalf("ReadServiceMeta: %v", err)
	}
	if got.Type != "bun@1.2" {
		t.Errorf("type: want bun@1.2, got %s", got.Type)
	}
	if got.Decisions["framework"] != "hono" {
		t.Errorf("decisions[framework]: want hono, got %s", got.Decisions["framework"])
	}
}

func TestReadServiceMeta_NotFound_ReturnsNil(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	got, err := ReadServiceMeta(dir, "nonexistent")
	if err != nil {
		t.Fatalf("ReadServiceMeta: %v", err)
	}
	if got != nil {
		t.Error("expected nil for nonexistent meta")
	}
}

func TestServiceMeta_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		meta ServiceMeta
	}{
		{
			"full",
			ServiceMeta{
				Hostname:         "apidev",
				Type:             "go@1",
				Mode:             "standard",
				StageHostname:    "apistage",
				DeployFlow:       "ssh",
				Dependencies:     []string{"db", "cache", "storage"},
				BootstrapSession: "sess123",
				BootstrappedAt:   "2026-03-04T12:00:00Z",
				Decisions:        map[string]string{"framework": "fiber", "db_driver": "pgx"},
			},
		},
		{
			"minimal",
			ServiceMeta{
				Hostname:         "db",
				Type:             "postgresql@16",
				BootstrapSession: "sess123",
				BootstrappedAt:   "2026-03-04T12:00:00Z",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data, err := json.Marshal(tt.meta)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var got ServiceMeta
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got.Hostname != tt.meta.Hostname {
				t.Errorf("hostname: want %s, got %s", tt.meta.Hostname, got.Hostname)
			}
			if got.Type != tt.meta.Type {
				t.Errorf("type: want %s, got %s", tt.meta.Type, got.Type)
			}
		})
	}
}
