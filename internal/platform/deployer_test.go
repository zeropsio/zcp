// Tests for: platform/deployer.go — SystemSSHDeployer exec wrappers.
package platform

import (
	"strings"
	"testing"
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
	}
	for _, want := range mustContain {
		if !strings.Contains(joined, want) {
			t.Errorf("sshArgsBg output missing %q\nfull: %s", want, joined)
		}
	}
}
