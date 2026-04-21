package analyze

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeMainSession writes a synthetic JSONL stream mirroring Claude
// Code's tool_use/tool_result shapes. Each helper constructs the
// minimum JSON a bar function reads.
func writeMainSession(t *testing.T, records []string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "main-session.jsonl")
	body := strings.Join(records, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return dir
}

func assistantToolUse(id, name, inputJSON string) string {
	return `{"type":"assistant","uuid":"u-` + id + `","timestamp":"2026-04-21T00:00:00Z","message":{"content":[{"type":"tool_use","id":"` + id + `","name":"` + name + `","input":` + inputJSON + `}]}}`
}

func userToolResult(callID, resultTextJSON string) string {
	// resultTextJSON is the JSON body (quoted) to embed in the text block.
	return `{"type":"user","uuid":"u-` + callID + `-r","timestamp":"2026-04-21T00:00:01Z","message":{"content":[{"type":"tool_result","tool_use_id":"` + callID + `","content":` + resultTextJSON + `}]}}`
}

// TestCheckDeployReadmesRetryRounds pins B-20: failing deploy-phase
// completions count across all substeps (the engine rolls readmes-
// internal iterations into a single substep response, so phase-wide
// measurement captures the real retry signal).
func TestCheckDeployReadmesRetryRounds(t *testing.T) {
	t.Parallel()
	records := []string{
		assistantToolUse("c1", "mcp__zerops__zerops_workflow", `{"action":"complete","step":"deploy","substep":"readmes"}`),
		userToolResult("c1", `[{"type":"text","text":"{\"checkResult\":{\"passed\":false,\"checks\":[{\"name\":\"fragment_intro\",\"status\":\"fail\"}]}}"}]`),
		assistantToolUse("c2", "mcp__zerops__zerops_workflow", `{"action":"complete","step":"deploy","substep":"verify-dev"}`),
		userToolResult("c2", `[{"type":"text","text":"{\"checkResult\":{\"passed\":false,\"checks\":[{\"name\":\"comment_ratio\",\"status\":\"fail\"}]}}"}]`),
		assistantToolUse("c3", "mcp__zerops__zerops_workflow", `{"action":"complete","step":"deploy","substep":"readmes"}`),
		userToolResult("c3", `[{"type":"text","text":"{\"checkResult\":{\"passed\":true,\"checks\":[]}}"}]`),
		// Different step entirely — must be ignored.
		assistantToolUse("c4", "mcp__zerops__zerops_workflow", `{"action":"complete","step":"finalize","substep":"env-yamls"}`),
		userToolResult("c4", `[{"type":"text","text":"{\"checkResult\":{\"passed\":false,\"checks\":[{\"name\":\"x\",\"status\":\"fail\"}]}}"}]`),
	}
	dir := writeMainSession(t, records)
	scan, err := ScanSessions(dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	got := CheckDeployReadmesRetryRounds(scan, 2)
	if got.Observed != 2 {
		t.Errorf("observed=%d want=2 (failing_checks=%+v)", got.Observed, scan.CheckResultsByCallID)
	}
	if got.Status != StatusPass {
		t.Errorf("status=%s want=pass (2 <= threshold 2)", got.Status)
	}

	gotFail := CheckDeployReadmesRetryRounds(scan, 1)
	if gotFail.Status != StatusFail {
		t.Errorf("tight threshold status=%s want=fail", gotFail.Status)
	}
}

// TestCheckSessionlessExportAttempts pins B-21.
func TestCheckSessionlessExportAttempts(t *testing.T) {
	t.Parallel()
	records := []string{
		assistantToolUse("b1", "Bash", `{"command":"zcp sync recipe export /var/www/zcprecipator/foo"}`),
		assistantToolUse("b2", "Bash", `{"command":"zcp sync recipe export /var/www/zcprecipator/foo --session abc"}`),
		assistantToolUse("b3", "Bash", `{"command":"ZCP_SESSION_ID=xyz zcp sync recipe export /var/www/zcprecipator/foo"}`),
		assistantToolUse("b4", "Bash", `{"command":"ls /var/www"}`),
	}
	dir := writeMainSession(t, records)
	scan, err := ScanSessions(dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	got := CheckSessionlessExportAttempts(scan)
	if got.Observed != 1 {
		t.Errorf("observed=%d want=1 evidence=%v", got.Observed, got.EvidenceFiles)
	}
	if got.Status != StatusFail {
		t.Errorf("status=%s want=fail", got.Status)
	}
}

// TestCheckMarkerFixEditCycles pins the F-12 retrospective bar.
func TestCheckMarkerFixEditCycles(t *testing.T) {
	t.Parallel()
	records := []string{
		// Marker fix: old missing `#`, new has `#`.
		assistantToolUse("e1", "Edit", `{"file_path":"/var/www/apidev/README.md","old_string":"<!-- #ZEROPS_EXTRACT_START:intro -->","new_string":"<!-- #ZEROPS_EXTRACT_START:intro# -->"}`),
		// Unrelated edit: neither string carries a marker.
		assistantToolUse("e2", "Edit", `{"file_path":"/var/www/apidev/CLAUDE.md","old_string":"foo","new_string":"bar"}`),
		// Another marker fix on a different key.
		assistantToolUse("e3", "Edit", `{"file_path":"/var/www/workerdev/README.md","old_string":"<!-- #ZEROPS_EXTRACT_END:knowledge-base -->","new_string":"<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->"}`),
	}
	dir := writeMainSession(t, records)
	scan, err := ScanSessions(dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	got := CheckMarkerFixEditCycles(scan)
	if got.Observed != 2 {
		t.Errorf("observed=%d want=2 evidence=%v", got.Observed, got.EvidenceFiles)
	}
	if got.Status != StatusFail {
		t.Errorf("status=%s want=fail", got.Status)
	}
}

// TestCheckStandaloneFileAuthorship pins the F-13 retrospective bar.
func TestCheckStandaloneFileAuthorship(t *testing.T) {
	t.Parallel()
	records := []string{
		assistantToolUse("w1", "Write", `{"file_path":"/var/www/apidev/INTEGRATION-GUIDE.md","content":"x"}`),
		assistantToolUse("w2", "Write", `{"file_path":"/var/www/workerdev/GOTCHAS.md","content":"x"}`),
		assistantToolUse("w3", "Write", `{"file_path":"/var/www/appdev/README.md","content":"ok"}`),
	}
	dir := writeMainSession(t, records)
	scan, err := ScanSessions(dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	got := CheckStandaloneFileAuthorship(scan)
	if got.Observed != 2 {
		t.Errorf("observed=%d want=2 evidence=%v", got.Observed, got.EvidenceFiles)
	}
}

// TestCheckWriterFirstPassFailures pins B-23 — counts distinct failing
// checks in the first readmes-substep checkResult after a writer-
// described Agent dispatch.
func TestCheckWriterFirstPassFailures(t *testing.T) {
	t.Parallel()
	records := []string{
		assistantToolUse("a1", "Agent", `{"description":"Recipe writer sub-agent","subagent_type":"general-purpose","prompt":"..."}`),
		assistantToolUse("c1", "mcp__zerops__zerops_workflow", `{"action":"complete","step":"deploy","substep":"readmes"}`),
		userToolResult("c1", `[{"type":"text","text":"{\"checkResult\":{\"passed\":false,\"checks\":[{\"name\":\"fragment_intro\",\"status\":\"fail\"},{\"name\":\"fragment_integration-guide\",\"status\":\"fail\"},{\"name\":\"fragment_knowledge-base\",\"status\":\"fail\"}]}}"}]`),
	}
	dir := writeMainSession(t, records)
	scan, err := ScanSessions(dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	got := CheckWriterFirstPassFailures(scan, 3)
	if got.Observed != 3 {
		t.Errorf("observed=%d want=3 evidence=%v", got.Observed, got.EvidenceFiles)
	}
	if got.Status != StatusPass {
		t.Errorf("status=%s want=pass (3 <= threshold 3)", got.Status)
	}
}

// TestComputeSessionMetrics exercises the aggregation layer — role
// dispatch + close-step completion.
func TestComputeSessionMetrics(t *testing.T) {
	t.Parallel()
	records := []string{
		assistantToolUse("a1", "Agent", `{"description":"Recipe writer sub-agent"}`),
		assistantToolUse("a2", "Agent", `{"description":"editorial-review pass"}`),
		assistantToolUse("a3", "Agent", `{"description":"code-review sub-agent"}`),
		assistantToolUse("c1", "mcp__zerops__zerops_workflow", `{"action":"complete","step":"close"}`),
		userToolResult("c1", `[{"type":"text","text":"{\"checkResult\":{\"passed\":true,\"checks\":[]}}"}]`),
	}
	dir := writeMainSession(t, records)
	scan, err := ScanSessions(dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	got := ComputeSessionMetrics(scan)
	if got.SubAgentCount != 3 {
		t.Errorf("sub_agent_count=%d want=3", got.SubAgentCount)
	}
	if !got.EditorialReviewDispatched {
		t.Errorf("editorial_review_dispatched=false want=true")
	}
	if !got.CodeReviewDispatched {
		t.Errorf("code_review_dispatched=false want=true")
	}
	if !got.CloseStepCompleted {
		t.Errorf("close_step_completed=false want=true")
	}
}
