//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/devsup"
)

// echoPortEnv, set, makes the test binary the dev server a test supervises:
// it echoes every connection's bytes back on that port.
const echoPortEnv = "ZCP_DEVSUP_TEST_ECHO_PORT"

func TestMain(m *testing.M) {
	if port := os.Getenv(echoPortEnv); port != "" {
		os.Exit(echoServer(port))
	}
	os.Exit(m.Run())
}

func echoServer(port string) int {
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:"+port)
	if err != nil {
		return 3
	}
	for {
		c, err := ln.Accept()
		if err != nil {
			return 0
		}
		go func() {
			defer c.Close()
			_, _ = io.Copy(c, c)
		}()
	}
}

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
	code, out, _ := ctlOutputs(t, state, args...)
	return code, out
}

// ctlOutputs is ctl with what it wrote to stderr too.
func ctlOutputs(t *testing.T, state string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(append([]string{"ctl", "--state", state}, args...), &out, &errOut)
	return code, out.String(), errOut.String()
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
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	for _, bad := range [][]string{{}, {"--json"}, {"start", "a", "b"}, {"logs", "web", "--lines", "x"}, {"status", "--bogus"}} {
		if _, err := parseCtl(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestParseCtl_Env_SetPairsAndClear(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		args    []string
		want    ctlArgs
		wantErr string
	}{
		{
			name: "pairs, a value holding = and an empty value",
			args: []string{"env", "--set", "HQ_HOST=node-id-7.runtime.hqpool.zerops", "--json", "--set", "Q=a=b", "--set", "EMPTY="},
			want: ctlArgs{verb: "env", json: true, lines: 50, env: map[string]string{"HQ_HOST": "node-id-7.runtime.hqpool.zerops", "Q": "a=b", "EMPTY": ""}},
		},
		{name: "clear", args: []string{"env", "--clear"}, want: ctlArgs{verb: "env", lines: 50, clear: true}},
		{name: "a pair without =", args: []string{"env", "--set", "HQ_HOST"}, wantErr: `"HQ_HOST" is not NAME=VALUE`},
		{name: "a name set twice", args: []string{"env", "--set", "A=1", "--set", "A=2"}, wantErr: "A is set twice"},
		{name: "--set without a value", args: []string{"env", "--set"}, wantErr: "--set needs a value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseCtl(tt.args)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %+v (%v), want %+v", got, err, tt.want)
			}
		})
	}
}

// runningSupervisor runs a supervisor on decl, with env added to the test's
// environment, and returns its state directory once its socket answers.
func runningSupervisor(t *testing.T, decl string, grace time.Duration, env ...string) string {
	t.Helper()
	root, state := t.TempDir(), shortDir(t)
	if err := os.MkdirAll(filepath.Join(root, ".zcp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, devsup.DeclarationPath), []byte(decl), 0o644); err != nil {
		t.Fatal(err)
	}
	sup, err := devsup.New(devsup.Config{Root: root, StateDir: state, Env: append(os.Environ(), env...), Grace: grace})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = sup.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		code, _ := ctl(t, state, "status", "--json")
		if code != devsup.CodeNoSupervisor {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the supervisor's socket never answered")
		}
	}
	return state
}

// ctlState runs one ctl call with --json and decodes the state it answers.
func ctlState(t *testing.T, state string, args ...string) (int, devsup.State, string) {
	t.Helper()
	code, out := ctl(t, state, append([]string{"--json"}, args...)...)
	var st devsup.State
	_ = json.Unmarshal([]byte(out), &st)
	return code, st, out
}

// waitFor polls cond for up to 15 s.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(15 * time.Second); !cond(); time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// zcp reads `ctl version` before it sends a newer verb: an older ctl fails
// on a flag it does not know.
func TestCtl_VersionNamesCapabilities(t *testing.T) {
	t.Parallel()
	code, out := ctl(t, shortDir(t), "version", "--json")
	var got struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal([]byte(out), &got); code != 0 || err != nil || !reflect.DeepEqual(got.Capabilities, []string{"env", "activity"}) {
		t.Fatalf("exit %d out %q", code, out)
	}
}

// `activity` says `ctl status` reports the preview server's
// previewLastConnAt and previewOpenConns: zcp reads them only from a
// supervisor whose `ctl version` lists it (docs/spec-mate.md §2.8a.2).
func TestCapabilities_IncludeActivity(t *testing.T) {
	t.Parallel()
	code, out := ctl(t, shortDir(t), "version", "--json")
	var got struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal([]byte(out), &got); code != 0 || err != nil {
		t.Fatalf("exit %d out %q (%v)", code, out, err)
	}
	if !strings.Contains(","+strings.Join(got.Capabilities, ",")+",", ",activity,") {
		t.Fatalf("capabilities = %q, want activity among them", got.Capabilities)
	}
}

