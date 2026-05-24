// Package adapters provides per-agent container-init adapters for ZCP.
//
// Today's container template pre-installs Claude Code; a future template
// may pre-install Codex CLI / Gemini CLI / Antigravity alongside. ZCP's
// init flow asks each registered adapter "are you applicable here?"
// (Detect), "is your install in a usable state?" (Validate), and if both
// pass, "write your per-agent config files" (ContainerInit).
//
// Adapters are stateless. All context (HOME, project root, runtime info,
// test affordances) flows in via Env. This keeps the dispatch loop pure
// and the test surface obvious.
//
// Backward-compat contract: ZCP is a published product. Adapters MUST
// use the merge helpers in merge.go to upsert ZCP-owned keys without
// clobbering user content in pre-existing config files.
package adapters

import "github.com/zeropsio/zcp/internal/runtime"

// Env carries everything a container adapter needs to do its work.
// Production callers populate from os.Getenv + runtime.Detect; tests
// substitute paths and command runners.
type Env struct {
	// BaseDir is the project root (cwd of `zcp init`).
	BaseDir string

	// Home is the resolved home directory for the current user
	// (init.resolveHome handles HOME="" / HOME="/" container quirks).
	Home string

	// RT carries Zerops container detection results.
	RT runtime.Info

	// VSCodeWorkDir is the in-container workspace path Claude Code's
	// VS Code extension treats as the project root. Defaults to
	// /var/www; tests override.
	VSCodeWorkDir string

	// CommandRunner shells out to install extensions / probe binaries.
	// Default invokes exec.Command; tests inject a fake to record calls.
	CommandRunner func(name string, args ...string) error

	// CommandOutput captures stdout from a command for adapters that
	// need to parse output (e.g. version detection like
	// `codex --version`). Default delegates to exec.Command.Output().
	// Tests substitute a fake that returns canned bytes + error.
	CommandOutput func(name string, args ...string) ([]byte, error)

	// LookPath probes whether a named binary is on PATH. Default
	// delegates to exec.LookPath; tests override to simulate
	// installed / missing binaries deterministically.
	LookPath func(name string) (string, error)
}

// Adapter writes per-agent configuration into a container so the agent
// can call the ZCP MCP server without first-run friction (trust dialogs,
// API-key approval, etc.).
//
// Implementations live one-per-file in this package
// (claude.go, codex.go, …).
type Adapter interface {
	// Name returns the canonical agent identifier used in logs and
	// (eventually) in registry / detection telemetry. Lowercase,
	// dash-separated — matches the values declared in container
	// provisioning YAML's ZCP_AGENT_TYPE env var.
	Name() string

	// Detect reports whether this agent is applicable to the current
	// environment. CLI agents check binary presence + container
	// installability; an IDE adapter (deferred) would check
	// project-level marker files.
	//
	// false → skip silently (this container doesn't carry the agent).
	// true → proceed to Validate.
	Detect(env Env) bool

	// Validate runs deeper checks after Detect succeeded: version
	// compatibility (e.g. Codex hooks require ≥ v0.131.0), auth
	// coherence, etc. Returns informational warnings (operator should
	// know but init can proceed) and a hard error (skip this
	// adapter's ContainerInit).
	Validate(env Env) (warnings []string, err error)

	// ContainerInit writes / merges this agent's per-agent config files.
	// Idempotent: re-runs upsert ZCP-owned keys without overwriting
	// user-added entries (use merge.go primitives).
	ContainerInit(env Env) error
}
