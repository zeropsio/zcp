package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/topology"
)

// A dev pool's image bakes the part's dependencies into /var/www through
// run.prepareCommands: they survive a reset, and the checkout's `git clean
// -fd` keeps them (they are ignored). A branch whose lockfile differs needs an
// incremental install on top, which only the repository knows how to run, so
// it declares it as `prepare` in .zcp/dev-servers.json (docs/spec-mate.md
// §2.8a.1). After a successful checkout zcp runs it in /var/www as a call of
// its own, then starts or reloads the supervisor, which starts the declared
// servers. Its failure never hands the container back and is never a checkout
// failure: the lease holds the branch either way, and the acquire result
// reports it (docs/spec-workflows.md §8 DP-14).

// leasePrepareTimeout bounds the repository's prepare command; an incremental
// install takes seconds, a full one on a cold cache tens of seconds.
const leasePrepareTimeout = 5 * time.Minute

// leasePrepareOutputLines is how much of a failed prepare's output the result
// carries.
const leasePrepareOutputLines = 20

// LeasePrepare is what the repository's prepare command did after an
// acquire's checkout.
type LeasePrepare struct {
	Command string `json:"command"`
	OK      bool   `json:"ok"`
	// ExitCode is the failed command's exit status (-1 when it has none: it
	// did not finish in time, or ssh failed).
	ExitCode int   `json:"exitCode,omitempty"`
	Millis   int64 `json:"ms"`
	// Output is the tail of a failed command's output.
	Output string `json:"output,omitempty"`
}

// afterLeaseCheckout runs what follows a successful checkout: the
// repository's prepare command, then the supervisor's start when none
// answers, the servers' sibling overlay when the supervisor takes it
// (sendCheckoutOverlay, under the group's lock), and — for a supervisor that
// answered before the checkout, run.start's in a container the platform just
// created, which booted on an empty /var/www — a reload after that overlay,
// so the declared servers start under it. It fills v's prepare report, the
// overlay the supervisor took and the notes, and answers whether a supervisor
// answers on the lease now — one zcp found or started — whose dev servers the
// acquire then reads; nothing in it fails the acquire.
func afterLeaseCheckout(ctx context.Context, d LeaseDeps, alias string, v *LeaseView) bool {
	secrets := leaseSecrets(d, v.Pool)
	read, note := readLeaseCheckout(ctx, d.SSH, alias, "so no prepare command ran")
	if read.declaration != nil && strings.TrimSpace(read.declaration.Prepare) != "" {
		v.Prepare, note = runLeasePrepare(ctx, d.SSH, alias, read.declaration.Prepare, secrets)
	}
	r, err := ensureLeaseSupervisor(ctx, d.SSH, alias)
	r.log = scrubSecrets(r.log, secrets)
	supervised := err == nil && (r.outcome == leaseSupervisorRunning || r.outcome == leaseSupervisorStarted)
	var wireNote, refused string
	if supervised {
		env, sent, wn, envErr := sendCheckoutOverlay(ctx, d, alias, read)
		wireNote = wn
		switch {
		case envErr != nil:
			refused = overlayRefusedNote(alias, env, envErr)
		case sent && len(env.pools) > 0:
			v.SiblingEnv = &env
		}
		if r.outcome == leaseSupervisorRunning {
			if _, rerr := supervisorCtl(ctx, d.SSH, alias, "reload"); rerr != nil {
				r.reloadErr = rerr
			}
		}
	} else {
		_, _, wireNote = checkoutOverlay(d.StateDir, alias, read, d.previewURLs(ctx))
	}
	v.Note = joinNotes(v.Note, note, wireNote, scrubSecrets(leaseSupervisorNote(alias, r, err), secrets), refused)
	return supervised
}

