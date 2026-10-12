// Tests for: tools/workflow_mac_pair.go — zerops_workflow action="mac-pair",
// zcp's step-by-step guidance for pairing the user's Mac with this Mate, read
// from the Mate's own `mac status --json` (a stub on PATH here).
package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/runtime"
)

// The commands come from the Mate and are repeated verbatim: these are
// deliberately not what a real Mate prints, so a hardcoded command shows.
const (
	macFixtureVPN    = "zcli vpn up fixture-project-7Q"
	macFixturePair   = "ssh fixture-zcp mac host | sh"
	macFixtureRemove = "ssh fixture-zcp mac host --remove | sh"
)

func macFixture(stage, mac string, missing string) string {
	return `{"stage":"` + stage + `",` +
		`"pairCommands":["` + macFixtureVPN + `","` + macFixturePair + `"],` +
		`"removeCommand":"` + macFixtureRemove + `",` +
		`"mac":` + mac + `,"missing":` + missing + `}`
}

const macFixtureHost = `{"name":"Ales's Mac mini","user":"ales","address":"10.12.0.4","port":22193,` +
	`"macos":"26.5","agentDevice":"0.21.24","node":"/usr/local/bin/node"}`

const macFixtureHostNoNode = `{"name":"Ales's Mac mini","user":"ales","address":"10.12.0.4","port":22193,` +
	`"macos":null,"agentDevice":null,"node":null}`

// stubMac puts an executable `mac` on PATH that prints stdout, writes stderr
// and exits with code when called as `mac status --json`. It touches ran so a
// test can tell whether zcp called it at all.
func stubMac(t *testing.T, stdout, stderr string, code int, sleep bool) (ran string) {
	t.Helper()
	dir := t.TempDir()
	ran = filepath.Join(dir, "ran")
	out := filepath.Join(dir, "stdout")
	errOut := filepath.Join(dir, "stderr")
	if err := os.WriteFile(out, []byte(stdout), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(errOut, []byte(stderr), 0o600); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"touch '" + ran + "'\n" +
		`[ "$*" = "status --json" ] || { echo "unexpected arguments: $*" >&2; exit 64; }` + "\n"
	if sleep {
		script += "exec sleep 10\n"
	}
	script += "cat '" + out + "'\ncat '" + errOut + "' >&2\nexit " + strconv.Itoa(code) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "mac"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":/usr/bin:/bin")
	return ran
}

type macPairCase struct {
	name    string
	stdout  string
	stderr  string
	code    int
	sleep   bool
	noMac   bool
	rt      runtime.Info
	timeout time.Duration
	isError bool
	stage   string
	want    []string
	notWant []string
}

