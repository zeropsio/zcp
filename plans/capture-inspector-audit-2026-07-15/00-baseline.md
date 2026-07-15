# Capture Inspector audit — baseline

Date: 2026-07-15

Audit plan: `plans/capture-inspector-audit-code-review-2026-07-15.md`

## Frozen target

- Base: `8f1ef6f29e3b70809a43dbbe6363f0e8dafa03d3`
- Audited head: `b5bd502ad8ab31d854bb938460612936e221887c`
- Range: `8f1ef6f2...b5bd502a`
- Source branch had advanced to `b5da2fd1`; that later commit was deliberately excluded.
- Detached controls:
  - `/tmp/zcp-capture-audit-base`
  - `/tmp/zcp-capture-audit-head`

Commits in scope:

1. `8a6314b3 feat(capture): add persistent raw capture inspector`
2. `13daa3b7 feat(capture): add isolated forensic inspector UI`
3. `e1eaff09 fix(capture): use page scrolling for flow map`
4. `c07fb09d fix(capture): show flow details in fixed sidebar`
5. `b5bd502a docs(capture): add technical inspector handoff`

Authority order used by the audit: tests, implementation, authoritative specs, then handoff/plans.

## Environment

- macOS 26.5.2, Darwin arm64
- Go `go1.25.6 darwin/arm64`
- Node `v24.11.1`; npm `11.6.2`
- golangci-lint `v2.8.0`
- Local Playwright module and cached Chromium reused from the source worktree; no package or browser installation was performed.
- `ZCP_API_KEY`, `ANTHROPIC_API_KEY`, capture variables, and `CLAUDE_CONFIG_DIR` were absent at preflight.
- No live Zerops/provider operation or real-credential test was run.

Raw environment evidence: `tmp/capture-inspector-audit-2026-07-15/environment.txt`.

## Change size

- 97 changed files
- 23,408 insertions, 79 deletions
- Go package count: 33 base, 37 head
- Base binary: 28,135,682 bytes
- Head binary: 30,508,898 bytes
- Delta: +2,373,216 bytes
- Both native builds succeeded.

Hashes and exact figures are in `tmp/capture-inspector-audit-2026-07-15/build-comparison.txt`.

## Baseline qualification

A clean detached checkout lacks the repository's gitignored synchronized knowledge Markdown. The first base and head short-suite runs therefore failed in the same unrelated knowledge/recipe tests. The same local 38-file Markdown corpus was copied into both detached controls, matching the CI workflow's knowledge-sync precondition. With that identical precondition:

- base `go test ./... -short -count=1`: PASS
- head `go test ./... -short -count=1`: PASS

The initial and qualified logs were both retained; the initial environmental failures were not classified as feature regressions.

## Canonical real-capture corpus

The fixed local corpus contained:

- 4 capture manifests
- 107 regular files
- 39,488,875 bytes
- 144 provider exchanges
- 14 Claude sessions
- 18 MCP streams

Before any inspection, SHA-256 plus mode/size/mtime inventories were recorded. CLI inspection, metadata HTTP traversal, tagged browser traversal, reveal-gated browser detail, and screenshots left the canonical corpus byte hashes and recorded metadata unchanged.

Evidence:

- `tmp/capture-inspector-audit-2026-07-15/corpus-before.sha256`
- `tmp/capture-inspector-audit-2026-07-15/corpus-before.stat`
- `tmp/capture-inspector-audit-2026-07-15/real-corpus-cli/`
- `tmp/capture-inspector-audit-2026-07-15/real-corpus-web/`
- `tmp/capture-inspector-audit-2026-07-15/real-corpus-browser/`

## Artifact handling

Tracked reports contain no capability URL, cookie, credential, prompt, result, or raw capture body. Large logs, synthetic RED probes, metadata JSON, plaintext screenshots, and corpus hashes remain gitignored under `tmp/capture-inspector-audit-2026-07-15/`.

Audit-only probe files were copied into the gitignored evidence directory and removed from both detached worktrees. Both detached Git worktrees were clean before final verification.

## Explicit exclusions

- Later source-branch changes after `b5bd502a`
- `feat/anthropic-proxy-capture`
- production remediation
- release/install operations
- live credential/provider/Zerops tests
- destructive disk-full and OS-crash tests outside disposable synthetic fixtures
