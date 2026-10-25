// Tests for: ops/lease_pool_credential.go — inside a Mate, a lease of a
// repository on its HQ puts the Mate credential on the pool's
// ZCP_HQ_GIT_TOKEN, never its GIT_TOKEN (the user's), and a re-enrollment
// renews it on every pool zcp put it on (docs/spec-workflows.md §8 DP-16).
package ops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

const (
	poolMateCredential = "the-mate-credential-0001"
	poolOldCredential  = "the-revoked-credential-0000"
)

// inAMate makes fx a Mate enrolled with leaseHQURL whose enrollment holds
// poolMateCredential; a fresh session's wait is short.
func inAMate(fx *leaseFixture) {
	fx.deps.HQAddress = func() string { return leaseHQURL }
	fx.deps.PoolCredential = PoolHQCredential{
		Read:        func() (string, bool) { return poolMateCredential, true },
		SessionWait: time.Millisecond,
	}
}

// writePoolCredentialRecord gives the state dir zcp's record that pool
// (serviceID) was given the credential fingerprint names.
func writePoolCredentialRecord(t *testing.T, stateDir, pool, serviceID, fingerprint string) {
	t.Helper()
	path := filepath.Join(stateDir, "leases", "credentials", pool+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]string{"poolServiceId": serviceID, "fingerprint": fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// readPoolCredentialRecord is zcp's record for pool, false when it has none.
func readPoolCredentialRecord(t *testing.T, stateDir, pool string) (map[string]string, fs.FileMode, bool) {
	t.Helper()
	path := filepath.Join(stateDir, "leases", "credentials", pool+".json")
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, false
	}
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rec map[string]string
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatalf("record %s: %v", raw, err)
	}
	return rec, info.Mode().Perm(), true
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

// poolHQCredential is the HQ credential the pool service holds on the mock
// platform (PoolHQCredentialEnvKey).
func poolHQCredential(t *testing.T, client platform.Client, serviceID string) (platform.ServiceEnvVar, bool) {
	t.Helper()
	return poolEnvVar(t, client, serviceID, PoolHQCredentialEnvKey)
}

// assertNoFileHolds fails when any file under dir carries secret.
func assertNoFileHolds(t *testing.T, dir, secret string) {
	t.Helper()
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // an unreadable entry holds nothing to check
		}
		raw, readErr := os.ReadFile(path)
		if readErr == nil && strings.Contains(string(raw), secret) {
			t.Errorf("%s holds the credential", path)
		}
		return nil
	})
}

// sessionFingerprint is what `sha256sum` prints for value in a session.
func sessionFingerprint(value string) []byte {
	sum := sha256.Sum256([]byte(value))
	return []byte(hex.EncodeToString(sum[:]) + "  -\n")
}

// TestLeaseAcquire_HQRepoInAMate_PutsTheMateCredentialOnThePool: before it
// takes a container for a repository on its HQ, a Mate makes the pool's
// ZCP_HQ_GIT_TOKEN the Mate credential its enrollment holds now — never its
// GIT_TOKEN, the user's — written as a
// sensitive secret when zcp's fingerprint for the pool is absent, names
// another credential (the Mate enrolled again) or another pool (re-created
// under the same hostname), and left alone when it matches: the Mate's key
// reads the secret back REDACTED, so the fingerprint is the only compare. The
// record keeps the fingerprint, never the credential.
func TestLeaseAcquire_HQRepoInAMate_PutsTheMateCredentialOnThePool(t *testing.T) {
	t.Parallel()
	current := hq.CredentialFingerprint(poolMateCredential)
	tests := []struct {
		name      string
		recordFor string // the pool service id the record names, "" for no record
		recorded  string // the fingerprint it names
		wantWrite bool
	}{
		{"no fingerprint yet", "", "", true},
		{"the fingerprint matches", "svc-pool", current, false},
		{"the Mate enrolled again", "svc-pool", hq.CredentialFingerprint(poolOldCredential), true},
		{"the pool was re-created", "svc-gone", current, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fx := newLeaseFixture(t)
			inAMate(fx)
			ssh := &leaseSSH{}
			fx.deps.SSH = ssh
			fx.swarm.acquired = acquiredAs(3)
			if tt.recordFor != "" {
				writePoolCredentialRecord(t, fx.stateDir, testPoolHost, tt.recordFor, tt.recorded)
			}
			mock := fx.deps.Client.(*platform.Mock)

			view, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, leaseHQRepo, "feat")
			if err != nil {
				t.Fatalf("acquire: %v", err)
			}

			writes := mock.CallCount("CreateServiceEnvVar")
			if tt.wantWrite != (writes == 1) || writes > 1 {
				t.Fatalf("HQ credential writes = %d, want a write: %v", writes, tt.wantWrite)
			}
			if tt.wantWrite {
				token, ok := poolHQCredential(t, mock, "svc-pool")
				if !ok || token.Content != poolMateCredential || !token.Sensitive {
					t.Errorf("the pool's %s = %+v (found %v), want the Mate credential as a sensitive secret", PoolHQCredentialEnvKey, token, ok)
				}
			}
			if token, ok := poolGitToken(t, mock, "svc-pool"); ok {
				t.Errorf("the acquire wrote the pool's GIT_TOKEN, the user's: %+v", token)
			}
			rec, mode, ok := readPoolCredentialRecord(t, fx.stateDir, testPoolHost)
			if !ok || rec["poolServiceId"] != "svc-pool" || rec["fingerprint"] != current {
				t.Errorf("record = %v (found %v), want svc-pool with the current fingerprint", rec, ok)
			}
			if ok && mode != 0o600 {
				t.Errorf("record mode = %o, want 0600", mode)
			}
			if len(fx.swarm.acquireReqs) != 1 || !strings.Contains(strings.Join(ssh.commands(), "\n"), "fetch -q --prune origin") {
				t.Errorf("the acquire did not take a container and check it out: %d acquire(s), %v", len(fx.swarm.acquireReqs), ssh.commands())
			}
			assertNoFileHolds(t, fx.stateDir, poolMateCredential)
			for _, out := range append(ssh.commands(), view.Note, view.Alias) {
				if strings.Contains(out, poolMateCredential) {
					t.Errorf("the credential reached %q", out)
				}
			}
		})
	}
}

