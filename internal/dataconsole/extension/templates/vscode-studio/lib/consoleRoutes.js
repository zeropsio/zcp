"use strict";

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
module.exports = Object.freeze({
  "routes": [
    {
      "method": "GET",
      "path": "/api/services",
      "mutating": false
    },
    {
      "method": "POST",
      "path": "/api/refresh",
      "mutating": false
    },
    {
      "method": "GET",
      "path": "/api/tree",
      "mutating": false
    },
    {
      "method": "GET",
      "path": "/api/stat",
      "mutating": false
    },
    {
      "method": "GET",
      "path": "/api/blob",
      "mutating": false
    },
    {
      "method": "GET",
      "path": "/api/download",
      "mutating": false
    },
    {
      "method": "PUT",
      "path": "/api/blob",
      "mutating": true
    },
    {
      "method": "DELETE",
      "path": "/api/blob",
      "mutating": true
    },
    {
      "method": "GET",
      "path": "/api/table",
      "mutating": false
    },
    {
      "method": "GET",
      "path": "/api/table/count",
      "mutating": false
    },
    {
      "method": "POST",
      "path": "/api/query",
      "mutating": false
    },
    {
      "method": "GET",
      "path": "/api/search",
      "mutating": false
    },
    {
      "method": "POST",
      "path": "/api/cell",
      "mutating": true
    },
    {
      "method": "PUT",
      "path": "/api/cell",
      "mutating": true
    },
    {
      "method": "POST",
      "path": "/api/row",
      "mutating": true
    },
    {
      "method": "DELETE",
      "path": "/api/row",
      "mutating": true
    },
    {
      "method": "PUT",
      "path": "/api/entry",
      "mutating": true
    },
    {
      "method": "DELETE",
      "path": "/api/entry",
      "mutating": true
    },
    {
      "method": "POST",
      "path": "/api/upload",
      "mutating": true
    },
    {
      "method": "POST",
      "path": "/api/rename",
      "mutating": true
    },
    {
      "method": "PUT",
      "path": "/api/ttl",
      "mutating": true
    },
    {
      "method": "POST",
      "path": "/api/kv/create",
      "mutating": true
    },
    {
      "method": "POST",
      "path": "/api/document/create",
      "mutating": true
    },
    {
      "method": "GET",
      "path": "/api/node",
      "mutating": false
    },
    {
      "method": "DELETE",
      "path": "/api/node",
      "mutating": true
    }
  ]
});
