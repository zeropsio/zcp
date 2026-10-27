// Tests for: commandFlags (cmd/zcp/cli_args.go) through the subcommands that
// act on the container — a help request or a mistyped flag answers before
// the command touches anything.
//
// NOT parallel — every case redirects HOME, the working directory and stdout.
package main

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/telemetry"
)

func TestSubcommands_HelpOrUnknownFlag_ChangesNothing(t *testing.T) {
	for _, tc := range []struct {
		name     string
		run      func() int
		wantCode int
		wantHelp string
	}{
		{"init --help", func() int { return runInitCmd([]string{"--help"}) }, 0, "usage: zcp init"},
		{"init --guided -h", func() int { return runInitCmd([]string{"--guided", "-h"}) }, 0, "usage: zcp init"},
		{"init --bogus", func() int { return runInitCmd([]string{"--bogus"}) }, 2, ""},
		{"init nginx --help", func() int { return runInitCmd([]string{"nginx", "--help"}) }, 0, "usage: zcp init nginx"},
		{"init sshfs --bogus", func() int { return runInitCmd([]string{"sshfs", "--bogus"}) }, 2, ""},
		{"update --help", func() int { return runUpdate([]string{"--help"}) }, 0, "usage: zcp update"},
		{"update --bogus", func() int { return runUpdate([]string{"--bogus"}) }, 2, ""},
		{"service --help", func() int { return runServiceCmd([]string{"--help"}) }, 0, "usage: zcp service"},
		{"service start nginx --help", func() int { return runServiceCmd([]string{"start", "nginx", "--help"}) }, 0, "usage: zcp service"},
		{"service start nginx --bogus", func() int { return runServiceCmd([]string{"start", "nginx", "--bogus"}) }, 1, ""},
		{"mate --help", func() int { return runMateCmd([]string{"--help"}) }, 0, "usage: zcp mate"},
		{"mate status --help", func() int { return runMateCmd([]string{"status", "--help"}) }, 0, "usage: zcp mate status"},
		{"mate status --bogus", func() int { return runMateCmd([]string{"status", "--bogus"}) }, 2, ""},
		{"hq --help", func() int { return runHQCmd([]string{"--help"}) }, 0, "usage: zcp hq"},
		{"hq status --help", func() int { return runHQCmd([]string{"status", "--help"}) }, 0, "usage: zcp hq"},
		{"hq status --bogus", func() int { return runHQCmd([]string{"status", "--bogus"}) }, 2, ""},
		{"telemetry --help", func() int { return runTelemetryCmd([]string{"--help"}, telemetry.Config{}) }, 0, "Usage: zcp telemetry"},
		{"telemetry enable --help", func() int { return runTelemetryCmd([]string{"enable", "--help"}, telemetry.Config{}) }, 0, "Usage: zcp telemetry"},
		{"telemetry enable --bogus", func() int { return runTelemetryCmd([]string{"enable", "--bogus"}, telemetry.Config{}) }, 2, ""},
		{"schema --help", func() int { return runSchema([]string{"--help"}) }, 0, "Usage: zcp schema"},
		{"schema sync --help", func() int { return runSchema([]string{"sync", "--help"}) }, 0, "Usage: zcp schema sync"},
		{"schema check --bogus", func() int { return runSchema([]string{"check", "--bogus"}) }, 2, ""},
		{"catalog sync --help", func() int { return runCatalog([]string{"sync", "--help"}) }, 0, "Usage: zcp catalog sync"},
		{"catalog sync --bogus", func() int { return runCatalog([]string{"sync", "--bogus"}) }, 2, ""},
		{"agent mark-oauth --help", func() int { runAgent([]string{"mark-oauth", "--help"}); return 0 }, 0, "Usage: zcp agent"},
		{"self --help", selfCmd("--help"), 0, "usage: zcp self"},
		{"self restart --help", selfCmd("restart", "--help"), 0, "usage: zcp self restart"},
		{"self restart --bogus", selfCmd("restart", "--bogus"), 2, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, work := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Chdir(work)
			stdout, _ := captureOutput(t, func() {
				if code := tc.run(); code != tc.wantCode {
					t.Errorf("exit = %d, want %d", code, tc.wantCode)
				}
			})
			if tc.wantHelp != "" && !strings.Contains(stdout, tc.wantHelp) {
				t.Errorf("stdout = %q, want usage %q", stdout, tc.wantHelp)
			}
			if tc.wantHelp == "" && stdout != "" {
				t.Errorf("stdout = %q, want nothing", stdout)
			}
			for _, dir := range []string{home, work} {
				if entries, _ := os.ReadDir(dir); len(entries) != 0 {
					t.Errorf("%s gained %v", dir, entries)
				}
			}
		})
	}
}

// selfCmd runs `zcp self <args>` in a container whose platform client a
// help request or a mistyped flag never builds: building it fails the call.
func selfCmd(args ...string) func() int {
	noClient := func(string, string) (serviceRestarter, error) {
		return nil, errors.New("a help request or a mistyped flag built the platform client")
	}
	return func() int { return runSelfCmd(args, os.Stdout, lookupIn(selfLiveEnv()), noClient) }
}
