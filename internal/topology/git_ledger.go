package topology

// LedgerEntry is one zcp deploy record (docs/spec-workflows.md §4.9). It is
// the annotated git tag's message (single-line JSON) on
// DeployTagName(entry.Project, entry.Target, entry.AppVersionID) — the tag
// itself points at SHA. The platform stays the authority for the ACTIVE
// appVersion; "what runs" is a JOIN (active appVersion → the tag of that
// name), never a moving pointer.
type LedgerEntry struct {
	SHA          string `json:"sha"`
	AppVersionID string `json:"appVersionId"`
	Target       string `json:"target"`
	Project      string `json:"project"`
	At           string `json:"at"` // RFC3339
	// Dirty is true when this deploy shipped uncommitted changes on top of
	// SHA (the working-tree path, §4.9: no explicit sha was given, but the
	// source's HEAD was recorded anyway). The tag still names SHA — the
	// clean base commit actually on disk when HEAD was read — never a
	// synthetic "dirty" commit.
	Dirty bool `json:"dirty,omitempty"`
}

// DeployTagName returns the annotated git tag name recording one zcp
// deploy (docs/spec-workflows.md §4.9). projectID + target + appVersionID
// make it unique across concurrent targets and repeat deploys of the same
// commit; keyed by appVersionID (not a timestamp) so a retried deploy of
// the same appVersion overwrites its own tag (-f) instead of accumulating
// duplicates. Never refs/zcp/* — an ordinary annotated tag survives a
// plain `git fetch --tags` the way a custom ref namespace would not, and
// needs no ref-pruning exemption (docs/spec-mate.md §6 governs refs/zcp/*
// only).
func DeployTagName(projectID, target, appVersionID string) string {
	return "zcp/deploy/" + projectID + "/" + target + "/" + appVersionID
}

// DeployTagPrefix returns the tag namespace covering every zcp deploy
// against one (project, target) pair — no trailing slash: git ref
// patterns (for-each-ref, tag -l) already match a prefix up to the next
// slash boundary on their own.
func DeployTagPrefix(projectID, target string) string {
	return "zcp/deploy/" + projectID + "/" + target
}
