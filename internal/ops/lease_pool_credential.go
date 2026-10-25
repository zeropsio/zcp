package ops

import (
	"cmp"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/platform"
)

// A Mate's HQ takes git only from the user `mate` with the Mate credential, and
// a lease of a repository on it fetches and pushes with the pool's
// ZCP_HQ_GIT_TOKEN (PoolHQCredentialEnvKey, DP-12). So inside a Mate zcp puts
// the credential there itself before it takes a container, as git-push-setup
// puts it on a wired pair's dev service (docs/spec-mate.md §10.10, *The
// credential*), and the repository pass renews it after the Mate enrolls again
// (RenewPoolHQCredentials) — docs/spec-workflows.md §8 DP-16. It is never the
// pool's GIT_TOKEN: every container of a pool reads the pool's whole service
// env, and a lease of a repository outside HQ answers its host with GIT_TOKEN,
// which stays the user's — zcp never writes it (DP-6). A credential under a
// variable only the HQ helper reads cannot reach another host, and both kinds
// of repository work on one pool.
//
// The Mate's API key writes a sensitive secret but reads it back REDACTED, so
// zcp cannot compare by reading: it keeps its own record of what it wrote, a
// fingerprint per pool (hq.CredentialFingerprint, the one a pair's record
// keeps) in .zcp/state/leases/credentials/<pool>.json, mode 0600 — never the
// credential. A pool has no ServiceMeta of its own (DP-2); a pool that keys a
// pair (docs/spec-workflows.md §13 PM-10) is renewed with that pair: the HQ
// pass takes this record over as the pair's credential fingerprint
// (HQRepoRef.CredentialFingerprint) when it names the pool service the pair
// records (devPool.serviceId), and removes the file (PoolCredentialRecord,
// ForgetPoolCredentialRecord). The walk of this directory still renews every
// pool that keys no pair.

// PoolHQCredential is how an acquire puts the Mate credential on a pool
// (PoolHQCredentialEnvKey). The zero value — zcp outside a Mate — never
// writes.
type PoolHQCredential struct {
	// Read answers the Mate credential this Mate's HQ enrollment holds, read
	// at each acquire; false when it holds none zcp can read.
	Read func() (string, bool)
	// SessionWait is the pause between two reads of a fresh session's
	// fingerprint after a write; zero means poolCredentialSessionWait.
	SessionWait time.Duration
}

// poolCredentialSessionReads/Wait bound how long an acquire that wrote the
// pool's HQ credential waits for a fresh session in the leased container to read
// it: a service env write reaches fresh ssh sessions within the ~5-10 s
// propagation window, no restart (git-push-setup's session check waits out the
// same window), and a clean acquire can be quicker than that.
const (
	poolCredentialSessionReads = 8
	poolCredentialSessionWait  = 3 * time.Second
)

// poolCredentialProbe prints the SHA-256 of the session's HQ credential
// (PoolHQCredentialEnvKey) — the hash, never the value; an unset one hashes as
// the empty string.
const poolCredentialProbe = `printf '%s' "$` + PoolHQCredentialEnvKey + `" | sha256sum`

// poolCredentialRecord is zcp's record of the Mate credential it put on a pool:
// which pool service (a pool re-created under its hostname holds nothing zcp
// wrote) and which credential, by fingerprint.
type poolCredentialRecord struct {
	PoolServiceID string `json:"poolServiceId"`
	Fingerprint   string `json:"fingerprint"`
}

func poolCredentialDir(stateDir string) string {
	return filepath.Join(leaseDir(stateDir), "credentials")
}

func poolCredentialPath(stateDir, pool string) string {
	return filepath.Join(poolCredentialDir(stateDir), pool+".json")
}

// readPoolCredential is the pool's record, false when there is none zcp can
// read: an unreadable record makes the next acquire write, never skip.
func readPoolCredential(stateDir, pool string) (poolCredentialRecord, bool) {
	if stateDir == "" || !leasePoolNameRe.MatchString(pool) {
		return poolCredentialRecord{}, false
	}
	raw, err := os.ReadFile(poolCredentialPath(stateDir, pool))
	if err != nil {
		return poolCredentialRecord{}, false
	}
	var rec poolCredentialRecord
	if json.Unmarshal(raw, &rec) != nil || rec.PoolServiceID == "" || rec.Fingerprint == "" {
		return poolCredentialRecord{}, false
	}
	return rec, true
}

