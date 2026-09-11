// Package observer implements the farm observer (docs/spec-eval-farm.md §7):
// an advisory evaluation of one finished run's bundle. It reads the bundle's
// already-uploaded, already-redacted files through the Bundle interface
// (bundle.go), numbers the run into steps (steps.go), builds a bounded
// plain-text digest of them (digest.go), invokes `claude -p` once with the
// embedded observer prompt (prompt.go, run.go), parses and validates the
// model's answer and checks every quote against the run's own record
// (observation.go), and renders the stored document as plain text
// (render.go).
//
// The observer is advisory only: it never produces or changes a verdict
// (§7.1) and it writes only under runs/<runId>/observer/ (§7.6). A Bundle is
// either a pulled directory (DirBundle — the local verb,
// cmd/zcp/eval_farm_observe.go, which never reaches the sink, §7.7) or the
// farm bucket itself (SinkBundle, sinkbundle.go); Store (store.go) is the
// only writer, and it refuses any key outside a valid run's observer/ prefix
// and never overwrites an observation.
package observer
