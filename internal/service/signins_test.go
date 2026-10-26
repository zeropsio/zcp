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
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/mate"
)

// identityEnv is the live env a linked container's seed reads: HQ's origin,
// the project and its key.
func identityEnv(origin, key string) func(string) string {
	return func(name string) string {
		switch name {
		case hq.EnvOrigin:
			return origin
		case hq.EnvKey:
			return key
		case hq.EnvProjectID:
			return "p-mate"
		}
		return ""
	}
}

func TestSeedSignIns_ReadsHQAndLeavesAnAbsentAnswerVisible(t *testing.T) {
	// non-parallel: HOME and stderr are process-wide.
	tests := []struct {
		name, answer, store, wantReason string
		status                          int
		keyed                           bool
		wantCalls                       int
		owed                            bool
	}{
		{"HQ signer", `{"projectId":"p-mate","signers":{"codex":"u-bo"}}`, "", "", 200, true, 1, false},
		{"existing store", `{"projectId":"p-mate","signers":{"codex":"u-bo"}}`, `{"codex":{"by":"u-own","at":1}}`, "", 200, true, 0, false},
		{"no key", "", "", "ZCP_API_KEY", 200, false, 0, false},
		{"HQ unavailable", `{"code":"not_active"}`, "", "unavailable", 503, true, 2, true},
		{"HQ record absent", `{"code":"mate_not_found"}`, "", "mate_not_found", 404, true, 1, false},
		{"other project", `{"projectId":"p-other","signers":{"codex":"u-bo"}}`, "", "project", 200, true, 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodGet || r.URL.Path != "/api/mate/self" || r.Header.Get("Authorization") != "Project test-key" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.answer))
			}))
			defer srv.Close()
			key := ""
			if tt.keyed {
				key = "test-key"
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
			lookup := identityEnv(srv.URL, key)
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
				switch {
				case tt.owed:
					if !strings.Contains(string(output), tt.wantReason) || !strings.Contains(string(output), "tried again") {
						t.Errorf("an unanswered seed needs its reason and its retry: %s", output)
					}
				case !strings.Contains(string(output), tt.wantReason) || (tt.keyed && (!strings.Contains(string(output), "remove") || !strings.Contains(string(output), mate.SignInsSeededPath()))):
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

func TestSeedSignIns_ChangedKeyRetriesOnlyTheFailedInput(t *testing.T) {
	// non-parallel: HOME is process-wide.
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "key changes", true: "legacy failed marker"}[legacy], func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("Authorization") == "Project first" {
					w.WriteHeader(http.StatusNotFound)
					_, _ = w.Write([]byte(`{"code":"mate_not_found"}`))
					return
				}
				_, _ = w.Write([]byte(`{"projectId":"p-mate","signers":{"codex":"u-bo"}}`))
			}))
			defer srv.Close()
			if legacy {
				if err := os.MkdirAll(filepath.Dir(mate.SignInsSeededPath()), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(mate.SignInsSeededPath(), []byte("read the Mate's signers from HQ: not enrolled with HQ yet"), 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				first := identityEnv(srv.URL, "first")
				seedSignIns(context.Background(), first)
				seedSignIns(context.Background(), first)
				if calls != 1 {
					t.Fatalf("same failed key made %d calls, want 1", calls)
				}
			}
			second := identityEnv(srv.URL, "second")
			seedSignIns(context.Background(), second)
			seedSignIns(context.Background(), second)
			raw, err := os.ReadFile(mate.SignInsPath())
			if err != nil {
				t.Fatalf("fresh key input did not recover the seed: %v", err)
			}
			if !strings.Contains(string(raw), "u-bo") {
				t.Errorf("store = %s", raw)
			}
			want := 2
			if legacy {
				want = 1
			}
			if calls != want {
				t.Errorf("HQ calls = %d, want %d", calls, want)
			}
		})
	}
}