// storePoolCredential records rec for pool, atomically with mode 0600.
func storePoolCredential(stateDir, pool string, rec poolCredentialRecord) error {
	if stateDir == "" || !leasePoolNameRe.MatchString(pool) {
		return errors.New("no state directory or an unusable pool name")
	}
	if err := os.MkdirAll(poolCredentialDir(stateDir), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", poolCredentialDir(stateDir), err)
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := atomicWriteFile(poolCredentialPath(stateDir, pool), data); err != nil {
		return fmt.Errorf("pool credential record %s: %w", pool, err)
	}
	return nil
}

// forgetPoolCredential removes the pool's record: the next acquire of an HQ
// repository there writes the credential afresh.
func forgetPoolCredential(stateDir, pool string) error {
	if stateDir == "" || !leasePoolNameRe.MatchString(pool) {
		return nil
	}
	if err := os.Remove(poolCredentialPath(stateDir, pool)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove pool credential record %s: %w", pool, err)
	}
	return nil
}

// writePoolHQCredential puts credential on the pool's PoolHQCredentialEnvKey as
// a sensitive service secret (EnvSetService: the hand-rolled userData write)
// and records its fingerprint; the pool's GIT_TOKEN is never touched. The
// record is best-effort: one that cannot be saved costs a second write at the
// next acquire, never a lease.
func writePoolHQCredential(ctx context.Context, client platform.Client, stateDir, pool, serviceID, credential string) error {
	if _, err := EnvSetService(ctx, client, serviceID, PoolHQCredentialEnvKey, credential, true); err != nil {
		return fmt.Errorf("write %s on pool %s: %w", PoolHQCredentialEnvKey, pool, err)
	}
	_ = storePoolCredential(stateDir, pool, poolCredentialRecord{PoolServiceID: serviceID, Fingerprint: hq.CredentialFingerprint(credential)})
	return nil
}

// poolCredentialState is what an acquire did about the pool's HQ credential.
type poolCredentialState struct {
	// pool is the pool zcp holds the HQ credential of for this lease; "" for a
	// repository outside HQ, or outside a Mate, which authenticates with the
	// pool's GIT_TOKEN — the user's.
	pool        string
	fingerprint string
	// wrote: this acquire wrote it, so a fresh session may not read it yet.
	wrote bool
}

// ensurePoolHQCredential makes the pool's PoolHQCredentialEnvKey the Mate
// credential the enrollment holds now, before a lease of repo takes a container: written when
// zcp's record for the pool is absent, names another pool service or another
// credential (the Mate enrolled again), and left alone when it matches. A
// repository outside the Mate's HQ, or zcp outside a Mate, changes nothing,
// and no acquire ever writes the pool's GIT_TOKEN.
// Without a credential to read, or with a write that fails, the acquire is
// refused before it takes a container.
func ensurePoolHQCredential(ctx context.Context, d LeaseDeps, pool *platform.ServiceStack, repo string) (poolCredentialState, error) {
	if d.PoolCredential.Read == nil || !IsHQRemote(repo, d.hqAddress()) {
		return poolCredentialState{}, nil
	}
	credential, ok := d.PoolCredential.Read()
	if !ok || credential == "" {
		return poolCredentialState{}, platform.NewPlatformError(platform.ErrPrerequisiteMissing,
			fmt.Sprintf("Pool '%s' cannot be given this Mate's HQ credential as its %s: the Mate's HQ enrollment holds none zcp can read", pool.Name, PoolHQCredentialEnvKey),
			"A lease of a repository on HQ fetches with it, so no container was taken. This Mate's own service (zcp service mate) keeps enrolling it; acquire again once it is enrolled. It is no user's token, so never ask for one or make one up")
	}
	state := poolCredentialState{pool: pool.Name, fingerprint: hq.CredentialFingerprint(credential)}
	if rec, found := readPoolCredential(d.StateDir, pool.Name); found && rec.PoolServiceID == pool.ID && rec.Fingerprint == state.fingerprint {
		return state, nil
	}
	if err := writePoolHQCredential(ctx, d.Client, d.StateDir, pool.Name, pool.ID, credential); err != nil {
		code := platform.ErrAPIError
		var pe *platform.PlatformError
		if errors.As(err, &pe) {
			code = pe.Code
		}
		out := platform.NewPlatformError(code,
			fmt.Sprintf("Pool '%s' could not be given this Mate's HQ credential as its %s, which a lease of a repository on HQ fetches with: %v", pool.Name, PoolHQCredentialEnvKey, err),
			"No container was taken; acquire again. It is no user's token, so never ask for one or make one up")
		out.Cause = err
		return poolCredentialState{}, out
	}
	state.wrote = true
	return state, nil
}

// checkout puts the leased container on the branch, a new one cut from base
// (checkoutLease, which answers the commit it cut from), once a fresh session
// there reads a credential this acquire wrote. HQ's git refusing the pool's
// HQ credential (a PREREQUISITE_MISSING checkout) means it is not what zcp's
// record says — the secret changed or HQ revoked it since — so the record
// goes, and the next acquire writes the credential afresh.
func (s poolCredentialState) checkout(ctx context.Context, d LeaseDeps, rec leaseRecord, repo, branch, base string) (leaseCheckedOut, error) {
	s.awaitSession(ctx, d, rec)
	got, err := checkoutLease(ctx, d, rec, repo, branch, base)
	var pe *platform.PlatformError
	if s.pool != "" && errors.As(err, &pe) && pe.Code == platform.ErrPrerequisiteMissing {
		_ = forgetPoolCredential(d.StateDir, s.pool)
	}
	return got, err
}

// awaitSession reads, after a write, the fingerprint of the HQ credential in
// fresh sessions of the leased container until it is the written credential's,
// within poolCredentialSessionReads reads. A session that cannot say — ssh
// fails, no sha256sum — ends the wait: the checkout then tells.
func (s poolCredentialState) awaitSession(ctx context.Context, d LeaseDeps, rec leaseRecord) {
	if !s.wrote || d.SSH == nil {
		return
	}
	wait := cmp.Or(d.PoolCredential.SessionWait, poolCredentialSessionWait)
	for read := range poolCredentialSessionReads {
		if read > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
		}
		out, err := d.SSH.ExecSSH(ctx, rec.alias(), poolCredentialProbe)
		if err != nil {
			return
		}
		seen, ok := readSessionFingerprint(out)
		if !ok || seen == s.fingerprint {
			return
		}
	}
}

