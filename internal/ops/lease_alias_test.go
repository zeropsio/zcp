// Tests for: ops/lease_alias.go — the ssh alias file of a dev-pool lease.
package ops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func aliasRecord(pool string, number int64, hostname string) leaseRecord {
	return leaseRecord{PoolHostname: pool, PoolServiceID: "svc-pool", ContainerID: "c-" + pool, ContainerNumber: number, ContainerHostname: hostname, Lease: testLease}
}

// stanzaFor is the expected ssh config of one lease, written out by hand: the
// options are the ones zcp's own ssh config template carries (User zerops,
// no host-key prompts, a multiplexed control socket), keyed by local uid so
// root's sshfs and the zerops user never share a socket.
func stanzaFor(alias, hostname string) string {
	return "Host " + alias + "\n" +
		"    HostName " + hostname + "\n" +
		"    User zerops\n" +
		"    StrictHostKeyChecking no\n" +
		"    UserKnownHostsFile /dev/null\n" +
		"    LogLevel ERROR\n" +
		"    ControlMaster auto\n" +
		"    ControlPath /tmp/ssh-mux-%i-%r@%h:%p\n" +
		"    ControlPersist 600\n"
}

const aliasFileHeader = "# Managed by zcp: dev-pool lease aliases, rewritten from .zcp/state/leases. Do not edit.\n"

func TestLeaseAliasStanza_RendersRecords(t *testing.T) {
	t.Parallel()

	got, err := renderLeaseAliases([]leaseRecord{
		aliasRecord("webpool", 12, "node-id-12.runtime.webpool.zerops"),
		aliasRecord("apppool", 3, "node-id-3.runtime.apppool.zerops"),
	})
	if err != nil {
		t.Fatalf("renderLeaseAliases: %v", err)
	}
	want := aliasFileHeader + "\n" +
		stanzaFor("apppool-3", "node-id-3.runtime.apppool.zerops") + "\n" +
		stanzaFor("webpool-12", "node-id-12.runtime.webpool.zerops")
	if got != want {
		t.Errorf("rendered file differs.\n got:\n%s\nwant:\n%s", got, want)
	}

	empty, err := renderLeaseAliases(nil)
	if err != nil || empty != aliasFileHeader {
		t.Errorf("no leases = %q, %v; want just the header (a valid, empty ssh config)", empty, err)
	}
}

func TestLeaseAliasStanza_RejectsInjectedHostname(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, hostname string }{
		{"newline_proxy_command", "node-id-3.runtime.apppool.zerops\n    ProxyCommand curl evil"},
		{"trailing_newline", "node-id-3.runtime.apppool.zerops\n"},
		{"space_option", "node-id-3.runtime.apppool.zerops -oProxyCommand=x"},
		{"foreign_host", "evil.example.com"},
		{"empty", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			out, err := renderLeaseAliases([]leaseRecord{
				aliasRecord("webpool", 1, "node-id-1.runtime.webpool.zerops"),
				aliasRecord("apppool", 3, tt.hostname),
			})
			if err == nil {
				t.Fatalf("rendered %q for hostname %q", out, tt.hostname)
			}
			if out != "" || strings.Contains(err.Error(), "ProxyCommand") {
				t.Errorf("a refusal must render nothing and not echo the payload: out=%q err=%v", out, err)
			}
		})
	}

	t.Run("bad_alias", func(t *testing.T) {
		t.Parallel()

		// Uppercase is no platform hostname, so the alias is refused.
		if out, err := renderLeaseAliases([]leaseRecord{aliasRecord("Evil", 3, "node-id-3.runtime.apppool.zerops")}); err == nil {
			t.Errorf("rendered %q for an invalid alias", out)
		}
	})
}

