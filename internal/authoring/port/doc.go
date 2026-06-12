// Package port is the OSS port flow — the maintainer-only authoring tool
// that takes foreign self-hosted software (Strapi, PostHog, umami), gets it
// running on Zerops by iterating against live deploys + logs (Stage A:
// port → debug → harden), then captures the working deployment as a curated
// recipe (Stage B: capture & publish).
//
// It registers the gated `zerops_port` MCP tool (ZCP_AUTHORING=1 only,
// mirroring the recipe.Register model) with five actions: start (recon —
// deterministic classification of an agent-researched target descriptor,
// zero deploy), iterate (the agent-driven deploy-debug loop continuation),
// harden (rubric grading into the measured FitCeiling), capture (Stage B
// emit + curated two-channel publish), and status (compaction recovery).
//
// The loop is AGENT-DRIVEN across tool turns, never an engine-internal
// coroutine: the agent runs every deploy via the existing core tools
// (zerops_deploy / zerops_import / zerops_env), observes the
// FailureClassification, and passes what it observed back here; the
// handlers classify, grade, and record — they never deploy. Per-PID state
// lives in the authoring-owned `.zcp/state/port/` namespace
// (docs/spec-authoring-boundary.md C3).
//
// Spec: docs/spec-oss-port-flow.md.
package port