// TestMacPair_GuidesEachStage_FromMateStatus: every call re-reads the Mate's
// `mac status --json` and says the one next step for the stage it reports,
// repeating the Mate's own commands; anything zcp cannot read is reported as
// `mac` failing, never guessed into a stage.
//
// non-parallel: each case points PATH at its own stub (t.Setenv).
func TestMacPair_GuidesEachStage_FromMateStatus(t *testing.T) {
	mate := runtime.Info{InContainer: true, MateEnabled: true}
	again := `zerops_workflow action="mac-pair"`
	cases := []macPairCase{
		{
			name:   "not paired: run the Mate's commands in order, with the prerequisites",
			stdout: macFixture("not-paired", "null", "[]"),
			rt:     mate, stage: "not-paired",
			want: []string{macFixtureVPN, macFixturePair, "zcli login", "Node.js", "password", "node", again},
		},
		{
			name:   "unreachable: wake, sign in, connect the VPN, else pair again",
			stdout: macFixture("unreachable", macFixtureHost, "[]"),
			rt:     mate, stage: "unreachable",
			want: []string{"Ales's Mac mini", "awake", "logged in", macFixtureVPN, macFixturePair, again},
		},
		{
			name:   "needs permissions: the missing ones, where to turn them on, the node path",
			stdout: macFixture("needs-permissions", macFixtureHost, `["Screen Recording"]`),
			rt:     mate, stage: "needs-permissions",
			want:    []string{"Screen Recording", "Privacy & Security", "/usr/local/bin/node", "Cmd-Shift-G", macFixturePair, again},
			notWant: []string{"Accessibility"},
		},
		{
			name:   "needs permissions without a known node path: the user finds it",
			stdout: macFixture("needs-permissions", macFixtureHostNoNode, `["Accessibility","Screen Recording"]`),
			rt:     mate, stage: "needs-permissions",
			want: []string{"Accessibility", "Screen Recording", "which node", again},
		},
		{
			name:   "ready: name, macOS, how to work there and how to unpair; nothing to call again",
			stdout: macFixture("ready", macFixtureHost, "[]"),
			rt:     mate, stage: "ready",
			want:    []string{"Ales's Mac mini", "26.5", "mac sync", "mac run", "mac device", "mac screenshot", "mac help", "mac unpair", macFixtureRemove},
			notWant: []string{again},
		},
		{
			name:   "mac fails: its exit and stderr, no stage",
			stdout: "", stderr: "mac: state file unreadable", code: 3,
			rt: mate, isError: true,
			want: []string{"mac status", "mac: state file unreadable"},
		},
		{
			name:   "unparsable output is mac failing, not a stage",
			stdout: "Paired with Ales's Mac mini\n",
			rt:     mate, isError: true,
			want: []string{"mac status"},
		},
		{
			name:   "an unknown stage is mac failing, not a guess",
			stdout: macFixture("sleeping", macFixtureHost, "[]"),
			rt:     mate, isError: true,
			want: []string{"sleeping"},
		},
		{
			name:  "no mac on PATH: this Mate predates Mac pairing",
			noMac: true,
			rt:    mate, isError: true,
			want: []string{"Mate update"},
		},
		{
			name:   "outside a Mate: refused, mac never runs",
			stdout: macFixture("ready", macFixtureHost, "[]"),
			rt:     runtime.Info{InContainer: true}, isError: true,
			want: []string{"Mate"},
		},
		{
			name:  "mac hangs: bounded, reported as not answering",
			sleep: true,
			rt:    mate, timeout: 300 * time.Millisecond, isError: true,
			want: []string{"did not answer"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var ran string
			if tc.noMac {
				t.Setenv("PATH", t.TempDir())
			} else {
				ran = stubMac(t, tc.stdout, tc.stderr, tc.code, tc.sleep)
			}
			timeout := tc.timeout
			if timeout == 0 {
				timeout = 10 * time.Second
			}
			start := time.Now()
			res, typed, err := macPair(context.Background(), tc.rt, timeout)
			if err != nil || typed != nil {
				t.Fatalf("want (result, nil, nil), got typed=%v err=%v", typed, err)
			}
			if tc.sleep && time.Since(start) > 5*time.Second {
				t.Errorf("a hanging mac held the call for %s", time.Since(start))
			}
			text := getTextContent(t, res)
			if res.IsError != tc.isError {
				t.Fatalf("isError=%v want %v:\n%s", res.IsError, tc.isError, text)
			}
			// Search the decoded words, not their JSON escapes.
			var decoded struct {
				Message string `json:"message"`
				Error   string `json:"error"`
			}
			if err := json.Unmarshal([]byte(text), &decoded); err == nil {
				text += "\n" + decoded.Message + "\n" + decoded.Error
			}
			for _, w := range tc.want {
				if !strings.Contains(text, w) {
					t.Errorf("missing %q in:\n%s", w, text)
				}
			}
			for _, w := range tc.notWant {
				if strings.Contains(text, w) {
					t.Errorf("unexpected %q in:\n%s", w, text)
				}
			}
			if !tc.rt.MateEnabled && ran != "" {
				if _, statErr := os.Stat(ran); statErr == nil {
					t.Error("mac ran outside a Mate")
				}
			}
			if tc.stage == "" {
				return
			}
			var got struct {
				Stage         string   `json:"stage"`
				PairCommands  []string `json:"pairCommands"`
				RemoveCommand string   `json:"removeCommand"`
				Message       string   `json:"message"`
			}
			if err := json.Unmarshal([]byte(getTextContent(t, res)), &got); err != nil {
				t.Fatalf("result is not JSON: %v\n%s", err, text)
			}
			if got.Stage != tc.stage || got.Message == "" {
				t.Errorf("stage=%q message=%q, want stage %q and a message", got.Stage, got.Message, tc.stage)
			}
			if len(got.PairCommands) != 2 || got.RemoveCommand != macFixtureRemove {
				t.Errorf("the Mate's commands are not carried: %+v", got)
			}
		})
	}
}
