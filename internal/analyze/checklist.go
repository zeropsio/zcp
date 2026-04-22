package analyze

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// ChecklistFromReport emits a Markdown worksheet bound to r.
// Structural + session-metric rows are pre-filled from the report;
// content-quality rows carry `<analyst-fill>` placeholders the
// analyst replaces before commit. The format is stable so the
// commit-hook pending-cell counter has a deterministic grep target.
//
// machineReportSHA is included in the front matter so the pre-commit
// hook can cross-check.
func ChecklistFromReport(w io.Writer, r *MachineReport, machineReportSHA string) error {
	b := &strings.Builder{}
	writeChecklistHeader(b, r, machineReportSHA)
	writePhaseReached(b, r)
	writeStructuralRows(b, r)
	writeSessionMetricRows(b, r)
	writeCloseDispatchRows(b, r)
	writeWriterQualityRows(b, r)
	writeRetryAttribution(b, r)
	writeFinalVerification(b)
	_, err := io.WriteString(w, b.String())
	return err
}

func writeChecklistHeader(b *strings.Builder, r *MachineReport, sha string) {
	fmt.Fprintf(b, "# runs/%s verification checklist\n\n", r.Run)
	fmt.Fprintf(b, "**Machine report SHA**: `%s`\n", sha)
	fmt.Fprintf(b, "**Generated at**: %s\n", r.GeneratedAt)
	fmt.Fprintf(b, "**Tier**: %s\n", r.Tier)
	fmt.Fprintf(b, "**Slug**: %s\n", r.Slug)
	fmt.Fprintf(b, "**Deliverable**: `%s`\n", r.DeliverableDir)
	b.WriteString("**Analyst**: <analyst-fill>\n")
	b.WriteString("**Analyst session start (UTC)**: <analyst-fill>\n\n")
}

func writePhaseReached(b *strings.Builder, r *MachineReport) {
	b.WriteString("## Phase reached\n\n")
	check := func(ok bool) string {
		if ok {
			return "x"
		}
		return " "
	}
	fmt.Fprintf(b, "- [%s] `close` complete (auto)\n", check(r.SessionMetrics.CloseStepCompleted))
	fmt.Fprintf(b, "- [%s] editorial-review dispatched (auto)\n", check(r.SessionMetrics.EditorialReviewDispatched))
	fmt.Fprintf(b, "- [%s] code-review dispatched (auto)\n", check(r.SessionMetrics.CodeReviewDispatched))
	fmt.Fprintf(b, "- [%s] close-browser-walk attempted (auto)\n\n", check(r.SessionMetrics.CloseBrowserWalkAttempted))
	b.WriteString("If `close` is not complete, downstream cells must be `unmeasurable-valid` with explicit justification. If `close` IS complete, no downstream cell may be `unmeasurable`.\n\n")
}

