package ops

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/devsup"
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
// repository's prepare command, then the supervisor's start or reload. It
// fills v's prepare report and notes; nothing in it fails the acquire.
func afterLeaseCheckout(ctx context.Context, ssh SSHDeployer, alias string, v *LeaseView) {
	prep, note := prepareLease(ctx, ssh, alias)
	v.Prepare = prep
	v.Note = joinNotes(v.Note, note, leaseSupervisorNote(ensureLeaseSupervisorAfterCheckout(ctx, ssh, alias)))
}

// leaseDeclarationCommand reads the checked-out declaration, closed by a
// devsup section: 0 with the file above it, 1 when there is none.
func leaseDeclarationCommand(workDir string) string {
	f := shellQuote(devDeclarationFile(workDir))
	return `if [ -f ` + f + ` ]; then cat ` + f + `; echo; echo '@@zcp-devsup declaration 0'; else echo '@@zcp-devsup declaration 1'; fi`
}

// leasePrepareCommand runs the repository's prepare command in workDir, with
// the ssh session's env and no stdin (a prompt fails instead of waiting).
// Both are shell-quoted.
func leasePrepareCommand(workDir, prepare string) string {
	return "cd " + shellQuote(workDir) + " && /bin/sh -c " + shellQuote(prepare) + " </dev/null"
}

// prepareLease reads the checked-out declaration and runs its prepare
// command, if it declares one. note says what went wrong, "" when nothing did.
func prepareLease(ctx context.Context, ssh SSHDeployer, alias string) (*LeasePrepare, string) {
	readCtx, cancel := context.WithTimeout(ctx, containerIdentityTimeout)
	out, err := ssh.ExecSSH(readCtx, alias, leaseDeclarationCommand(leaseWorkDir))
	cancel()
	if err != nil {
		return nil, fmt.Sprintf("zcp could not read the checked-out %s, so no prepare command ran (%s)", devsup.DeclarationPath, sshFailureText(err))
	}
	sections, err := splitDevsupSections(string(out))
	decl, ok := sections["declaration"]
	if err != nil || !ok {
		return nil, fmt.Sprintf("zcp could not read the checked-out %s, so no prepare command ran (unreadable answer %q)", devsup.DeclarationPath, boundedText(string(out)))
	}
	if decl.code != 0 {
		return nil, ""
	}
	d, err := devsup.ParseDeclaration([]byte(decl.body))
	if err != nil {
		return nil, fmt.Sprintf("the checked-out %s cannot be read, so its prepare command did not run: %v", devsup.DeclarationPath, err)
	}
	if strings.TrimSpace(d.Prepare) == "" {
		return nil, ""
	}
	return runLeasePrepare(ctx, ssh, alias, d.Prepare)
}

// runLeasePrepare runs prepare in the lease's work dir and reports it.
func runLeasePrepare(ctx context.Context, ssh SSHDeployer, alias, prepare string) (*LeasePrepare, string) {
	runCtx, cancel := context.WithTimeout(ctx, leasePrepareTimeout)
	defer cancel()
	begun := time.Now()
	out, err := ssh.ExecSSH(runCtx, alias, leasePrepareCommand(leaseWorkDir, prepare))
	p := &LeasePrepare{Command: prepare, OK: err == nil, Millis: time.Since(begun).Milliseconds()}
	if err == nil {
		return p, ""
	}
	p.ExitCode = exitStatus(err)
	p.Output = boundedTail(lastLines(string(out), leasePrepareOutputLines))
	if p.Output == "" {
		p.Output = boundedText(sshFailureText(err))
	}
	rerun := shellQuote("cd " + leaseWorkDir + " && " + prepare)
	return p, fmt.Sprintf("the repository's prepare command failed (exit %d) — the branch is checked out and the lease is held, but its dev servers may miss what the command installs; run it again over SSH: ssh %s %s",
		p.ExitCode, alias, rerun) + supervisorOutputClause(p.Output)
}
