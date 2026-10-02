package main

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
)

type stubHQ struct {
	status hq.Status
	result hq.Result
	err    error
}

func (s stubHQ) Status(context.Context) hq.Status { return s.status }

func (s stubHQ) Enroll(context.Context) (hq.Result, error) { return s.result, s.err }

// withHQ points runHQCmd at svc for the test; not parallel — it swaps a
// package seam.
func withHQ(t *testing.T, svc hqService, err error) {
	t.Helper()
	orig := newHQService
	newHQService = func(context.Context) (hqService, error) { return svc, err }
	t.Cleanup(func() { newHQService = orig })
}

func TestRunHQCmd_UnknownSubcommand_Fails(t *testing.T) {
	for _, args := range [][]string{nil, {}, {"bogus"}, {"--json"}} {
		if got := runHQCmd(args); got != 1 {
			t.Errorf("runHQCmd(%v) = %d, want 1", args, got)
		}
	}
}

func TestRunHQCmd_Status_JSONExitsZero(t *testing.T) {
	official := hq.Official{Verdict: hq.VerdictOfficial, ProjectID: "hq1", Address: "https://hq.example"}
	tests := []struct {
		name string
		svc  hqService
		err  error
		want map[string]any
	}{
		{"enrolled", stubHQ{status: hq.Status{HQ: official, Enrolled: true}}, nil, map[string]any{
			"hq":       map[string]any{"verdict": "official", "projectId": "hq1", "address": "https://hq.example"},
			"enrolled": true,
		}},
		{"no HQ", stubHQ{status: hq.Status{HQ: hq.Official{Verdict: hq.VerdictNone}}}, nil, map[string]any{
			"hq": map[string]any{"verdict": "none"}, "enrolled": false,
		}},
		{"no credentials", nil, errors.New("auth: no token"), map[string]any{
			"hq": map[string]any{"verdict": "unknown"}, "enrolled": false, "error": "auth: no token",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withHQ(t, tt.svc, tt.err)
			var code int
			stdout := captureStdout(t, func() { code = runHQCmd([]string{"status", "--json"}) })
			if code != 0 {
				t.Fatalf("exit = %d, want 0", code)
			}
			var got map[string]any
			if err := json.Unmarshal([]byte(stdout), &got); err != nil {
				t.Fatalf("stdout %q: %v", stdout, err)
			}
			wantRaw, _ := json.Marshal(tt.want)
			gotRaw, _ := json.Marshal(got)
			if string(gotRaw) != string(wantRaw) {
				t.Errorf("status = %s, want %s", gotRaw, wantRaw)
			}
		})
	}
}

func TestRunHQCmd_Enroll_ExitCodeAndReason(t *testing.T) {
	tests := []struct {
		name     string
		svc      hqService
		wantCode int
		want     map[string]any
	}{
		{"enrolled", stubHQ{result: hq.Result{HQ: "https://hq.example", Changed: true}}, 0,
			map[string]any{"hq": "https://hq.example", "changed": true}},
		{"already enrolled", stubHQ{result: hq.Result{HQ: "https://hq.example"}}, 0,
			map[string]any{"hq": "https://hq.example", "changed": false}},
		{"no official HQ", stubHQ{err: &hq.NoHQError{Official: hq.Official{Verdict: hq.VerdictUnclear}}}, 1,
			map[string]any{"changed": false, "error": "no official HQ (unclear)"}},
		{"refused", stubHQ{err: &hq.RefusedError{Status: 403, Code: "project_not_in_org"}}, 1,
			map[string]any{"changed": false, "error": "hq refused: 403 project_not_in_org"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withHQ(t, tt.svc, nil)
			var code int
			stdout := captureStdout(t, func() { code = runHQCmd([]string{"enroll", "--json"}) })
			if code != tt.wantCode {
				t.Fatalf("exit = %d, want %d", code, tt.wantCode)
			}
			var got map[string]any
			if err := json.Unmarshal([]byte(stdout), &got); err != nil {
				t.Fatalf("stdout %q: %v", stdout, err)
			}
			wantRaw, _ := json.Marshal(tt.want)
			gotRaw, _ := json.Marshal(got)
			if string(gotRaw) != string(wantRaw) {
				t.Errorf("enroll = %s, want %s", gotRaw, wantRaw)
			}
		})
	}
}

func TestCLIDispatch_HQ_IsAVerb(t *testing.T) {
	t.Parallel()
	if _, ok := cliDispatch()["hq"]; !ok {
		t.Fatal(`"hq" is not a CLI verb: zcp hq would start the MCP server instead`)
	}
}

// TestRunHQGitCredential: `zcp hq git-credential` is the credential helper
// git runs on every fetch and push to HQ from the Mate's shell — it prints the
// Mate credential under HQ's git user, for the enrolled HQ only, and nothing
// else on stdout. Declining exits non-zero with nothing printed, so a helper
// chain goes on to its next answer and never sends an empty password.
func TestRunHQGitCredential(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "enrollment.json")
	if err := hq.SaveEnrollment(path, hq.Enrollment{HQ: "https://hq.example", ProjectID: "p1", Credential: "secret"}); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		args     []string
		stdin    string
		want     string
		wantCode int
	}{
		{"get for the HQ", []string{"get"}, "protocol=https\nhost=hq.example\n\n", "username=mate\npassword=secret\n", 0},
		{"get for another host", []string{"get"}, "protocol=https\nhost=github.com\n\n", "", 1},
		{"store keeps nothing", []string{"store"}, "protocol=https\nhost=hq.example\nusername=mate\npassword=secret\n\n", "", 0},
		{"erase keeps nothing", []string{"erase"}, "protocol=https\nhost=hq.example\n\n", "", 0},
		{"no action", nil, "", "", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var out strings.Builder
			if code := runHQGitCredential(tt.args, strings.NewReader(tt.stdin), &out, path); code != tt.wantCode {
				t.Fatalf("exit %d, want %d", code, tt.wantCode)
			}
			if out.String() != tt.want {
				t.Errorf("stdout = %q, want %q", out.String(), tt.want)
			}
		})
	}
}

// TestIsHQGitCredential: the helper's verb is answered before the CLI's
// telemetry, whose one-time notice goes to stdout and whose flush would hold
// every git operation.
func TestIsHQGitCredential(t *testing.T) {
	t.Parallel()
	tests := []struct {
		args []string
		want bool
	}{
		{[]string{"hq", "git-credential", "get"}, true},
		{[]string{"hq", "git-credential"}, true},
		{[]string{"hq", "status"}, false},
		{[]string{"hq"}, false},
		{nil, false},
	}
	for _, tt := range tests {
		if got := isHQGitCredential(tt.args); got != tt.want {
			t.Errorf("isHQGitCredential(%q) = %v, want %v", tt.args, got, tt.want)
		}
	}
}
