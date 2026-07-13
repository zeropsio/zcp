package capture

import (
	"strconv"
	"strings"
	"testing"
)

func TestMatchInspectionSources_FindsAtomInsideNestedJSONText(t *testing.T) {
	t.Parallel()

	modeBody := "### Confirm mode per service\n\nChoose on the OUTCOME, not iteration habit."
	result := `{"kind":"session-active","current":{"detailedGuide":` + strconv.Quote("prefix\n\n"+modeBody+"\n\nsuffix") + `}}`
	result = strings.ReplaceAll(result, `\"`, `"`)
	documents := []inspectionSourceDocument{
		{AtomID: "bootstrap-mode-prompt", File: "internal/content/atoms/bootstrap-mode-prompt.md", Body: modeBody},
		{AtomID: "unrelated", File: "internal/content/atoms/unrelated.md", Body: "completely different guidance"},
	}

	matches, err := matchInspectionSources(result, documents)
	if err != nil {
		t.Fatalf("matchInspectionSources() error = %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("matches = %+v, want one exact atom", matches)
	}
	if matches[0].AtomID != "bootstrap-mode-prompt" || matches[0].File != "internal/content/atoms/bootstrap-mode-prompt.md" || matches[0].MatchedBytes != len([]byte(modeBody)) {
		t.Fatalf("match = %+v", matches[0])
	}
}

func TestMatchInspectionSources_DoesNotClaimChangedAtom(t *testing.T) {
	t.Parallel()

	documents := []inspectionSourceDocument{{AtomID: "mode", File: "mode.md", Body: "Choose simple as the safe default."}}
	matches, err := matchInspectionSources("Choose dev from the explicit user signal.", documents)
	if err != nil {
		t.Fatalf("matchInspectionSources() error = %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("matches = %+v, want no historical/source claim without exact bytes", matches)
	}
}