// TestSeedSignIns_MovedByTheDefaultOriginSeedsFromZcp: the unit's zcp starts
// the server and so never inherits the origin it publishes; with none in its
// environment, it reads the HQ the installed Mate declares (spec-mate §3.5).
func TestSeedSignIns_MovedByTheDefaultOriginSeedsFromZcp(t *testing.T) {
	// non-parallel: HOME is process-wide.
	t.Setenv("HOME", t.TempDir())
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"projectId":"p-mate","signers":{"codex":"u-bo"}}`))
	}))
	defer srv.Close()
	pkg := filepath.Join(mate.CurrentLink(), "node_modules", mate.PackageName, "package.json")
	if err := os.MkdirAll(filepath.Dir(pkg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pkg, []byte(`{"version":"0.15.901","mate":{"hqOrigin":"`+srv.URL+`/"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	seedSignIns(context.Background(), identityEnv("", "test-key"))
	if calls != 1 {
		t.Fatalf("HQ calls = %d, want 1", calls)
	}
	raw, err := os.ReadFile(mate.SignInsPath())
	if err != nil || !strings.Contains(string(raw), "u-bo") {
		t.Fatalf("store = %s, %v", raw, err)
	}
}

func TestSeedSignIns_NoKeyLeavesTheAttemptUnspent(t *testing.T) {
	// non-parallel: HOME is process-wide.
	t.Setenv("HOME", t.TempDir())
	seedSignIns(context.Background(), identityEnv("https://hq.test", ""))
	if _, err := os.Stat(mate.SignInsSeededPath()); !os.IsNotExist(err) {
		t.Fatalf("a launch without a key spent the seed attempt: %v", err)
	}
}

func TestMateLaunchSetup_UnenrolledDoesNotSeed(t *testing.T) {
	// non-parallel: HOME and the launch seam are process-wide.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ZCP_API_KEY", "")
	t.Setenv("projectId", "")
	old := mateSeedSignIns
	mateSeedSignIns = launchSeedSignIns
	t.Cleanup(func() { mateSeedSignIns = old })
	mateLaunchSetup()
	if _, err := os.Stat(mate.SignInsSeededPath()); !os.IsNotExist(err) {
		t.Fatalf("a launch without a key spent the seed attempt: %v", err)
	}
}

// TestSeedSignIns_EmptyAnswerReadsHQOncePerStart pins the launch seed and the
// keep's first seed of one start to a single HQ read while HQ names no signers.
func TestSeedSignIns_EmptyAnswerReadsHQOncePerStart(t *testing.T) {
	// non-parallel: HOME is process-wide.
	t.Setenv("HOME", t.TempDir())
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"projectId":"p-mate"}`))
	}))
	defer srv.Close()
	lookup := identityEnv(srv.URL, "test-key")
	seedSignIns(context.Background(), lookup)
	seedSignIns(context.Background(), lookup)
	if calls != 1 {
		t.Errorf("HQ calls = %d, want 1", calls)
	}
	if _, err := os.Stat(mate.SignInsPath()); !os.IsNotExist(err) {
		t.Errorf("an answer without signers wrote a store: %v", err)
	}
	kept, err := mate.ReadSignInsSeedStatus(mate.SignInsSeededPath())
	if err != nil || kept.State != "empty" || kept.Start == "" {
		t.Errorf("marker = %+v, %v; want empty stamped with this start", kept, err)
	}
}

// TestSeedSignIns_ASignInSeedHQCouldNotTakeIsTriedAgainUntilHQTakesIt drives the
// retry loop against a fake HQ with an injected wait: a 503 leaves the seed
// owed and the delays bounded; a 404 or a 200 ends it.
func TestSeedSignIns_ASignInSeedHQCouldNotTakeIsTriedAgainUntilHQTakesIt(t *testing.T) {
	// non-parallel: HOME and stderr are process-wide.
	tests := []struct {
		name       string
		statuses   []int
		wantCalls  int
		wantWaits  int
		wantStored bool
		wantState  string
	}{
		{"503 then 200", []int{503, 200}, 2, 1, true, "seeded"},
		{"503 forever stops after the bounded delays", []int{503}, 1 + len(seedRetryDelays), len(seedRetryDelays), false, "retry"},
		{"404 is not retried", []int{404, 200}, 1, 0, false, "failed"},
		{"200 needs no retry", []int{200}, 1, 0, true, "seeded"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				status := tt.statuses[min(calls, len(tt.statuses)-1)]
				calls++
				w.WriteHeader(status)
				switch status {
				case 503:
					_, _ = w.Write([]byte(`{"code":"not_active"}`))
				case 404:
					_, _ = w.Write([]byte(`{"code":"mate_not_found"}`))
				default:
					_, _ = w.Write([]byte(`{"projectId":"p-mate","signers":{"codex":"u-bo"}}`))
				}
			}))
			defer srv.Close()
			lookup := identityEnv(srv.URL, "test-key")
			waits := 0
			if seedSignIns(context.Background(), lookup) {
				retrySignInSeed(context.Background(), lookup, func(context.Context, time.Duration) bool { waits++; return true })
			}
			if calls != tt.wantCalls || waits != tt.wantWaits {
				t.Errorf("HQ calls = %d, waits = %d; want %d, %d", calls, waits, tt.wantCalls, tt.wantWaits)
			}
			if _, err := os.Stat(mate.SignInsPath()); (err == nil) != tt.wantStored {
				t.Errorf("store written = %v, want %v", err == nil, tt.wantStored)
			}
			if kept, err := mate.ReadSignInsSeedStatus(mate.SignInsSeededPath()); err != nil || kept.State != tt.wantState {
				t.Errorf("marker = %+v, %v; want %s", kept, err, tt.wantState)
			}
		})
	}
}