// TestLeaseAcquire_PoolTokenStaysTheUsers: outside a Mate, for a repository
// outside HQ, and in a container enrolled with no HQ, the pool's GIT_TOKEN is
// the user's (DP-6): zcp never writes it and keeps no record of it.
func TestLeaseAcquire_PoolTokenStaysTheUsers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		repo  string
		setup func(*leaseFixture)
	}{
		{"a repository outside HQ, in a Mate", testRepo, func(fx *leaseFixture) { inAMate(fx) }},
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
			if _, _, ok := readPoolCredentialRecord(t, fx.stateDir, testPoolHost); ok {
				t.Error("zcp keeps a credential record for a pool it never wrote")
			}
		})
	}
}

// TestLeaseAcquire_HQRepo_WaitsUntilASessionSeesTheWrittenCredential: a
// service env write reaches a fresh ssh session within seconds, not at once,
// and a clean acquire is quicker than that. So an acquire that wrote the
// pool's HQ credential reads its fingerprint in a fresh session of the leased
// container — the hash, never the value — until it is the credential's, and
// only then checks out. An acquire that wrote nothing reads nothing.
func TestLeaseAcquire_HQRepo_WaitsUntilASessionSeesTheWrittenCredential(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		recorded   bool
		wantProbes int
	}{
		{"just written: read until the session has it", false, 3},
		{"already held: nothing to wait for", true, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fx := newLeaseFixture(t)
			inAMate(fx)
			if tt.recorded {
				writePoolCredentialRecord(t, fx.stateDir, testPoolHost, "svc-pool", hq.CredentialFingerprint(poolMateCredential))
			}
			probes := 0
			ssh := &leaseSSH{fn: func(_, command string) ([]byte, error) {
				if !strings.Contains(command, "sha256sum") {
					return nil, nil
				}
				probes++
				if probes < 3 {
					return sessionFingerprint(""), nil // the session has no GIT_TOKEN yet
				}
				return sessionFingerprint(poolMateCredential), nil
			}}
			fx.deps.SSH = ssh
			fx.swarm.acquired = acquiredAs(3)

			if _, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, leaseHQRepo, "feat"); err != nil {
				t.Fatalf("acquire: %v", err)
			}
			if probes != tt.wantProbes {
				t.Errorf("fingerprint reads = %d, want %d", probes, tt.wantProbes)
			}
			cmds := ssh.commands()
			fetch := -1
			for i, c := range cmds {
				if strings.Contains(c, poolMateCredential) {
					t.Errorf("a session command carries the credential: %s", c)
				}
				if strings.Contains(c, "fetch -q --prune origin") && fetch < 0 {
					fetch = i
				}
			}
			if fetch != tt.wantProbes {
				t.Errorf("the checkout ran at command %d of %v, want right after %d fingerprint read(s)", fetch, cmds, tt.wantProbes)
			}
		})
	}
}

