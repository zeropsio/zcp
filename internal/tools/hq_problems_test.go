package tools

import (
	"encoding/json"
	"maps"
	"os"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/workflow"
)

const actionableProblem = `{"code":"recipe_invalid","what":"The declared minimum CPU is invalid","where":{"appId":"app-1","projectId":"project-1","environment":"Production","service":"api","tier":"production","file":"production/import.yaml","repo":"group","change":7,"head":"bad-head","releaseTag":"v1.2.3","operationId":"operation-1"},"rule":{"source":"zerops-schema","code":"minimum","schemaUrl":"https://schema.example/import.json","schemaDigest":"sha256:abc","schemaPath":"/properties/minCpu/minimum","instancePath":"/services/0/minCpu"},"next":{"actor":"Mate","action":"Correct the minimum CPU and propose the corrected tier","target":"production/import.yaml"},"evidence":{"processId":"process-1","appVersionId":"version-1","logUrl":"https://logs.example/process-1"}}`

// problemState writes the link frame the tool will read.
func problemState(t *testing.T, lab *hqLab, fields string) {
	t.Helper()
	lab.snapshotMate(t.Context())
	raw, err := os.ReadFile(lab.rt.HQ.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Link string                     `json:"link"`
		Mate map[string]json.RawMessage `json:"mate"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	var extra map[string]json.RawMessage
	if err := json.Unmarshal([]byte(fields), &extra); err != nil {
		t.Fatal(err)
	}
	maps.Copy(doc.Mate, extra)
	raw, err = json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lab.rt.HQ.StatePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func problemToolResult(t *testing.T, lab *hqLab, fields string, develop bool) string {
	t.Helper()
	problemState(t, lab, fields)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, lab.mock, lab.hq.srv.Client(), labMate, nil,
		workflow.NewEngine(lab.stateDir, workflow.EnvContainer, nil), nil, lab.stateDir, "", nil, lab.ssh, lab.rt, "")
	input := map[string]any{"action": "status"}
	if develop {
		input = map[string]any{"action": "start", "workflow": "develop", "intent": "fix the login", "scope": []string{"appdev"}}
	}
	result := callTool(t, srv, "zerops_workflow", input)
	var text strings.Builder
	for _, content := range result.Content {
		if block, ok := content.(*mcp.TextContent); ok {
			text.WriteString(block.Text + "\n")
		}
	}
	resultText := text.String()
	if result.IsError && strings.Contains(resultText, "zcp-envelope") {
		t.Error("ASSERTION: an error with HQ findings must not fabricate a lifecycle envelope")
	}
	if blocks := strings.Split(resultText, "```json zcp-envelope"); len(blocks) > 1 && strings.Contains(strings.Split(blocks[len(blocks)-1], "```")[0], "The declared minimum CPU is invalid") {
		t.Error("ASSERTION: HQ findings belong to the tool text, off the lifecycle wire")
	}
	return resultText
}

func assertActionableProblem(t *testing.T, text string) {
	t.Helper()
	for _, want := range []string{"The declared minimum CPU is invalid", "app-1", "Production", "api", "production/import.yaml", "zerops-schema", "/properties/minCpu/minimum", "/services/0/minCpu", "Mate", "Correct the minimum CPU and propose the corrected tier", "operation-1", "https://logs.example/process-1"} {
		if !strings.Contains(text, want) {
			t.Errorf("ASSERTION: HQ finding must retain %q in tool output", want)
		}
	}
}

func TestHQProblems_NoOpenChange_StatusAndDevelopReportHQFinding(t *testing.T) {
	// non-parallel: lab setup uses process-wide test credentials.
	for _, develop := range []bool{false, true} {
		name := "status"
		if develop {
			name = "develop"
		}
		t.Run(name, func(t *testing.T) {
			lab := newHQLab(t)
			reads := lab.hq.callCount("GET /api/mate/self")
			text := problemToolResult(t, lab, `{"changes":[],"problems":[`+actionableProblem+`]}`, develop)
			assertActionableProblem(t, text)
			wantReads := 0
			if n := lab.hq.callCount("GET /api/mate/self") - reads; n != wantReads {
				t.Errorf("ASSERTION: %s preserves its existing self passes, got %d", name, n)
			}
		})
	}
}

func TestHQProblems_PresentProblems_EachStatusReadsNewFindings(t *testing.T) {
	// non-parallel: lab setup uses process-wide test credentials.
	lab := newHQLab(t)
	reads := lab.hq.callCount("GET /api/mate/self")
	first := problemToolResult(t, lab, `{"changes":[],"problems":[]}`, false)
	if strings.Contains(first, "The declared minimum CPU is invalid") {
		t.Error("ASSERTION: the first status must not report a finding HQ has not added")
	}
	if n := lab.hq.callCount("GET /api/mate/self") - reads; n != 0 {
		t.Errorf("ASSERTION: status made a network state read to HQ's present empty problems once, got %d", n)
	}
	second := problemToolResult(t, lab, `{"changes":[],"problems":[`+actionableProblem+`]}`, false)
	assertActionableProblem(t, second)
	if n := lab.hq.callCount("GET /api/mate/self") - reads; n != 0 {
		t.Errorf("ASSERTION: two statuses with present problems read HQ twice, got %d", n)
	}
}

func TestHQProblems_NoRepositories_DevelopStillReportsHQFinding(t *testing.T) {
	// non-parallel: lab setup uses process-wide test credentials.
	lab := newHQLab(t)
	lab.stateDir = t.TempDir()
	text := problemToolResult(t, lab, `{"changes":[],"problems":[`+actionableProblem+`]}`, true)
	assertActionableProblem(t, text)
}

func TestHQProblems_RepositoriesWithoutProblems_EachStatusReadsNewFindings(t *testing.T) {
	// non-parallel: lab setup uses process-wide test credentials.
	lab := newHQLab(t)
	reads := lab.hq.callCount("GET /api/mate/self")
	first := problemToolResult(t, lab, `{"changes":[]}`, false)
	if strings.Contains(first, "The declared minimum CPU is invalid") {
		t.Error("ASSERTION: status must not invent a finding when HQ omits problems")
	}
	if n := lab.hq.callCount("GET /api/mate/self") - reads; n != 0 {
		t.Errorf("ASSERTION: status made a network state read to HQ with repositories and omitted problems once, got %d", n)
	}
	second := problemToolResult(t, lab, `{"changes":[],"problems":[`+actionableProblem+`]}`, false)
	assertActionableProblem(t, second)
	if n := lab.hq.callCount("GET /api/mate/self") - reads; n != 0 {
		t.Errorf("ASSERTION: two statuses with repositories read HQ twice despite earlier omitted problems, got %d", n)
	}
}

func TestHQProblems_InvalidGroupHead_StatusReportsValidationFinding(t *testing.T) {
	// non-parallel: lab setup uses process-wide test credentials.
	lab := newHQLab(t)
	text := problemToolResult(t, lab, `{"changes":[{"repo":"group","number":7,"state":"open","head":"bad-head","recipeValidation":{"head":"bad-head","state":"invalid","problems":[`+actionableProblem+`]}}]}`, false)
	assertActionableProblem(t, text)
}

func TestHQProblems_OmittedProblems_StatusOutputUnchanged(t *testing.T) {
	// non-parallel: lab setup uses process-wide test credentials.
	lab := newHQLab(t)
	old := problemToolResult(t, lab, `{}`, false)
	empty := problemToolResult(t, lab, `{"problems":[]}`, false)
	// The envelope's generated timestamp varies; compare the entire prose.
	old = strings.Split(old, "```json zcp-envelope")[0]
	empty = strings.Split(empty, "```json zcp-envelope")[0]
	if old != empty {
		t.Errorf("ASSERTION: an older HQ changes no output\nold:\n%s\nempty:\n%s", old, empty)
	}
	if strings.Contains(old, "HQ problems") {
		t.Error("ASSERTION: omitted findings add no problems section")
	}
}

func TestHQProblems_RepeatedAndDistinctFindings_StatusPreservesHQFindings(t *testing.T) {
	// non-parallel: lab setup uses process-wide test credentials.
	lab := newHQLab(t)
	distinct := strings.ReplaceAll(actionableProblem, "The declared minimum CPU is invalid", "The production tier could not be read")
	text := problemToolResult(t, lab, `{"problems":[`+actionableProblem+`],"changes":[{"repo":"group","number":7,"head":"bad-head","problems":[`+actionableProblem+`],"recipeValidation":{"head":"bad-head","state":"unavailable","problems":[`+distinct+`]}}]}`, false)
	if n := strings.Count(text, "The declared minimum CPU is invalid"); n != 1 {
		t.Errorf("ASSERTION: repeated HQ findings appear once, got %d", n)
	}
	if !strings.Contains(text, "The production tier could not be read") {
		t.Error("ASSERTION: distinct validation findings must survive beside change findings")
	}
}
