package workflow

import (
	"encoding/json"
	"strings"
)

// Plan is the typed replacement for today's free-form Next section.
// It is produced by a pure function BuildPlan(envelope) and is the ONLY
// source of "what should happen next" in any tool response.
//
// Contract:
//   - Primary is never nil (zero NextAction.Label is invalid).
//   - Secondary is set when a second action is commonly done in tandem
//     (e.g. "close current develop" + "start next develop").
//   - Alternatives holds genuinely alternative paths, presented as
//     alternatives to Primary, not an ordered continuation.
//   - PerService carries one action per service in the work-session scope
//     that is still pending (deploy missing/failed, or deploy ok but verify
//     missing/failed). Populated only in develop-active phase; green services
//     are excluded so the map mirrors the remaining work. Callers render the
//     section only when len(PerService) > 1 — with a single service the
//     Primary action already points at it unambiguously.
type Plan struct {
	Primary      NextAction            `json:"primary"`
	PerService   map[string]NextAction `json:"perService,omitempty"`
	Secondary    *NextAction           `json:"secondary,omitempty"`
	Alternatives []NextAction          `json:"alternatives,omitempty"`
}

// NextAction describes one concrete tool call suggested to the LLM. An Args
// value is a string; a list-valued tool argument (zerops_lease's `pools`)
// carries ListArg's encoding.
type NextAction struct {
	Label     string            `json:"label"`
	Tool      string            `json:"tool"`
	Args      map[string]string `json:"args"`
	Rationale string            `json:"rationale"`
}

// IsZero reports whether the NextAction has not been set — used by Plan
// validators that require Primary to be populated.
func (a NextAction) IsZero() bool {
	return a.Label == "" && a.Tool == ""
}

// ListArg encodes a list-valued tool argument as a NextAction.Args value: a
// JSON array of strings, which the status render writes unquoted —
// `pools=["a","b"]` — the way the tool call is written.
func ListArg(values []string) string {
	b, err := json.Marshal(values)
	if err != nil {
		return ""
	}
	return string(b)
}

// isListArg reports whether an Args value carries ListArg's encoding.
func isListArg(value string) bool {
	if !strings.HasPrefix(value, "[") {
		return false
	}
	var values []string
	return json.Unmarshal([]byte(value), &values) == nil
}
