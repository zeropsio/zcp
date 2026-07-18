# Phase 4 axis-F candidates — Codex PRE-WORK (2026-04-27)

Round type: PRE-WORK per §10.1 Phase 4 row 1
Reviewer: Codex
Inputs read: all 30 atom files read end-to-end with line numbers.

> **Artifact write protocol note (carries over).** Codex sandbox blocks artifact writes; reconstructed verbatim from text response.

## Per-atom audit

### `develop-first-deploy-asset-pipeline-local`

- **General-knowledge candidate at lines 44-47**: "The dev server drops `public/build/hot` with localhost URLs; the framework's Vite helper detects it..."
  - Verdict: DROP — recoverable: 225 B
  - Reasoning: Vite hot-file behavior and HMR routing are framework knowledge.
- **Lines 19-23**: "Any view rendering `@vite(...)` ... HTTP 500..."
  - Verdict: NUANCE-PRESERVE — recoverable: 131 B
  - Reasoning: Keep ZCP verify implication; drop framework-helper examples.
- **Lines 33-36**: "The build writes `public/build/manifest.json`..."
  - Verdict: NUANCE-PRESERVE — recoverable: 110 B

### `develop-first-deploy-asset-pipeline-container`

- **Lines 45-48**: "The dev server drops `public/build/hot`..."
  - Verdict: NUANCE-PRESERVE — recoverable: 170 B
- **Lines 20-24**: "Any view rendering `@vite(...)` ... HTTP 500..."
  - Verdict: NUANCE-PRESERVE — recoverable: 115 B
- **Lines 33-35**: "PHP-FPM picks it up on the next request."
  - Verdict: NUANCE-PRESERVE — recoverable: 55 B

### `develop-dynamic-runtime-start-local`

- **Lines 26-29**: "Use whatever dev command your framework offers (`npm run dev`, `bun --hot`, `vite`..."
  - Verdict: DROP — recoverable: 228 B
- **Lines 37-38**: "A 2xx/3xx/4xx response proves the server is up. A connection refused means it is not listening."
  - Verdict: DROP — recoverable: 95 B

### `develop-first-deploy-write-app`

- **Lines 29-33**: "Return 200 on success; embed a cheap dependency check..."
  - Verdict: NUANCE-PRESERVE — recoverable: 175 B
- **Lines 27-28**: "Not `npm install`, not `build`..."
  - Verdict: NUANCE-PRESERVE — recoverable: 45 B

### `develop-platform-rules-local`

- **Lines 25-28**: "Whatever dev command your framework gives you works: `npm run dev`, `bun --hot`, `vite`..."
  - Verdict: DROP — recoverable: 145 B

### `develop-first-deploy-scaffold-yaml`

- **Lines 49-54**: "When a foreground runtime expects assets at `ContentRootPath = CWD`..."
  - Verdict: NUANCE-PRESERVE — recoverable: 100 B

### `develop-deploy-modes`

- **Lines 26-27**: "Pick when the runtime expects assets at root..."
  - Verdict: NUANCE-PRESERVE — recoverable: 100 B

### `develop-implicit-webserver`

- **Lines 35-36**: "composer-based apps often need `public/index.php`..."
  - Verdict: DROP — recoverable: 96 B

### `develop-dev-server-triage`

- **Lines 41-49**: "server runs but is broken; read logs and response body; do NOT restart..."
  - Verdict: NUANCE-PRESERVE — recoverable: 85 B

### `bootstrap-provision-local`

- **Line 35**: "it contains secrets"
  - Verdict: DROP — recoverable: 20 B

## Total recoverable bytes

| Bucket | Count | Bytes | Notes |
|---|---:|---:|---|
| DROP | 6 | 809 B | general-LLM-knowledge prose |
| NUANCE-PRESERVE | 10 | 1086 B | partially-general; tighten to ZCP-specific only |
| KEEP-AS-ZCP-SPECIFIC | 0 | 0 B | not actually general-knowledge |
| **Total Phase 4 recoverable** | 16 | **1895 B** | target 1-2 KB ✓ |

## Phase 4 work plan (priority by bytes)

1. `develop-dynamic-runtime-start-local` — 323 B DROP
2. `develop-first-deploy-asset-pipeline-local` — 466 B (mix)
3. `develop-first-deploy-asset-pipeline-container` — 340 B (NUANCE-PRESERVE)
4. `develop-first-deploy-write-app` — 220 B (NUANCE-PRESERVE)
5. `develop-platform-rules-local` — 145 B DROP
6. `develop-first-deploy-scaffold-yaml` — 100 B
7. `develop-deploy-modes` — 100 B
8. `develop-implicit-webserver` — 96 B DROP
9. `develop-dev-server-triage` — 85 B
10. `bootstrap-provision-local` — 20 B DROP

## Risks + watch items

- Do not remove ZCP-specific behavior (`L7`, `zerops_verify`, `zerops_deploy`, SSHFS, `deployFiles`, container-restart) just because it mentions common concepts.
- Asset-pipeline atoms: preserve build-before-verify/deploy ordering.
- DeployFiles atoms: preserve tilde-extract vs preserve distinction.
- Dev-server triage: keep all `DevServerResult` state-to-next-action mappings; tighten only generic HTTP-class explanation.

20 atoms had no axis-F content after full read — no Phase 4 work needed in those files.
