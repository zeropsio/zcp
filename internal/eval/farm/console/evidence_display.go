package console

import (
	"bytes"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

func indentJSONOrOriginal(raw string) string {
	var indented bytes.Buffer
	if err := json.Indent(&indented, []byte(raw), "", "  "); err != nil {
		return raw
	}
	return indented.String()
}

func displayToolInput(raw string) string {
	return indentJSONOrOriginal(raw)
}

func displayToolResult(raw string) string {
	return observer.DecodeJSONEscapes(indentJSONOrOriginal(raw))
}

func visibleEvidenceSteps(steps []observer.Step) []observer.Step {
	visible := make([]observer.Step, 0, len(steps))
	for _, step := range steps {
		if step.Kind == observer.StepThinking && strings.TrimSpace(step.Text) == "" {
			continue
		}
		visible = append(visible, step)
	}
	return visible
}

// stepTargetView is a link to one canonical recorded step. Available is false
// when the record has no displayable step with that number; Href is a local
// fragment when the active filter renders it and an unfiltered URL otherwise.
type stepTargetView struct {
	N         int
	Href      string
	Available bool
}

type stepTargetIndex struct {
	runID    string
	query    url.Values
	all      map[int]bool
	rendered map[int]bool
}

func newStepTargetIndex(runID string, query url.Values, all, rendered []observer.Step) stepTargetIndex {
	idx := stepTargetIndex{runID: runID, query: query, all: make(map[int]bool, len(all)), rendered: make(map[int]bool, len(rendered))}
	for _, step := range all {
		idx.all[step.N] = true
	}
	for _, step := range rendered {
		idx.rendered[step.N] = true
	}
	return idx
}

func (idx stepTargetIndex) target(n int) stepTargetView {
	target := stepTargetView{N: n, Available: idx.all[n]}
	if !target.Available {
		return target
	}
	if idx.rendered[n] {
		target.Href = "#s" + fmtInt(n)
		return target
	}
	target.Href = listURL("/r/"+idx.runID, idx.query, map[string]string{"steps": filterAll}) + "#s" + fmtInt(n)
	return target
}

func fmtInt(n int) string {
	return strconv.Itoa(n)
}
