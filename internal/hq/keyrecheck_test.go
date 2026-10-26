package hq

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestClient_RecheckKey_ReportsHQsOwnVerdictOnTheKey: zcp asks HQ to recheck
// its key with `POST /api/mate/key-recheck` and reads HQ's three answers as
// HQ's protocol states them — 204 the key still resolves, 401 key_refused
// Zerops refuses it, 503 zerops_unanswered HQ could not tell (spec-mate
// §3.3, TC-21).
func TestClient_RecheckKey_ReportsHQsOwnVerdictOnTheKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		status int
		body   string
		want   KeyVerdict
		fails  bool
	}{
		{"204 holds the key", http.StatusNoContent, "", KeyHeld, false},
		{"401 key_refused refuses it", http.StatusUnauthorized, `{"code":"key_refused"}`, KeyRefused, false},
		{"503 zerops_unanswered tells nothing", http.StatusServiceUnavailable, `{"code":"zerops_unanswered"}`, "", true},
		{"401 project_key_required is not a verdict on the key", http.StatusUnauthorized, `{"code":"project_key_required"}`, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var method, path, authorization string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				method, path, authorization = r.Method, r.URL.EscapedPath(), r.Header.Get("Authorization")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer srv.Close()

			verdict, err := keyedClient(t, srv).Once().RecheckKey(context.Background())

			if (err != nil) != tt.fails || verdict != tt.want {
				t.Errorf("verdict, err = %q, %v; want %q, failing %v", verdict, err, tt.want, tt.fails)
			}
			if method != http.MethodPost || path != "/api/mate/key-recheck" || authorization != "Project good" {
				t.Errorf("HQ saw %s %s as %q", method, path, authorization)
			}
		})
	}
}

// TestKeyRechecker_AsksHQAtMostOnceAMinute: a refusal Zerops gave zcp's key
// is reported to HQ once, and every refusal inside the minute after the ask
// goes unreported; one after the minute asks again (TC-21: at most once a
// minute). Time is the test's clock.
func TestKeyRechecker_AsksHQAtMostOnceAMinute(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	asked := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		asked++
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	var told []string
	rechecker := &KeyRechecker{
		Open: func() (Client, error) { return keyedClient(t, srv).Once(), nil },
		Now:  func() time.Time { return now },
		Tell: func(s string) { told = append(told, s) },
	}

	steps := []struct {
		after time.Duration
		asks  int
	}{
		{0, 1},
		{59 * time.Second, 1},
		{time.Second, 2},
		{0, 2},
	}
	for _, step := range steps {
		now = now.Add(step.after)
		rechecker.Refused(context.Background())
		if asked != step.asks {
			t.Fatalf("at +%s HQ was asked %d times, want %d", step.after, asked, step.asks)
		}
	}
	if len(told) != 2 || !strings.Contains(told[0], "still resolves") {
		t.Errorf("reported %q, want two reports of HQ's answer", told)
	}
}

// TestKeyRechecker_ReportsHQsAnswerAndTrustsNoneOfIt: a refusal HQ confirms,
// one it cannot tell, and an HQ that does not answer are each reported in
// words; zcp's own state is never what changes.
func TestKeyRechecker_ReportsHQsAnswerAndTrustsNoneOfIt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"key_refused", http.StatusUnauthorized, `{"code":"key_refused"}`, "refuses the key"},
		{"zerops_unanswered", http.StatusServiceUnavailable, `{"code":"zerops_unanswered"}`, "could not tell"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer srv.Close()
			var told []string
			rechecker := &KeyRechecker{
				Open: func() (Client, error) { return keyedClient(t, srv).Once(), nil },
				Now:  time.Now,
				Tell: func(s string) { told = append(told, s) },
			}

			rechecker.Refused(context.Background())

			if len(told) != 1 || !strings.Contains(told[0], tt.want) {
				t.Errorf("reported %q, want one report holding %q", told, tt.want)
			}
		})
	}
}
