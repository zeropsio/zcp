package tools

import (
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

// flexBoolTrue is the JSON-literal "true" needle used in several branches
// of UnmarshalJSON below. Extracted as a constant to satisfy goconst.
const flexBoolTrue = "true"

// FlexBool is a boolean field that tolerates both JSON `true`/`false`
// values AND their string equivalents (`"true"`, `"false"`, and their
// common case variants). This exists because some LLM agents serialize
// tool arguments with stringified primitives — sending `{"includeEnvs":
// "true"}` instead of `{"includeEnvs": true}` — and the MCP SDK's
// default boolean schema rejects the stringified form with a
// non-actionable error message. The agent then spends tool calls (and
// context window) retrying or abandoning the call entirely.
//
// The v7 showcase run's post-mortem log shows a run where the agent
// failed zerops_discover once and zerops_env twice because of exactly
// this type mismatch. Making the affected boolean inputs tolerant at
// the unmarshal layer eliminates the class of error without changing
// the tool surface the agent was already using correctly.
//
// A zero FlexBool is equivalent to false, matching the plain `bool`
// semantics callers used to get. Use the Bool method (or an explicit
// `bool(f)` conversion) in handlers.
type FlexBool bool

// Bool returns the underlying boolean value.
func (f FlexBool) Bool() bool { return bool(f) }

// UnmarshalJSON accepts booleans, stringified booleans, and null/empty.
// Rejects anything else with an error clear enough for the agent to fix.
func (f *FlexBool) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(string(data))

	// Handle null/empty as false — matches the "field omitted" convention
	// for optional booleans throughout the tool inputs.
	if s == "" || s == "null" {
		*f = false
		return nil
	}

	// Native boolean values.
	if s == flexBoolTrue {
		*f = true
		return nil
	}
	if s == "false" {
		*f = false
		return nil
	}

	// Stringified forms: `"true"` and `"false"`, case-insensitive. We
	// strip quotes manually instead of calling json.Unmarshal into a
	// string to keep the accepted-value set tightly constrained — if
	// an agent sends `"yes"` or `"1"` we want to reject those instead
	// of silently coercing.
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		inner := strings.ToLower(s[1 : len(s)-1])
		switch inner {
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

// MarshalJSON emits a plain JSON boolean. Stringified forms are only
// accepted on input — our output stays on the strict side of the wire
// so anything consuming our responses gets canonical booleans.
func (f FlexBool) MarshalJSON() ([]byte, error) {
	if f {
		return []byte("true"), nil
	}
	return []byte("false"), nil
}

// flexBoolSchema returns a jsonschema.Schema that accepts either a JSON
// boolean or a string. The precise "which strings are booleans" check
// lives in FlexBool.UnmarshalJSON so the schema stays broad (any string)
// and rejection happens at the unmarshal layer with a specific error
// message that names the field and the bad value — rather than at the
// schema layer which only says "did not validate against any of oneOf".
//
// The tradeoff: a payload like {"includeEnvs": "yes"} passes schema
// validation and fails one layer deeper. The error reaches the agent as
// `FlexBool: expected boolean or "true"/"false" string, got "yes"`,
// which is actionable. The v7 post-mortem failure mode — an agent
// passing `"true"` and getting a schema error with zero recovery path —
// is the one we care about eliminating.
func flexBoolSchema(description string) *jsonschema.Schema {
	return &jsonschema.Schema{
		Description: description,
		OneOf: []*jsonschema.Schema{
			{Type: "boolean"},
			{Type: "string"},
		},
	}
}

// objectSchema is a small helper for building the InputSchema of a tool
// when a plain `For[T]()` would reject our FlexBool fields. Callers pass
// the properties map plus (optionally) a list of required fields.
func objectSchema(properties map[string]*jsonschema.Schema, required ...string) *jsonschema.Schema {
	// Copy properties so the caller cannot mutate the map after building.
	p := make(map[string]*jsonschema.Schema, len(properties))
	maps.Copy(p, properties)
	s := &jsonschema.Schema{
		Type:       "object",
		Properties: p,
	}
	if len(required) > 0 {
		s.Required = append(s.Required, required...)
	}
	return s
}

// Ensure the json.Marshaler + json.Unmarshaler contracts are satisfied.
var (
	_ json.Unmarshaler = (*FlexBool)(nil)
	_ json.Marshaler   = FlexBool(false)
)
