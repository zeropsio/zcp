package eval

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// CheckResult is the verdict of one required-mode result row, or the
// aggregated task result. See docs/spec-testing-architecture.md §10.1.
type CheckResult string

const (
	CheckPassed  CheckResult = "passed"
	CheckFailed  CheckResult = "failed"
	CheckBlocked CheckResult = "blocked"
	CheckNotRun  CheckResult = "not-run"
)

// RequiredCheck is one result row — the single owner of the verdict for one
// executable check. Field names/JSON tags are pinned by
// docs/spec-testing-architecture.md §10.1's row table.
type RequiredCheck struct {
	ID         string      `json:"id"`
	Check      string      `json:"check"`
	Scope      string      `json:"scope"`
	Result     CheckResult `json:"result"`
	Expected   string      `json:"expected,omitempty"`
	Observed   string      `json:"observed,omitempty"`
	ObservedAt time.Time   `json:"observedAt"`
	Source     string      `json:"source,omitempty"`
	Message    string      `json:"message,omitempty"`
}

// TaskOutcome is the frozen task-result dimension of BehavioralResult /
// meta.json. See docs/spec-testing-architecture.md §10.1.
type TaskOutcome struct {
	Mode     string      `json:"mode"`
	Result   CheckResult `json:"result"`
	FrozenAt time.Time   `json:"frozenAt"`
}

// TaskEndSimulator records how the user-sim loop stopped. Recorded, never
// graded — a DONE classification is not itself a pass.
type TaskEndSimulator struct {
	TerminatedBy string `json:"terminatedBy,omitempty"`
	Error        string `json:"error,omitempty"`
}

// TaskEndEvidence is the task-end-freeze dimension of BehavioralResult /
// meta.json. See docs/spec-testing-architecture.md §10.2.
type TaskEndEvidence struct {
	ObservedAt    time.Time        `json:"observedAt"`
	Settled       bool             `json:"settled"`
	LiveProcesses []string         `json:"liveProcesses,omitempty"`
	Simulator     TaskEndSimulator `json:"simulator"`
	Persisted     bool             `json:"persisted"`
	PersistError  string           `json:"persistError,omitempty"`
}

// aggregateTaskResult combines result rows into one task result per
// docs/spec-testing-architecture.md §10.1: any failed → failed; else any
// blocked → blocked; else every row passed → passed; no rows (no task work)
// → not-run.
func aggregateTaskResult(rows []RequiredCheck) CheckResult {
	if len(rows) == 0 {
		return CheckNotRun
	}
	sawBlocked := false
	for _, row := range rows {
		switch row.Result {
		case CheckFailed:
			return CheckFailed
		case CheckBlocked:
			sawBlocked = true
		case CheckPassed, CheckNotRun:
		}
	}
	if sawBlocked {
		return CheckBlocked
	}
	return CheckPassed
}

// expectedServiceRowID builds the stable row id for one expected-service
// sub-check, per docs/spec-testing-architecture.md §10.1's row-id table.
func expectedServiceRowID(hostname, subcheck string) string {
	return fmt.Sprintf("expected_service/%s/%s", hostname, subcheck)
}

// noFailedProcessesRowID builds the stable row id for the no-failed-processes
// check, per docs/spec-testing-architecture.md §10.1's row-id table.
func noFailedProcessesRowID(projectID string) string {
	return "no_failed_processes/" + projectID
}

// VerificationDocumentFormat2 is the formatVersion of the single-object
// verification.json contract (docs/spec-testing-architecture.md §10.1).
const VerificationDocumentFormat2 = "zcp-eval-verification-2"

// VerificationDocument is the single-object verification.json shape written
// in both observe and required mode. Replaces the retired bare-array format.
type VerificationDocument struct {
	FormatVersion string                `json:"formatVersion"`
	Mode          string                `json:"mode"`
	Result        CheckResult           `json:"result"`
	FrozenAt      time.Time             `json:"frozenAt"`
	Checks        []RequiredCheck       `json:"checks"`
	Advisory      []VerificationFinding `json:"advisory"`
}

// WriteVerificationDocument serializes doc to verification.json in outDir
// via temp+fsync+rename (§10.2 step 5). Replaces the retired
// WriteVerificationFindings bare-array writer — the single-object format is
// written in both modes.
func WriteVerificationDocument(outDir string, doc VerificationDocument) error {
	if doc.Checks == nil {
		doc.Checks = []RequiredCheck{}
	}
	if doc.Advisory == nil {
		doc.Advisory = []VerificationFinding{}
	}
	if err := writeJSONAtomic(outDir, "verification.json", doc); err != nil {
		return fmt.Errorf("write verification.json: %w", err)
	}
	return nil
}

// notRunRows builds the declared-but-unevaluated row set for a scenario
// whose task work never completed (execution failed before agent.initial
// finished). Every row a fully-evaluated run would have produced is present,
// marked CheckNotRun, per docs/spec-testing-architecture.md §10.1 point 7.
func notRunRows(sc *Scenario, projectID string) []RequiredCheck {
	if sc == nil || sc.Verification == nil {
		return nil
	}
	var rows []RequiredCheck
	for _, exp := range sc.Verification.ExpectedServices {
		hasSubcheck := false
		if len(exp.Status) > 0 {
			rows = append(rows, RequiredCheck{ID: expectedServiceRowID(exp.Hostname, "status"), Check: "service_status", Scope: exp.Hostname, Result: CheckNotRun})
			hasSubcheck = true
		}
		if exp.Type != "" {
			rows = append(rows, RequiredCheck{ID: expectedServiceRowID(exp.Hostname, "type"), Check: "service_type", Scope: exp.Hostname, Result: CheckNotRun})
			hasSubcheck = true
		}
		if exp.SubdomainProbe != nil {
			rows = append(rows, RequiredCheck{ID: expectedServiceRowID(exp.Hostname, "subdomain_probe"), Check: "subdomain_probe", Scope: exp.Hostname, Result: CheckNotRun})
			hasSubcheck = true
		}
		if !hasSubcheck {
			rows = append(rows, RequiredCheck{ID: expectedServiceRowID(exp.Hostname, "exists"), Check: "expected_service", Scope: exp.Hostname, Result: CheckNotRun})
		}
	}
	if sc.Verification.NoFailedProcesses {
		rows = append(rows, RequiredCheck{ID: noFailedProcessesRowID(projectID), Check: "no_failed_processes", Scope: projectID, Result: CheckNotRun})
	}
	return rows
}

// writeJSONAtomic marshals v and writes it to filepath.Join(dir, name) via
// temp-file + fsync + rename, per docs/spec-testing-architecture.md §10.2
// step 5. Returns an error describing what failed without removing any file
// already successfully written.
func writeJSONAtomic(dir, name string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", name, err)
	}
	data = append(data, '\n')
	path := filepath.Join(dir, name)
	temp, err := os.CreateTemp(dir, "."+name+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp for %s: %w", name, err)
	}
	tempPath := temp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("chmod temp for %s: %w", name, err)
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write temp for %s: %w", name, err)
	}
	if err := errors.Join(temp.Sync(), temp.Close()); err != nil {
		return fmt.Errorf("sync/close temp for %s: %w", name, err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("rename into place %s: %w", name, err)
	}
	removeTemp = false
	return nil
}