// TestLeaseAcquire_HQRepo_NoCredentialOnThePool_TakesNoContainer: an acquire
// that cannot put the Mate credential on the pool — the enrollment holds none
// zcp can read, or the write fails — refuses before it takes a container, and
// its error never carries the credential.
func TestLeaseAcquire_HQRepo_NoCredentialOnThePool_TakesNoContainer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		setup    func(*leaseFixture)
		wantCode string
	}{
		{"the enrollment holds no credential to read", func(fx *leaseFixture) {
			fx.deps.PoolCredential.Read = func() (string, bool) { return "", false }
		}, platform.ErrPrerequisiteMissing},
		{"the write fails", func(fx *leaseFixture) {
			fx.deps.Client.(*platform.Mock).WithError("CreateServiceEnvVar",
				platform.NewPlatformError(platform.ErrAPIError, "userData create refused", ""))
		}, platform.ErrAPIError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fx := newLeaseFixture(t)
			inAMate(fx)
			tt.setup(fx)
			fx.deps.SSH = &leaseSSH{}
			fx.swarm.acquired = acquiredAs(3)

			_, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, leaseHQRepo, "feat")
			var pe *platform.PlatformError
			if !errors.As(err, &pe) {
				t.Fatalf("err = %v, want a refusal", err)
			}
			if pe.Code != tt.wantCode {
				t.Errorf("code = %s, want %s", pe.Code, tt.wantCode)
			}
			for _, want := range []string{"GIT_TOKEN", testPoolHost, "never ask"} {
				if !strings.Contains(pe.Message+" "+pe.Suggestion, want) {
					t.Errorf("refusal %q / %q lacks %q", pe.Message, pe.Suggestion, want)
				}
			}
			if strings.Contains(err.Error()+pe.Suggestion, poolMateCredential) {
				t.Errorf("the refusal carries the credential: %v", err)
			}
			if len(fx.swarm.acquireReqs) != 0 {
				t.Errorf("a container was taken (%d acquire(s)) for a pool zcp could not give the credential", len(fx.swarm.acquireReqs))
			}
			if _, _, ok := readPoolCredentialRecord(t, fx.stateDir, testPoolHost); ok {
				t.Error("zcp recorded a credential it did not put on the pool")
			}
		})
	}
}

// TestLeaseAcquire_HQRepo_RefusedCheckout_WritesAfreshNextTime: HQ's git
// refusing what zcp believes the pool holds — its secret changed since zcp
// wrote it — makes zcp forget its fingerprint, so the next acquire writes the
// credential again instead of trusting the stale record.
func TestLeaseAcquire_HQRepo_RefusedCheckout_WritesAfreshNextTime(t *testing.T) {
	t.Parallel()
	fx := newLeaseFixture(t)
	inAMate(fx)
	writePoolCredentialRecord(t, fx.stateDir, testPoolHost, "svc-pool", hq.CredentialFingerprint(poolMateCredential))
	ssh := &leaseSSH{fn: func(_, command string) ([]byte, error) {
		if strings.Contains(command, "fetch -q --prune origin") {
			return nil, sshFailure("remote: Unauthorized\nfatal: Authentication failed for 'https://hq.example.invalid/git/a1/web/'", exitError(t, 128))
		}
		return nil, nil
	}}
	fx.deps.SSH = ssh
	fx.swarm.acquired = acquiredAs(3)
	mock := fx.deps.Client.(*platform.Mock)

	_, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, leaseHQRepo, "feat")
	var pe *platform.PlatformError
	if !errors.As(err, &pe) || pe.Code != platform.ErrPrerequisiteMissing {
		t.Fatalf("err = %v, want %s", err, platform.ErrPrerequisiteMissing)
	}
	if mock.CallCount("CreateServiceEnvVar") != 0 {
		t.Fatal("zcp wrote a credential its record says the pool holds")
	}
	if _, _, ok := readPoolCredentialRecord(t, fx.stateDir, testPoolHost); ok {
		t.Error("the record of a credential HQ refused survives the refusal")
	}

	ssh.setHandler(nil)
	if _, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, leaseHQRepo, "feat"); err != nil {
		t.Fatalf("the next acquire: %v", err)
	}
	if token, ok := poolHQCredential(t, mock, "svc-pool"); mock.CallCount("CreateServiceEnvVar") != 1 || !ok || token.Content != poolMateCredential {
		t.Errorf("the next acquire did not write the credential afresh: %d write(s), %s %+v", mock.CallCount("CreateServiceEnvVar"), PoolHQCredentialEnvKey, token)
	}
}