func writeStructuralRows(b *strings.Builder, r *MachineReport) {
	b.WriteString("## Structural integrity bars (auto)\n\n")
	rows := []struct {
		id   string
		name string
		res  BarResult
	}{
		{"B-15", "ghost_env_dirs", r.StructuralIntegrity.GhostEnvDirs},
		{"B-16", "tarball_per_codebase_md", r.StructuralIntegrity.TarballPerCodebaseMd},
		{"B-17", "marker_exact_form", r.StructuralIntegrity.MarkerExactForm},
		{"B-18", "standalone_duplicate_files", r.StructuralIntegrity.StandaloneDuplicateFiles},
		{"B-22", "atom_template_vars_bound", r.StructuralIntegrity.AtomTemplateVarsBound},
	}
	for _, row := range rows {
		fmt.Fprintf(b, "- [x] %s %s: threshold %d, observed %d, **status %s**",
			row.id, row.name, row.res.Threshold, row.res.Observed, row.res.Status)
		if len(row.res.EvidenceFiles) > 0 {
			fmt.Fprintf(b, " — evidence: %s", FormatEvidencePaths(row.res.EvidenceFiles))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

func writeSessionMetricRows(b *strings.Builder, r *MachineReport) {
	b.WriteString("## Session-metric bars (auto)\n\n")
	rows := []struct {
		id   string
		name string
		res  BarResult
	}{
		{"B-20", "deploy_readmes_retry_rounds", r.SessionMetrics.DeployReadmesRetryRounds},
		{"B-21", "sessionless_export_attempts", r.SessionMetrics.SessionlessExportAttempts},
		{"B-23", "writer_first_pass_failures", r.SessionMetrics.WriterFirstPassFailures},
		{"B-24", "dispatch_integrity", r.SessionMetrics.DispatchIntegrity},
	}
	for _, row := range rows {
		fmt.Fprintf(b, "- [x] %s %s: threshold %d, observed %d, **status %s**",
			row.id, row.name, row.res.Threshold, row.res.Observed, row.res.Status)
		if len(row.res.EvidenceFiles) > 0 {
			fmt.Fprintf(b, " — evidence: %s", FormatEvidencePaths(row.res.EvidenceFiles))
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(b, "- sub_agent_count: %d\n\n", r.SessionMetrics.SubAgentCount)
}

func writeCloseDispatchRows(b *strings.Builder, r *MachineReport) {
	if len(r.DispatchIntegrity) == 0 {
		return
	}
	b.WriteString("## Dispatch integrity (analyst-fill for diff_status)\n\n")
	b.WriteString("Byte-diff each captured Agent dispatch prompt against `BuildXxxDispatchBrief(plan)` output. Status `clean` or `divergent`. Divergent dispatches must list root cause.\n\n")
	names := make([]string, 0, len(r.DispatchIntegrity))
	for k := range r.DispatchIntegrity {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, n := range names {
		res := r.DispatchIntegrity[n]
		fmt.Fprintf(b, "- **%s**:\n", n)
		fmt.Fprintf(b, "  - [ ] dispatch_vs_source_diff: Status: <analyst-fill>\n")
		if res.DiffStatus != "" {
			fmt.Fprintf(b, "  - auto diff_status: %s\n", res.DiffStatus)
		}
		fmt.Fprintf(b, "  - [ ] Read-receipt: <analyst-fill>\n")
	}
	b.WriteString("\n")
}

func writeWriterQualityRows(b *strings.Builder, r *MachineReport) {
	if len(r.WriterReadmes) == 0 && len(r.WriterClaudeMd) == 0 {
		return
	}
	b.WriteString("## Writer content quality (analyst-fill, required)\n\n")
	keys := make([]string, 0, len(r.WriterReadmes))
	for k := range r.WriterReadmes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		rr := r.WriterReadmes[k]
		fmt.Fprintf(b, "### %s\n\n", k)
		if !rr.FileExists {
			fmt.Fprintf(b, "- [ ] File presence: **fail** (missing — likely stranded by F-10)\n")
			fmt.Fprintf(b, "- [ ] Read-receipt: `unmeasurable-valid` (file absent)\n\n")
			continue
		}
		writeFragmentRow(b, "intro", rr.IntroFragment)
		writeFragmentRow(b, "integration-guide", rr.IntegrationGuideFragment)
		writeFragmentRow(b, "knowledge-base", rr.KnowledgeBaseFragment)
		b.WriteString("- [ ] Read-receipt: <analyst-fill timestamp>\n\n")
	}
	keys = keys[:0]
	for k := range r.WriterClaudeMd {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		cc := r.WriterClaudeMd[k]
		fmt.Fprintf(b, "### %s\n\n", k)
		if !cc.FileExists {
			fmt.Fprintf(b, "- [ ] File presence: **fail** (missing — likely stranded by F-10)\n")
			fmt.Fprintf(b, "- [ ] Read-receipt: `unmeasurable-valid` (file absent)\n\n")
			continue
		}
		fmt.Fprintf(b, "- [x] size ≥ 1200 bytes: %v (observed %d bytes)\n", cc.SizeGE1200, cc.SizeBytes)
		fmt.Fprintf(b, "- [x] base sections present: %v (want 4)\n", cc.BaseSectionsPresent)
		fmt.Fprintf(b, "- [x] custom sections ≥ 2: %v (observed %d)\n", cc.CustomSectionsGE2, cc.CustomSectionCount)
		fmt.Fprintf(b, "- auto **status %s**\n", cc.Status)
		b.WriteString("- [ ] Analyst narrative sign-off (≤ 2 sentences): <analyst-fill>\n")
		b.WriteString("- [ ] Read-receipt: <analyst-fill timestamp>\n\n")
	}
}

func writeFragmentRow(b *strings.Builder, name string, fc FragmentCompliance) {
	fmt.Fprintf(b, "- **%s fragment** — ", name)
	fmt.Fprintf(b, "auto markers_present=%v exact_form=%v h3=%d bullets=%d: **status %s**\n",
		fc.MarkersPresent, fc.MarkersExactForm, fc.H3Count, fc.GotchaBulletCount, fc.Status)
	b.WriteString("  - [ ] Analyst qualitative grade: <analyst-fill>\n")
}

func writeRetryAttribution(b *strings.Builder, r *MachineReport) {
	if len(r.SessionMetrics.RetryCycleAttributions) == 0 {
		return
	}
	b.WriteString("## Retry-cycle attribution (analyst-fill)\n\n")
	b.WriteString("| Cycle | Timestamp | Substep | Failing checks | Attribution |\n")
	b.WriteString("|---|---|---|---|---|\n")
	for _, rc := range r.SessionMetrics.RetryCycleAttributions {
		checks := FormatEvidencePaths(rc.FailingChecks)
		if len(checks) > 80 {
			checks = checks[:77] + "..."
		}
		attr := rc.Attribution
		if attr == "" {
			attr = "<analyst-fill>"
		}
		fmt.Fprintf(b, "| %d | %s | %s | %s | %s |\n", rc.Cycle, rc.Timestamp, rc.Substep, checks, attr)
	}
	b.WriteString("\n")
}

func writeFinalVerification(b *strings.Builder) {
	b.WriteString("## Final verification\n\n")
	b.WriteString("- [ ] All cells are non-`pending`\n")
	b.WriteString("- [ ] Every Read-receipt timestamp is after analyst session start\n")
	b.WriteString("- [ ] No `unmeasurable-invalid` cells\n")
	b.WriteString("- [ ] Machine-report SHA matches file content\n")
	b.WriteString("- [ ] Checklist SHA matches file content\n\n")
	b.WriteString("**Analyst sign-off**: <analyst-fill name, timestamp>\n")
}

// Sha256File returns the hex-encoded SHA256 of the file at path. Used
// by the CLI to embed SHAs into the checklist front matter and by the
// pre-commit hook to validate that verdict.md references the current
// machine-report + checklist bytes.
func Sha256File(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
