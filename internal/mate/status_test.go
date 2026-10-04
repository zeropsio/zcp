package mate_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/mate"
)

// TestUpdateStatus_WritesSchemaV1 pins the file the mate server reads
// (ZCP_STATUS_FILE): every field of schema v1 present, by its wire name, with
// empty lists as [] — a section nobody has written yet reads as nothing
// happened (runtimes "none", stand-up "idle"), never as a missing key.
func TestUpdateStatus_WritesSchemaV1(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	if err := mate.UpdateStatus(path, func(*mate.Status) {}); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if doc["version"] != float64(1) {
		t.Errorf("version = %v, want 1", doc["version"])
	}
	if _, err := time.Parse(time.RFC3339, doc["updatedAt"].(string)); err != nil {
		t.Errorf("updatedAt %q is not RFC3339: %v", doc["updatedAt"], err)
	}
	tests := []struct {
		section string
		keys    []string
		state   string
	}{
		{"runtimes", []string{"endedAt", "error", "services", "startedAt", "state", "updatedAt"}, "none"},
		{"standup", []string{"endedAt", "error", "phase", "services", "startedAt", "state"}, "idle"},
	}
	for _, tt := range tests {
		t.Run(tt.section, func(t *testing.T) {
			section, ok := doc[tt.section].(map[string]any)
			if !ok {
				t.Fatalf("%s missing: %s", tt.section, raw)
			}
			keys := make([]string, 0, len(section))
			for k := range section {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			if !slices.Equal(keys, tt.keys) {
				t.Errorf("%s keys = %v, want %v", tt.section, keys, tt.keys)
			}
			if section["state"] != tt.state {
				t.Errorf("%s.state = %v, want %q", tt.section, section["state"], tt.state)
			}
			if services, ok := section["services"].([]any); !ok || len(services) != 0 {
				t.Errorf("%s.services = %#v, want []", tt.section, section["services"])
			}
		})
	}
}

// TestUpdateStatus_ServiceEntries pins the per-service wire names, the
// stand-up's process the mate server reads its liveness by, and the start of
// the call it matches a section to.
func TestUpdateStatus_ServiceEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	err := mate.UpdateStatus(path, func(s *mate.Status) {
		s.Runtimes.State = mate.RuntimesImporting
		s.Runtimes.Services = []mate.RuntimeService{{Hostname: "appdev", State: mate.ServiceCreating, ProcessID: "p1"}}
		s.Standup.State = mate.StandupRunning
		s.Standup.Services = []mate.StandupService{{Hostname: "appdev", Step: mate.StepBuild, State: mate.StepRunning, ProcessID: "p2", At: "2026-10-01T10:00:00Z"}}
		s.Standup.Process = &mate.StandupProcess{PID: 42, Start: "1234"}
		s.Standup.CallStartedAt = "2026-10-01T10:05:00Z"
	})
	if err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	raw, _ := os.ReadFile(path)
	var doc struct {
		Runtimes struct {
			Services []map[string]any `json:"services"`
		} `json:"runtimes"`
		Standup struct {
			Services      []map[string]any `json:"services"`
			Process       map[string]any   `json:"process"`
			CallStartedAt string           `json:"callStartedAt"`
		} `json:"standup"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if doc.Standup.CallStartedAt != "2026-10-01T10:05:00Z" {
		t.Errorf("standup.callStartedAt = %q, want the call's start", doc.Standup.CallStartedAt)
	}
	tests := []struct {
		name  string
		entry map[string]any
		want  map[string]any
	}{
		{"runtime service", doc.Runtimes.Services[0], map[string]any{"hostname": "appdev", "state": "creating", "processId": "p1", "error": ""}},
		{"stand-up service", doc.Standup.Services[0], map[string]any{"hostname": "appdev", "step": "build", "state": "running", "processId": "p2", "at": "2026-10-01T10:00:00Z", "error": ""}},
		{"stand-up process", doc.Standup.Process, map[string]any{"pid": float64(42), "start": "1234"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.entry) != len(tt.want) {
				t.Errorf("entry %v has keys beyond %v", tt.entry, tt.want)
			}
			for k, v := range tt.want {
				if tt.entry[k] != v {
					t.Errorf("%s = %v, want %v", k, tt.entry[k], v)
				}
			}
		})
	}
}

// TestUpdateStatus_SectionsHaveOneWriterEach: the boot import and the
// stand-up write the same file from two processes, each its own section; a
// write of one never loses the other, however they interleave.
func TestUpdateStatus_SectionsHaveOneWriterEach(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			if err := mate.UpdateStatus(path, func(s *mate.Status) {
				if i%2 == 0 {
					s.Runtimes.Services = append(s.Runtimes.Services, mate.RuntimeService{Hostname: "r"})
				} else {
					s.Standup.Services = append(s.Standup.Services, mate.StandupService{Hostname: "s"})
				}
			}); err != nil {
				t.Errorf("UpdateStatus: %v", err)
			}
		})
	}
	wg.Wait()
	got, err := mate.ReadStatus(path)
	if err != nil {
		t.Fatalf("ReadStatus: %v", err)
	}
	if len(got.Runtimes.Services) != 10 || len(got.Standup.Services) != 10 {
		t.Errorf("lost writes: %d runtime entries, %d stand-up entries, want 10 and 10", len(got.Runtimes.Services), len(got.Standup.Services))
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, e := range entries {
		if e.Name() != "status.json" && e.Name() != "status.json.lock" {
			t.Errorf("left behind %s", e.Name())
		}
	}
}

// TestReadStatus covers what a reader meets: no file (an older zcp, or one
// not launched yet), a file from a newer schema with fields this one does not
// know, and a torn or foreign file — which reads as absent and is replaced by
// the next write rather than failing it.
func TestReadStatus(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantFound bool
		wantState string
	}{
		{"no file", "", false, ""},
		{"unknown fields", `{"version":1,"updatedAt":"2026-10-01T10:00:00Z","later":true,"runtimes":{"state":"done","services":[],"extra":1},"standup":{"state":"idle","services":[]}}`, true, "done"},
		{"not json", `{"version":1,"runt`, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "status.json")
			if tt.body != "" {
				if err := os.WriteFile(path, []byte(tt.body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := mate.ReadStatus(path)
			if found := err == nil; found != tt.wantFound {
				t.Fatalf("ReadStatus err = %v, want found=%v", err, tt.wantFound)
			}
			if got.Runtimes.State != tt.wantState {
				t.Errorf("runtimes.state = %q, want %q", got.Runtimes.State, tt.wantState)
			}
			if err := mate.UpdateStatus(path, func(s *mate.Status) { s.Standup.State = mate.StandupDone }); err != nil {
				t.Fatalf("UpdateStatus over %s: %v", tt.name, err)
			}
		})
	}
}

// TestStatusFilePath: the launch names the file in ZCP_STATUS_FILE; a
// process that did not inherit it (an agent that passes only the variables
// it declares) finds the same file at the default.
func TestStatusFilePath(t *testing.T) {
	t.Setenv("HOME", "/home/zerops")
	tests := []struct {
		name string
		env  string
		want string
	}{
		{"not named", "", "/home/zerops/.zcp/state/mate-status.json"},
		{"named", "/tmp/x/status.json", "/tmp/x/status.json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(mate.EnvStatusFile, tt.env)
			if got := mate.StatusFilePath(); got != tt.want {
				t.Errorf("StatusFilePath() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestUpdateSection_OnlyTheBootImportMovesItsClock: the runtimes section
// carries the time the boot import last wrote it, and a stand-up's write
// never moves it.
func TestUpdateSection_OnlyTheBootImportMovesItsClock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	old := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	tests := []struct {
		name  string
		write func() error
		moved bool
	}{
		{"the boot import writes", func() error { return mate.UpdateRuntimes(path, func(*mate.RuntimesStatus) {}) }, true},
		{"the stand-up writes", func() error {
			return mate.UpdateStandup(path, func(s *mate.StandupStatus) { s.State = mate.StandupRunning })
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := mate.UpdateStatus(path, func(s *mate.Status) { s.Runtimes.UpdatedAt = old }); err != nil {
				t.Fatal(err)
			}
			if err := tt.write(); err != nil {
				t.Fatal(err)
			}
			st, err := mate.ReadStatus(path)
			if err != nil {
				t.Fatal(err)
			}
			if moved := st.Runtimes.UpdatedAt != old; moved != tt.moved {
				t.Errorf("runtimes.updatedAt moved = %v, want %v", moved, tt.moved)
			}
		})
	}
}
