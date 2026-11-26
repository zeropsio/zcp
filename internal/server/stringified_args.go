package server

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// argumentTypeRefusal is the SDK's answer when a call's argument has the
// wrong JSON type for its tool's input schema; it names the top-level
// property. The SDK checks the arguments before any handler runs.
var argumentTypeRefusal = regexp.MustCompile(`^validating "arguments": validating root: validating /properties/([^/:]+): type: `)

// decodeStringifiedArguments returns middleware for agents whose client sends
// an object, array, number or boolean argument JSON-encoded in a string — closeMode as
// "{\"appdev\":\"auto\"}" — which the SDK refuses before the handler runs
// and the agent cannot fix from its side. When a call is refused for one
// property's type and that property is a string holding one of those JSON
// values, the call runs again with it decoded; each property once. A value
// the schema takes as a string is never refused, so it is never touched, and
// any other refusal goes back as it was.
func decodeStringifiedArguments() mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			result, err := next(ctx, method, req)
			if method != methodCallTool || err != nil || req == nil {
				return result, err
			}
			raw, ok := req.GetParams().(*mcp.CallToolParamsRaw)
			if !ok || raw == nil {
				return result, err
			}
			decoded := map[string]bool{}
			for {
				name := refusedArgument(result)
				if name == "" || decoded[name] {
					return result, err
				}
				arguments, ok := decodeArgument(raw.Arguments, name)
				if !ok {
					return result, err
				}
				decoded[name] = true
				raw.Arguments = arguments
				if result, err = next(ctx, method, req); err != nil {
					return result, err
				}
			}
		}
	}
}

// refusedArgument names the property an SDK type refusal is about; "" for
// any other answer.
func refusedArgument(result mcp.Result) string {
	r, ok := result.(*mcp.CallToolResult)
	if !ok || r == nil || !r.IsError || r.GetError() == nil {
		return ""
	}
	if m := argumentTypeRefusal.FindStringSubmatch(r.GetError().Error()); m != nil {
		return m[1]
	}
	return ""
}

// decodeArgument answers arguments with property name decoded from a string
// holding a JSON object, array, number or boolean; false when it is no such string.
func decodeArgument(arguments json.RawMessage, name string) (json.RawMessage, bool) {
	var args map[string]json.RawMessage
	if err := json.Unmarshal(arguments, &args); err != nil {
		return nil, false
	}
	var text string
	if err := json.Unmarshal(args[name], &text); err != nil {
		return nil, false
	}
	text = strings.TrimSpace(text)
	if !json.Valid([]byte(text)) {
		return nil, false
	}
	switch text[0] {
	case '{', '[':
	case 't', 'f', '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
	default:
		return nil, false
	}
	args[name] = json.RawMessage(text)
	out, err := json.Marshal(args)
	if err != nil {
		return nil, false
	}
	return out, true
}