// leaseSecrets are the secrets zcp holds that every process of a pool
// container reads in the pool's service env, and so a repository's prepare
// or a supervisor that prints its env prints: this Mate's HQ credential
// (PoolHQCredentialEnvKey) and the pool's orchestrator bearer as zcp's live
// env names it (liveBearer). The person's GIT_TOKEN zcp never holds, so it
// cannot take it out (review R4-6).
func leaseSecrets(d LeaseDeps, pool string) []string {
	var secrets []string
	if d.PoolCredential.Read != nil {
		if credential, ok := d.PoolCredential.Read(); ok && credential != "" {
			secrets = append(secrets, credential)
		}
	}
	if pool != "" {
		if bearer, _ := liveBearer(d, pool); bearer != "" {
			secrets = append(secrets, bearer)
		}
	}
	return secrets
}

// scrubSecrets is text with every one of secrets replaced by [redacted].
func scrubSecrets(text string, secrets []string) string {
	for _, secret := range secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "[redacted]")
		}
	}
	return text
}

// acquiredDevServers reads the dev servers of the lease behind rec once, as
// the acquire left them, and projects them as the envelope does
// (DevServerStates): each server's status verbatim — a server whose port does
// not answer yet is starting; nothing here waits for it — and the preview
// server's preview URL, as zerops_dev_server status reports it. A read that
// fails leaves them unread (nil): the acquire's notes already say what is
// wrong with the supervisor, and zerops_dev_server action=status reads it.
func acquiredDevServers(ctx context.Context, d LeaseDeps, rec leaseRecord) []topology.DevServerState {
	ds, err := ReadDevServers(ctx, d.SSH, rec.alias(), "")
	if err != nil {
		return nil
	}
	ds.Service = rec.PoolHostname
	states := DevServerStates(ctx, d.Client, d.ProjectID, &DevServerResult{Hostname: rec.alias(), Pool: rec.PoolHostname, Read: ds})
	// What this read found is the lease's last-known dev servers from now on,
	// for every envelope (NoteLeaseDevServers).
	noteLeaseDevServers(d.StateDir, rec, states, d.now())
	if len(states) == 0 {
		return nil
	}
	return states
}

// NoteLeaseDevServers records, in the record of the lease at alias, its dev
// servers as a call that read its supervisor at at found them (states, none
// when it runs none): every later envelope carries them as the lease's
// last-known ones, with when (docs/spec-mate.md §2.8a.3). Best-effort: a
// lease zcp holds no record of, or a record that cannot be read or written,
// keeps what it had.
func NoteLeaseDevServers(stateDir, alias string, states []topology.DevServerState, at time.Time) {
	rec, ok, err := leaseTargetRecord(stateDir, alias)
	if err != nil || !ok {
		return
	}
	noteLeaseDevServers(stateDir, rec, states, at)
}

// noteLeaseDevServers is NoteLeaseDevServers on rec, the record as the call
// knew it: a record its alias names another lease by now is left alone.
func noteLeaseDevServers(stateDir string, rec leaseRecord, states []topology.DevServerState, at time.Time) {
	if stateDir == "" {
		return
	}
	unlock, err := lockLeaseRecord(stateDir, rec.alias())
	if err != nil {
		return
	}
	defer unlock()
	if leaseRecordReplaced(stateDir, rec) {
		return
	}
	cur, err := loadLeaseRecord(stateDir, rec.alias())
	if err != nil {
		return
	}
	cur.DevServers, cur.DevServersReadAt = slices.Clone(states), at.UTC().Format(time.RFC3339)
	_ = writeLeaseRecordLocked(stateDir, cur)
}

// leaseCheckoutRead is what an acquire reads in the container after its
// checkout: the checked-out declaration — nil when there is none or zcp could
// not read it — and the version of the image's zcp-devsup, empty without one.
type leaseCheckoutRead struct {
	declaration *devsup.Declaration
	supervisor  SupervisorVersion
}

