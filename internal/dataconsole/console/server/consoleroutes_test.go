package server

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

// consoleRoutesJSPath is the checked-in broker route contract the Zerops Studio
// extension's host broker (consoleClient.js) `require()`s at runtime. It lives
// next to consoleClient.js in the extension's lib/ tree, not webui/dist/:
// consoleClient.js runs in the VS Code EXTENSION HOST (Node), never in the
// browser/webview, so a sibling `require()` is the natural, dependency-free load
// path — unlike the SPA's dist/ assets (browser-loaded via <script src>), it
// needs no runtime filesystem coupling to a materialized media directory.
const consoleRoutesJSPath = "../../extension/templates/vscode-studio/lib/consoleRoutes.js"

// routeDescriptor is the wire shape of one broker-visible route: the exact
// method+path shape server.go exposes, and whether it mutates.
type routeDescriptor struct {
	Method   string `json:"method"`
	Path     string `json:"path"`
	Mutating bool   `json:"mutating"`
}

// consoleRouteDescriptors expands apiRoutes() — the Go-owned single source — into
// one descriptor per method+path shape. A route entry that groups several
// methods under one pattern (e.g. POST+PUT /api/cell) yields one descriptor per
// method, matching routeForMethod's per-method dispatch and what a caller
// actually sends on the wire.
func consoleRouteDescriptors() []routeDescriptor {
	routes := (&Server{}).apiRoutes()
	out := make([]routeDescriptor, 0, len(routes))
	for _, rt := range routes {
		for _, method := range rt.methods {
			out = append(out, routeDescriptor{Method: method, Path: rt.pattern, Mutating: rt.mutating})
		}
	}
	return out
}

const consoleRoutesJSHeader = `"use strict";

// GENERATED FILE — do not hand-edit. Mirrors apiRoutes() in
// internal/dataconsole/console/server/server.go: the exact method + /api path
// shapes the server exposes, and which are mutating. This is the single owner
// consoleClient.js derives its ALLOW/MUTATING allowlists from, so the broker
// can never silently drift from the route table it mirrors.
//
// Regenerate: go test ./internal/dataconsole/console/server/... -run
// TestConsoleRoutesJS_DriftGuard -v, then copy the "Got" content from the
// failure output into this file (same drift-guard pattern as
// internal/dataconsole/console/contract_test.go / webui/dist/contract.js).
`

// generatedConsoleRoutesJS renders the current apiRoutes() table into the exact
// checked-in JS module shape.
func generatedConsoleRoutesJS(t *testing.T) string {
	t.Helper()
	payload := struct {
		Routes []routeDescriptor `json:"routes"`
	}{Routes: consoleRouteDescriptors()}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("marshal console routes: %v", err)
	}
	return consoleRoutesJSHeader + "module.exports = Object.freeze(" + string(b) + ");\n"
}

// TestConsoleRoutesJS_DriftGuard is the S23 broker-route drift pin: the
// checked-in lib/consoleRoutes.js the extension host requires at runtime MUST
// equal the method+path+mutating shape server.go's apiRoutes() actually
// registers. A route added, removed, or re-classified in Go without
// regenerating this file fails here loudly — see consoleRoutesJSHeader above
// for the regen command.
func TestConsoleRoutesJS_DriftGuard(t *testing.T) {
	t.Parallel()
	got := []byte(generatedConsoleRoutesJS(t))
	want, err := os.ReadFile(consoleRoutesJSPath)
	if err != nil {
		t.Fatalf("read %s: %v", consoleRoutesJSPath, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s drifted from server.apiRoutes(). Regenerate (see consoleRoutesJSHeader in consoleroutes_test.go).\nGot:\n%s\nWant:\n%s", consoleRoutesJSPath, got, want)
	}
}
