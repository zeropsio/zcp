package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/mate"
)

func TestSeedSignIns_ReadsHQAndLeavesAnAbsentAnswerVisible(t *testing.T) {
	// non-parallel: HOME and stderr are process-wide.
	tests := []struct {
		name, answer, store, wantReason string
		status                          int
		enrolled                        bool
		wantCalls                       int
	}{
		{"HQ signer", `{"projectId":"p-mate","signers":{"codex":"u-bo"}}`, "", "", 200, true, 1},
		{"existing store", `{"projectId":"p-mate","signers":{"codex":"u-bo"}}`, `{"codex":{"by":"u-own","at":1}}`, "", 200, true, 0},
		{"not enrolled", "", "", "not enrolled", 200, false, 0},
		{"HQ unavailable", `{"code":"not_active"}`, "", "unavailable", 503, true, 1},
		{"HQ record absent", `{"code":"mate_not_found"}`, "", "mate_not_found", 404, true, 1},
		{"other project", `{"projectId":"p-other","signers":{"codex":"u-bo"}}`, "", "project", 200, true, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodGet || r.URL.Path != "/api/mate/self" || r.Header.Get("Authorization") != "Mate test-credential" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.answer))
			}))
			defer srv.Close()
			if tt.enrolled {
				if err := hq.SaveEnrollment(hq.EnrollmentPath(), hq.Enrollment{HQ: srv.URL, ProjectID: "p-mate", Credential: "test-credential"}); err != nil {
					t.Fatal(err)
				}
			}
			if tt.store != "" {
				if err := os.MkdirAll(filepath.Dir(mate.SignInsPath()), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(mate.SignInsPath(), []byte(tt.store), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			log, err := os.CreateTemp(t.TempDir(), "stderr")
			if err != nil {
				t.Fatal(err)
			}
			old := os.Stderr
			os.Stderr = log
			defer func() { os.Stderr = old; _ = log.Close() }()
			lookup := func(key string) string {
				if key == "projectId" {
					return "p-mate"
				}
				return ""
			}
			seedSignIns(context.Background(), lookup)
			seedSignIns(context.Background(), lookup)
			if calls != tt.wantCalls {
				t.Errorf("HQ calls = %d, want %d", calls, tt.wantCalls)
			}
			output, err := os.ReadFile(log.Name())
			if err != nil {
				t.Fatal(err)
			}
			raw, readErr := os.ReadFile(mate.SignInsPath())
			switch {
			case tt.store != "":
				if string(raw) != tt.store {
					t.Errorf("existing store changed: %s", raw)
				}
			case tt.wantReason != "":
				if !os.IsNotExist(readErr) {
					t.Errorf("absent HQ answer wrote store: %s, %v", raw, readErr)
				}
				if !strings.Contains(string(output), tt.wantReason) || !strings.Contains(string(output), "remove") || !strings.Contains(string(output), mate.SignInsSeededPath()) {
					t.Errorf("failure needs its reason and manual again: %s", output)
				}
			default:
				var got map[string]mate.SignIn
				if err := json.Unmarshal(raw, &got); err != nil {
					t.Fatal(err)
				}
				if got["codex"].By != "u-bo" || got["codex"].At <= 0 {
					t.Errorf("store = %s", raw)
				}
			}
		})
	}
}
