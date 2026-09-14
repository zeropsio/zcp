package topology

// LedgerEntry is one deploy-from-commit ledger record (docs/spec-workflows.md
// §4.5). It is written as a single-line JSON commit message on a
// refs/zcp/deploy/<n> ref, so `git log refs/zcp/deploy/*` is the ledger's
// human-readable history.
type LedgerEntry struct {
	SHA          string `json:"sha"`
	AppVersionID string `json:"appVersionId"`
	Target       string `json:"target"`
	Project      string `json:"project"`
	At           string `json:"at"` // RFC3339
}

// DeployRefPrefix is the git ref namespace holding one ref per
// deploy-from-commit attempt (refs/zcp/deploy/<unix-nanos>). refs/zcp/* are
// never pruned by mate (docs/spec-mate.md §6).
const DeployRefPrefix = "refs/zcp/deploy/"

// EnvRefName returns the git ref that records which commit currently runs
// on the given target hostname (refs/zcp/env/<hostname>).
func EnvRefName(hostname string) string {
	return "refs/zcp/env/" + hostname
}
