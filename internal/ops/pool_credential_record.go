package ops

import "context"

// A pool's record of the Mate credential zcp wrote to its HQ credential
// variable (PoolHQCredentialEnvKey)
// (lease_pool_credential.go, docs/spec-workflows.md §8 DP-16) is handed over
// once the pool keys a pair: the pair's HQ record then keeps the fingerprint,
// the repository pass renews the pool from it, and the per-pool record goes,
// so one pass never renews the pool twice. These are the two reads the
// hand-over needs; ops reads no ServiceMeta, so the pass (tools) makes it.

// PoolCredentialRecord is zcp's record of the Mate credential an acquire put
// on pool (PoolHQCredentialEnvKey): the pool service it wrote and the fingerprint of what
// it wrote — never the credential. found is false when there is none zcp can
// read.
func PoolCredentialRecord(stateDir, pool string) (poolServiceID, fingerprint string, found bool) {
	rec, found := readPoolCredential(stateDir, pool)
	return rec.PoolServiceID, rec.Fingerprint, found
}

// ForgetPoolCredentialRecord removes pool's record, once a pair keyed by the
// pool keeps its fingerprint. An acquire that finds none writes the credential
// afresh and records it again, for the next pass to take over.
func ForgetPoolCredentialRecord(stateDir, pool string) error {
	return forgetPoolCredential(stateDir, pool)
}

// LockPoolCredential takes pool's credential lock, the one every acquire on
// the pool decides and writes the credential under (lockPoolCredential): the
// pass renewing a pool keying a pair holds it from its re-read of the per-pool
// record through its write. The caller MUST call the returned release
// function.
func LockPoolCredential(ctx context.Context, stateDir, pool string) (func(), error) {
	return lockPoolCredential(ctx, stateDir, pool)
}
