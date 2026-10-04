package hq

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/zeropsio/zcp/internal/runtime"
)

// The states an outcome names: enrolled; no single official HQ in the org;
// HQ's own refusal, with its code.
const (
	OutcomeEnrolled = "enrolled"
	OutcomeNoHQ     = "no_hq"
	OutcomeRefused  = "refused"
)

// Outcome is the keep loop's last word on enrolling, for the Mate server to
// say why its setup waits (spec-mate §2.8): beside the enrollment, and never
// carrying the credential.
type Outcome struct {
	State string    `json:"state"`
	Code  string    `json:"code,omitempty"`
	At    time.Time `json:"at"`
}

// OutcomePath is where the outcome lives: beside the enrollment.
func OutcomePath() string {
	return filepath.Join(runtime.HomeDir(), ".zcp", "hq", "outcome.json")
}

// OutcomeOf is what an attempt's answer says, at at; recorded is false for an
// answer that says nothing about this Mate — HQ or Zerops not answering —
// which leaves the outcome before it standing.
func OutcomeOf(err error, at time.Time) (o Outcome, recorded bool) {
	var noHQ *NoHQError
	var refused *RefusedError
	switch {
	case err == nil:
		return Outcome{State: OutcomeEnrolled, At: at}, true
	case errors.As(err, &noHQ):
		return Outcome{State: OutcomeNoHQ, At: at}, true
	case errors.As(err, &refused):
		return Outcome{State: OutcomeRefused, Code: refused.Code, At: at}, true
	}
	return Outcome{}, false
}

// SaveOutcome writes o to path in a single rename.
func SaveOutcome(path string, o Outcome) error {
	if err := saveOwnerOnly(path, o); err != nil {
		return fmt.Errorf("hq outcome: %w", err)
	}
	return nil
}