// leaseDeclarationCommand reads the image's zcp-devsup version and the
// checked-out declaration, each closed by a devsup section: version with
// `ctl version`'s exit (127 without the binary); declaration 0 with the file
// above it, 1 when there is none.
func leaseDeclarationCommand(workDir string) string {
	f := shellQuote(devDeclarationFile(workDir))
	return `if command -v zcp-devsup >/dev/null 2>&1; then zcp-devsup ctl version --json 2>/dev/null; c=$?; echo; echo "@@zcp-devsup version $c"; else echo '@@zcp-devsup version 127'; fi; ` +
		`if [ -f ` + f + ` ]; then cat ` + f + `; echo; echo '@@zcp-devsup declaration 0'; else echo '@@zcp-devsup declaration 1'; fi`
}

// leasePrepareCommand runs the repository's prepare command in workDir, with
// the ssh session's env and no stdin (a prompt fails instead of waiting).
// Both are shell-quoted.
func leasePrepareCommand(workDir, prepare string) string {
	return "cd " + shellQuote(workDir) + " && /bin/sh -c " + shellQuote(prepare) + " </dev/null"
}

// readLeaseCheckout reads the checked-out declaration and the supervisor's
// version. note says why the declaration could not be read, then what that
// means (unread: "so no prepare command ran"), "" when it could or there is
// none.
func readLeaseCheckout(ctx context.Context, ssh SSHDeployer, alias, unread string) (leaseCheckoutRead, string) {
	readCtx, cancel := context.WithTimeout(ctx, containerIdentityTimeout)
	out, err := ssh.ExecSSH(readCtx, alias, leaseDeclarationCommand(leaseWorkDir))
	cancel()
	if err != nil {
		return leaseCheckoutRead{}, fmt.Sprintf("zcp could not read the checked-out %s, %s (%s)", devsup.DeclarationPath, unread, sshFailureText(err))
	}
	sections, err := splitDevsupSections(string(out))
	decl, ok := sections["declaration"]
	if err != nil || !ok {
		return leaseCheckoutRead{}, fmt.Sprintf("zcp could not read the checked-out %s, %s (unreadable answer %q)", devsup.DeclarationPath, unread, boundedText(string(out)))
	}
	var read leaseCheckoutRead
	if version, ok := sections["version"]; ok && version.code == devsupCodeOK {
		_ = json.Unmarshal([]byte(version.body), &read.supervisor)
	}
	if decl.code != 0 {
		return read, ""
	}
	d, err := devsup.ParseDeclaration([]byte(decl.body))
	if err != nil {
		return read, fmt.Sprintf("the checked-out %s cannot be read, %s: %v", devsup.DeclarationPath, unread, err)
	}
	read.declaration = &d
	return read, ""
}

// runLeasePrepare runs prepare in the lease's work dir and reports it, a
// failed one's output with secrets taken out before its tail is bounded, so
// no fragment of one survives the cut.
func runLeasePrepare(ctx context.Context, ssh SSHDeployer, alias, prepare string, secrets []string) (*LeasePrepare, string) {
	runCtx, cancel := context.WithTimeout(ctx, leasePrepareTimeout)
	defer cancel()
	begun := time.Now()
	out, err := ssh.ExecSSH(runCtx, alias, leasePrepareCommand(leaseWorkDir, prepare))
	p := &LeasePrepare{Command: prepare, OK: err == nil, Millis: time.Since(begun).Milliseconds()}
	if err == nil {
		return p, ""
	}
	p.ExitCode = exitStatus(err)
	p.Output = boundedTail(lastLines(scrubSecrets(string(out), secrets), leasePrepareOutputLines))
	if p.Output == "" {
		p.Output = boundedText(scrubSecrets(sshFailureText(err), secrets))
	}
	rerun := shellQuote("cd " + leaseWorkDir + " && " + prepare)
	return p, fmt.Sprintf("the repository's prepare command failed (exit %d) — the branch is checked out and the lease is held, but its dev servers may miss what the command installs; run it again over SSH: ssh %s %s",
		p.ExitCode, alias, rerun) + supervisorOutputClause(p.Output)
}
