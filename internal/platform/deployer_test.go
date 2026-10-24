// Tests for: platform/deployer.go — SystemSSHDeployer exec wrappers.
package platform

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSSHArgs_HostKeyOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		hostname string
		command  string
		want     []string
	}{
		{
			name:     "includes StrictHostKeyChecking and UserKnownHostsFile",
			hostname: "appstage",
			command:  "cd /var/www && zcli push",
			want: []string{
				"-o", "StrictHostKeyChecking=no",
				"-o", "UserKnownHostsFile=/dev/null",
				// LogLevel=ERROR suppresses the ssh client's stderr chatter
				// ("Warning: Permanently added ... to the list of known
				// hosts.", printed on EVERY connection because the known-
				// hosts file is /dev/null). ExecSSH returns CombinedOutput,
				// so without this the warning corrupts every parsed
				// single-value read — live-verified: the launch gate's
				// `git remote get-url origin` value carried the warning and
				// false-blocked remote-mismatch on an exactly-matching URL.
				"-o", "LogLevel=ERROR",
				"-o", "ServerAliveInterval=15",
				"-o", "ServerAliveCountMax=3",
				"appstage", "cd /var/www && zcli push",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := sshArgs(tt.hostname, tt.command)
			if len(got) != len(tt.want) {
				t.Fatalf("sshArgs() = %v (len %d), want %v (len %d)", got, len(got), tt.want, len(tt.want))
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("sshArgs()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestSSHArgsBg verifies the background-spawn variant includes every
// flag required for fire-and-forget detachment. The bug class this
// prevents — v17 `zerops_dev_server start` hung exactly 300s on a
// well-formed `nohup ... & disown` pattern — is load-bearing enough
// to check every flag explicitly instead of just counting entries.
func TestSSHArgsBg(t *testing.T) {
	t.Parallel()

	got := sshArgsBg("apidev", "echo spawned")

	// The command positional must still be the last argument. Anything
	// after it is interpreted as ssh's remote argv, which breaks quoting.
	if got[len(got)-1] != "echo spawned" {
		t.Errorf("last arg = %q, want the command string", got[len(got)-1])
	}
	if got[len(got)-2] != "apidev" {
		t.Errorf("penultimate arg = %q, want hostname", got[len(got)-2])
	}

	joined := strings.Join(got, " ")
	mustContain := []string{
		// -T disables pty allocation so no controlling tty is created on the
		// remote side — without this, backgrounded processes can end up
		// tied to a phantom pty that delays channel close.
		"-T",
		// -n redirects the ssh client's stdin from /dev/null so the Go
		// parent process's stdin can't feed bytes into the ssh channel
		// (which would keep the channel open waiting for more input).
		"-n",
		// BatchMode=yes disables every prompt. Auth failures return fast
		// instead of hanging on a password prompt.
		"BatchMode=yes",
		// ConnectTimeout=5 bounds the TCP dial phase. An unreachable
		// container fails in seconds instead of the kernel default.
		"ConnectTimeout=5",
		// Keepalives tight enough to tear down a dead channel well under
		// our 10-second spawn budget.
		"ServerAliveInterval=5",
		"ServerAliveCountMax=2",
		"StrictHostKeyChecking=no",
		"UserKnownHostsFile=/dev/null",
		// Same client-noise suppression as the foreground args — combined
		// output must stay parseable.
		"LogLevel=ERROR",
	}
	for _, want := range mustContain {
		if !strings.Contains(joined, want) {
			t.Errorf("sshArgsBg output missing %q\nfull: %s", want, joined)
		}
	}
}

// TestExecSSH_StoppedByItsContext_SaysWhy: an ssh call its context stops —
// cancelled, or past its ceiling — says so in its error, so the stop is not
// taken for the remote command being killed.
//
// non-parallel: t.Setenv puts a fake ssh first on PATH.
func TestExecSSH_StoppedByItsContext_SaysWhy(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte("#!/bin/sh\nexec sleep 10\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := NewSystemSSHDeployer().ExecSSH(ctx, "builder", "true")
	var sshErr *SSHExecError
	if !errors.As(err, &sshErr) || !errors.Is(sshErr.Err, context.DeadlineExceeded) {
		t.Errorf("ExecSSH stopped by its deadline = %v, want an SSHExecError carrying context.DeadlineExceeded", err)
	}
}

// TestSystemSSHDeployer_StreamSSH_StdoutOnly: a streamed command's stdout —
// and only its stdout — reaches the writer byte for byte, binary and large as
// a git bundle is, while its stderr stays apart; a failed command's stderr
// rides its *SSHExecError. The ssh client runs without a tty (-T), which would
// otherwise rewrite the bytes it carries.
//
// non-parallel: t.Setenv puts a fake ssh first on PATH.
func TestSystemSSHDeployer_StreamSSH_StdoutOnly(t *testing.T) {
	bin := t.TempDir()
	argsFile := filepath.Join(bin, "args")
	// The fake ssh records its argv, then runs the remote command (its last
	// argument) in a local shell.
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > '" + argsFile + "'\nfor last; do :; done\nexec sh -c \"$last\"\n"
	if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	d := NewSystemSSHDeployer()

	t.Run("stdout streamed, stderr apart", func(t *testing.T) {
		// 4 MiB of every byte value — NUL and CR LF among them, which a tty
		// would rewrite — then progress chatter on stderr.
		payload := make([]byte, 4<<20)
		for i := range payload {
			payload[i] = byte(i*7 + i/256)
		}
		payloadFile := filepath.Join(t.TempDir(), "payload")
		if err := os.WriteFile(payloadFile, payload, 0o600); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err := d.StreamSSH(context.Background(), "appdev", "cat '"+payloadFile+"'; echo 'progress chatter' >&2", &out); err != nil {
			t.Fatalf("StreamSSH: %v", err)
		}
		if !bytes.Equal(out.Bytes(), payload) {
			t.Fatalf("stdout carried %d bytes, want the %d bytes written, byte for byte, and no stderr", out.Len(), len(payload))
		}
		args, err := os.ReadFile(argsFile)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimRight(string(args), "\n"), "\n")
		if lines[0] != "-T" {
			t.Errorf("ssh argv = %q, want -T first: no tty may rewrite the stream", lines)
		}
		for _, want := range []string{"StrictHostKeyChecking=no", "UserKnownHostsFile=/dev/null", "LogLevel=ERROR", "appdev"} {
			if !strings.Contains(string(args), want) {
				t.Errorf("ssh argv = %q, want %q", lines, want)
			}
		}
	})

	t.Run("failure keeps stderr in the error", func(t *testing.T) {
		var out strings.Builder
		err := d.StreamSSH(context.Background(), "appdev", `printf 'partial'; echo 'fatal: not a git repository' >&2; exit 128`, &out)
		var sshErr *SSHExecError
		if !errors.As(err, &sshErr) {
			t.Fatalf("StreamSSH = %v, want an SSHExecError", err)
		}
		if !strings.Contains(sshErr.Output, "fatal: not a git repository") || strings.Contains(sshErr.Output, "partial") {
			t.Errorf("error output = %q, want the stderr alone", sshErr.Output)
		}
		if sshErr.Hostname != "appdev" {
			t.Errorf("error hostname = %q", sshErr.Hostname)
		}
	})

	t.Run("stopped by its context says so", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		var out strings.Builder
		err := d.StreamSSH(ctx, "appdev", "sleep 10", &out)
		var sshErr *SSHExecError
		if !errors.As(err, &sshErr) || !errors.Is(sshErr.Err, context.DeadlineExceeded) {
			t.Errorf("StreamSSH stopped by its deadline = %v, want an SSHExecError carrying context.DeadlineExceeded", err)
		}
	})
}
