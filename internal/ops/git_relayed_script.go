package ops

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/platform"
)

// RelayedSSH runs zcp's own scripts in a Mate's dev service or pool
// container as relayed sessions (docs/spec-mate.md §6.8): every git the
// script starts — a lazy fetch of a partial clone included — asks zcp for
// its credential over the session, and zcp answers HQ's own origin with the
// Mate's project key (hq.AnswerGitFor) from Relay. No credential is stored
// in the container, put in argv or in the script: the container's git holds
// it in memory for the request only. Its ExecSSH returns what an
// SSHDeployer's does — the script's stdout and stderr, an
// *platform.SSHExecError on a non-zero exit — so a caller reads its output
// as it always did.
type RelayedSSH struct {
	SSH   SSHStdioRunner
	Relay BrokeredGitRelay
}

// relayedScriptCeiling bounds one relayed script, as the SSH deployer bounds
// every command it runs: git against HQ ends itself sooner (HQGitBound).
const relayedScriptCeiling = 5 * time.Minute

// ExecSSH runs command, a /bin/sh script, in hostname as a relayed session.
func (r RelayedSSH) ExecSSH(ctx context.Context, hostname, command string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, relayedScriptCeiling)
	defer cancel()
	nonce, err := relayNonce()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBrokeredGitSetup, err)
	}
	var (
		mu  sync.Mutex
		out bytes.Buffer
	)
	combined := lockedWriter{mu: &mu, w: &out}
	// A script names no git argv to decide GitHub access from: every token
	// it is given reads.
	call := newRelayCall(r.Relay, hostname, func(context.Context) string { return hq.GitHubAccessRead }, combined)
	exit, err := runRelayedSession(ctx, r.SSH, hostname, buildRelayedScriptCommand(command, nonce, brokeredGitSchemes(r.Relay.Identity.Origin)), nonce, call, nil, combined)
	mu.Lock()
	output := out.Bytes()
	mu.Unlock()
	if err != nil {
		return output, &platform.SSHExecError{Hostname: hostname, Output: string(output), Err: err}
	}
	if exit != 0 {
		return output, &platform.SSHExecError{Hostname: hostname, Output: string(output), Err: fmt.Errorf("exit status %d", exit)}
	}
	return output, nil
}

// buildRelayedScriptCommand is a relayed session running script: the relay
// prologue, then git's configuration for every git the script starts, from
// the environment (git 2.31 or later) — the empty helper clearing every
// persisted one, then zcp's own helper (relayHelper), and auto-gc and
// auto-maintenance in the foreground — under GIT_ALLOW_PROTOCOL (schemes,
// HQ's), then script itself in a subshell reading /dev/null, and the relay
// epilogue with its exit code. A -c a script's git takes comes after this
// configuration, so a script against HQ names no helper of its own.
func buildRelayedScriptCommand(script, nonce string, schemes []string) string {
	config := [][2]string{
		{"credential.helper", ""},
		{"credential.helper", relayHelper(nonce)},
		{"credential.useHttpPath", "true"},
		{"gc.autoDetach", "false"},
		{"maintenance.autoDetach", "false"},
	}
	var b strings.Builder
	b.WriteString(relayPrologue(nonce))
	fmt.Fprintf(&b, "export GIT_CONFIG_COUNT=%d", len(config))
	for i, kv := range config {
		fmt.Fprintf(&b, " GIT_CONFIG_KEY_%d=%s GIT_CONFIG_VALUE_%d=%s", i, ShellQuote(kv[0]), i, ShellQuote(kv[1]))
	}
	b.WriteString(" GIT_ALLOW_PROTOCOL=" + ShellQuote(strings.Join(schemes, ":")) + " GIT_TERMINAL_PROMPT=0; ")
	b.WriteString("( " + script + "\n) < /dev/null")
	b.WriteString(relayEpilogue(nonce))
	return "/bin/sh -c " + ShellQuote(b.String())
}
