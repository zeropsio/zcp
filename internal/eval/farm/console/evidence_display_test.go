package console

import (
	"html"
	"reflect"
	"strings"
	"testing"
)

// TestEvidenceDisplay_JSONIndent_PreservesRawLexemes pins the console-only
// presentation boundary: JSON is indented without decoding and re-encoding
// the canonical step, and result escape decoding happens after indentation.
func TestEvidenceDisplay_JSONIndent_PreservesRawLexemes(t *testing.T) {
	srv, store, _ := testServer(t)
	seedBatch(t, store, "js1", "off", []runFixture{{
		runID: "js1-a", scenario: "a", startedAt: fixedNow(t)(), durationS: "5s", taskResult: "passed", done: true,
	}}, true, map[string]string{"js1-a": "passed"})
	transcript := strings.Join([]string{
		`{"type":"system","subtype":"init"}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu-json","name":"json_tool","input":{"z":1e+09,"dup":1,"dup":2,"big":9007199254740993,"escaped":"\u003cscript\u003e"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu-json","content":[{"type":"text","text":"{\"message\":\"line\\nnext\",\"n\":1e+09}"}]}]}}`,
	}, "\n") + "\n"
	store.putText(t, "runs/js1-a/results/"+testResultsTS+"/a/transcript.jsonl", transcript)

	before, err := loadSteps(t.Context(), store, "js1-a")
	if err != nil {
		t.Fatalf("load seeded steps: %v", err)
	}
	wantInput := "{\n  \"z\": 1e+09,\n  \"dup\": 1,\n  \"dup\": 2,\n  \"big\": 9007199254740993,\n  \"escaped\": \"\\u003cscript\\u003e\"\n}"
	wantResult := "{\n  \"message\": \"line\nnext\",\n  \"n\": 1e+09\n}"
	body := html.UnescapeString(doGET(t, srv.Handler(), "/r/js1-a").Body.String())
	if !strings.Contains(body, wantInput) {
		t.Errorf("display input lost raw order/duplicates/number lexemes or was not indented:\n%s", body)
	}
	if !strings.Contains(body, wantResult) {
		t.Errorf("display result was not indented before escape decoding:\n%s", body)
	}
	after, err := loadSteps(t.Context(), store, "js1-a")
	if err != nil {
		t.Fatalf("reload seeded steps: %v", err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Errorf("display mutated canonical steps:\nbefore=%#v\nafter=%#v", before, after)
	}
	if strings.Contains(doGET(t, srv.Handler(), "/r/js1-a").Body.String(), "<script>") {
		t.Error("display bypassed html/template escaping")
	}
}