// previewStatus is a server's preview activity as `ctl status --json` puts it.
type previewStatus struct {
	PreviewPort       int        `json:"previewPort"`
	PreviewLastConnAt *time.Time `json:"previewLastConnAt"`
	PreviewOpenConns  *int       `json:"previewOpenConns"`
}

func ctlPreview(t *testing.T, state, name string) (previewStatus, string) {
	t.Helper()
	code, out := ctl(t, state, "status", name, "--json")
	var st struct {
		Servers map[string]previewStatus `json:"servers"`
	}
	if err := json.Unmarshal([]byte(out), &st); code != 0 || err != nil {
		t.Fatalf("status %s: exit %d out %q (%v)", name, code, out, err)
	}
	return st.Servers[name], out
}

// freeTCPPort is a port nothing listened on a moment ago.
func freeTCPPort(t *testing.T) int {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener address %v is not TCP", ln.Addr())
	}
	return addr.Port
}

// TestCtlStatus_PreviewActivity: `ctl status --json` reports, for the preview
// server, when its forward last took a connection (previewLastConnAt) and how
// many it holds open (previewOpenConns, 0 included), counted at the forward
// while the bytes pass unchanged; a server without a preview port has
// neither (docs/spec-mate.md §2.8a.2).
func TestCtlStatus_PreviewActivity(t *testing.T) {
	t.Parallel()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	web, preview := freeTCPPort(t), freeTCPPort(t)
	cmd := echoPortEnv + "=" + strconv.Itoa(web) + " exec '" + exe + "'"
	state := runningSupervisor(t, `{"version":1,"previewPortBase":`+strconv.Itoa(preview-1)+`,"servers":[`+
		`{"name":"web","port":`+strconv.Itoa(web)+`,"command":`+strconv.Quote(cmd)+`},`+
		`{"name":"worker","command":"exec sleep 300"}]}`, time.Second, "ZEROPS_StableNumber=1")

	// The forward opens once web listens; the first connection it takes is
	// the one held open.
	var conn net.Conn
	before := time.Now()
	waitFor(t, "the preview port to forward", func() bool {
		d := net.Dialer{Timeout: time.Second}
		c, err := d.DialContext(context.Background(), "tcp", "127.0.0.1:"+strconv.Itoa(preview))
		if err != nil {
			return false
		}
		conn = c
		return true
	})
	t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	got := make([]byte, 5)
	if _, err := conn.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(conn, got); err != nil || string(got) != "hello" {
		t.Fatalf("the preview port answered %q (%v), want the bytes back unchanged", got, err)
	}

	p, out := ctlPreview(t, state, "web")
	if p.PreviewPort != preview || p.PreviewOpenConns == nil || *p.PreviewOpenConns != 1 ||
		p.PreviewLastConnAt == nil || p.PreviewLastConnAt.Before(before.Truncate(time.Microsecond)) || p.PreviewLastConnAt.After(time.Now()) {
		t.Fatalf("web with one preview connection: %s\nwant previewPort %d, previewOpenConns 1, previewLastConnAt from %s on", out, preview, before)
	}
	last := *p.PreviewLastConnAt

	_ = conn.Close()
	var zero string
	waitFor(t, "the closed connection to leave the count", func() bool {
		p, zero = ctlPreview(t, state, "web")
		return p.PreviewOpenConns != nil && *p.PreviewOpenConns == 0
	})
	if !strings.Contains(zero, `"previewOpenConns": 0`) || p.PreviewLastConnAt == nil || !p.PreviewLastConnAt.Equal(last) {
		t.Fatalf("web after the connection closed: %s\nwant previewOpenConns 0 on the wire and previewLastConnAt %s kept", zero, last)
	}
	if w, out := ctlPreview(t, state, "worker"); w.PreviewOpenConns != nil || w.PreviewLastConnAt != nil {
		t.Fatalf("worker has no preview port, yet reports preview activity: %s", out)
	}
}

func TestCtl_Env_ReplacesTheOverlay(t *testing.T) {
	t.Parallel()
	state := runningSupervisor(t, `{"version":1,"servers":[]}`, time.Second)
	for _, step := range []struct {
		args []string
		want map[string]string
	}{
		{[]string{"--set", "HQ_HOST=hq1", "--set", "API_HOST=api1"}, map[string]string{"HQ_HOST": "hq1", "API_HOST": "api1"}},
		{[]string{"--set", "HQ_HOST=hq2"}, map[string]string{"HQ_HOST": "hq2"}},
		{[]string{"--clear"}, map[string]string{}},
		{[]string{"--clear", "--set", "HQ_HOST=hq3"}, map[string]string{"HQ_HOST": "hq3"}},
	} {
		code, st, out := ctlState(t, state, append([]string{"env"}, step.args...)...)
		if code != 0 || !reflect.DeepEqual(st.Env, step.want) {
			t.Fatalf("env %q: exit %d, out %s, want env %v", step.args, code, out, step.want)
		}
		if _, st, _ := ctlState(t, state, "status"); !reflect.DeepEqual(st.Env, step.want) {
			t.Fatalf("status after env %q: env %v", step.args, st.Env)
		}
	}
}

