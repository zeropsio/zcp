package analyze

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Workflow action sentinel — used by several bars to filter scan
// contents. Defined as a constant so the goconst linter is happy and
// refactoring is a single-edit operation.
const actionComplete = "complete"

// AtomRootDefault names the embedded atom corpus relative to the repo
// root. The CLI uses it as the default for B-22 scans; tests pass a
// fixture directory.
const AtomRootDefault = "internal/content/workflows/recipe/briefs"

// RunRecipeAnalysis executes every Tier-1 bar against the supplied
// inputs and returns a fully populated MachineReport. The function is
// pure (no side effects beyond file reads) so the CLI wrapper handles
// stdout/stderr and exit codes.
func RunRecipeAnalysis(in ReportInput) (*MachineReport, error) {
	if in.DeliverableDir == "" {
		return nil, fmt.Errorf("deliverable dir required")
	}
	if _, err := os.Stat(in.DeliverableDir); err != nil {
		return nil, fmt.Errorf("deliverable %q: %w", in.DeliverableDir, err)
	}
	if in.AppCodebases == nil {
		in.AppCodebases = DiscoverCodebases(in.DeliverableDir)
	}

	r := &MachineReport{
		Run:              in.Run,
		GeneratedAt:      time.Now().UTC().Format("2006-01-02T15:04:05Z07:00"),
		GeneratorVersion: "v8.109.0-harness",
		Tier:             in.Tier,
		Slug:             in.Slug,
		DeliverableDir:   in.DeliverableDir,
		SessionsLogsDir:  in.SessionsLogsDir,
		SchemaVersion:    SchemaVersion,
	}

	r.StructuralIntegrity = StructuralIntegrity{
		GhostEnvDirs:             CheckGhostEnvDirs(in.DeliverableDir),
		TarballPerCodebaseMd:     CheckPerCodebaseMarkdown(in.DeliverableDir, in.AppCodebases),
		MarkerExactForm:          CheckMarkerExactForm(in.DeliverableDir),
		StandaloneDuplicateFiles: CheckStandaloneDuplicateFiles(in.DeliverableDir),
		AtomTemplateVarsBound:    CheckAtomTemplateVarsBound(AtomRootDefault, DefaultAllowedAtomFields),
	}

	if in.SessionsLogsDir != "" {
		if _, err := os.Stat(in.SessionsLogsDir); err == nil {
			scan, err := ScanSessions(in.SessionsLogsDir)
			if err != nil {
				return nil, fmt.Errorf("scan sessions: %w", err)
			}
			r.SessionMetrics = ComputeSessionMetrics(scan)
			// Merge the retrospective-evidence bars (F-12, F-13) into
			// the structural_integrity block as extra evidence. The
			// two are exposed as named top-level fields via JSON tags.
			markerFix := CheckMarkerFixEditCycles(scan)
			standAuth := CheckStandaloneFileAuthorship(scan)
			r.SessionMetrics.RetryCycleAttributions = buildRetryCycles(scan)
			// Surface evidence bars alongside structural ones: they
			// share an output section in the verdict so co-locating
			// them makes citation paths consistent.
			r.StructuralIntegrity.MarkerExactForm = mergeEvidence(r.StructuralIntegrity.MarkerExactForm, markerFix, "session-log Edit cycles")
			r.StructuralIntegrity.StandaloneDuplicateFiles = mergeEvidence(r.StructuralIntegrity.StandaloneDuplicateFiles, standAuth, "session-log Write authorship")
		}
	}

	r.WriterReadmes, r.WriterClaudeMd = CollectWriterCompliance(in.DeliverableDir, in.AppCodebases)
	r.DispatchIntegrity = buildDispatchIntegrity(in.SessionsLogsDir)
	return r, nil
}

// mergeEvidence combines a structural-scan bar with a JSONL-derived
// evidence bar. The resulting bar fails when EITHER signal fails; the
// observed count uses the maximum; evidence files concatenate with a
// label so an analyst can trace which source surfaced each hit.
func mergeEvidence(primary, secondary BarResult, secondaryLabel string) BarResult {
	merged := primary
	if secondary.Status == StatusFail && primary.Status != StatusFail {
		merged.Status = StatusFail
	}
	if secondary.Observed > primary.Observed {
		merged.Observed = secondary.Observed
	}
	for _, f := range secondary.EvidenceFiles {
		merged.EvidenceFiles = append(merged.EvidenceFiles, secondaryLabel+": "+f)
	}
	if merged.Description == "" && secondary.Description != "" {
		merged.Description = secondary.Description
	}
	return merged
}

// buildRetryCycles collects every failing workflow-step complete
// response into RetryCycle records. Attribution is left blank for
// analyst fill via the checklist.
func buildRetryCycles(scan *SessionScan) []RetryCycle {
	var cycles []RetryCycle
	cycle := 0
	for _, wc := range scan.WorkflowCalls {
		if wc.Input.Action != actionComplete {
			continue
		}
		cr, ok := scan.CheckResultsByCallID[wc.ID]
		if !ok || cr.Passed {
			continue
		}
		cycle++
		cycles = append(cycles, RetryCycle{
			Cycle:         cycle,
			Timestamp:     wc.Timestamp,
			Substep:       wc.Input.Step + "/" + wc.Input.Substep,
			FailingChecks: cr.FailingChecks,
		})
	}
	return cycles
}

// buildDispatchIntegrity walks sub-agent meta files and records a
// skeletal DispatchResult per role. Full byte-diff (B-24) is out of
// scope for the v1 harness — a placeholder status of "unverified"
// forces the analyst to cite the diff explicitly in the checklist.
func buildDispatchIntegrity(sessionsLogsDir string) map[string]DispatchResult {
	roles, err := LoadSubAgentRoleMap(sessionsLogsDir)
	if err != nil || len(roles) == 0 {
		return map[string]DispatchResult{}
	}
	out := make(map[string]DispatchResult, len(roles))
	for file, desc := range roles {
		out[desc] = DispatchResult{
			DiffStatus:             "unverified",
			TemplateVarsResolved:   false,
			UnresolvedTemplateVars: nil,
		}
		_ = file
	}
	return out
}

// DiscoverCodebases heuristically enumerates first-level subdirectories
// that look like codebase dirs (contain a zerops.yaml OR a package.json
// OR end in "dev"). Used when the caller doesn't specify
// AppCodebases explicitly.
func DiscoverCodebases(deliverableDir string) []string {
	entries, err := os.ReadDir(deliverableDir)
	if err != nil {
		return nil
	}
	var codebases []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if name == "environments" || name == "SESSIONS_LOGS" || name == "subagents" || name == ".git" {
			continue
		}
		sub := filepath.Join(deliverableDir, name)
		// Signal 1: has zerops.yaml or package.json.
		if _, err := os.Stat(filepath.Join(sub, "zerops.yaml")); err == nil {
			codebases = append(codebases, name)
			continue
		}
		if _, err := os.Stat(filepath.Join(sub, "package.json")); err == nil {
			codebases = append(codebases, name)
			continue
		}
		// Signal 2: conventional -dev suffix (apidev, appdev, workerdev).
		if len(name) > 3 && name[len(name)-3:] == "dev" {
			codebases = append(codebases, name)
		}
	}
	sort.Strings(codebases)
	return codebases
}

// WriteReport serializes the report as pretty-printed JSON to w.
func WriteReport(w io.Writer, r *MachineReport) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}
