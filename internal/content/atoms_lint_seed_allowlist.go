package content

// This file holds the seed allowlist entries from the initial corpus
// audit (engine plan E4 §3 — 80-atom one-pass review on 2026-04-27).
// Each entry suppresses ONE rule for ONE atom line and MUST carry a
// one-line rationale tying back to spec §11.5/§11.6 signal numbers
// when applicable.
//
// New atom edits should prefer the inline marker convention
// (`<!-- axis-{k,m,n}-keep: signal-#N -->`) over allowlist entries.
// The allowlist is reserved for:
//
//   - HIGH-signal guardrail clusters baked into existing atoms during
//     cycles 1+2+3, where a per-line rationale is already documented
//     here rather than scattered through atom bodies.
//   - Structurally-unavoidable cases (e.g. shared per-env headings in
//     trigger walkthroughs) that markers would visually clutter.
//   - Cluster-#5 sub-agent terminology (spec §11.5 caveat) — `agent`
//     in `develop-verify-matrix.md` refers to a spawned sub-agent.
//
// Key format: "<atomFile>::<exact trimmed line>" — must match the
// snippet emitted by the lint engine byte-for-byte.

// axisLAllowlist: <atomFile>::<exact trimmed line> → rationale.
// HARD-FORBID forms (env-only title qualifiers) are normally fixed in
// the atom body. Empty by default.
var axisLAllowlist = map[string]string{}

// axisKAllowlist: <atomFile>::<exact trimmed line> → rationale.
// Per spec §11.5 Axis K: HIGH-signal guardrails (signals #1-#5) are
// MANDATORY KEEP. Each entry below is a HIGH-signal guardrail
// preserved during the 2026-04-27 audit.
var axisKAllowlist = map[string]string{
	// Mode definitions — stage-pair / SSHFS negation IS the model
	// (signal #1 negation tied to mode/action).
	"bootstrap-mode-prompt.md::- **dev** — single mutable dev container, SSHFS-mountable, no stage pair.": "spec §11.5 axis-K signal-#1: mode definition; `no stage pair` IS the dev-mode contract",
	"bootstrap-mode-prompt.md::no SSHFS mutation lifecycle.":                                              "spec §11.5 axis-K signal-#1: simple-mode definition; `no SSHFS` IS the simple-mode contract",
	"develop-close-mode-auto-dev.md::Dev mode has no stage pair: deploy the single runtime container,":    "spec §11.5 axis-K signal-#1: dev-mode operational contract",

	// Local-mode SSHFS negation — signal #2 cross-env contrast tied to
	// a positive operational claim (committed-tree builds vs SSHFS).
	"bootstrap-provision-local.md::**No SSHFS** — `zerops_mount` is unavailable in local mode; files live":       "spec §11.5 axis-K signal-#1+#2: local-mode tool-availability guardrail",
	"develop-close-mode-auto-local.md::Local mode builds from your committed tree — no SSHFS, no dev container.": "spec §11.5 axis-K signal-#2: cross-env contrast prevents SSHFS-mode reflex",
	"develop-local-workflow.md::ready via `zerops_deploy`. There is no SSHFS mount in local mode — the":          "spec §11.5 axis-K signal-#2: local-mode flow framing",

	// Tool-selection guardrails — signal #3 (`Do NOT use X`,
	// `container-only`, `local-only`).
	"develop-dynamic-runtime-start-local.md::**Do NOT use `zerops_dev_server`** — that tool is container-only (it":                                     "spec §11.5 axis-K signal-#3: tool-selection guardrail; `zerops_dev_server` is container-only",
	"develop-platform-rules-container.md::Mount basics in `claude_container.md` (boot shim). Container-only":                                           "spec §11.5 axis-K signal-#3: container-mode tool gating",
	"develop-platform-rules-local.md::| Code | Edit the working directory. No SSHFS, no `/var/www/{hostname}` mount — that shape is container-only. |": "spec §11.5 axis-K signal-#1+#3: local-mode platform-rules row contrasts container path",
	"develop-platform-rules-local.md::container-only. Use the framework's normal dev command.":                                                         "spec §11.5 axis-K signal-#3: dev-server selection rule",
	"develop-push-git-deploy-local.md::credential manager). `GIT_TOKEN` and `.netrc` are container-only and do":                                        "spec §11.5 axis-K signal-#3: credential-channel separation",

	// Negation tied to action — signal #1 (`Don't run X`, `Do NOT run`).
	"bootstrap-recipe-import.md::services. Don't edit resource limits, `buildFromGit`, `priority`,":           "spec §11.5 axis-K signal-#1: recipe-import field-mutability guardrail",
	"develop-first-deploy-write-app.md::**Don't run `git init` from the ZCP-side mount.** Push-dev deploy":    "spec §11.5 axis-K signal-#1: prevents user-init reflex on dev container",
	"export.md::non-fatal issues. Do NOT run `git init`, `git config user.*`, or":                             "spec §11.5 axis-K signal-#1: export procedure forbids manual git init",
	"strategy-push-git-push-container.md::Do not run `git init`, `.netrc` configuration, or `git remote add`": "spec §11.5 axis-K signal-#1: push-git setup forbids manual git init",
}

