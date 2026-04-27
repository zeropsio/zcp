// Probe binary for atom-corpus-hygiene plan §6.4. Recreated from
// commit c8d87406 (the prior atom-corpus-context-trim plan) with the
// new `develop_simple_deployed_container` fixture added per the
// hygiene plan §7 Phase 0 step 4.
//
// Computes three layered metrics for any envelope shape:
//
//	synthesize_bodies_join — strings.Join(bodies, "\n\n---\n\n")
//	render_status_markdown — RenderStatus(Response{Env, Guidance})
//	mcp_wire_jsonrpc_frame — full JSON-RPC frame as the stdio writer
//	                         emits it (json.Encoder, SetEscapeHTML(false),
//	                         trailing newline added by Encode).
//
// Per-atom listing prints rendered size + matching service hostname so
// per-service render duplication is visible at a glance.
//
// Built and deleted as part of the hygiene plan; not shipped (Phase 8
// EXIT deletes this directory).
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

type fixture struct {
	name string
	env  workflow.StateEnvelope
}

func main() {
	corpus, err := workflow.LoadAtomCorpus()
	if err != nil {
		panic(err)
	}

	for _, f := range fixtures() {
		measure(f, corpus)
		fmt.Println()
	}
}

func fixtures() []fixture {
	dynStandardPair := func(devHost, stageHost, runtime string) []workflow.ServiceSnapshot {
		return []workflow.ServiceSnapshot{
			{
				Hostname: devHost, TypeVersion: runtime,
				RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard,
				StageHostname: stageHost,
				Strategy:      topology.StrategyUnset, Bootstrapped: true, Deployed: false,
			},
			{
				Hostname: stageHost, TypeVersion: runtime,
				RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStage,
				Strategy: topology.StrategyUnset, Bootstrapped: true, Deployed: false,
			},
		}
	}
	implicitWebStandardPair := func(devHost, stageHost, runtime string) []workflow.ServiceSnapshot {
		return []workflow.ServiceSnapshot{
			{
				Hostname: devHost, TypeVersion: runtime,
				RuntimeClass: topology.RuntimeImplicitWeb, Mode: topology.ModeStandard,
				StageHostname: stageHost,
				Strategy:      topology.StrategyUnset, Bootstrapped: true, Deployed: false,
			},
			{
				Hostname: stageHost, TypeVersion: runtime,
				RuntimeClass: topology.RuntimeImplicitWeb, Mode: topology.ModeStage,
				Strategy: topology.StrategyUnset, Bootstrapped: true, Deployed: false,
			},
		}
	}

	twoPair := append(dynStandardPair("appdev", "appstage", "nodejs@22"), dynStandardPair("apidev", "apistage", "nodejs@22")...)

	return []fixture{
		{
			name: "develop_first_deploy_standard_container",
			env: workflow.StateEnvelope{
				Phase:       workflow.PhaseDevelopActive,
				Environment: workflow.EnvContainer,
				Services:    dynStandardPair("appdev", "appstage", "nodejs@22"),
			},
		},
		{
			name: "develop_first_deploy_implicit_webserver_standard",
			env: workflow.StateEnvelope{
				Phase:       workflow.PhaseDevelopActive,
				Environment: workflow.EnvContainer,
				Services:    implicitWebStandardPair("appdev", "appstage", "php-nginx@8.4"),
			},
		},
		{
			name: "develop_first_deploy_two_runtime_pairs_standard",
			env: workflow.StateEnvelope{
				Phase:       workflow.PhaseDevelopActive,
				Environment: workflow.EnvContainer,
				Services:    twoPair,
			},
		},
		{
			name: "develop_first_deploy_standard_single_service (hypothetical)",
			env: workflow.StateEnvelope{
				Phase:       workflow.PhaseDevelopActive,
				Environment: workflow.EnvContainer,
				Services: []workflow.ServiceSnapshot{
					{
						Hostname: "appdev", TypeVersion: "nodejs@22",
						RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard,
						StageHostname: "appstage",
						Strategy:      topology.StrategyUnset, Bootstrapped: true, Deployed: false,
					},
				},
			},
		},
		{
			// Hygiene plan §7 Phase 0 step 4 addition (user-test 2026-04-26):
			// single simple-mode deployed service edit task. Mirrors the
			// existing develop_push_dev_simple_container fixture shape but
			// with a different hostname so the probe output is greppable.
			name: "develop_simple_deployed_container",
			env: workflow.StateEnvelope{
				Phase:       workflow.PhaseDevelopActive,
				Environment: workflow.EnvContainer,
				Services: []workflow.ServiceSnapshot{{
					Hostname: "weatherdash", TypeVersion: "go@1.22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeSimple,
					Strategy: topology.StrategyPushDev, Bootstrapped: true, Deployed: true,
				}},
			},
		},
	}
}

