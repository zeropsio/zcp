package port

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

// flexBoolTrue is the JSON-literal "true" needle used in several branches
// of UnmarshalJSON below. Extracted as a constant to satisfy goconst.
const flexBoolTrue = "true"

// FlexBool is a boolean field that tolerates both JSON `true`/`false`
// values AND their string equivalents (`"true"`, `"false"`, case
// variants). Some LLM agents serialize tool arguments with stringified
// primitives; the SDK's default boolean schema rejects the stringified
// form with a non-actionable error. Deliberately duplicated from
// internal/tools (a trivial utility — importing tools from authoring is
// forbidden by the boundary; same rationale as C5's today()/shortRand()).
//
// A zero FlexBool is equivalent to false. Use an explicit `bool(f)`
// conversion in handlers.
type FlexBool bool

// UnmarshalJSON accepts booleans, stringified booleans, and null/empty.
// Rejects anything else with an error clear enough for the agent to fix.
func (f *FlexBool) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(string(data))

	if s == "" || s == "null" {
		*f = false
		return nil
	}
	if s == flexBoolTrue {
		*f = true
		return nil
	}
	if s == "false" {
		*f = false
		return nil
	}
	// Stringified forms, tightly constrained — `"yes"` / `"1"` are
	// rejected instead of silently coerced.
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		switch strings.ToLower(s[1 : len(s)-1]) {
		case flexBoolTrue:
			*f = true
			return nil
		case "false":
			*f = false
			return nil
		}
	}
	return fmt.Errorf("FlexBool: expected boolean or \"true\"/\"false\" string, got %s", s)
}

// MarshalJSON emits a plain JSON boolean — stringified forms are only
// accepted on input.
func (f FlexBool) MarshalJSON() ([]byte, error) {
	if f {
		return []byte("true"), nil
	}
	return []byte("false"), nil
}

// flexBoolSchema returns a schema accepting either a JSON boolean or a
// string; the precise "which strings are booleans" check lives in
// UnmarshalJSON so the rejection message names the bad value.
func flexBoolSchema(description string) *jsonschema.Schema {
	return &jsonschema.Schema{
		Description: description,
		OneOf: []*jsonschema.Schema{
			{Type: "boolean"},
			{Type: "string"},
		},
	}
}

// Ensure the json.Marshaler + json.Unmarshaler contracts are satisfied.
var (
	_ json.Unmarshaler = (*FlexBool)(nil)
	_ json.Marshaler   = FlexBool(false)
)
