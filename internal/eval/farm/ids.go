package farm

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// These patterns implement FM-47's batch, scenario, legacy-run and new-run
// grammars (docs/spec-eval-farm.md §7.6).
var (
	batchIDPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	componentPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	runIDPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,127}$`)
	newRunIDPattern  = regexp.MustCompile(`^r1_[0-9a-z]+_[a-z0-9][a-z0-9-]*_[a-z0-9][a-z0-9-]*$`)
)

// ValidBatchID reports whether id matches the batch-id grammar (§7.6 FM-47).
func ValidBatchID(id string) bool {
	return batchIDPattern.MatchString(id)
}

func ValidScenarioID(id string) bool { return componentPattern.MatchString(id) }

// ValidRunID accepts canonical versioned run IDs and readable legacy IDs.
func ValidRunID(id string) bool {
	if len(id) > 128 {
		return false
	}
	if runIDPattern.MatchString(id) {
		return true
	}
	if !newRunIDPattern.MatchString(id) {
		return false
	}
	batch, scenario, ok := DecodeRunID(id)
	if !ok {
		return false
	}
	canonical, err := EncodeRunID(batch, scenario)
	return err == nil && canonical == id
}

// EncodeRunID creates the versioned, injective identity used by new runs.
// The length prefix makes the batch boundary unambiguous even when either
// component contains hyphens.
func EncodeRunID(batch, scenario string) (string, error) {
	if !ValidBatchID(batch) || !ValidScenarioID(scenario) {
		return "", fmt.Errorf("farm: invalid batch or scenario id")
	}
	id := "r1_" + strconv.FormatInt(int64(len(batch)), 36) + "_" + batch + "_" + scenario
	if len(id) > 128 {
		return "", fmt.Errorf("farm: run id exceeds 128 characters")
	}
	return id, nil
}

// DecodeRunID returns the original batch and scenario for a new ID. Legacy
// IDs deliberately return false because their separator is ambiguous.
func DecodeRunID(id string) (batch, scenario string, ok bool) {
	if !newRunIDPattern.MatchString(id) {
		return "", "", false
	}
	parts := strings.SplitN(id, "_", 4)
	if len(parts) != 4 {
		return "", "", false
	}
	n, err := strconv.ParseInt(parts[1], 36, 64)
	if err != nil || n < 1 || n != int64(len(parts[2])) || !ValidBatchID(parts[2]) || !ValidScenarioID(parts[3]) {
		return "", "", false
	}
	return parts[2], parts[3], true
}
