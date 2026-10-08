package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// non-parallel: the CLI reads process environment and writes os.Stdout.
func TestMateStatus_LocalStartup_CapabilityWithoutNetwork(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer server.Close()
	t.Setenv("ZCP_MATE_MANIFEST_URL", server.URL)
	text := captureStdout(t, func() {
		if code := runMateStatus([]string{"--local", "--json"}); code != 0 {
			t.Fatalf("exit=%d", code)
		}
	})
	var result struct {
		Updater struct {
			Protocol           int    `json:"protocol"`
			Phase              string `json:"phase"`
			RollbackCompatible bool   `json:"rollbackCompatible"`
		} `json:"updater"`
	}
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("startup made %d network calls", calls)
	}
	if result.Updater.Protocol != 1 || result.Updater.Phase != "idle" || result.Updater.RollbackCompatible {
		t.Fatalf("startup capability=%+v", result.Updater)
	}
}