// goneByIDClient answers the by-id read of the service gone the way the
// platform answers a deleted service's (live-verified): HTTP 400
// serviceStackNotFound, not a 404.
type goneByIDClient struct {
	*platform.Mock
	gone string
}

func (c goneByIDClient) GetService(ctx context.Context, id string) (*platform.ServiceStack, error) {
	if id == c.gone {
		pe := platform.NewPlatformError(platform.ErrAPIError, "Service stack not found.", "")
		pe.APICode = "serviceStackNotFound"
		return nil, pe
	}
	return c.Mock.GetService(ctx, id)
}

// TestRenewPoolHQCredentials_PoolGoneBy400_RecordDropped: a pool whose by-id
// read answers 400 serviceStackNotFound is gone like one answering 404 — its
// record is dropped, never reported as a failure retried at every pass.
func TestRenewPoolHQCredentials_PoolGoneBy400_RecordDropped(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writePoolCredentialRecord(t, stateDir, "gonepool", "svc-gone", hq.CredentialFingerprint(poolOldCredential))
	client := goneByIDClient{Mock: platform.NewMock(), gone: "svc-gone"}

	failures := RenewPoolHQCredentials(context.Background(), client, stateDir, poolMateCredential)

	if len(failures) != 0 {
		t.Errorf("failures = %+v, want none: the pool is gone", failures)
	}
	if _, _, ok := readPoolCredentialRecord(t, stateDir, "gonepool"); ok {
		t.Error("the record of a pool whose by-id read answers 400 serviceStackNotFound survives the renewal")
	}
}

// TestRenewPoolHQCredentials_RewritesEveryPoolWhoseFingerprintDiffers: after
// the Mate enrolls again, HQ revokes the credential every pool zcp gave it,
// and a lease already held there keeps working only once its pool's HQ
// credential is the new one (a fresh session reads it live). The renewal writes exactly
// the pools whose record names another credential, drops the record of a pool
// that is gone, and reports a pool it could not write — never the credential.
func TestRenewPoolHQCredentials_RewritesEveryPoolWhoseFingerprintDiffers(t *testing.T) {
	t.Parallel()
	pools := []platform.ServiceStack{
		{ID: "svc-pool", Name: "devpool", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1"}},
		{ID: "svc-web", Name: "webpool", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1"}},
	}
	old, current := hq.CredentialFingerprint(poolOldCredential), hq.CredentialFingerprint(poolMateCredential)
	tests := []struct {
		name         string
		failWrites   bool
		wantWrites   int
		wantFailures []string
		wantDevpool  string
	}{
		{"renews the stale pool only", false, 1, nil, current},
		{"a write that fails is reported and retried", true, 1, []string{"devpool"}, old},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := t.TempDir()
			mock := platform.NewMock().WithServices(pools)
			if tt.failWrites {
				mock.WithError("CreateServiceEnvVar", platform.NewPlatformError(platform.ErrAPIError, "userData create refused", ""))
			}
			writePoolCredentialRecord(t, stateDir, "devpool", "svc-pool", old)
			writePoolCredentialRecord(t, stateDir, "webpool", "svc-web", current)
			writePoolCredentialRecord(t, stateDir, "gonepool", "svc-gone", old)

			failures := RenewPoolHQCredentials(context.Background(), mock, stateDir, poolMateCredential)

			if got := mock.CallCount("CreateServiceEnvVar"); got != tt.wantWrites {
				t.Errorf("HQ credential writes = %d, want %d", got, tt.wantWrites)
			}
			failed := make([]string, 0, len(failures))
			for _, f := range failures {
				failed = append(failed, f.Pool)
				if f.Err == nil || strings.Contains(f.Err.Error(), poolMateCredential) {
					t.Errorf("failure %+v: want an error that does not carry the credential", f)
				}
			}
			if strings.Join(failed, ",") != strings.Join(tt.wantFailures, ",") {
				t.Errorf("failures = %v, want %v", failed, tt.wantFailures)
			}
			if !tt.failWrites {
				if token, ok := poolHQCredential(t, mock, "svc-pool"); !ok || token.Content != poolMateCredential || !token.Sensitive {
					t.Errorf("devpool's %s = %+v, want the renewed credential, sensitive", PoolHQCredentialEnvKey, token)
				}
			}
			if rec, _, _ := readPoolCredentialRecord(t, stateDir, "devpool"); rec["fingerprint"] != tt.wantDevpool {
				t.Errorf("devpool's record = %v, want fingerprint %s", rec, tt.wantDevpool)
			}
			if rec, _, _ := readPoolCredentialRecord(t, stateDir, "webpool"); rec["fingerprint"] != current {
				t.Errorf("webpool's record = %v, want it kept", rec)
			}
			if _, _, ok := readPoolCredentialRecord(t, stateDir, "gonepool"); ok {
				t.Error("the record of a pool that is gone survives the renewal")
			}
			assertNoFileHolds(t, stateDir, poolMateCredential)
		})
	}
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

// TestLeaseAcquire_HQThenOutsideHQ_TheMateCredentialReachesOnlyHQ: inside a
// Mate an acquire of an HQ repository gives the pool the Mate credential, and
// every container of the pool reads the pool's service env. A later lease of
// a repository outside HQ on the same pool fetches at its checkout, in a warm
// clone and through the helper its repository keeps for the agent's push —
// none of which may answer the Mate credential to that other host. The HQ
// lease still fetches as `mate` with it.
func TestLeaseAcquire_HQThenOutsideHQ_TheMateCredentialReachesOnlyHQ(t *testing.T) {
	t.Parallel()
	requireGit(t)
	fx := newLeaseFixture(t)
	inAMate(fx)
	ssh := &leaseSSH{}
	fx.deps.SSH = ssh
	fx.swarm.acquired = acquiredAs(3)
	if _, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, leaseHQRepo, "feat"); err != nil {
		t.Fatalf("acquire of the HQ repository: %v", err)
	}
	fx.swarm.acquired = secondContainer()
	if _, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, testRepo, "feat"); err != nil {
		t.Fatalf("acquire of the repository outside HQ: %v", err)
	}
	checkouts := ssh.checkouts()
	if len(checkouts) != 2 {
		t.Fatalf("checkouts = %v, want one per lease", checkouts)
	}
	session := poolSession(t, fx.deps.Client, "svc-pool")

	if user, password, _ := inlineAnswer(t, checkouts[0].command, session, leaseHQRepo); user != "mate" || password != poolMateCredential {
		t.Errorf("the HQ lease's fetch answers %q / %q, want mate with the Mate credential", user, password)
	}
	outside := map[string]func() (string, string, bool){
		"the checkout's fetch": func() (string, string, bool) { return inlineAnswer(t, checkouts[1].command, session, testRepo) },
		"a warm clone of it": func() (string, string, bool) {
			return inlineAnswer(t, leaseWarmCloneCommand(leaseWorkDir, testRepo, leaseHQURL, leaseHelperScope(testRepo)), session, testRepo)
		},
		"its persisted helper": func() (string, string, bool) { return persistedAnswer(t, testRepo, session) },
	}
	for what, ask := range outside {
		if _, password, answered := ask(); answered && strings.Contains(password, poolMateCredential) {
			t.Errorf("%s answers github.com with the Mate credential", what)
		}
	}
}

