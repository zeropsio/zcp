package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/preprocess"
)

// PreprocessInput is the input type for zerops_preprocess.
type PreprocessInput struct {
	Input  string            `json:"input,omitempty"  jsonschema:"Single preprocessor expression to expand (e.g. '<@generateRandomString(<32>)>'). Use this OR inputs, not both."`
	Inputs map[string]string `json:"inputs,omitempty" jsonschema:"Map of named preprocessor expressions to expand as a batch. All entries share a single variable store, so setVar/getVar across entries work. Use the 'order' field to control setVar/getVar evaluation order."`
	Order  []string          `json:"order,omitempty"  jsonschema:"Optional key order for batch inputs. When setVar/getVar is used across entries, the key that sets a variable must appear before the key that reads it. If omitted for batch inputs, evaluation order is undefined."`
}

// PreprocessResult is returned from zerops_preprocess.
type PreprocessResult struct {
	Expanded  string            `json:"expanded,omitempty"`
	Expansion map[string]string `json:"expansion,omitempty"`
}

// RegisterPreprocess registers the zerops_preprocess tool.
func RegisterPreprocess(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_preprocess",
		Description: "Expand Zerops preprocessor expressions via the official zParser library — output is byte-for-byte what the platform produces at recipe import time. Functions: generateRandomString(<N>), generateRandomInt(<min>,<max>), generateRandomBytes(<N>), generateED25519Key/RSA2048Key/RSA4096Key(<name>), pickRandom, setVar, getVar. Modifiers: sha256, sha512, bcrypt, argon2id, toHex, toString, upper, lower, title. Single: input=expr. Batch: inputs={name:expr} + order=[...] shares a variable store so setVar/getVar span keys. Primary use: generating workspace shared-secret values matching the deliverable's preprocessor output exactly. zerops_env set also auto-expands values containing <@...> syntax — use this tool when you need the raw value, batched correlation, or key-pair generation.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Expand Zerops preprocessor expressions",
			ReadOnlyHint: true,
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input PreprocessInput) (*mcp.CallToolResult, any, error) {
		hasSingle := input.Input != ""
		hasBatch := len(input.Inputs) > 0

		if !hasSingle && !hasBatch {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				"Provide either 'input' (single expression) or 'inputs' (batch map)",
				"")), nil, nil
		}
		if hasSingle && hasBatch {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				"Use 'input' OR 'inputs', not both",
				"")), nil, nil
		}

		if hasSingle {
			expanded, err := preprocess.Expand(ctx, input.Input)
			if err != nil {
				return convertError(platform.NewPlatformError(
					platform.ErrInvalidParameter,
					fmt.Sprintf("preprocessor expansion failed: %v", err),
					"Check your <@...> syntax")), nil, nil
			}
			return jsonResult(PreprocessResult{Expanded: expanded}), nil, nil
		}

		expanded, err := preprocess.Batch(ctx, input.Order, input.Inputs)
		if err != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("preprocessor expansion failed: %v", err),
				"Check your <@...> syntax, or verify all keys listed in 'order' exist in 'inputs'")), nil, nil
		}
		return jsonResult(PreprocessResult{Expansion: expanded}), nil, nil
	})
}
