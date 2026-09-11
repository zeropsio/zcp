package farm

import "regexp"

// batchIDPattern and runIDPattern implement FM-47's id grammar
// (docs/spec-eval-farm.md §7.6): a batch id is lowercase alphanumeric with
// hyphens, starting with an alphanumeric, up to 63 characters; a run id is
// `<batch>-<scenarioId>` over the same alphabet, up to 128 characters.
var (
	batchIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	runIDPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,127}$`)
)

// ValidBatchID reports whether id matches the batch-id grammar (§7.6 FM-47).
func ValidBatchID(id string) bool {
	return batchIDPattern.MatchString(id)
}

// ValidRunID reports whether id matches the run-id grammar (§7.6 FM-47):
// `<batch>-<scenarioId>`, `^[a-z0-9][a-z0-9-]{0,127}$`.
func ValidRunID(id string) bool {
	return runIDPattern.MatchString(id)
}
