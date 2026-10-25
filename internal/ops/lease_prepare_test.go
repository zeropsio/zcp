// Tests for: ops/lease_prepare.go, the acquire path of lease.go — a pool image
// bakes the part's dependencies into /var/www; a branch whose lockfile differs
// needs an incremental install. The repository declares it as `prepare` in
// .zcp/dev-servers.json; after a successful checkout zcp runs it in /var/www as
// a call of its own, then starts or reloads the supervisor. Its failure never
// hands the container back and is never a checkout failure; the acquire result
// reports it (docs/spec-workflows.md §8 DP-14).
package ops

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/platform"
)

// acquireContainer answers the commands an acquire runs in a leased
// container: the checkout, the declaration read (decl; "" for no file), the
// prepare (prepOut, failing with prepErr) and the supervisor's start or reload.
func acquireContainer(decl, prepOut string, prepErr error, supervisor string) func(host, command string) ([]byte, error) {
	return func(_, command string) ([]byte, error) {
		switch {
		case strings.Contains(command, "@@zcp-devsup declaration"):
			if decl == "" {
				return []byte("@@zcp-devsup declaration 1\n"), nil
			}
			return []byte(decl + "\n@@zcp-devsup declaration 0\n"), nil
		case strings.Contains(command, "/bin/sh -c "):
			return []byte(prepOut), prepErr
		case strings.Contains(command, "zcp-devsup run"):
			return []byte(supervisor), nil
		case strings.Contains(command, ctlCall):
			return []byte(ctlTook), nil
		}
		return nil, nil
	}
}

// ctlCall marks a supervisor call (`zcp-devsup ctl '<verb>' …`: an overlay,
// a reload); ctlTook is a supervisor's answer taking it.
const (
	ctlCall = "zcp-devsup ctl '"
	ctlTook = "{}\n@@zcp-devsup ctl 0\n"
)

// commandIndex is the index of the first command containing sub, -1 when none.
func commandIndex(cmds []string, sub string) int {
	for i, c := range cmds {
		if strings.Contains(c, sub) {
			return i
		}
	}
	return -1
}

func TestLeaseAcquire_RunsTheRepositorysPrepareAfterTheCheckout(t *testing.T) {
	t.Parallel()
	const started = "@@zcp-devsup check 2\n@@zcp-devsup started 0\n"
	const prep = `npm install --prefer-offline && echo 'it''s done'`
	withPrepare := `{"version":1,"prepare":` + jsonString(t, prep) + `,"servers":[{"name":"web","port":3000,"command":"npm run dev"}]}`
	tests := []struct {
		name        string
		decl        string
		prepOut     string
		prepErr     error
		wantPrepare bool // a prepare command ran
		wantOK      bool
		wantNote    []string
		noteAbsent  bool
	}{
		{name: "no declaration: nothing to prepare", noteAbsent: true},
		{name: "a declaration without prepare", decl: `{"version":1,"servers":[]}`, noteAbsent: true},
		{name: "prepare succeeds", decl: withPrepare, prepOut: "added 3 packages in 1s\n", wantPrepare: true, wantOK: true, noteAbsent: true},
		{
			name: "prepare fails: the lease stays, the result says so", decl: withPrepare,
			prepOut: "npm ERR! code ERESOLVE\n", prepErr: sshFailure("npm ERR! code ERESOLVE\n", exitError(t, 1)),
			wantPrepare: true, wantNote: []string{"prepare", "exit 1", "ERESOLVE", "devpool-3"},
		},
		{
			name: "an unreadable declaration: nothing runs, the result says why", decl: `{"version":1,"servers":[`,
			wantNote: []string{devsup.DeclarationPath, "prepare"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fx := newLeaseFixture(t)
			fx.swarm.acquired = acquiredAs(3)
			ssh := &leaseSSH{}
			ssh.setHandler(acquireContainer(tt.decl, tt.prepOut, tt.prepErr, started))
			fx.deps.SSH = ssh

			view, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, testRepo, "feature")
			if err != nil {
				t.Fatalf("acquire: %v (a prepare never fails the acquire)", err)
			}
			if view.State != LeaseStateHeld || len(fx.swarm.releases) != 0 || len(fx.swarm.deletes) != 0 {
				t.Errorf("view %+v, releases %+v, deletes %+v; want the lease held, never handed back", view, fx.swarm.releases, fx.swarm.deletes)
			}
			cmds := ssh.commands()
			checkout, read := commandIndex(cmds, "fetch -q --prune origin"), commandIndex(cmds, "@@zcp-devsup declaration")
			prepare, supervisor := commandIndex(cmds, "/bin/sh -c "), commandIndex(cmds, "zcp-devsup run")
			if checkout < 0 || read < checkout || supervisor < read {
				t.Fatalf("commands = %v, want the checkout, the declaration read, then the supervisor", cmds)
			}
			if !tt.wantPrepare {
				if prepare >= 0 || view.Prepare != nil {
					t.Errorf("a prepare ran (%v, %+v) though none is declared", cmds, view.Prepare)
				}
			} else {
				if prepare < read || prepare > supervisor {
					t.Fatalf("commands = %v, want the prepare after the read and before the supervisor", cmds)
				}
				if want := "cd " + shellQuote(leaseWorkDir) + " && /bin/sh -c " + shellQuote(prep) + " </dev/null"; cmds[prepare] != want {
					t.Errorf("prepare command = %q, want %q", cmds[prepare], want)
				}
				if strings.Contains(cmds[prepare], "fetch") || strings.Contains(cmds[checkout], prep) {
					t.Errorf("the prepare must be a call of its own, apart from the checkout: %v", cmds)
				}
				if view.Prepare == nil || view.Prepare.Command != prep || view.Prepare.OK != tt.wantOK {
					t.Fatalf("prepare = %+v, want %q ok=%v", view.Prepare, prep, tt.wantOK)
				}
				if !tt.wantOK && (view.Prepare.ExitCode != 1 || !strings.Contains(view.Prepare.Output, "ERESOLVE")) {
					t.Errorf("a failed prepare reports %+v, want exit 1 and its output", view.Prepare)
				}
			}
			if tt.noteAbsent && view.Note != "" {
				t.Errorf("note = %q, want none", view.Note)
			}
			for _, want := range tt.wantNote {
				if !strings.Contains(view.Note, want) {
					t.Errorf("note %q lacks %q", view.Note, want)
				}
			}
			if _, err := loadLeaseRecord(fx.stateDir, "devpool-3"); err != nil {
				t.Errorf("the lease record is gone: %v", err)
			}
		})
	}
}