func measure(f fixture, corpus []workflow.KnowledgeAtom) {
	matches, err := workflow.Synthesize(f.env, corpus)
	if err != nil {
		fmt.Printf("[%s] synthesize error: %v\n", f.name, err)
		return
	}

	bodies := make([]string, len(matches))
	for i, m := range matches {
		bodies[i] = m.Body
	}
	joined := strings.Join(bodies, "\n\n---\n\n")
	rendered := workflow.RenderStatus(workflow.Response{Envelope: f.env, Guidance: bodies})
	wire, err := wireFrameBytes(rendered, 1)
	if err != nil {
		fmt.Printf("[%s] wire encode error: %v\n", f.name, err)
		return
	}

	cap32 := 32 * 1024
	cap28 := 28 * 1024

	fmt.Printf("=== %s ===\n", f.name)
	fmt.Printf("  atom-renders:           %d\n", len(matches))
	fmt.Printf("  synthesize_bodies_join: %7d B   delta-vs-28KB-cap: %+d\n", len(joined), len(joined)-cap28)
	fmt.Printf("  render_status_markdown: %7d B\n", len(rendered))
	fmt.Printf("  mcp_wire_jsonrpc_frame: %7d B   delta-vs-32KB-cap: %+d\n", len(wire), len(wire)-cap32)
	fmt.Printf("  per-atom (host shows duplication):\n")

	type row struct {
		id, host string
		size     int
	}
	rows := make([]row, 0, len(matches))
	for _, m := range matches {
		host := ""
		if m.Service != nil {
			host = m.Service.Hostname
		}
		rows = append(rows, row{m.AtomID, host, len(m.Body)})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].size > rows[j].size })
	for _, r := range rows {
		fmt.Printf("    %5d  %-50s  %s\n", r.size, r.id, r.host)
	}
}

// wireFrameBytes returns the byte count of the full JSON-RPC frame as the
// SDK's stdio writer emits it for one Response message.
//
// Mirrors github.com/modelcontextprotocol/go-sdk@v1.5.0:
//   - internal/jsonrpc2/messages.go::EncodeMessage + jsonMarshal — uses
//     json.NewEncoder + SetEscapeHTML(false) + Encode, then strips the
//     trailing newline added by Encode (messages.go:240-241).
//   - mcp/transport.go::ioConn.Write line 651 — appends exactly one '\n'
//     before writing to the underlying io.Writer.
//
// Net: probe Encode leaves one trailing '\n' in the buffer; SDK strip+add
// produces the same single '\n'. So len(probe) == len(SDK on stdout).
//
// ID field is `any` (not int64) to match the SDK's wireCombined.ID type
// (wire.go:59) — `omitempty` on `any` only skips nil, while on int64 it
// would skip zero IDs.
func wireFrameBytes(text string, id any) ([]byte, error) {
	result := &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	wire := struct {
		Jsonrpc string          `json:"jsonrpc"`
		ID      any             `json:"id,omitempty"`
		Result  json.RawMessage `json:"result,omitempty"`
	}{Jsonrpc: "2.0", ID: id, Result: resultJSON}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(wire); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
