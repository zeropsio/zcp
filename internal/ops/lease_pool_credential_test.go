// Tests for: ops/lease_pool_credential.go — inside a Mate, a lease of a
// repository on its HQ puts the Mate credential on the pool's GIT_TOKEN, and a
// re-enrollment renews it on every pool zcp put it on
// (docs/spec-workflows.md §8 DP-16).
package ops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/platform"
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

// poolGitToken is the GIT_TOKEN svc holds on the mock platform.
func poolGitToken(t *testing.T, client platform.Client, serviceID string) (platform.ServiceEnvVar, bool) {
	t.Helper()
	envs, err := client.GetServiceEnv(context.Background(), serviceID)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range envs {
		if e.Key == GitTokenEnvKey {
			return e, true
		}
	}
	return platform.ServiceEnvVar{}, false
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
// GIT_TOKEN the Mate credential its enrollment holds now — written as a
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
				t.Fatalf("GIT_TOKEN writes = %d, want a write: %v", writes, tt.wantWrite)
			}
			if tt.wantWrite {
				token, ok := poolGitToken(t, mock, "svc-pool")
				if !ok || token.Content != poolMateCredential || !token.Sensitive {
					t.Errorf("the pool's GIT_TOKEN = %+v (found %v), want the Mate credential as a sensitive secret", token, ok)
				}
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
// pool's GIT_TOKEN reads its fingerprint in a fresh session of the leased
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
	if token, ok := poolGitToken(t, mock, "svc-pool"); mock.CallCount("CreateServiceEnvVar") != 1 || !ok || token.Content != poolMateCredential {
		t.Errorf("the next acquire did not write the credential afresh: %d write(s), GIT_TOKEN %+v", mock.CallCount("CreateServiceEnvVar"), token)
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
// and a lease already held there keeps working only once its pool's GIT_TOKEN
// is the new one (a fresh session reads it live). The renewal writes exactly
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
				t.Errorf("GIT_TOKEN writes = %d, want %d", got, tt.wantWrites)
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
				if token, ok := poolGitToken(t, mock, "svc-pool"); !ok || token.Content != poolMateCredential || !token.Sensitive {
					t.Errorf("devpool's GIT_TOKEN = %+v, want the renewed credential, sensitive", token)
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