// A stop's grace can outlast ctl's wait: env answers once the overlay is
// stored and the restarts follow.
func TestCtl_Env_AnswersBeforeTheRestarts(t *testing.T) {
	t.Parallel()
	// web ignores SIGTERM, so its restart cannot finish before its grace ends:
	// an answer inside the grace came before the restart, whatever the state
	// it carries shows of the restart the supervisor has already begun.
	const grace = 8 * time.Second
	state := runningSupervisor(t, `{"version":1,"servers":[{"name":"web","command":"trap '' TERM; echo \"HQ_HOST=${HQ_HOST-unset}\"; exec sleep 300"}]}`, grace)
	var pid int
	waitFor(t, "web to run", func() bool {
		_, st, _ := ctlState(t, state, "status", "web")
		pid = st.Servers["web"].Pid
		return st.Servers["web"].Status == devsup.StatusRunning && pid > 0
	})

	asked := time.Now()
	code, st, out := ctlState(t, state, "env", "--set", "HQ_HOST=node-id-7.runtime.hqpool.zerops")
	took := time.Since(asked)

	if code != 0 || st.Env["HQ_HOST"] != "node-id-7.runtime.hqpool.zerops" {
		t.Fatalf("env: exit %d out %s", code, out)
	}
	if sv := st.Servers["web"]; took >= grace || sv.Restarts != 0 {
		t.Fatalf("env answered after web's restart (took %s): %+v", took, sv)
	}
	waitFor(t, "web to restart under the overlay", func() bool {
		_, st, _ := ctlState(t, state, "status", "web")
		sv := st.Servers["web"]
		return sv.Restarts == 1 && sv.Pid != pid && sv.Pid > 0 && sv.LastRestartReason == "env changed"
	})
	waitFor(t, "web to print the overlay", func() bool {
		_, out := ctl(t, state, "logs", "web", "--lines", "1")
		return out == "HQ_HOST=node-id-7.runtime.hqpool.zerops\n"
	})
}

func TestCtl_Env_RejectsReservedAndMalformedKeys(t *testing.T) {
	t.Parallel()
	state := runningSupervisor(t, `{"version":1,"servers":[]}`, time.Second)
	kept := map[string]string{"HQ_HOST": "node-id-7.runtime.hqpool.zerops"}
	if code, st, out := ctlState(t, state, "env", "--set", "HQ_HOST="+kept["HQ_HOST"]); code != 0 || !reflect.DeepEqual(st.Env, kept) {
		t.Fatalf("env: exit %d out %s", code, out)
	}
	// The supervisor refuses a name (a JSON answer); ctl refuses a malformed
	// command line before it calls (on stderr, as for any bad flag).
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--set", "ZEROPS_StableNumber=9"}, `"ZEROPS_StableNumber"`},
		{[]string{"--set", "hostname=x"}, `"hostname"`},
		{[]string{"--set", "serviceId=x"}, `"serviceId"`},
		{[]string{"--set", "projectId=x"}, `"projectId"`},
		{[]string{"--set", "PATH=/tmp"}, `"PATH"`},
		{[]string{"--set", "HOME=/tmp"}, `"HOME"`},
		{[]string{"--set", "1HQ=x"}, `"1HQ"`},
		{[]string{"--set", "HQ-HOST=x"}, `"HQ-HOST"`},
		{[]string{"--set", "=x"}, `""`},
		{[]string{"--set", "API_HOST=fine", "--set", "PATH=/tmp"}, `"PATH"`},
		{[]string{"--set", "HQ_HOST"}, `"HQ_HOST" is not NAME=VALUE`},
		{[]string{"--set", "A=1", "--set", "A=2"}, "A is set twice"},
		{nil, "env needs --set NAME=VALUE or --clear"},
	} {
		code, out, errOut := ctlOutputs(t, state, append([]string{"env", "--json"}, tc.args...)...)
		said := errOut
		if out != "" {
			var got struct {
				Code  int    `json:"code"`
				Error string `json:"error"`
			}
			if err := json.Unmarshal([]byte(out), &got); err != nil || got.Code != devsup.CodeFailed {
				t.Errorf("env %q: out %q is not a JSON failure", tc.args, out)
			}
			said = got.Error
		}
		if code != devsup.CodeFailed || !strings.Contains(said, tc.want) {
			t.Errorf("env %q: exit %d out %q stderr %q, want exit 1 naming %s", tc.args, code, out, errOut, tc.want)
		}
	}
	if code, st, out := ctlState(t, state, "status"); code != 0 || !reflect.DeepEqual(st.Env, kept) {
		t.Fatalf("a refused env changed the overlay: exit %d out %s", code, out)
	}
	// --set and --clear belong to env only.
	if code, _ := ctl(t, state, "status", "--clear"); code != devsup.CodeFailed {
		t.Fatalf("status --clear: exit %d", code)
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
