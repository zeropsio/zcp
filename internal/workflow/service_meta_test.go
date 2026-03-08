package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
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
		Dependencies:     []string{"db", "cache"},
		BootstrapSession: "abc123",
		BootstrappedAt:   "2026-03-04T12:00:00Z",
		Decisions:        map[string]string{DecisionDeployStrategy: StrategyPushDev},
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
	if got.Decisions[DecisionDeployStrategy] != StrategyPushDev {
		t.Errorf("decisions[deployStrategy]: want %s, got %s", StrategyPushDev, got.Decisions[DecisionDeployStrategy])
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
		Dependencies:     []string{"db"},
		BootstrapSession: "sess1",
		BootstrappedAt:   "2026-03-04T12:00:00Z",
		Decisions:        map[string]string{"framework": "hono", DecisionDeployStrategy: StrategyPushDev},
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
	if got.Decisions[DecisionDeployStrategy] != StrategyPushDev {
		t.Errorf("decisions[deployStrategy]: want %s, got %s", StrategyPushDev, got.Decisions[DecisionDeployStrategy])
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
				Dependencies:     []string{"db", "cache", "storage"},
				BootstrapSession: "sess123",
				BootstrappedAt:   "2026-03-04T12:00:00Z",
				Decisions:        map[string]string{"framework": "fiber", "db_driver": "pgx", DecisionDeployStrategy: StrategyPushDev},
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

func TestServiceMeta_NoDeployFlowField(t *testing.T) {
	t.Parallel()

	// Verify DeployFlow is not serialized — the field should not exist.
	meta := &ServiceMeta{
		Hostname:         "appdev",
		Type:             "nodejs@22",
		BootstrapSession: "sess1",
		BootstrappedAt:   "2026-03-04T12:00:00Z",
		Decisions:        map[string]string{DecisionDeployStrategy: StrategyPushDev},
	}
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, ok := raw["deployFlow"]; ok {
		t.Error("deployFlow field should not exist in JSON output")
	}
}

func TestStrategyConstants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		constant string
		want     string
	}{
		{"StrategyPushDev", StrategyPushDev, "push-dev"},
		{"StrategyCICD", StrategyCICD, "ci-cd"},
		{"StrategyManual", StrategyManual, "manual"},
		{"DecisionDeployStrategy", DecisionDeployStrategy, "deployStrategy"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.constant != tt.want {
				t.Errorf("%s: want %q, got %q", tt.name, tt.want, tt.constant)
			}
		})
	}
}

func TestListServiceMetas_MultipleMetas(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	metas := []*ServiceMeta{
		{Hostname: "appdev", Type: "nodejs@22", BootstrapSession: "s1", BootstrappedAt: "2026-03-04T12:00:00Z"},
		{Hostname: "db", Type: "postgresql@16", BootstrapSession: "s1", BootstrappedAt: "2026-03-04T12:00:00Z"},
		{Hostname: "cache", Type: "keydb@6", BootstrapSession: "s1", BootstrappedAt: "2026-03-04T12:00:00Z"},
	}
	for _, m := range metas {
		if err := WriteServiceMeta(dir, m); err != nil {
			t.Fatalf("WriteServiceMeta(%s): %v", m.Hostname, err)
		}
	}

	got, err := ListServiceMetas(dir)
	if err != nil {
		t.Fatalf("ListServiceMetas: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 metas, got %d", len(got))
	}

	// Sort by hostname for deterministic comparison.
	sort.Slice(got, func(i, j int) bool { return got[i].Hostname < got[j].Hostname })
	wantHostnames := []string{"appdev", "cache", "db"}
	for i, want := range wantHostnames {
		if got[i].Hostname != want {
			t.Errorf("metas[%d].Hostname: want %s, got %s", i, want, got[i].Hostname)
		}
	}
}

func TestListServiceMetas_EmptyDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Create the services directory but leave it empty.
	if err := os.MkdirAll(filepath.Join(dir, "services"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	got, err := ListServiceMetas(dir)
	if err != nil {
		t.Fatalf("ListServiceMetas: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want 0 metas, got %d", len(got))
	}
}

func TestListServiceMetas_NonExistentDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Don't create the services directory — should return empty, no error.
	got, err := ListServiceMetas(dir)
	if err != nil {
		t.Fatalf("ListServiceMetas: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want 0 metas for nonexistent dir, got %d", len(got))
	}
}

func TestDeleteServiceMeta_Success(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	meta := &ServiceMeta{
		Hostname:         "appdev",
		Type:             "nodejs@22",
		BootstrapSession: "s1",
		BootstrappedAt:   "2026-03-04T12:00:00Z",
	}
	if err := WriteServiceMeta(dir, meta); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	// Verify it exists.
	got, err := ReadServiceMeta(dir, "appdev")
	if err != nil {
		t.Fatalf("ReadServiceMeta: %v", err)
	}
	if got == nil {
		t.Fatal("expected meta to exist before delete")
	}

	// Delete it.
	if err := DeleteServiceMeta(dir, "appdev"); err != nil {
		t.Fatalf("DeleteServiceMeta: %v", err)
	}

	// Verify it's gone.
	got, err = ReadServiceMeta(dir, "appdev")
	if err != nil {
		t.Fatalf("ReadServiceMeta after delete: %v", err)
	}
	if got != nil {
		t.Error("expected nil after delete")
	}
}

func TestDeleteServiceMeta_NotFound_NoError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Deleting a non-existent meta should not error (idempotent).
	if err := DeleteServiceMeta(dir, "nonexistent"); err != nil {
		t.Fatalf("DeleteServiceMeta should be idempotent, got: %v", err)
	}
}

func TestDeleteServiceMeta_NoServicesDir_NoError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// No services/ directory at all — should not error.
	if err := DeleteServiceMeta(dir, "anything"); err != nil {
		t.Fatalf("DeleteServiceMeta should be idempotent, got: %v", err)
	}
}
