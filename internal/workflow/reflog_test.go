package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendReflogEntry_NewFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	targets := []BootstrapTarget{
		{
			Runtime:      RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
			Dependencies: []Dependency{{Hostname: "db", Type: "postgresql@16"}},
		},
	}

	err := AppendReflogEntry(path, "Deploy Node.js app", targets, "sess123", "2026-03-04")
	if err != nil {
		t.Fatalf("AppendReflogEntry: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "<!-- ZEROPS:REFLOG -->") {
		t.Error("missing ZEROPS:REFLOG marker")
	}
	if !strings.Contains(content, "Deploy Node.js app") {
		t.Error("missing intent")
	}
	if !strings.Contains(content, "appdev") {
		t.Error("missing hostname")
	}
	if !strings.Contains(content, "nodejs@22") {
		t.Error("missing type")
	}
	if !strings.Contains(content, "standard") {
		t.Error("missing bootstrap mode")
	}
	if !strings.Contains(content, "db (postgresql@16)") {
		t.Error("missing dependency")
	}
}

func TestAppendReflogEntry_ExistingFile_Appends(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	existing := "# My Project\n\nSome existing content.\n"
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatalf("write existing: %v", err)
	}

	targets := []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2"}},
	}

	err := AppendReflogEntry(path, "Test append", targets, "sess456", "2026-03-04")
	if err != nil {
		t.Fatalf("AppendReflogEntry: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	content := string(data)

	if !strings.HasPrefix(content, "# My Project") {
		t.Error("existing content should be preserved")
	}
	if !strings.Contains(content, "Test append") {
		t.Error("new entry should be appended")
	}
}

func TestAppendReflogEntry_MultipleEntries(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	t1 := []BootstrapTarget{{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2"}}}
	t2 := []BootstrapTarget{{Runtime: RuntimeTarget{DevHostname: "apidev", Type: "go@1"}}}

	if err := AppendReflogEntry(path, "First bootstrap", t1, "sess1", "2026-03-01"); err != nil {
		t.Fatalf("first entry: %v", err)
	}
	if err := AppendReflogEntry(path, "Second bootstrap", t2, "sess2", "2026-03-02"); err != nil {
		t.Fatalf("second entry: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)

	count := strings.Count(content, "<!-- ZEROPS:REFLOG -->")
	if count != 2 {
		t.Errorf("expected 2 REFLOG markers, got %d", count)
	}
	if !strings.Contains(content, "First bootstrap") {
		t.Error("missing first entry")
	}
	if !strings.Contains(content, "Second bootstrap") {
		t.Error("missing second entry")
	}
}

func TestAppendReflogEntry_DevMode(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	targets := []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: "myapp", Type: "bun@1.2", BootstrapMode: "dev"}},
	}

	err := AppendReflogEntry(path, "Dev mode app", targets, "sess-dev", "2026-03-08")
	if err != nil {
		t.Fatalf("AppendReflogEntry: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "dev") {
		t.Error("reflog entry should contain 'dev' mode")
	}
	if strings.Contains(content, "standard") {
		t.Error("reflog entry should not contain 'standard' for dev mode target")
	}
}

func TestAppendReflogEntry_MultiTarget(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	targets := []BootstrapTarget{
		{
			Runtime:      RuntimeTarget{DevHostname: "webdev", Type: "php-nginx@8.4"},
			Dependencies: []Dependency{{Hostname: "db", Type: "postgresql@16"}},
		},
		{
			Runtime:      RuntimeTarget{DevHostname: "apidev", Type: "nodejs@22"},
			Dependencies: []Dependency{{Hostname: "db", Type: "postgresql@16"}, {Hostname: "cache", Type: "valkey@7.2"}},
		},
	}

	err := AppendReflogEntry(path, "Multi-runtime project", targets, "sess789", "2026-03-04")
	if err != nil {
		t.Fatalf("AppendReflogEntry: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)

	if !strings.Contains(content, "webdev") {
		t.Error("missing first runtime")
	}
	if !strings.Contains(content, "apidev") {
		t.Error("missing second runtime")
	}
	if !strings.Contains(content, "cache (valkey@7.2)") {
		t.Error("missing cache dependency")
	}
}
