// Tests for: ops/lease_checkout.go — inside a Mate a lease of a repository on
// its HQ reaches HQ relayed (LeaseDeps.HQGit), answered with the Mate's
// project key over the session: the pool stores no HQ credential, and its
// GIT_TOKEN stays the user's (docs/spec-workflows.md §8 DP-16).
package ops

import (
	"context"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// relayedMark opens every command a test's HQGit runs (relayedThrough), so a
// test tells git zcp ran relayed from git it ran over plain ssh.
const relayedMark = ": relayed; "

// relayedThrough is HQGit in a test: the deps' own SSH at the call, every
// command marked relayedMark.
type relayedThrough struct{ deps *LeaseDeps }

func (r relayedThrough) ExecSSH(ctx context.Context, host, command string) ([]byte, error) {
	return r.deps.SSH.ExecSSH(ctx, host, relayedMark+command)
}

// inAMate makes fx a Mate enrolled with leaseHQURL, whose git against HQ
// runs relayed (relayedThrough).
func inAMate(fx *leaseFixture) {
	fx.deps.HQAddress = func() string { return leaseHQURL }
	fx.deps.HQGit = relayedThrough{deps: &fx.deps}
}

// poolEnvVar is the variable key the service holds on the mock platform.
func poolEnvVar(t *testing.T, client platform.Client, serviceID, key string) (platform.ServiceEnvVar, bool) {
	t.Helper()
	envs, err := client.GetServiceEnv(context.Background(), serviceID)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range envs {
		if e.Key == key {
			return e, true
		}
	}
	return platform.ServiceEnvVar{}, false
}

// poolGitToken is the GIT_TOKEN the service holds on the mock platform.
func poolGitToken(t *testing.T, client platform.Client, serviceID string) (platform.ServiceEnvVar, bool) {
	t.Helper()
	return poolEnvVar(t, client, serviceID, GitTokenEnvKey)
}

// poolSession is the env a fresh ssh session in any container of the pool
// gets: every variable of the pool service (DP-6, live-verified).
func poolSession(t *testing.T, client platform.Client, serviceID string) map[string]string {
	t.Helper()
	envs, err := client.GetServiceEnv(context.Background(), serviceID)
	if err != nil {
		t.Fatal(err)
	}
	session := make(map[string]string, len(envs))
	for _, e := range envs {
		session[e.Key] = e.Content
	}
	return session
}

// inlineHelperRe finds the helper a composed command hands git for one call:
// `-c credential.helper= -c credential.helper=<shell-quoted helper>`.
var inlineHelperRe = regexp.MustCompile(`-c credential\.helper= -c credential\.helper=('(?:[^']|'\\'')*')`)

// inlineAnswer asks the helper command hands git inline for a credential to
// remoteURL, in a session with env, the way that git call would, and returns
// the user and password it answered and whether it answered one at all. A
// script the command runs through `sh -c '…'` (the warm clone's) is read one
// quoting level down, where its git call sits.
func inlineAnswer(t *testing.T, command string, env map[string]string, remoteURL string) (user, password string, answered bool) {
	t.Helper()
	m := inlineHelperRe.FindStringSubmatch(strings.ReplaceAll(command, `'\''`, `'`))
	if m == nil {
		t.Fatalf("the command hands git no helper inline:\n%s", command)
	}
	u, err := url.Parse(remoteURL)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := gitShell(t, t.TempDir(), t.TempDir(), env,
		"protocol="+u.Scheme+"\nhost="+u.Host+"\npath="+strings.TrimPrefix(u.Path, "/")+"\n\n",
		"git -c credential.helper= -c credential.helper="+m[1]+" credential fill")
	for line := range strings.SplitSeq(out, "\n") {
		if v, ok := strings.CutPrefix(line, "username="); ok {
			user = v
		}
		if v, ok := strings.CutPrefix(line, "password="); ok {
			password, answered = v, true
		}
	}
	return user, password, answered
}

// persistedAnswer asks the helper a lease's checkout persists for repo for a
// credential to repo, in a session with env, as the agent's own push does.
func persistedAnswer(t *testing.T, repo string, env map[string]string) (user, password string, answered bool) {
	t.Helper()
	dir, home := t.TempDir(), t.TempDir()
	if out, err := gitShell(t, dir, home, nil, "", "git init -q && "+leaseCredentialHelperConfig(repo, leaseHQURL, leaseHelperScope(repo))); err != nil {
		t.Fatalf("persist the helper: %v\n%s", err, out)
	}
	return answeredCredential(t, dir, home, env, repo)
}

// secondContainer is the pool's next container an acquire reserves.
func secondContainer() platform.SwarmAcquired {
	return platform.SwarmAcquired{Container: reservedContainer("c-def", 4), Lease: platform.SwarmLease(testLease)}
}

// TestLeaseAcquire_PoolTokenStaysTheUsers: in a Mate or outside one, for a
// repository on HQ or outside it, and in a container enrolled with no HQ, the
// pool's env is the user's (DP-6): zcp never writes it.
func TestLeaseAcquire_PoolTokenStaysTheUsers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		repo  string
		setup func(*leaseFixture)
	}{
		{"a repository outside HQ, in a Mate", testRepo, func(fx *leaseFixture) { inAMate(fx) }},
		{"a repository on HQ, in a Mate", leaseHQRepo, func(fx *leaseFixture) { inAMate(fx) }},
		{"a repository on HQ, outside a Mate", leaseHQRepo, func(fx *leaseFixture) {
			fx.deps.HQAddress = func() string { return leaseHQURL }
		}},
		{"a Mate enrolled with no HQ", leaseHQRepo, func(fx *leaseFixture) {
			inAMate(fx)
			fx.deps.HQAddress = func() string { return "" }
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fx := newLeaseFixture(t)
			tt.setup(fx)
			fx.deps.SSH = &leaseSSH{}
			fx.swarm.acquired = acquiredAs(3)
			mock := fx.deps.Client.(*platform.Mock)

			if _, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, tt.repo, "feat"); err != nil {
				t.Fatalf("acquire: %v", err)
			}
			if n := mock.CallCount("CreateServiceEnvVar") + mock.CallCount("DeleteUserData"); n != 0 {
				t.Errorf("zcp touched the pool's env %d time(s); its GIT_TOKEN is the user's", n)
			}
		})
	}
}