// readSessionFingerprint is the fingerprint in sha256sum's answer for stdin
// (`<64 hex>  -`), false when out holds none.
func readSessionFingerprint(out []byte) (string, bool) {
	for line := range strings.SplitSeq(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 || f[1] != "-" || len(f[0]) != 64 {
			continue
		}
		if _, err := hex.DecodeString(f[0]); err != nil {
			continue
		}
		return strings.ToLower(f[0][:16]), true
	}
	return "", false
}

// PoolCredentialFailure is a pool whose HQ credential a renewal could not
// rewrite; Err never carries the credential.
type PoolCredentialFailure struct {
	Pool string
	Err  error
}

// RenewPoolHQCredentials gives every pool zcp put the Mate credential on the
// credential as the enrollment holds it now: a pool whose record names another
// one — the Mate enrolled again, and HQ revoked the old — is written, so a
// lease held there reaches HQ again from its next fresh session; a pool that
// is gone loses its record (one re-created under its name is written at its
// first acquire). Returns the pools it could not write, by name; their records
// stay, so the next pass tries again.
func RenewPoolHQCredentials(ctx context.Context, client platform.Client, stateDir, credential string) []PoolCredentialFailure {
	if stateDir == "" || credential == "" {
		return nil
	}
	entries, err := os.ReadDir(poolCredentialDir(stateDir))
	if err != nil {
		return nil
	}
	fingerprint := hq.CredentialFingerprint(credential)
	var failures []PoolCredentialFailure
	for _, e := range entries {
		pool, isRecord := strings.CutSuffix(e.Name(), ".json")
		if e.IsDir() || !isRecord {
			continue
		}
		rec, found := readPoolCredential(stateDir, pool)
		if !found || rec.Fingerprint == fingerprint {
			continue
		}
		// By id: the record names the pool service zcp wrote, and the service
		// search lags writes.
		if _, err := client.GetService(ctx, rec.PoolServiceID); err != nil {
			if IsServiceGone(err) {
				_ = forgetPoolCredential(stateDir, pool)
				continue
			}
			failures = append(failures, PoolCredentialFailure{Pool: pool, Err: fmt.Errorf("read pool %s: %w", pool, err)})
			continue
		}
		if err := writePoolHQCredential(ctx, client, stateDir, pool, rec.PoolServiceID, credential); err != nil {
			failures = append(failures, PoolCredentialFailure{Pool: pool, Err: err})
		}
	}
	return failures
}