// TestLeaseAcquire_HQRepo_TheUsersGitTokenStays: a pool whose GIT_TOKEN the
// user set — a lease of their GitHub repository held there — and then an
// acquire of an HQ repository inside a Mate. zcp never writes over the
// user's GIT_TOKEN: the held lease keeps fetching and pushing with the
// user's token, and the HQ lease fetches as `mate` with the Mate credential —
// both kinds of repository on one pool.
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
	if _, password, _ := inlineAnswer(t, leaseWarmCloneCommand(leaseWorkDir, testRepo, leaseHQURL, leaseHelperScope(testRepo)), session, testRepo); password != usersToken {
		t.Errorf("a warm clone of the user's repository fetches with %q, want the user's token", password)
	}
	if user, password, _ := inlineAnswer(t, checkouts[1].command, session, leaseHQRepo); user != "mate" || password != poolMateCredential {
		t.Errorf("the HQ lease fetches with %q / %q, want mate with the Mate credential", user, password)
	}
	if user, password, _ := persistedAnswer(t, leaseHQRepo, session); user != "mate" || password != poolMateCredential {
		t.Errorf("the HQ lease pushes with %q / %q, want mate with the Mate credential", user, password)
	}
}

// TestPoolHQCredentialVariable_IsACredentialEverywhere: the variable a pool
// holds this Mate's HQ credential under is masked at every echo site, kept
// out of a generated .env, out of export and launch bundles and out of
// Mate's vault, like GIT_TOKEN.
func TestPoolHQCredentialVariable_IsACredentialEverywhere(t *testing.T) {
	t.Parallel()
	const key = "ZCP_HQ_GIT_TOKEN"
	if _, masked := RedactEnvValue(key, poolMateCredential, "swarm@1", false); !masked {
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
