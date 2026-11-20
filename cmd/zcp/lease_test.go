package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// TestCLIDispatch_LeaseIsAVerb: `zcp lease …` is a CLI verb, never MCP serve
// mode — which would wait on stdin for a client that never comes.
func TestCLIDispatch_LeaseIsAVerb(t *testing.T) {
	t.Parallel()
	if _, ok := cliDispatch()["lease"]; !ok {
		t.Fatal(`"lease" is not a CLI verb: run() would fall through to serve mode and wait on stdin`)
	}
}

// TestRunLeaseCmd_BadArguments_ErrorOnStdoutExitOne: a call the verb cannot
// run answers exit 1 with its error and code as JSON under --json, before anything is
// resolved or released, and never reads stdin.
func TestRunLeaseCmd_BadArguments_ErrorOnStdoutExitOne(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"no subcommand", []string{"--json"}, "usage"},
		{"unknown subcommand", []string{"status", "--json"}, "usage"},
		{"acquire without a pool or branch", []string{"acquire", "--json"}, "usage"},
		{"no branch", []string{"release", "--json"}, "--branch"},
		{"empty branch", []string{"release", "--branch", "", "--json"}, "--branch"},
		{"force is never taken", []string{"release", "--branch", "feat-x", "--force", "--json"}, "force"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			resolved := false
			resolve := func(context.Context) (ops.LeaseDeps, error) {
				resolved = true
				return ops.LeaseDeps{}, errors.New("not reached")
			}
			code := runLeaseCmd(tt.args, &out, resolve)
			if code != 1 {
				t.Errorf("exit %d, want 1", code)
			}
			if resolved {
				t.Error("a bad call resolved the platform client")
			}
			var answer map[string]string
			// The error carries its wire code beside the words.
			if err := json.Unmarshal(out.Bytes(), &answer); err != nil || !strings.Contains(answer["error"], tt.want) || answer["code"] != platform.ErrInvalidUsage || len(answer) != 2 {
				t.Errorf("stdout = %q, want one JSON error naming %q with code %s", out.String(), tt.want, platform.ErrInvalidUsage)
			}
		})
	}
}

// TestRunLeaseCmd_ResolveFails_ErrorExitOne: a container whose platform
// client cannot be built answers exit 1 with why, as JSON.
func TestRunLeaseCmd_ResolveFails_ErrorExitOne(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	code := runLeaseCmd([]string{"release", "--branch", "feat-x", "--repo", "https://github.com/acme/app", "--json"}, &out,
		func(context.Context) (ops.LeaseDeps, error) { return ops.LeaseDeps{}, errors.New("auth: no token") })
	if code != 1 || !strings.Contains(out.String(), `"error"`) || !strings.Contains(out.String(), "no token") {
		t.Errorf("exit %d, stdout %q; want 1 and the error", code, out.String())
	}
}

// TestRunLeaseCmd_NothingHeld_ReleasedNothingExitZero: a branch this zcp
// holds no lease of is released already — exit 0, `{"released":[]}` — so a
// retry after a restart never fails.
func TestRunLeaseCmd_NothingHeld_ReleasedNothingExitZero(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	code := runLeaseCmd([]string{"release", "--branch", "feat-x", "--json"}, &out,
		func(context.Context) (ops.LeaseDeps, error) { return ops.LeaseDeps{StateDir: stateDir}, nil })
	if code != 0 || strings.TrimSpace(out.String()) != `{"released":[]}` {
		t.Errorf("exit %d, stdout %q; want 0 and nothing released", code, out.String())
	}
}

// witness: cmd/zcp/lease.go | fs.StringVar(&pool, "pool", "", → fs.StringVar(&pool, "pools", "",
func TestRunLeaseCmd_Acquire_DispatchesSinglePool(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name     string
		args     []string
		wantExit int
	}{
		{"main refused by acquire", []string{"--pool", "devpool", "--branch", "main"}, 3},
		{"pool required", []string{"--branch", "main"}, 1},
		{"branch required", []string{"--pool", "devpool"}, 1},
		{"several pools", []string{"--pools", "devpool,webpool", "--branch", "main"}, 1},
		{"from", []string{"--pool", "devpool", "--branch", "main", "--from", "feature"}, 1},
		{"force", []string{"--pool", "devpool", "--branch", "main", "--force"}, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			args := append([]string{"acquire", "--json"}, tt.args...)
			exit := runLeaseCmd(args, &out, func(context.Context) (ops.LeaseDeps, error) {
				return ops.LeaseDeps{}, nil // main is refused before any platform read.
			})
			var a struct {
				Refused *struct {
					Code string `json:"code"`
				} `json:"refused"`
				Code string `json:"code"`
			}
			if err := json.Unmarshal(out.Bytes(), &a); err != nil || exit != tt.wantExit {
				t.Fatalf("exit %d, stdout %q, want %d and one JSON answer", exit, out.String(), tt.wantExit)
			}
			if exit == 3 && (a.Refused == nil || a.Refused.Code != platform.ErrInvalidParameter) ||
				exit == 1 && a.Code != platform.ErrInvalidUsage {
				t.Fatalf("unexpected answer %s", out.String())
			}
		})
	}
}
