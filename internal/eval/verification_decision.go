package eval

import (
	"time"

	"github.com/zeropsio/zcp/internal/capture"
)

// evaluateToolArgRows emits one toolArg stub row per declared entry
// (docs/spec-eval-farm.md §4.1/§4.2 FM-59/FM-60: decision rows over
// transcript.jsonl). Body lands in S3/S4.
func evaluateToolArgRows(entries []ToolArgEntry, _ string, _ []capture.MCPToolCall, _ bool, now time.Time) []RequiredCheck {
	rows := make([]RequiredCheck, 0, len(entries))
	for i := range entries {
		rows = append(rows, notImplementedStub("toolArg", i+1, now))
	}
	return rows
}

// evaluateToolResultRows emits one toolResult stub row per declared entry
// (docs/spec-eval-farm.md §4.1/§4.2 FM-59: decision rows over captured MCP
// results). Body lands in S3/S4.
func evaluateToolResultRows(entries []ToolResultEntry, _ string, _ []capture.MCPToolCall, _ bool, now time.Time) []RequiredCheck {
	rows := make([]RequiredCheck, 0, len(entries))
	for i := range entries {
		rows = append(rows, notImplementedStub("toolResult", i+1, now))
	}
	return rows
}

// evaluateMustOfferRows emits one mustOffer stub row per declared entry
// (docs/spec-eval-farm.md §4.1/§4.2 FM-59: decision rows over route-menu /
// next-step text). Body lands in S3/S4.
func evaluateMustOfferRows(entries []string, _ string, _ []capture.MCPToolCall, _ bool, now time.Time) []RequiredCheck {
	rows := make([]RequiredCheck, 0, len(entries))
	for i := range entries {
		rows = append(rows, notImplementedStub("mustOffer", i+1, now))
	}
	return rows
}