// TestLeaseAcquire_HQRepo_TheUsersGitTokenStays: a pool whose GIT_TOKEN the
// user set — a lease of their GitHub repository held there — and then an
// acquire of an HQ repository inside a Mate. zcp never writes over the
// user's GIT_TOKEN: the held lease keeps fetching and pushing with the
// user's token, and the HQ lease fetches relayed, and keeps no helper that
// could answer HQ from the pool's env — both kinds of repository on one pool.
func TestLeaseAcquire_HQRepo_TheUsersGitTokenStays(t *testing.T) {
	t.Parallel()
	requireGit(t)
	const usersToken = "the-users-own-github-token"
	fx := newLeaseFixture(t)
	inAMate(fx)
	mock := fx.deps.Client.(*platform.Mock)
	mock.WithServiceEnv("svc-pool", []platform.ServiceEnvVar{
		{ID: "e-api", Key: "API_TOKEN", Content: testPoolToken, Sensitive: true},
		{ID: "e-git", Key: GitTokenEnvKey, Content: usersToken, Sensitive: true},
	})
	ssh := &leaseSSH{}
	fx.deps.SSH = ssh
	fx.swarm.acquired = acquiredAs(3)
	if _, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, testRepo, "feat"); err != nil {
		t.Fatalf("acquire of the user's repository: %v", err)
	}
	fx.swarm.acquired = secondContainer()
	if _, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, leaseHQRepo, "feat"); err != nil {
		t.Fatalf("acquire of the HQ repository: %v", err)
	}
	checkouts := ssh.checkouts()
	if len(checkouts) != 2 {
		t.Fatalf("checkouts = %v, want one per lease", checkouts)
	}

	if token, ok := poolGitToken(t, mock, "svc-pool"); !ok || token.Content != usersToken {
		t.Errorf("the pool's GIT_TOKEN = %+v (held %v), want the user's own token, never written over", token, ok)
	}
	session := poolSession(t, mock, "svc-pool")
	if _, password, _ := inlineAnswer(t, checkouts[0].command, session, testRepo); password != usersToken {
		t.Errorf("the user's lease fetches with %q, want the user's token", password)
	}
	if _, password, _ := persistedAnswer(t, testRepo, session); password != usersToken {
		t.Errorf("the user's lease pushes with %q, want the user's token", password)
	}
	if _, password, _ := inlineAnswer(t, leaseWarmCloneCommand(LeaseWorkDir, testRepo, leaseHelperScope(testRepo)), session, testRepo); password != usersToken {
		t.Errorf("a warm clone of the user's repository fetches with %q, want the user's token", password)
	}
	if !strings.HasPrefix(checkouts[1].command, relayedMark) || strings.HasPrefix(checkouts[0].command, relayedMark) {
		t.Errorf("checkouts = %v, want only the HQ lease's relayed", checkouts)
	}
	if strings.Contains(checkouts[1].command, "credential.") {
		t.Errorf("the HQ lease names or keeps a helper that could answer HQ from the pool's env:\n%s", checkouts[1].command)
	}
}

// TestPoolHQCredentialVariable_IsACredentialEverywhere: the retired variable
// pools an older zcp wrote this Mate's HQ credential to still carry it, so it
// is masked at every echo site, kept out of a generated .env, out of export
// and launch bundles and out of Mate's vault, like GIT_TOKEN.
func TestPoolHQCredentialVariable_IsACredentialEverywhere(t *testing.T) {
	t.Parallel()
	const key = "ZCP_HQ_GIT_TOKEN"
	if _, masked := RedactEnvValue(key, "the-mate-credential-0001", "swarm@1", false); !masked {
		t.Errorf("RedactEnvValue(%s) echoes the value", key)
	}
	if !platformInternalKeys[key] {
		t.Errorf("%s is not kept out of a generated .env", key)
	}
	if !topology.IsClassifyInfrastructure(key) {
		t.Errorf("%s is not classified infrastructure: an export or launch bundle would copy it", key)
	}
	if !isVaultHidden(key) {
		t.Errorf("%s is listed in Mate's vault", key)
	}
}