func TestLeasePrepare_FailureIsNeverACheckoutFailure(t *testing.T) {
	t.Parallel()
	// git's own words in a prepare's output (a dependency fetched from a
	// private repository) must not read as the pool's GIT_TOKEN failing.
	out := "fatal: Authentication failed for 'https://github.com/acme/private-dep/'\n"
	fx := newLeaseFixture(t)
	fx.swarm.acquired = acquiredAs(3)
	ssh := &leaseSSH{}
	ssh.setHandler(acquireContainer(`{"version":1,"prepare":"npm ci","servers":[]}`, out, sshFailure(out, exitError(t, 128)), "@@zcp-devsup check 0\n"))
	fx.deps.SSH = ssh

	view, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, testRepo, "feature")
	var pe *platform.PlatformError
	if errors.As(err, &pe) {
		t.Fatalf("err = %v (code %s), want the acquire to succeed", err, pe.Code)
	}
	if err != nil {
		t.Fatal(err)
	}
	if view.Prepare == nil || view.Prepare.OK || view.Prepare.ExitCode != 128 {
		t.Errorf("prepare = %+v, want a failure with exit 128", view.Prepare)
	}
	for _, banned := range []string{platform.ErrGitTokenInvalid, platform.ErrGitTokenMissing} {
		if strings.Contains(view.Note, banned) {
			t.Errorf("note %q classifies the prepare as a checkout credential failure", view.Note)
		}
	}
}

func TestLeaseAcquire_ReloadsASupervisorThatAlreadyRuns(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		reload   string // the supervisor's answer to the reload
		wantNote []string
	}{
		{name: "the reload takes the checked-out declaration", reload: ctlTook},
		{
			name:     "the reload is refused",
			reload:   "{\"code\":4,\"error\":\"web: cwd escapes the repo root\"}\n@@zcp-devsup ctl 4\n",
			wantNote: []string{"cwd escapes the repo root", devsup.DeclarationPath},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fx := newLeaseFixture(t)
			fx.swarm.acquired = acquiredAs(3)
			ssh := &leaseSSH{}
			acquire := acquireContainer("", "", nil, "@@zcp-devsup check 0\n")
			ssh.setHandler(func(host, command string) ([]byte, error) {
				if strings.Contains(command, "zcp-devsup ctl 'reload'") {
					return []byte(tt.reload), nil
				}
				return acquire(host, command)
			})
			fx.deps.SSH = ssh

			view, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, testRepo, "feature")
			if err != nil {
				t.Fatal(err)
			}
			cmds := ssh.commands()
			ensure, reload := commandIndex(cmds, "zcp-devsup run"), commandIndex(cmds, "zcp-devsup ctl 'reload'")
			if ensure < 0 || reload < ensure {
				t.Errorf("the supervisor step does not reload a supervisor that already runs, after ensuring it: %v", cmds)
			}
			if len(tt.wantNote) == 0 && view.Note != "" {
				t.Errorf("note = %q, want none", view.Note)
			}
			for _, want := range tt.wantNote {
				if !strings.Contains(view.Note, want) {
					t.Errorf("note %q lacks %q", view.Note, want)
				}
			}
		})
	}
}

func TestDeclareDevServer_KeepsTheRepositorysPrepare(t *testing.T) {
	t.Parallel()
	d := devsup.Declaration{Version: 1, PreviewPortBase: 9000, Prepare: "npm install --prefer-offline"}
	got, changed := DeclareDevServer(d, devsup.Server{Name: "web", Port: 3000, Command: "npm run dev"})
	if !changed || got.Prepare != d.Prepare || got.PreviewPortBase != 9000 {
		t.Errorf("declared = %+v (changed %v), want the prepare and preview base kept", got, changed)
	}
}

// jsonString is s as a JSON string literal.
func jsonString(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
