//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/devsup"
)

func shortDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("/tmp", "devsupcmd") //nolint:usetesting // the unix socket path must stay under 104 bytes on macOS; t.TempDir() is longer
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	return d
}

func ctl(t *testing.T, state string, args ...string) (int, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(append([]string{"ctl", "--state", state}, args...), &out, &errOut)
	return code, out.String()
}

func TestCtl_NoSupervisorExitsTwo(t *testing.T) {
	t.Parallel()
	code, out := ctl(t, shortDir(t), "status", "--json")
	if code != devsup.CodeNoSupervisor {
		t.Fatalf("exit = %d, want 2", code)
	}
	var got struct {
		Code  int    `json:"code"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.Code != 2 || got.Error == "" {
		t.Fatalf("out = %q (%v)", out, err)
	}
}

func TestCtl_VersionNamesTheFormats(t *testing.T) {
	t.Parallel()
	code, out := ctl(t, shortDir(t), "version", "--json")
	var got struct {
		Version string `json:"version"`
		Formats []int  `json:"formats"`
	}
	if err := json.Unmarshal([]byte(out), &got); code != 0 || err != nil || got.Version != version || !reflect.DeepEqual(got.Formats, []int{1}) {
		t.Fatalf("exit %d out %q", code, out)
	}
}

func TestCtl_DrivesARunningSupervisor(t *testing.T) {
	t.Parallel()
	root, state := t.TempDir(), shortDir(t)
	if err := os.MkdirAll(filepath.Join(root, ".zcp"), 0o755); err != nil {
		t.Fatal(err)
	}
	decl := `{"version":1,"servers":[{"name":"web","command":"echo hello; exec sleep 300"}]}`
	if err := os.WriteFile(filepath.Join(root, devsup.DeclarationPath), []byte(decl), 0o644); err != nil {
		t.Fatal(err)
	}
	sup, err := devsup.New(devsup.Config{Root: root, StateDir: state, Env: os.Environ(), Grace: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = sup.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	state1 := func(args ...string) (int, devsup.State) {
		t.Helper()
		var code int
		var out string
		deadline := time.Now().Add(10 * time.Second)
		for {
			code, out = ctl(t, state, append([]string{"--json"}, args...)...)
			if code != devsup.CodeNoSupervisor || time.Now().After(deadline) {
				break
			}
			time.Sleep(20 * time.Millisecond) // the socket is not bound yet
		}
		var st devsup.State
		_ = json.Unmarshal([]byte(out), &st)
		return code, st
	}

	if code, st := state1("status"); code != 0 || st.Servers["web"].Source != devsup.SourceDeclared {
		t.Fatalf("status: %d %+v", code, st)
	}
	// web's output may trail its start; the stop below must not cut it off.
	var code int
	var out string
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		code, out = ctl(t, state, "logs", "web", "--lines", "1")
		if out == "hello\n" || time.Now().After(deadline) {
			break
		}
	}
	if code != 0 || out != "hello\n" {
		t.Fatalf("logs: %d %q", code, out)
	}
	if code, st := state1("stop", "web"); code != 0 || st.Servers["web"].Status != devsup.StatusStopped {
		t.Fatalf("stop: %d %+v", code, st)
	}
	if code, _ := state1("start", "nope"); code != devsup.CodeUnknownName {
		t.Fatalf("start nope: exit %d, want 3", code)
	}
	if code, st := state1("run", "--name", "crew-ada", "--cwd", ".", "--", "exec", "sleep", "300"); code != 0 ||
		st.Servers["crew-ada"].Command != "exec sleep 300" || st.Servers["crew-ada"].Source != devsup.SourceRuntime {
		t.Fatalf("run: %d %+v", code, st)
	}
	if code, _ := ctl(t, state, "logs", "nope"); code != devsup.CodeUnknownName {
		t.Fatalf("logs nope: exit %d", code)
	}
}

func TestParseCtl_FlagsAnywhereBeforeTheCommand(t *testing.T) {
	t.Parallel()
	got, err := parseCtl([]string{"run", "--json", "--name", "crew-ada", "--port", "4100", "--cwd", ".crew/ada", "--", "bun", "run", "dev", "--port", "4100"})
	if err != nil {
		t.Fatal(err)
	}
	want := ctlArgs{verb: "run", name: "crew-ada", json: true, lines: 50, cwd: ".crew/ada", port: 4100, command: "bun run dev --port 4100"}
	if got != want {
		t.Fatalf("got %+v", got)
	}
	for _, bad := range [][]string{{}, {"--json"}, {"start", "a", "b"}, {"logs", "web", "--lines", "x"}, {"status", "--bogus"}} {
		if _, err := parseCtl(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestStatusText_NamesThePreviewPort(t *testing.T) {
	t.Parallel()
	st := devsup.State{Servers: map[string]devsup.ServerState{
		"web": {Status: devsup.StatusRunning, PreviewPort: 9003},
		"api": {Status: devsup.StatusRunning},
	}}
	got := statusText(st)
	if !strings.Contains(got, "preview :9003") || strings.Count(got, "preview") != 1 {
		t.Fatalf("statusText = %q", got)
	}
}