func TestLeaseAliasInstall_RefusedRecord_RunsNothing(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	if err := writeLeaseRecord(stateDir, aliasRecord("apppool", 3, "node-id-3.runtime.apppool.zerops")); err != nil {
		t.Fatal(err)
	}
	// A record edited on disk after it was written.
	path := filepath.Join(stateDir, "leases", "apppool-3.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.Replace(string(raw), "node-id-3.runtime.apppool.zerops", `node-id-3.runtime.apppool.zerops\nProxyCommand evil`, 1))
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	var ran [][]string
	run := func(_ context.Context, name string, args ...string) error {
		ran = append(ran, append([]string{name}, args...))
		return nil
	}
	if err := InstallLeaseAliases(context.Background(), run, stateDir, "/etc/ssh/ssh_config.d/zcp-leases.conf"); err == nil {
		t.Fatal("InstallLeaseAliases accepted a record with an injected hostname")
	}
	if len(ran) != 0 {
		t.Errorf("ran %v; nothing may reach sudo for a refused record", ran)
	}
}

func TestLeaseAliasInstall_UsesArgvSudoNoShell(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	if err := writeLeaseRecord(stateDir, aliasRecord("apppool", 3, "node-id-3.runtime.apppool.zerops")); err != nil {
		t.Fatal(err)
	}
	const dest = "/etc/ssh/ssh_config.d/zcp-leases.conf"

	var ran [][]string
	var installed string
	run := func(_ context.Context, name string, args ...string) error {
		argv := append([]string{name}, args...)
		ran = append(ran, argv)
		// The runner stands in for sudo: read the source file now, as it is
		// gone once the call returns.
		src := args[len(args)-2]
		raw, err := os.ReadFile(src)
		if err != nil {
			t.Errorf("source file %s unreadable while installing: %v", src, err)
		}
		installed = string(raw)
		return nil
	}
	if err := InstallLeaseAliases(context.Background(), run, stateDir, dest); err != nil {
		t.Fatalf("InstallLeaseAliases: %v", err)
	}

	if len(ran) != 1 {
		t.Fatalf("commands = %v, want exactly one sudo install", ran)
	}
	argv := ran[0]
	if argv[0] != "sudo" || !slices.Contains(argv, "-n") || !slices.Contains(argv, "install") || argv[len(argv)-1] != dest {
		t.Errorf("argv = %q, want sudo -n install ... %s", argv, dest)
	}
	for _, a := range argv {
		if a == "sh" || a == "bash" || a == "-c" || strings.ContainsAny(a, ";|&$`\n") {
			t.Errorf("argv %q carries a shell or shell metacharacters (%q)", argv, a)
		}
	}
	if want := aliasFileHeader + "\n" + stanzaFor("apppool-3", "node-id-3.runtime.apppool.zerops"); installed != want {
		t.Errorf("installed content differs.\n got:\n%s\nwant:\n%s", installed, want)
	}
	left, _ := filepath.Glob(filepath.Join(stateDir, "leases", ".aliases-*"))
	if len(left) != 0 {
		t.Errorf("temp files left behind: %v", left)
	}
}

// hostFixture is a lease fixture whose host side is fake: the install runner
// copies the file it is handed to a temp "alias file" (what sudo install
// does), and the mounter is the mock mounter. events records the order of
// host and orchestrator actions.
type hostFixture struct {
	*leaseFixture
	mounter   *mockMounter
	aliasFile string
	events    []string
	installs  [][]string
}

type orderedSwarm struct {
	*fakeSwarm
	events *[]string
}

func (o orderedSwarm) Release(ctx context.Context, id, lease string, reset bool) (platform.SwarmContainer, error) {
	*o.events = append(*o.events, "orchestrator-release")
	return o.fakeSwarm.Release(ctx, id, lease, reset)
}

type eventMounter struct {
	*mockMounter
	events *[]string
}

func (e eventMounter) Mount(ctx context.Context, h, p string) error {
	*e.events = append(*e.events, "mount")
	return e.mockMounter.Mount(ctx, h, p)
}

func (e eventMounter) Unmount(ctx context.Context, h, p string) error {
	*e.events = append(*e.events, "unmount")
	return e.mockMounter.Unmount(ctx, h, p)
}

func newHostFixture(t *testing.T) *hostFixture {
	t.Helper()
	hf := &hostFixture{leaseFixture: newLeaseFixture(t), mounter: newMockMounter(), aliasFile: filepath.Join(t.TempDir(), "zcp-leases.conf")}
	swarm := orderedSwarm{fakeSwarm: hf.swarm, events: &hf.events}
	hf.deps.Swarm = func(string, string) SwarmAPI { return swarm }
	hf.deps.Host = LeaseHost{
		Mounter:   eventMounter{mockMounter: hf.mounter, events: &hf.events},
		AliasFile: hf.aliasFile,
		Run: func(_ context.Context, name string, args ...string) error {
			hf.events = append(hf.events, "install-aliases")
			hf.installs = append(hf.installs, append([]string{name}, args...))
			raw, err := os.ReadFile(args[len(args)-2])
			if err != nil {
				return err
			}
			return os.WriteFile(args[len(args)-1], raw, 0o644)
		},
	}
	return hf
}

func TestLeaseAcquire_HostSide_AliasThenMount(t *testing.T) {
	t.Parallel()
	hf := newHostFixture(t)
	hf.swarm.acquired = acquiredAs(3)

	view, err := LeaseAcquire(context.Background(), hf.deps, testPoolHost, "feature")
	if err != nil {
		t.Fatalf("LeaseAcquire: %v", err)
	}
	if view.State != LeaseStateHeld || view.Note != "" {
		t.Errorf("view = %+v, want a plain held lease", view)
	}
	if got, want := strings.Join(hf.events, ","), "install-aliases,mount"; got != want {
		t.Errorf("host events = %s, want %s (the stanza must be in place before the mount)", got, want)
	}
	raw, err := os.ReadFile(hf.aliasFile)
	if err != nil {
		t.Fatalf("alias file: %v", err)
	}
	if !strings.Contains(string(raw), stanzaFor("devpool-3", nodeHostname(3))) {
		t.Errorf("alias file lacks the devpool-3 stanza:\n%s", raw)
	}
	if hf.mounter.states["/var/www/devpool-3"] != platform.MountStateActive {
		t.Errorf("devpool-3 is not mounted at /var/www/devpool-3: %v", hf.mounter.states)
	}
}

func TestLeaseAcquire_AliasInstallFails_HeldButUnmounted(t *testing.T) {
	t.Parallel()
	hf := newHostFixture(t)
	hf.swarm.acquired = acquiredAs(3)
	hf.deps.Host.Run = func(context.Context, string, ...string) error { return errors.New("sudo: a password is required") }

	view, err := LeaseAcquire(context.Background(), hf.deps, testPoolHost, "feature")
	if err != nil {
		t.Fatalf("LeaseAcquire must keep the lease it took, got %v", err)
	}
	if view.State != LeaseStateUnmounted || !strings.Contains(view.Note, "ssh alias") {
		t.Errorf("view = %+v, want state unmounted with the alias failure in the note", view)
	}
	if _, err := os.Stat(filepath.Join(hf.stateDir, "leases", "devpool-3.json")); err != nil {
		t.Errorf("the record must stay so zcp init can restore the lease: %v", err)
	}
	if len(hf.mounter.states) != 0 {
		t.Errorf("mounted %v without an alias", hf.mounter.states)
	}
	if strings.Contains(view.Note, testLease) {
		t.Errorf("note leaks the lease: %q", view.Note)
	}
}

func TestLeaseRelease_HostSide_UnmountsFirstAndDropsAlias(t *testing.T) {
	t.Parallel()
	hf := newHostFixture(t)
	hf.swarm.acquired = acquiredAs(3)
	ctx := context.Background()
	if _, err := LeaseAcquire(ctx, hf.deps, testPoolHost, "feature"); err != nil {
		t.Fatal(err)
	}
	hf.swarm.acquired = acquiredAs(4)
	hf.swarm.acquired.Container.ID = "c-def"
	if _, err := LeaseAcquire(ctx, hf.deps, testPoolHost, "other"); err != nil {
		t.Fatal(err)
	}
	hf.events = nil

	view, err := LeaseRelease(ctx, hf.deps, "devpool-3", true)
	if err != nil {
		t.Fatalf("LeaseRelease: %v", err)
	}
	if view.State != LeaseStateReleased {
		t.Errorf("view = %+v, want released", view)
	}
	if got, want := strings.Join(hf.events, ","), "unmount,orchestrator-release,install-aliases"; got != want {
		t.Errorf("events = %s, want %s (unmount before the release; aliases re-rendered after)", got, want)
	}
	raw, err := os.ReadFile(hf.aliasFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "devpool-3") || !strings.Contains(string(raw), "Host devpool-4") {
		t.Errorf("alias file after release must drop devpool-3 and keep devpool-4:\n%s", raw)
	}
}

func TestLeaseList_MissingMount_Unmounted(t *testing.T) {
	t.Parallel()
	hf := newHostFixture(t)
	ctx := context.Background()
	for _, n := range []int64{3, 4, 5} {
		hf.swarm.acquired = acquiredAs(n)
		hf.swarm.acquired.Container.ID = "c-" + strconv.FormatInt(n, 10)
		if _, err := LeaseAcquire(ctx, hf.deps, testPoolHost, ""); err != nil {
			t.Fatalf("acquire #%d: %v", n, err)
		}
	}
	hf.swarm.containers = []platform.SwarmContainer{reservedContainer("c-3", 3), reservedContainer("c-4", 4), reservedContainer("c-5", 5)}
	// #4 lost its mount (a redeploy); #5 lost its stanza.
	delete(hf.mounter.states, "/var/www/devpool-4")
	raw, err := os.ReadFile(hf.aliasFile)
	if err != nil {
		t.Fatal(err)
	}
	trimmed := strings.Replace(string(raw), "Host devpool-5", "# gone", 1)
	if err := os.WriteFile(hf.aliasFile, []byte(trimmed), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := LeaseList(ctx, hf.deps, testPoolHost)
	if err != nil {
		t.Fatalf("LeaseList: %v", err)
	}
	states := map[string]LeaseView{}
	for _, v := range got {
		states[v.Alias] = v
	}
	if states["devpool-3"].State != LeaseStateHeld {
		t.Errorf("devpool-3 = %+v, want held (alias and mount in place)", states["devpool-3"])
	}
	if v := states["devpool-4"]; v.State != LeaseStateUnmounted || !strings.Contains(v.Note, "/var/www/devpool-4") {
		t.Errorf("devpool-4 = %+v, want unmounted naming its mount path", v)
	}
	if v := states["devpool-5"]; v.State != LeaseStateUnmounted || !strings.Contains(v.Note, "ssh alias") {
		t.Errorf("devpool-5 = %+v, want unmounted naming its missing alias", v)
	}
}
