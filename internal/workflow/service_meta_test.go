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
		Mode:             "standard",
		StageHostname:    "appstage",
		DeployStrategy:   StrategyPushDev,
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
	if got.DeployStrategy != StrategyPushDev {
		t.Errorf("deployStrategy: want %s, got %s", StrategyPushDev, got.DeployStrategy)
	}
}

func TestWriteServiceMeta_CreatesDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	meta := &ServiceMeta{
		Hostname:         "db",
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
		Mode:             "standard",
		StageHostname:    "appstage",
		DeployStrategy:   StrategyPushDev,
		BootstrapSession: "sess1",
		BootstrappedAt:   "2026-03-04T12:00:00Z",
	}

	if err := WriteServiceMeta(dir, original); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	got, err := ReadServiceMeta(dir, "appdev")
	if err != nil {
		t.Fatalf("ReadServiceMeta: %v", err)
	}
	if got.DeployStrategy != StrategyPushDev {
		t.Errorf("deployStrategy: want %s, got %s", StrategyPushDev, got.DeployStrategy)
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
				Mode:             "standard",
				StageHostname:    "apistage",
				DeployStrategy:   StrategyPushDev,
				BootstrapSession: "sess123",
				BootstrappedAt:   "2026-03-04T12:00:00Z",
			},
		},
		{
			"minimal",
			ServiceMeta{
				Hostname:         "db",
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
			if got.DeployStrategy != tt.meta.DeployStrategy {
				t.Errorf("deployStrategy: want %s, got %s", tt.meta.DeployStrategy, got.DeployStrategy)
			}
		})
	}
}

func TestServiceMeta_NoDeployFlowField(t *testing.T) {
	t.Parallel()

	// Verify DeployFlow is not serialized — the field should not exist.
	meta := &ServiceMeta{
		Hostname:         "appdev",
		DeployStrategy:   StrategyPushDev,
		BootstrapSession: "sess1",
		BootstrappedAt:   "2026-03-04T12:00:00Z",
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
		{"StrategyPushGit", StrategyPushGit, "push-git"},
		{"StrategyManual", StrategyManual, "manual"},
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
		{Hostname: "appdev", BootstrapSession: "s1", BootstrappedAt: "2026-03-04T12:00:00Z"},
		{Hostname: "db", BootstrapSession: "s1", BootstrappedAt: "2026-03-04T12:00:00Z"},
		{Hostname: "cache", BootstrapSession: "s1", BootstrappedAt: "2026-03-04T12:00:00Z"},
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

func TestServiceMeta_IsComplete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		meta     ServiceMeta
		wantDone bool
	}{
		{
			"complete meta with BootstrappedAt",
			ServiceMeta{Hostname: "appdev", BootstrappedAt: "2026-03-04T12:00:00Z"},
			true,
		},
		{
			"incomplete meta without BootstrappedAt",
			ServiceMeta{Hostname: "appdev"},
			false,
		},
		{
			"empty BootstrappedAt string",
			ServiceMeta{Hostname: "appdev", BootstrappedAt: ""},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.meta.IsComplete(); got != tt.wantDone {
				t.Errorf("IsComplete(): want %v, got %v", tt.wantDone, got)
			}
		})
	}
}

func TestPruneServiceMetas_RemovesStaleEntries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		diskMetas     []*ServiceMeta
		liveHostnames []string
		wantRemaining []string
	}{
		{
			"removes stale metas not in live project",
			[]*ServiceMeta{
				{Hostname: "appdev", Mode: "standard", StageHostname: "appstage", BootstrapSession: "s1", BootstrappedAt: "2026-03-04T12:00:00Z"},
				{Hostname: "docs", Mode: "simple", BootstrapSession: "s2", BootstrappedAt: "2026-03-04T12:00:00Z"},
				{Hostname: "ghost", BootstrapSession: "s3", BootstrappedAt: "2026-03-04T12:00:00Z"},
			},
			[]string{"appdev", "appstage", "db"},
			[]string{"appdev"},
		},
		{
			"keeps meta when stage hostname is live",
			[]*ServiceMeta{
				{Hostname: "appdev", Mode: "standard", StageHostname: "appstage", BootstrapSession: "s1", BootstrappedAt: "2026-03-04T12:00:00Z"},
			},
			[]string{"appstage"}, // only stage exists
			[]string{"appdev"},
		},
		{
			"removes all when none are live",
			[]*ServiceMeta{
				{Hostname: "old1", BootstrapSession: "s1", BootstrappedAt: "2026-03-04T12:00:00Z"},
				{Hostname: "old2", BootstrapSession: "s2", BootstrappedAt: "2026-03-04T12:00:00Z"},
			},
			[]string{"unrelated"},
			nil,
		},
		{
			"keeps all when all are live",
			[]*ServiceMeta{
				{Hostname: "a", BootstrapSession: "s1", BootstrappedAt: "2026-03-04T12:00:00Z"},
				{Hostname: "b", BootstrapSession: "s1", BootstrappedAt: "2026-03-04T12:00:00Z"},
			},
			[]string{"a", "b"},
			[]string{"a", "b"},
		},
		{
			"empty live set removes all",
			[]*ServiceMeta{
				{Hostname: "x", BootstrapSession: "s1", BootstrappedAt: "2026-03-04T12:00:00Z"},
			},
			nil,
			nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()

			for _, m := range tt.diskMetas {
				if err := WriteServiceMeta(dir, m); err != nil {
					t.Fatalf("WriteServiceMeta(%s): %v", m.Hostname, err)
				}
			}

			live := make(map[string]bool, len(tt.liveHostnames))
			for _, h := range tt.liveHostnames {
				live[h] = true
			}

			pruned := PruneServiceMetas(dir, live)

			remaining, err := ListServiceMetas(dir)
			if err != nil {
				t.Fatalf("ListServiceMetas: %v", err)
			}

			var gotHostnames []string
			for _, m := range remaining {
				gotHostnames = append(gotHostnames, m.Hostname)
			}
			sort.Strings(gotHostnames)
			sort.Strings(tt.wantRemaining)

			if len(gotHostnames) != len(tt.wantRemaining) {
				t.Fatalf("remaining metas: want %v, got %v", tt.wantRemaining, gotHostnames)
			}
			for i := range gotHostnames {
				if gotHostnames[i] != tt.wantRemaining[i] {
					t.Errorf("remaining[%d]: want %s, got %s", i, tt.wantRemaining[i], gotHostnames[i])
				}
			}

			// Verify pruned count matches removed count.
			wantPruned := len(tt.diskMetas) - len(tt.wantRemaining)
			if pruned != wantPruned {
				t.Errorf("pruned count: want %d, got %d", wantPruned, pruned)
			}
		})
	}
}

func TestServiceMeta_NoStatusFieldInJSON(t *testing.T) {
	t.Parallel()

	meta := &ServiceMeta{
		Hostname:         "appdev",
		BootstrapSession: "sess1",
		BootstrappedAt:   "2026-03-04T12:00:00Z",
	}
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, ok := raw["status"]; ok {
		t.Error("status field should NOT exist in JSON — Status was removed")
	}
	if _, ok := raw["type"]; ok {
		t.Error("type field should NOT exist in JSON — Type was removed")
	}
	if _, ok := raw["decisions"]; ok {
		t.Error("decisions field should NOT exist in JSON — replaced by deployStrategy")
	}
	if _, ok := raw["dependencies"]; ok {
		t.Error("dependencies field should NOT exist in JSON — removed (never read in production)")
	}
}
