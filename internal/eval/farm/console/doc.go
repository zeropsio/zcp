// Package console implements the farm console (docs/spec-eval-farm.md §8): a
// hosted, authenticated HTTP view over the farm bucket, serving both HTML
// pages (§8.3, mostly landing with S4) and a markdown+JSON agent API (§8.4).
//
// The console never holds an account-wide Zerops key and never mutates a
// run's own bundle (nothing under runs/<runId>/{started.json, done.json,
// results/, capture/} — §7.6 FM-47); it only reads the bucket
// (server.go's Config.Store, an observer.ObjectStore) and, for the observer
// worker (S5), writes advisory observations through observer.Store, which is
// already restricted to runs/<runId>/observer/.
//
// Files: server.go (Config, routing, security headers), auth.go (login,
// session cookie, bearer auth — FM-50), view.go (the read model over
// manifests/summaries/bundles — batch list, batch detail, run detail,
// findings), api.go (the §8.4 markdown+JSON agent endpoints), window.go
// (the §8.4 window syntax).
package console