// axisMAllowlist: <atomFile>::<exact trimmed line> → rationale.
// Cluster-#5 grandfather: `develop-verify-matrix` uses "agent" to refer
// to a SPAWNED SUB-AGENT (Sonnet via the Agent template), not the
// author-perspective LLM noun. Spec: §11.5 cluster-#5 caveat.
var axisMAllowlist = map[string]string{
	"develop-verify-matrix.md::- **VERDICT: FAIL** → visual/functional issue; iterate from the agent's":  "spec §11.5 cluster-#5 caveat: `agent` is the spawned sub-agent (Sonnet via Agent template)",
	"develop-verify-matrix.md::- **VERDICT: UNCERTAIN** → fall back to `zerops_verify`; the agent could": "spec §11.5 cluster-#5 caveat: `agent` is the spawned sub-agent (Sonnet via Agent template)",
}

// axisNAllowlist: <atomFile>::<exact trimmed line> → rationale.
// Universal atoms whose env-specific detail is the topic itself
// (deploy-files semantics, runtime-path mechanics) or whose dual-env
// rendering is the operational structure (push-git trigger walkthrough).
var axisNAllowlist = map[string]string{
	// SSHFS / stage-pair callouts that are mode-definitional rather
	// than per-env edit guidance.
	"bootstrap-mode-prompt.md::- **dev** — single mutable dev container, SSHFS-mountable, no stage pair.":   "spec §11.6: `SSHFS-mountable` IS the dev-mode definition (not env-specific edit guidance)",
	"bootstrap-mode-prompt.md::no SSHFS mutation lifecycle.":                                                "spec §11.6: simple-mode contract; SSHFS-absence IS the model",
	"bootstrap-provision-rules.md::deploy; without it they sit at READY_TO_DEPLOY, blocking SSHFS and SSH.": "spec §11.6: lifecycle-state gating, SSHFS named as a downstream consequence",

	// Deploy-mode atoms whose topic IS the runtime filesystem layout
	// (`/var/www/`, deployFiles semantics).
	"develop-deploy-files-self-deploy.md::3. The runtime container's `/var/www/` is **overwritten** with that subset —":                                                      "spec §11.6: deployFiles destination IS the topic",
	"develop-deploy-modes.md::| Cross-deploy, preserve dir | `[./out]` | Lands at `/var/www/out/...`; use when `start` references that path or artifacts live in subdirs. |": "spec §11.6: deployFiles topology table; runtime path IS the contract",
	"develop-deploy-modes.md::| Cross-deploy, extract contents | `[./out/~]` | Tilde strips `out/`; use when runtime expects assets at `/var/www/`. |":                       "spec §11.6: deployFiles tilde semantics; runtime path IS the contract",
	"develop-deploy-modes.md::is correct even when `./out` is absent locally; the build creates it.":                                                                         "spec §11.6: build-vs-source contrast clarifies deploy mechanics",

	// First-deploy / manual-deploy atoms that are explicitly env-aware
	// even at the universal-atom layer (the env contrast IS the dual
	// guardrail).
	"develop-first-deploy-execute.md::its subdomain or (in container env) SSHing into it first will fail or": "spec §11.6: container-env-only side-effect call-out; per-env atom doesn't carry it",
	"develop-manual-deploy.md::`zerops_dev_server` in container env, or harness background task in local":    "spec §11.6: dual-env tool-selection contrast (cluster-#3 of axis-M too)",

	// push-git trigger walkthrough — the dual-env rendering IS the
	// operational structure (each step has a container shell + a local
	// shell side-by-side).
	"strategy-push-git-intro.md::Before picking a trigger, confirm the git remote exists. In container env:":                         "spec §11.6: trigger-walkthrough dual-env header (one of two)",
	"strategy-push-git-intro.md::In local env:":                                                                                      "spec §11.6: trigger-walkthrough dual-env header (one of two)",
	"strategy-push-git-trigger-actions.md::- Container env: `ssh {targetHostname} \"grep -E '^\\s*- setup:' /var/www/zerops.yaml\"`": "spec §11.6: dual-env command pair (container shell)",
	"strategy-push-git-trigger-actions.md::- Local env: `grep -E '^\\s*- setup:' <your-project-dir>/zerops.yaml`":                    "spec §11.6: dual-env command pair (local shell)",
	"strategy-push-git-trigger-actions.md::In local env, create this file directly in your repo; in container env":                   "spec §11.6: trigger walkthrough explicitly contrasts both envs",
	"strategy-push-git-trigger-actions.md::- Container env: follow `strategy-push-git-push-container` to commit the":                 "spec §11.6: dual-env follow-up commit instructions (container variant)",
	"strategy-push-git-trigger-actions.md::workflow file via SSH and `zerops_deploy strategy=git-push` it.":                          "spec §11.6: continuation of the container-env follow-up line",
	"strategy-push-git-trigger-actions.md::- Local env: commit locally and `zerops_deploy strategy=git-push` (or":                    "spec §11.6: dual-env follow-up commit instructions (local variant)",
}
