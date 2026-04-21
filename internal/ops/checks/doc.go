// Package checks holds reusable predicate functions for every recipe
// workflow gate that also needs an author-runnable pre-attest path. Each
// exported `Check<Name>(ctx, ...)` function is the single source of truth
// for that gate: the server-side tool-layer checker calls it, and (post
// C-7e) the `zcp check <name>` CLI shim calls the same function. Gate and
// shim cannot diverge because there is exactly one predicate
// implementation.
//
// Migration lineage: C-7a–d move bodies out of
// `internal/tools/workflow_checks_*.go` into per-check files under this
// package. The tools layer retains thin wrappers that adapt
// BootstrapState / RecipePlan inputs into the predicate signatures and
// forward results unchanged. C-7e layers the CLI shim tree (at
// `cmd/zcp/check/`) over the same surface. No predicate body lives in
// both places.
//
// Constants: `StatusPass` / `StatusFail` are exported so every check
// file can emit consistent Status values without re-declaring strings.
// These match the `workflow.StepCheck.Status` wire values the existing
// tool-layer and bootstrap checker use.
package checks

// StatusPass is the Status value a passing StepCheck carries.
const StatusPass = "pass"

// StatusFail is the Status value a failing StepCheck carries.
const StatusFail = "fail"
