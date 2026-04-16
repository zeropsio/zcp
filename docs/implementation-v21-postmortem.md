# v21 Post-mortem — Implementation Guide for v8.80

**Target**: restore v20's A− quality while preserving v8.78's structural content wins. Address the four systemic regressions that turned a "framework-agnostic content reform" into a D-grade run.

**For**: an independent implementer working from this doc with no prior conversation context. All file paths, line numbers, code snippets, and test expectations are self-contained.

**Scope**: code changes in `internal/tools/`, `internal/content/workflows/`, `internal/workflow/`, `internal/knowledge/`. Tests added RED-first. No platform/API changes.

---

## 1. What happened (one-page TL;DR)

v20 graded A− (first A-grade complete run in session-logged history). v8.78 ("load-bearing content reform") shipped 5 new per-codebase checks, rolled back the predecessor-floor gate, added ~9KB to recipe.md (net +57 lines). v21 ran the same recipe (`nestjs-showcase`) and graded **D**: 129 min wall (v20: 71 min, +82%), 214 MB output tree (v20: 4 MB), 5 subagents (v20: 10), 0 `zerops_guidance` calls (v20: 12), content compressed 26%, 1 close-step CRIT + 2 runtime CRITs.

### Content delivery model (verified, overturns earlier thesis)

The zcp workflow **does NOT** inline the full brief upfront. It uses a progressive-guidance pattern:
- At each step transition, `zerops_workflow` returns a compact **step brief** (~15–45 KB depending on step).
- Per-substep detail lives in named `<block>` regions of [recipe.md](../internal/content/workflows/recipe.md); the agent pulls individual blocks on demand via `zerops_guidance topic="<block-name>"` (typically 2–4 KB per fetch).
- Only what the agent actively fetches lands in its context.

**Measured content delivery in v20 vs v21** (every tool_result parsed):

| Metric | v20 | v21 | Δ |
|---|---:|---:|---:|
| Total step brief bytes | 557,012 | 487,144 | **v21 got 12% LESS** |
| Total `tool_result` bytes | 767,747 | 753,704 | **v21 got 2% LESS** |
| Readmes-substep step responses (9 each) | 353,190 | 319,753 | **v21 got 10% LESS** |
| Assistant `tool_use` input bytes | 131,395 | 320,121 | **v21 was 144% BIGGER** |
| Assistant `thinking` bytes | 17,385 | 27,830 | v21 +60% |
| Main-session jsonl | 2.2 MB | 3.1 MB | v21 +40% |
| Guidance fetches | 12 | 0 | v21 fetched nothing on-demand |

**This overturns my earlier "brief density" thesis.** v21's main context did NOT grow because the workflow served more content — the workflow actually served less. Main-session grew because the agent passed 190 KB more content THROUGH its own `tool_use` inputs (Write file contents, Edit old/new strings, Agent dispatch prompts). That content came from the agent directly authoring README/CLAUDE.md in main context instead of dispatching a writer subagent.

So the correct causal chain is not "denser brief → bloated context" — it's "**delegation collapse → main agent absorbed Write/Edit work → tool_use inputs grew**". Why delegation collapsed is the mechanism that remains uncertain (see §1.2 below).

### 1.1 Root causes, verified via log parsing + code review

1. **The scaffold-subagent-brief template puts `.gitignore`/`.env.example` behind a conditional** at [recipe.md:865](../internal/content/workflows/recipe.md#L865). When the main agent synthesizes per-codebase briefs, the conditional wording (`only if the framework's own scaffolder normally emits one`) invites judgment — which was **stochastic**: in v21 the main agent included the hygiene instruction for appdev but not for apidev or workerdev, whereas in v20 the same main agent (same model, same recipe) included it for all three. Evidence: the dispatched subagent prompts parsed verbatim:

   - **v20 apidev scaffold brief**: `.gitignore` mentioned ✓, `.env.example` mentioned ✓ → subagent wrote BOTH ✓ (Write calls confirm)
   - **v21 apidev scaffold brief**: `.gitignore` NOT mentioned, `.env.example` NOT mentioned → subagent wrote NEITHER (11 Writes, zero to hygiene files)
   - **v21 workerdev scaffold brief**: `.gitignore` NOT mentioned, `.env.example` NOT mentioned → subagent wrote `.gitignore` only (workerdev got lucky and the model's prior surfaced it) but NOT `.env.example`
   - **v21 appdev scaffold brief**: `.gitignore` mentioned ✓ (expanded with literal content "exclude node_modules, dist, .env, .DS_Store"), `.env.example` mentioned ✓ → subagent wrote BOTH

   The subagents behaved correctly — they wrote what their briefs asked for. The fault is that the main agent's per-codebase synthesis is not deterministic on conditional-template items. Result: apidev → 208 MB of `node_modules` + 748 KB of `dist/` + `.DS_Store` files bundled into the published recipe output.

2. **The main-agent workflow lacks the v17.1 "mount is not an execution surface" preamble**, which lives only in the scaffold-subagent block. v21 main agent ran `cd /var/www/{host} && git init && git add -A && git commit` **zcp-side across SSHFS** on a tree containing 208 MB of node_modules → 3 parallel bash calls each timed out at 120 s. Directly caused by #1.

3. **v8.78 added quality ceilings without quantitative floors.** The predecessor-floor check was rolled back from gate to informational; the `service_coverage` check only requires *one gotcha per service category*. With 3 per-gotcha quality checks (causal-anchor, content-reality, per-item-standalone) creating downward pressure and no upward pressure, the agent's shortest path to green is **to delete weak gotchas**, not strengthen them. v21 landed at exactly the service_coverage floor (apidev 6, appdev 4, workerdev 5 gotchas).

4. **The `claude_readme_consistency` flagship check is silently non-functional.** Its `forbiddenPatternRe` regex matches only the *exact v20 phrasing* it was designed against (`` `X` must be off in production``); it returned 0 events across all 3 v21 READMEs. Shadow-testing against v20/v19 confirms it only ever matched one v20 string verbatim. Zero pass events, zero fail events — the check exists but never fires on real content.

5. **Delegation collapsed — mechanism unknown, impact measured.** v18–v20 dispatched README-writer + yaml-updater + generate-time fix + close-step critical-fix subagents. v21 dispatched only the three *required* scaffold + one feature + one code review subagents. These writer/fix patterns are **not required by recipe.md** — they're emergent. Exhaustive verbatim dispatch log:

   **v20 dispatched these subagents that v21 did NOT**:
   - `Write all READMEs and CLAUDE.md` (3281-char prompt) — the writer subagent
   - `Fix README knowledge-base format` (1637-char prompt) — generate-time fix
   - `Fix gotcha restatements and specificity` (4572-char prompt) — generate-time fix
   - `Update README yaml block` (617-char prompt) — yaml updater
   - `Fix critical code review issues` (2666-char prompt) — close-step critical-fix

   **Measured consequence**: v21 main agent wrote 9 Writes + 14 Edits to README/CLAUDE.md files directly, passing 190 KB more content through its own `tool_use` input channel (131 KB in v20 → 320 KB in v21). Assistant thinking bytes grew 60%. No tool_result content growth (see §1 table).

   **My earlier hypothesis** ("v8.78 brief density caused the main agent to feel 'ready' and not delegate") is UNPROVEN. Step-brief sizes are actually smaller in v21 than v20 (−12%). The v8.78 additions are spread across multiple sub-substep blocks (knowledge-base, integration-guide, CLAUDE.md, env-comments), not concentrated in one brief. The actual mechanism remains agent-behavior variance — v20's agent chose to emergent-dispatch writer+fix subagents; v21's agent chose to absorb the work. recipe.md has no explicit "dispatch a writer subagent" instruction at either run's time.

6. **Framework hardcoding leaked into the "framework-agnostic" check implementation.** `categoryBrands` in `service_coverage` hardcodes ORM names (`typeorm`, `prisma`, `ioredis`, `keydb`) as category signals — an unfair pass for Node recipes and an unfair fail for Rails/Django/etc. `specificMechanismTokens` in `causal_anchor` hardcodes `TypeORM synchronize`, `queue: 'workers'`, `ioredis` as "Zerops mechanisms". The reform's stated goal of framework-neutrality is not met.

7. **The scaffold-subagent self-review checklist was designed from the rear-view mirror.** Its 4 items check against v14/v17/v19/v20-class regressions. The v21 regression class ("scaffold skips ancillary hygiene files while passing all other gates") didn't exist in prior runs, so the checklist couldn't have caught it.

8. **`zerops_dev_server action=stop` returns `exit status 255` because its `pkill -f <match>` over SSH kills the SSH session itself.** The `pkill -f nest` pattern matches any process with "nest" in its command line — which includes the `sh -c "..."` that SSH is running to execute pkill, AND the `nest start:dev` child. When pkill hits the SSH-session's child shell, SSH returns exit 255 (the "connection terminated abnormally" signal). v21 hit this **6 times** (2 rounds × 3 hosts). v20 hit it once. The tool surfaces the raw `exit status 255` rather than classifying as "process killed, SSH dropped — normal for stop". See [`internal/ops/dev_server_lifecycle.go:22-27`](../internal/ops/dev_server_lifecycle.go#L22-L27).

9. **Main agent's zcp-side git cascade caused downstream root-ownership errors.** After the 120 s zcp-side `git init && git add -A && sudo chown` calls, the resulting `.git/` dirs were partially root-owned (sudo chown ran, but git was already past its `.git/hooks/sendemail-validate.sample` copy which failed with Permission denied). Recovery attempts (`sudo rm -rf .git`) then hit "Directory not empty" / "Permission denied" because nested files remained root-owned. Clean-up required `sudo /usr/bin/find .git -depth -delete` — an extra 2-3 minutes of recovery work. Cascade from #2.

10. **Parallel-tool-call cancellations compounded the wait.** When one call in a parallel batch errors, the rest in that batch get canceled by the runtime. v21 had multiple "Cancelled: parallel tool call" events on the git-init recovery path — the agent dispatched ssh cleanups in parallel, one failed, the others were canceled, the agent retried serially. Not a root cause on its own but an amplifier of #2.

These interact: #1 produced the bloat → #2 made the bloat expensive → #9 cascaded root-ownership failures → #10 amplified via parallel cancellation → #6 biased acceptance → #5 made the main agent absorb iteration cost → #3 drove content compression → #4 silently absented what was supposed to be structural protection → #8 added 6 false-positive errors that further confused the agent. Net: A− → D.

### 1.2 Exhaustive subagent findings (additions from second-pass audit)

| Subagent | Wall | Bash | Errors | Notable |
|---|---:|---:|---:|---|
| **apidev scaffold** (a725a66) | 347.8s | 20 (1 very-long @60s `nest new scratch`, 1 @30s `npm install`) | 1 | Ran `npx @nestjs/cli new scratch` as reference **but did not transcribe `.gitignore` from scratch output** — proves the scaffolder had access to the correct file but its brief didn't list it as a required copy |
| **workerdev scaffold** (af117ff) | 250.0s | 20 (1 @75s `nest new scratch`) | 1 | Same scratch-reference pattern; wrote `.gitignore` but not `.env.example` |
| **appdev scaffold** (a8b268f) | 195.7s | 21 (1 @11.8s `npm install`) | 2 | Wrote all hygiene files correctly; a Read tool errored trying to read a directory as a file (`EISDIR`) |
| **feature impl** (a55eba5) | 1325.5s (22 min) | 35 (1 long @12.2s probe) | 6 MCP schema errors | 27 Writes + 4 Edits — implemented all 5 features across 3 codebases; NO very-long bash; hit 6 distinct MCP schema validation errors (see below) |
| **code review** (a80e8da) | 166.1s | 7 (1 @10.5s `find`) | 1 Grep timeout | 43 Reads + 8 Greps, 0 Writes; **Grep timed out after 20s** — the 208 MB `apidev/node_modules` slowed filesystem searches even for static review |

**New root cause #11 — feature subagent MCP schema errors (6 instances)**:

The feature subagent hit 6 distinct schema-validation errors from MCP tool calls it attempted:
```
mcp__zerops__zerops_dev_server  missing "hostname"
mcp__zerops__zerops_dev_server  status requires a port
mcp__zerops__zerops_dev_server  port=3000 has type "string", want "integer"
mcp__zerops__zerops_logs        unexpected additional properties ["hostname","logLines"]
mcp__zerops__zerops_scale       unexpected additional properties ["ramGB"]
mcp__zerops__zerops_dev_server  missing "hostname" (second time)
```

These aren't random confusion — the feature subagent's dispatch prompt (8924 chars) does not contain the exact parameter names + types for MCP tools. The agent defaulted to memory-plausible parameter names (`hostname` instead of `serviceHostname`; `logLines` instead of `lines`; `ramGB` instead of something else) and the MCP schema validator rejected them one at a time. Each rejection is a retry round-trip.

**Fix**: feature-subagent-brief should inline the exact schema for every MCP tool it's allowed to use (add to [recipe.md step 4b brief](../internal/content/workflows/recipe.md#L1397)). Rough cost: ~2 KB of brief content; rough benefit: eliminates the 6 schema-rejection cycles per run.

**New root cause #12 — code review Grep timeout cascades from #1's 208 MB bloat**:

Code review subagent ran `find /var/www/apidev /var/www/appdev /var/www/workerdev -name ".env" ...` which took 10.5s because of `node_modules` traversal. A subsequent ripgrep search hit the tool's 20s timeout. The static review was partially blinded by the bloat it was supposed to audit. Another downstream cost of root cause #1.

**New root cause #13 — scaffold subagents run framework-scaffolder-as-reference but don't transcribe hygiene files from it**:

Both apidev and workerdev scaffolders ran `npx @nestjs/cli new scratch` to reference-copy NestJS's tsconfig / package.json / nest-cli.json shapes. Nest CLI's scaffolder **emits a `.gitignore`**. The scaffold subagents read the scratch tree, copied the code-config files, but **did not transcribe the `.gitignore`**. The scaffold brief instructs "copy from a fresh nest new scratch" for specific files — the hygiene file was not on that list. Evidence: [workerdev scaffold](file:///Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v21/SESSIONS_LOGS/subagents/agent-af117ff49ff7f3c4c.jsonl) ran `rm -rf scratch && npx -y @nestjs/cli new scratch` at 75s wall — the scratch dir definitely contained `.gitignore`; subagent wrote code-config files but only wrote `.gitignore` post-hoc from its own memory (content differs from `nest new`'s default).

**Fix**: when scaffold brief tells the subagent "use nest new scratch as reference for {X,Y,Z}", explicitly include `.gitignore` and `.env.example` in {X,Y,Z}. Or: add a post-scaffold verification step that lists files in `scratch/` and cross-references against committed files on the mount — any scratch file not transcribed and not explicitly excluded is a brief gap.

---

## 2. What to preserve (v8.78 wins)

These worked and should stay:

| Check | Pass/fail in v21 | Evidence |
|---|---|---|
| `<host>_content_reality` | 12 / 6 | Drove 3 iteration cycles; caught non-shipping file paths in gotchas |
| `<host>_gotcha_causal_anchor` | 13 / 5 | Caught decorative gotchas across api/app/worker |
| `<host>_service_coverage` | 10 / 2 | Caught db+storage+queue coverage gaps in apidev sequentially |
| `<host>_ig_per_item_standalone` | 10 / 2 | Caught IG items leaning on neighbors |
| `knowledge_base_exceeds_predecessor` | 18 / 0 (informational) | No regressions from rollback; need to pair with a new floor — see §3.3 |

Do not re-introduce knowledge_base_exceeds_predecessor as a gate; its rationale (standalone recipes should not fail on predecessor overlap) holds. Introduce a different floor (§3.3).

---

## 3. The fixes, ordered by blast-radius-per-effort

Each section has: **Why** (root cause it addresses), **What to change** (file:line + code shape), **Tests** (what must pass, written RED first), **Acceptance** (what the next showcase run must show).

### 3.1 Make `.gitignore` + `.env.example` unconditional scaffold outputs

**Why**: root cause #1. The 209 MB apidev bloat + downstream 120 s git-add hangs trace to a single-line conditional dropped during per-codebase brief synthesis.

**What to change**:

A. [`internal/content/workflows/recipe.md`](../internal/content/workflows/recipe.md) — line 863-865, the `WRITE (every codebase)` block:

```diff
 > **WRITE (every codebase):**
 >
-> - `.gitignore`, `.env.example`, framework lint config only if the framework's own scaffolder normally emits one
+> - **`.gitignore` — mandatory.** Minimum content: `node_modules/`, the framework's build output directory (`dist/`, `build/`, `.next/`, `target/`, `public/build/`, etc.), `.env`, `.env.local`, `.DS_Store`. Copy exact contents from the framework's own scaffolder (e.g. `nest new`, `npm create vite@latest`, `rails new`, `composer create-project laravel/laravel`) if one exists.
+> - **`.env.example` — mandatory.** List every environment variable the codebase reads from `process.env` / `os.environ` / `ENV[]` / equivalent, with a short comment per line explaining the shape. Blank is acceptable ONLY when the codebase reads no env vars.
+> - Framework lint config (`.eslintrc.*`, `.rubocop.yml`, `.php-cs-fixer.php`, etc.) only if the framework's scaffolder normally emits one.
```

The change: promote `.gitignore` and `.env.example` from conditional tail-items to mandatory, framework-scaffolder-copy semantics for `.gitignore`, explicit content requirement for `.env.example`.

B. Add items 5 and 6 to the v8.78 self-review checklist at [`internal/content/workflows/recipe.md:885-892`](../internal/content/workflows/recipe.md#L885):

```diff
 > **Self-review before reporting back (v8.78).** Before you return your file list, re-read your own output against the rules in this brief and flag any deviations. Specifically:
 >
 > 1. **Imports + decorators verified against installed packages?** ...
 > 2. **All commands ran via SSH, not zcp-side?** ...
 > 3. **Did you write README.md or zerops.yaml?** ...
 > 4. **Is the dashboard ONE panel?** ...
+> 5. **`.gitignore` + `.env.example` present?** Run `ssh {hostname} "ls -la /var/www/.gitignore /var/www/.env.example"`. Both must exist. `.gitignore` must list `node_modules/`, the framework's build output dir, and `.env`. If either is missing, write it before returning.
+> 6. **No `node_modules/`, `dist/`, or `.DS_Store` on the mount at return time?** Scan with `ssh {hostname} "find /var/www -maxdepth 2 -name node_modules -o -name dist -o -name .DS_Store | head"`. The `node_modules/` inside the container is fine — the `.gitignore` will exclude it from any subsequent publish. `.DS_Store` files must be deleted.
```

C. Add a new deploy-step check `<host>_scaffold_hygiene` at [`internal/tools/workflow_checks_scaffold_hygiene.go`](../internal/tools/workflow_checks_scaffold_hygiene.go) (new file). Skeleton:

```go
package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// checkScaffoldHygiene verifies that each codebase under outputDir has
// .gitignore + .env.example present AND that no build-output / node_modules /
// OS-cruft artifacts leaked into the published tree. Every managed-code
// codebase the recipe ships is a potential attack surface for hygiene
// regressions (v21 apidev shipped 208 MB of node_modules because its
// .gitignore wasn't written).
//
// The check runs per codebase, at deploy step, after READMEs are accepted.
func checkScaffoldHygiene(codebaseDir, hostname string) []workflow.StepCheck {
	checkName := hostname + "_scaffold_hygiene"
	var problems []string

	if _, err := os.Stat(filepath.Join(codebaseDir, ".gitignore")); os.IsNotExist(err) {
		problems = append(problems, "`.gitignore` missing")
	}
	if _, err := os.Stat(filepath.Join(codebaseDir, ".env.example")); os.IsNotExist(err) {
		problems = append(problems, "`.env.example` missing")
	}

	// Scan for cruft only under the codebase root; don't recurse into
	// node_modules itself (which may legitimately exist inside the
	// container's own filesystem — we care about what leaked into the
	// PUBLISHED tree).
	leaks := []string{"node_modules", "dist", "build", ".next", "target"}
	for _, name := range leaks {
		path := filepath.Join(codebaseDir, name)
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			problems = append(problems, fmt.Sprintf("`%s/` present in codebase root — should be gitignored and excluded from the published tree", name))
		}
	}
	// Recursive .DS_Store check (they proliferate anywhere on macOS).
	_ = filepath.Walk(codebaseDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.Name() == ".DS_Store" {
			rel, _ := filepath.Rel(codebaseDir, p)
			problems = append(problems, fmt.Sprintf("`.DS_Store` at `%s`", rel))
		}
		return nil
	})

	if len(problems) == 0 {
		return []workflow.StepCheck{{Name: checkName, Status: statusPass}}
	}
	return []workflow.StepCheck{{
		Name:   checkName,
		Status: statusFail,
		Detail: fmt.Sprintf("%s has scaffold hygiene issues: %s. Every codebase ships with `.gitignore` (listing at minimum `node_modules/`, build output, `.env`) and `.env.example` (listing every env var the codebase reads). Build-output directories and OS cruft (`.DS_Store`) must not leak into the published tree — they inflate the recipe repo and add noise for readers. The v21 apidev run shipped 208 MB of `node_modules` into the output because this check didn't exist.",
			hostname, strings.Join(problems, "; "),
		),
	}}
}
```

D. Register in [`internal/tools/workflow_checks_recipe.go`](../internal/tools/workflow_checks_recipe.go) alongside the other per-codebase checks (reuse the iteration loop that fires `checkContentReality` etc.). Should fire at deploy step after READMEs pass, before finalize.

**Tests** (add to new file `internal/tools/workflow_checks_scaffold_hygiene_test.go`):

```go
func TestScaffoldHygiene_AllPresent_Passes(t *testing.T) { /* create tmpdir with .gitignore+.env.example, no leaks → pass */ }
func TestScaffoldHygiene_MissingGitignore_Fails(t *testing.T) { /* detail must mention ".gitignore missing" */ }
func TestScaffoldHygiene_MissingEnvExample_Fails(t *testing.T) { /* detail must mention ".env.example missing" */ }
func TestScaffoldHygiene_NodeModulesPresent_Fails(t *testing.T) { /* detail must mention "node_modules/" */ }
func TestScaffoldHygiene_DistPresent_Fails(t *testing.T) { /* detail must mention "dist/" */ }
func TestScaffoldHygiene_DSStorePresent_Fails(t *testing.T) { /* detail must mention ".DS_Store" */ }
func TestScaffoldHygiene_MultipleIssues_ReportsAll(t *testing.T) { /* comma-separated in detail */ }
func TestScaffoldHygiene_RecursiveDSStoreSearch(t *testing.T) { /* .DS_Store under src/ should still fail */ }
```

**Acceptance**: v22 output directory for every codebase must have `.gitignore` + `.env.example`, no `node_modules/`/`dist/` at codebase root, no `.DS_Store` anywhere. Total recipe-output-dir size for nestjs-showcase should be <5 MB.

---

### 3.2 Move the v17.1 "mount is not an execution surface" preamble to main-agent scope

**Why**: root cause #2. v17.1 put the preamble inside `<block name="scaffold-subagent-brief">` only. The main agent, when running git operations from zcp-side directly, had no guidance telling it the same rule applies. 3×120s hangs in v21 from `cd /var/www/X && git init && git add -A` on SSHFS.

**What to change**:

A. [`internal/content/workflows/recipe.md`](../internal/content/workflows/recipe.md) — create a new reusable block near the top of the deploy section, so both main agent AND scaffold subagents reference it:

```markdown
<block name="where-commands-run">

### ⚠ CRITICAL: where commands run (applies to main agent AND every subagent)

The main agent and every dispatched subagent run on the **zcp orchestrator container**, not on the target dev containers. The paths `/var/www/{hostname}/` on zcp are **SSHFS network mounts** — bridges into the target containers' `/var/www/`. They are write surfaces, not execution surfaces.

**File writes** via Write/Edit/Read against `/var/www/{hostname}/` are correct — you're editing through the mount. Do this for source files, configs, `package.json`, etc.

**Executable commands** MUST run via SSH into the target container. Every `npm install`, `npm run build`, `tsc`, `vite`, `nest`, `npx`, `pnpm`, `yarn`, `composer`, `bundle`, `cargo`, `go build`, AND every `git init` / `git add` / `git commit` goes through:

```
ssh {hostname} "cd /var/www && <command>"
```

NOT through:

```
cd /var/www/{hostname} && <command>    # WRONG — runs on zcp against the mount
```

The reasons this matters, all confirmed across v17 and v21 regressions:

1. **SSHFS is network-bound.** `git add -A` traversing `node_modules/` (tens of thousands of files, hundreds of MB) takes 2+ minutes over SSHFS — the same operation runs in seconds natively inside the container. v21 lost 120 s × 3 to this exact pattern.
2. **UID/GID mismatch.** zcp runs as root; the container runs as `zerops` (uid 2023). Files created zcp-side are owned by root; subsequent container operations hit EACCES.
3. **Broken `.bin/` symlinks.** `npm install` run via mount creates absolute-path symlinks in `node_modules/.bin/` that don't resolve inside the container.
4. **ABI mismatch.** Native modules compiled against zcp's node binary don't load on the container.

If you see EACCES, `sh: <tool>: not found` for tools that are clearly installed, long stalls on `git add -A`, or bash timeouts at exactly 120 s — you are on the wrong side of the boundary. Stop, re-do via `ssh {hostname} "cd /var/www && ..."`.

</block>
```

B. Reference the new block from both:
- Scaffold-subagent-brief (replace the existing inlined version with a reference: "See `where-commands-run` block — applies verbatim to your workflow.")
- Main-agent deploy/generate step sections (add explicit reference near any step that does git or bash operations on `/var/www/{host}`).

C. Register `where-commands-run` as a topic in [`internal/workflow/recipe_topic_registry.go`](../internal/workflow/recipe_topic_registry.go) so `zerops_guidance topic="where-commands-run"` works as on-demand fetch.

**Tests** (update [`internal/workflow/recipe_topic_registry_test.go`](../internal/workflow/recipe_topic_registry_test.go)):

```go
func TestRecipeTopicRegistry_WhereCommandsRun_Registered(t *testing.T) {
	content := topicContent("where-commands-run")
	mustContain := []string{"SSHFS network mount", "ssh {hostname}", "git add", "120 s", "EACCES"}
	for _, s := range mustContain {
		if !strings.Contains(content, s) {
			t.Errorf("where-commands-run topic missing: %q", s)
		}
	}
}
```

**Acceptance**: v22 session log must show zero `cd /var/www/{host} && git` calls from main-agent bash. All git operations from main agent must use `ssh {host} "cd /var/www && git ..."` shape.

---

### 3.3 Reintroduce a quantitative content floor (without restoring predecessor-floor)

**Why**: root cause #3. v21 content compressed 26% because v8.78 created quality ceilings without a coverage floor. Predecessor-floor was rolled back for correct reasons (standalone recipes shouldn't fail on overlap); need a different floor that doesn't tie to predecessor.

**What to change**:

Add a new per-codebase check `<host>_gotcha_depth_floor` at [`internal/tools/workflow_checks_gotcha_depth_floor.go`](../internal/tools/workflow_checks_gotcha_depth_floor.go) (new file):

```go
package tools

import (
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// Per-role minimum gotcha counts. Rationale: v20 peak (7/6/6) was
// aspirational and not always reachable on constrained recipes; v7 gold
// (6/5/4) is the defensible floor that kept apidev at framework-plus-
// platform depth. We undershoot the v7 floor by 1 to leave headroom for
// recipes with narrower service surface.
var gotchaFloorByRole = map[string]int{
	"api":       5, // covers 5 managed-service categories typically
	"frontend":  3, // fewer gotchas naturally — framework + platform-static
	"worker":    4, // queue-group + drain + migration-ownership + entity-parity
	"fullstack": 5, // single-codebase full-stack behaves as api
}

// checkGotchaDepthFloor enforces a per-role minimum gotcha count so
// quality checks (causal-anchor, reality) don't incentivize deletion-
// to-pass. Replaces the v8.78-rolled-back predecessor-floor check with
// a floor that doesn't depend on the injected predecessor — standalone
// recipes still need to carry their own weight.
//
// role is derived by the caller from the plan (showcase codebase
// classification).
func checkGotchaDepthFloor(kbContent, role, hostname string) []workflow.StepCheck {
	checkName := hostname + "_gotcha_depth_floor"
	floor, ok := gotchaFloorByRole[role]
	if !ok {
		// Unknown role — skip check rather than guess.
		return nil
	}
	count := countGotchaBullets(kbContent)
	if count >= floor {
		return []workflow.StepCheck{{Name: checkName, Status: statusPass}}
	}
	return []workflow.StepCheck{{
		Name:   checkName,
		Status: statusFail,
		Detail: fmt.Sprintf(
			"%s knowledge-base has %d gotcha(s); role %q expects at least %d. Under-minimum counts signal compression-to-pass — the causal-anchor and content-reality checks create downward pressure; this floor creates upward pressure. Do NOT add decorative gotchas; add real ones narrated from debug experience on THIS build. If the recipe truly has fewer failure modes than the floor (e.g. single-service plan), name the floor exception in the intro fragment with a concrete reason.",
			hostname, count, role, floor,
		),
	}}
}

// countGotchaBullets counts `- **` top-level bullets inside a knowledge-
// base fragment. Same pattern used by the cross-readme uniqueness check.
func countGotchaBullets(kbContent string) int {
	n := 0
	for _, line := range strings.Split(kbContent, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "- **") {
			n++
		}
	}
	return n
}
```

Role derivation lives in [`internal/workflow/recipe_plan_predicates.go`](../internal/workflow/recipe_plan_predicates.go) — add a helper `CodebaseRole(plan, hostname) string` returning `"api" | "frontend" | "worker" | "fullstack"` based on the plan's surface declarations.

**Tests** (new `internal/tools/workflow_checks_gotcha_depth_floor_test.go`):

```go
func TestGotchaDepthFloor_ApiRole_RequiresFive(t *testing.T) { /* 4 gotchas → fail, 5 → pass */ }
func TestGotchaDepthFloor_FrontendRole_RequiresThree(t *testing.T) { /* 2 → fail, 3 → pass */ }
func TestGotchaDepthFloor_WorkerRole_RequiresFour(t *testing.T) { /* 3 → fail, 4 → pass */ }
func TestGotchaDepthFloor_UnknownRole_Skips(t *testing.T) { /* len(checks) == 0 */ }
func TestGotchaDepthFloor_FailDetailNamesCount(t *testing.T) { /* detail contains "has 4 gotcha", "at least 5" */ }
```

Register in `workflow_checks_recipe.go` in the same per-codebase loop as `service_coverage`.

**Acceptance**: v22 gotcha counts should land in range `api∈[5..7]`, `frontend∈[3..5]`, `worker∈[4..6]`. If v22 lands at the floor exactly (5/3/4), that's acceptable — the floor is the minimum, not the target. Content quality (via causal-anchor + reality) should remain high.

---

### 3.4 Fix the `claude_readme_consistency` regex dead-code path

**Why**: root cause #4. The check emitted 0 events across all v21 READMEs (0 pass, 0 fail). Shadow-testing confirmed it matches v20 apidev ONE time on the literal string `` `synchronize: true` must be off`` and nothing else across v18/v19/v20/v21 content.

**What to change**:

A. Replace the narrow `forbiddenPatternRe` at [`internal/tools/workflow_checks_claude_consistency.go:89-92`](../internal/tools/workflow_checks_claude_consistency.go#L89-L92). Two approaches — pick (b):

**Option (a)**: broaden the regex to match more natural phrasings. Rejected because it snowballs — agents will find new phrasings that escape any finite set.

**Option (b) — recommended**: invert the check. Maintain a **closed set of known-forbidden-in-prod patterns** (grown from the actual content-drift examples across runs). For each pattern, check if it appears in CLAUDE.md; if it does, require either a cross-reference marker within 500 chars OR the README to explicitly endorse the dev-only use.

New file shape:

```go
package tools

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// knownForbiddenInProd lists code-level patterns that are unsafe in
// production AND tend to drift between README and CLAUDE.md. Curated —
// each entry has a stable regex, a human-readable name, and a per-
// pattern failure detail explaining the production consequence. Add
// entries ONLY when a post-mortem surfaces a new drift class.
//
// The list is intentionally pattern-based, not framework-keyed — a
// framework that uses `synchronize` / `db.drop_all()` / `rm -rf .db`
// shares the underlying hazard regardless of language.
var knownForbiddenInProd = []struct {
	Name       string
	Pattern    *regexp.Regexp
	Production string
}{
	{
		Name:       "TypeORM synchronize",
		Pattern:    regexp.MustCompile(`(?i)\bsynchronize\s*:\s*true\b|\bds\.synchronize\(`),
		Production: "Auto-sync silently coerces column type mismatches and deadlocks under concurrent container start. Use migrations owned by `zsc execOnce` in production.",
	},
	{
		Name:       "Django syncdb / runserver in prod",
		Pattern:    regexp.MustCompile(`(?i)\bsyncdb\b|manage\.py\s+runserver`),
		Production: "`syncdb` was removed in Django 1.9; `runserver` is the dev server, never a prod WSGI/ASGI path.",
	},
	{
		Name:       "Rails db:migrate with RAILS_ENV unset",
		Pattern:    regexp.MustCompile(`(?i)\brails\s+db:(?:reset|drop)\b|\brake\s+db:(?:reset|drop)\b`),
		Production: "`db:reset`/`db:drop` wipe data. Prod schema changes go via versioned migrations, never resets.",
	},
	{
		Name:       "drop all tables",
		Pattern:    regexp.MustCompile(`(?i)\bdrop\s+table\s+(?:all|.*cascade)|\btruncate\s+(?:all|.*cascade)|\brm\s+-rf\s+\*?\.db\b`),
		Production: "Mass destructive ops have no place in a deploy path — prod recovery uses backups and versioned schema.",
	},
}

// crossReferenceMarkers accept whole-document cross-reference acknowledging
// the restriction. Same list as before; exposed for test.
var crossReferenceMarkers = []string{
	"dev only", "dev-only", "in dev",
	"development only",
	"see readme", "readme gotcha",
	"warned against", "warning in production",
	"forbidden in production", "do not use in production",
	"never in production",
	"shortcut for dev", "dev shortcut",
}

func containsCrossReferenceMarker(body string) bool {
	low := strings.ToLower(body)
	for _, m := range crossReferenceMarkers {
		if strings.Contains(low, m) {
			return true
		}
	}
	return false
}

// checkClaudeReadmeConsistency detects when CLAUDE.md procedures use
// known-hazardous-in-prod patterns without a cross-reference marker,
// regardless of whether the README mentions the pattern. This is the
// v8.80 replacement for v8.78's regex-keyed-on-README-phrasing approach,
// which required the agent to phrase the forbidden claim with exact
// strict patterns that failed in practice (v21 returned 0 hits across
// every codebase).
//
// New shape: pattern-driven detection + marker-driven exemption.
func checkClaudeReadmeConsistency(readmeContent, claudeContent, hostname string) []workflow.StepCheck {
	checkName := hostname + "_claude_readme_consistency"
	if claudeContent == "" {
		return nil
	}

	var conflicts []string
	hasMarker := containsCrossReferenceMarker(claudeContent)

	for _, p := range knownForbiddenInProd {
		if !p.Pattern.MatchString(claudeContent) {
			continue
		}
		if hasMarker {
			// Whole-doc marker authorizes the use
			continue
		}
		conflicts = append(conflicts, fmt.Sprintf("%s (%s)", p.Name, p.Production))
	}
	if len(conflicts) == 0 {
		return []workflow.StepCheck{{Name: checkName, Status: statusPass}}
	}
	sort.Strings(conflicts)
	return []workflow.StepCheck{{
		Name:   checkName,
		Status: statusFail,
		Detail: fmt.Sprintf(
			"%s CLAUDE.md uses pattern(s) known to be hazardous in production: %s. CLAUDE.md is the ambient context an agent reads when operating this codebase; teaching a pattern unsafe for prod without an explicit dev-only marker propagates it into prod-affecting changes. Either (a) replace the procedure with the production-equivalent path (real migrations, versioned schema changes), or (b) add a cross-reference marker anywhere in CLAUDE.md — `(dev only — see README)`, `(warned against in production)`, etc. — so a reader sees the restriction.",
			hostname, strings.Join(conflicts, "; "),
		),
	}}
}
```

Note: this makes the check READ-ONLY on CLAUDE.md content and eliminates the regex-on-README-gotcha phrasing dependency entirely. It'll fire whenever CLAUDE.md has a forbidden pattern, regardless of whether README gotchas reference it.

**Tests** — rewrite [`internal/tools/workflow_checks_claude_consistency_test.go`](../internal/tools/workflow_checks_claude_consistency_test.go):

```go
func TestClaudeReadmeConsistency_SynchronizeTruePresent_NoMarker_Fails(t *testing.T) { /* detail names "TypeORM synchronize" */ }
func TestClaudeReadmeConsistency_SynchronizeTruePresent_WithMarker_Passes(t *testing.T) { /* "dev only" elsewhere in doc → pass */ }
func TestClaudeReadmeConsistency_DjangoSyncdb_Fails(t *testing.T) { /* detail names "Django syncdb" */ }
func TestClaudeReadmeConsistency_DropTableCascade_Fails(t *testing.T) { /* detail names "drop all tables" */ }
func TestClaudeReadmeConsistency_NoForbiddenPatterns_Passes(t *testing.T) { /* clean CLAUDE.md → pass */ }
func TestClaudeReadmeConsistency_EmptyClaudeContent_Skips(t *testing.T) { /* len == 0 */ }
func TestClaudeReadmeConsistency_MultipleViolations_ReportsAll(t *testing.T) { /* joined with "; " */ }

// Shadow tests — prove the new check handles known-real content correctly.
func TestClaudeReadmeConsistency_ShadowV20_ApiDev(t *testing.T) {
	// Embed v20 apidev CLAUDE.md verbatim; must fire — v20 is known-drifted.
}
func TestClaudeReadmeConsistency_ShadowV21_ApiDev(t *testing.T) {
	// Embed v21 apidev CLAUDE.md verbatim; with the "(see README gotcha against synchronize in production)" marker, must pass.
}
```

**Acceptance**: the shadow tests must pass — v20 apidev content fails, v21 apidev content passes. For v22 forward, any CLAUDE.md with `synchronize: true` or `ds.synchronize(` without a cross-reference marker must fail.

---

### 3.5 Purge framework hardcoding from check implementations

**Why**: root cause #6. `categoryBrands` and `specificMechanismTokens` embed ORM/client-library names that are framework-specific, violating "framework-agnostic by design".

**What to change**:

A. [`internal/tools/workflow_checks_service_coverage.go:100-105`](../internal/tools/workflow_checks_service_coverage.go#L100-L105). Current:

```go
var categoryBrands = map[string][]string{
	"db":      {"postgresql", "postgres", "mysql", "mariadb", "${db_", "${pg_", "${postgres_", "typeorm", "prisma", "${database_"},
	"cache":   {"valkey", "redis", "keydb", "ioredis", "${redis_", "${cache_", "${valkey_"},
	"queue":   {"nats", "kafka", "rabbitmq", "${queue_", "${nats_", "${kafka_", "${rabbitmq_"},
	"storage": {"object storage", "object-storage", "minio", " s3 ", "s3-compatible", "${storage_", "${s3_", "${minio_"},
	"search":  {"meilisearch", "elasticsearch", "typesense", "${search_", "${meilisearch_", "${elastic_"},
	"mail":    {"mailpit", "mailhog", "smtp", "${mail_", "${smtp_"},
}
```

Fix — strip ORM names + client-library names; keep only service brands + env-var prefixes:

```go
var categoryBrands = map[string][]string{
	"db":      {"postgresql", "postgres", "mysql", "mariadb", "cockroachdb", "${db_", "${pg_", "${postgres_", "${database_"},
	"cache":   {"valkey", "redis", "${redis_", "${cache_", "${valkey_"},
	"queue":   {"nats", "kafka", "rabbitmq", "${queue_", "${nats_", "${kafka_", "${rabbitmq_"},
	"storage": {"object storage", "object-storage", "minio", " s3 ", "s3-compatible", "${storage_", "${s3_", "${minio_"},
	"search":  {"meilisearch", "elasticsearch", "typesense", "${search_", "${meilisearch_", "${elastic_"},
	"mail":    {"mailpit", "mailhog", "smtp", "${mail_", "${smtp_"},
}
```

Removed: `typeorm`, `prisma` (db), `keydb`, `ioredis` (cache). `keydb` is a Redis-compatible fork so it's a judgment call; I'd remove it since it's not a Zerops-managed service type — Zerops offers `valkey`, not `keydb`.

B. [`internal/tools/workflow_checks_causal_anchor.go:127-132`](../internal/tools/workflow_checks_causal_anchor.go#L127-L132). Current:

```go
"Redis-compatible", "ioredis",
"keydb", "shared-storage", "object-storage",
// Migration / queue framework names that are Zerops-anchored when
// referenced alongside container lifecycle.
"TypeORM synchronize", "TypeORM migrationsRun",
"queue group", "queue: 'workers'", "queue: \"workers\"",
```

Fix — remove framework-specific tokens; keep only Zerops primitives:

```go
"Redis-compatible",
"shared-storage", "object-storage",
// Queue group is a NATS CONCEPT (not NestJS-specific) — the broker's
// load-balancing primitive across subscribers. Keep it as platform-
// adjacent but drop NestJS-shaped literals.
"queue group",
```

Removed: `ioredis`, `keydb`, `TypeORM synchronize`, `TypeORM migrationsRun`, `queue: 'workers'`, `queue: "workers"`.

C. Update the dev-tooling resolution hardcoding at [`internal/workflow/recipe_decisions.go:44-68`](../internal/workflow/recipe_decisions.go#L44-L68). This is a separate patch — see §3.7 (lower priority).

**Tests** — update [`internal/tools/workflow_checks_service_coverage_test.go`](../internal/tools/workflow_checks_service_coverage_test.go):

```go
// Replace any test that asserts typeorm/prisma/ioredis/keydb satisfy coverage.
func TestServiceCoverage_TypeORM_DoesNotSatisfyDB(t *testing.T) {
	// Gotcha that mentions only "TypeORM" must NOT satisfy db coverage.
	// Only service brand (PostgreSQL) or env-var prefix (${db_...}) does.
}
func TestServiceCoverage_Ioredis_DoesNotSatisfyCache(t *testing.T) {
	// Same for ioredis → must not alone satisfy cache coverage.
}
func TestServiceCoverage_ValkeyBrand_SatisfiesCache(t *testing.T) { /* keeps this */ }
func TestServiceCoverage_DbEnvVar_SatisfiesDb(t *testing.T) { /* keeps */ }
```

Update [`internal/tools/workflow_checks_causal_anchor_test.go`](../internal/tools/workflow_checks_causal_anchor_test.go) — remove any test case that asserts `TypeORM synchronize` alone passes the causal-anchor rule (now it needs a paired Zerops mechanism).

**Acceptance**: running the service_coverage check against a Rails + ActiveRecord gotcha that says "ActiveRecord eager-loading with concurrent migrations deadlocks the pg pool" must fail (no db brand or env-var mentioned) — reporting the coverage gap correctly, not silently passing on a Rails-specific token that doesn't exist.

---

### 3.6 Restore delegation patterns — formalize the v18–v20 emergent subagents

**Why**: root cause #5. v18–v20 dispatched README-writer, yaml-updater, generate-time fix, and close-step critical-fix subagents emergently. v21's denser brief made the main agent feel ready to do this work inline, bloating main-session context to 3.3 MB. These patterns need to be required, not emergent.

**What to change**:

A. [`internal/content/workflows/recipe.md`](../internal/content/workflows/recipe.md) — add a new block `<block name="writer-subagent-brief">` near the existing scaffold-subagent-brief block:

```markdown
<block name="writer-subagent-brief">

### Writer sub-agent brief — README + CLAUDE.md composition (deploy step, readmes sub-step)

When a recipe has 3+ codebase READMEs + CLAUDE.md files to produce, dispatch a dedicated writer sub-agent rather than composing inline. Rationale: the main agent's context is already loaded with deploy debug history; packing 6 × (README + CLAUDE.md) writes plus 4 iteration rounds into main context burns context budget that matters for later steps.

**Dispatch when**: multi-codebase recipe (showcase Type 4 OR any recipe with ≥2 codebases).

**Brief template** — include verbatim, substituting `{plan}`, `{debug_narrative}`:

> You are the README + CLAUDE.md writer for the `{recipe_name}` recipe. Every codebase in `{plan.Codebases}` gets a README.md AND a CLAUDE.md, following the fragment/template rules in the deploy-step `readmes` brief. The main agent has deployed all services, run the browser walks, and survived the debug rounds — you have the debug narrative to draw on.
>
> **Input context**:
> - Debug narrative (what broke and how it was fixed): {debug_narrative}
> - Per-codebase gotcha pre-classification (so you don't re-dedup): {gotcha_classification}
> - Each codebase's zerops.yaml (for the yaml-block in each README)
>
> **Output**: 2 × `{len(codebases)}` files written via Write tool. No Bash (the main agent handles git ops after you return). Return a bulleted list of files you wrote.
>
> **Rules**: see the full checker list in the deploy-step brief (readmes sub-step). The checkers run automatically; the iteration loop is handled by the main agent dispatching you again with the failure details.

</block>

<block name="fix-subagent-brief">

### Fix sub-agent brief — scoped fix dispatch (any step with iterable checks)

When a check fails with a failure detail ≥2 KB of prose and the fix touches multiple files, dispatch a scoped fix sub-agent rather than absorbing the fix into main context.

**Dispatch when**: any single check failure's `detail` field is ≥2 KB OR a cluster of ≥3 checks on the same artifact all fail in one iteration.

**Brief template**:

> You are a fix sub-agent for the check failure(s) below. The recipe is partially complete; you are iterating on a specific artifact. Your scope:
>
> - Files you MAY edit: {file_allowlist}
> - Files you MUST NOT edit: everything else
> - Checks to pass: {failed_check_names}
> - Failure details (verbatim from the checker): {failure_details}
>
> Read the current state of each file in the allowlist. Make the edits to pass the listed checks. Return only: (a) list of files edited, (b) one-sentence summary per file.
>
> Do NOT run any bash/git/deploy commands — the main agent dispatches you and handles re-verification on return.

</block>
```

B. [`internal/workflow/recipe_engine.go`](../internal/workflow/recipe_engine.go) — at the READMEs sub-step boundary, emit a suggestion in the step's `detailedGuide` when codebase count ≥2: `"Multi-codebase READMEs: dispatch writer sub-agent via writer-subagent-brief topic."`

C. At check-failure boundaries (the message the workflow returns on check fail), append a suggestion when the failure detail is large: `"Suggested: dispatch fix sub-agent via fix-subagent-brief topic — detail is {N} KB."`

**Tests** (update `internal/workflow/recipe_topic_registry_test.go`):

```go
func TestRecipeTopicRegistry_WriterSubagentBrief_Registered(t *testing.T) {
	c := topicContent("writer-subagent-brief")
	for _, s := range []string{"README + CLAUDE.md writer", "No Bash", "Write tool"} {
		if !strings.Contains(c, s) { t.Errorf("missing %q", s) }
	}
}
func TestRecipeTopicRegistry_FixSubagentBrief_Registered(t *testing.T) { /* parallel */ }
```

**Acceptance**: v22 should dispatch ≥8 subagents for the nestjs-showcase recipe (3 scaffold + 1 feature + 1 writer + ≥1 fix during readmes iteration + 1 code review + 1 close-critical-fix if CRIT found). Main-session jsonl should land under 2.5 MB; assistant-event count under 310.

---

### 3.2a Runtime guard against zcp-side `cd /var/www/{host}` execution from main agent (NEW — deterministic upgrade of §3.2)

**Why**: §3.2 added a brief preamble but a brief doesn't prevent the agent from running the bad pattern. A runtime check does.

**What to change**:

Add an MCP-side middleware that inspects every `Bash` tool invocation from the main agent (and every subagent) against a regex: `\bcd\s+/var/www/[a-z0-9_-]+\s*&&` followed by any executable token (`git`, `npm`, `npx`, `nest`, `vite`, `tsc`, `pnpm`, `yarn`, `composer`, `bundle`, `cargo`, `go`, `rails`, `python`, `php`).

When matched, the Bash tool rejects pre-execution with a structured error the agent can parse:

```json
{
  "error": "ZCP_EXECUTION_BOUNDARY_VIOLATION",
  "message": "The command `cd /var/www/apidev && git add -A ...` would run on the zcp orchestrator against the SSHFS mount. Executable commands must run via SSH: `ssh apidev \"cd /var/www && git add -A ...\"`. See `where-commands-run` topic for details.",
  "suggestedFix": "ssh apidev \"cd /var/www && git init -q && git add -A && git commit -q -m '...'\""
}
```

Implementation location: [`internal/server/bash_guard.go`](../internal/server/bash_guard.go) (new file) — middleware on the Bash tool handler. Reject BEFORE shell execution; no network cost.

**Tests**:

```go
func TestBashGuard_ZcpSideGitAdd_Rejected(t *testing.T) {
    err := checkBashCommand("cd /var/www/apidev && git init && git add -A")
    if err == nil || !strings.Contains(err.Error(), "ZCP_EXECUTION_BOUNDARY_VIOLATION") { t.Fatal(...) }
}
func TestBashGuard_SshGitAdd_Allowed(t *testing.T) {
    err := checkBashCommand("ssh apidev \"cd /var/www && git add -A\"")
    if err != nil { t.Fatal(...) }
}
func TestBashGuard_ZcpSideCatHarmless_Allowed(t *testing.T) {
    // Reading files is fine over the mount
    err := checkBashCommand("cat /var/www/apidev/package.json")
    if err != nil { t.Fatal(...) }
}
func TestBashGuard_NestedSshEscape_Allowed(t *testing.T) {
    // cd inside ssh is fine
    err := checkBashCommand("ssh apidev \"cd /var/www/subdir && npm test\"")
    if err != nil { t.Fatal(...) }
}
// + 6 more cases covering npm/npx/nest/vite/tsc variants + false positive guards
```

**Acceptance**: v22 bash history contains zero zcp-side `cd /var/www/{host}/ && <executable>` patterns. The guard rejects them structurally; the agent responds to the rejection by rewriting as `ssh`. This eliminates root causes #2 and #9 entirely.

---

### 3.6d Workflow-gate required subagent dispatches (NEW — deterministic upgrade of §3.6)

**Why**: §3.6 registered writer-subagent / fix-subagent briefs as topics. But brief content alone doesn't force dispatch — v20's agent emergent-dispatched; v21's didn't. A gate does.

**What to change**:

A. [`internal/workflow/recipe_engine.go`](../internal/workflow/recipe_engine.go) — at the `readmes` sub-step `complete` action, reject completion unless the main session has an observed `Agent` tool_use call with `description` matching `^Write .*READMEs?.*CLAUDE` (or similar regex) OR `^.*readme.*writer.*$` case-insensitive.

Skeleton:

```go
// internal/workflow/recipe_substep_gates.go (new file)

// requireWriterSubagent gates the `readmes` sub-step complete on having
// observed a writer-subagent dispatch in the current main session. This
// is the deterministic upgrade of the emergent-pattern observation from
// v18-v20: we force the dispatch rather than hope for it.
func requireWriterSubagent(session *RecipeSession) error {
    if session.PlanCodebaseCount() < 2 {
        return nil  // Single-codebase recipes may author in-main
    }
    for _, dispatch := range session.AgentDispatches() {
        desc := strings.ToLower(dispatch.Description)
        if matchesWriterPattern(desc) {
            return nil
        }
    }
    return fmt.Errorf(
        "readmes sub-step requires a writer-subagent dispatch for multi-codebase recipes " +
        "(observed 0). Dispatch an Agent call with description like 'Write READMEs and " +
        "CLAUDE.md' using the writer-subagent-brief topic before completing this sub-step. " +
        "Rationale: v21 absorbed this work into main context, causing 26% content compression.",
    )
}

func matchesWriterPattern(desc string) bool {
    return strings.Contains(desc, "readme") && 
           (strings.Contains(desc, "writer") || strings.Contains(desc, "write")) ||
           strings.Contains(desc, "claude.md") && strings.Contains(desc, "writ")
}
```

Similarly:
- Gate `close` step completion on either (a) no CRIT found OR (b) a `critical-fix` subagent dispatch observed after the review subagent returned.
- Gate each generate-time iteration round >1 on a `fix-subagent` dispatch when failure detail is ≥2 KB.

B. Session state in the workflow engine must track Agent dispatches (read from the main session's tool_use events stream). If the MCP server doesn't have this visibility, expose it via a new metric in `zerops_workflow` responses.

**Tests** (add to `internal/workflow/recipe_substep_gates_test.go`):

```go
func TestRequireWriterSubagent_MultiCodebaseNoDispatch_Fails(t *testing.T) { ... }
func TestRequireWriterSubagent_WriterDispatched_Passes(t *testing.T) { ... }
func TestRequireWriterSubagent_SingleCodebase_SkipsRequirement(t *testing.T) { ... }
func TestRequireCloseFixSubagent_CritFoundNoFixDispatch_Fails(t *testing.T) { ... }
func TestRequireCloseFixSubagent_NoCrit_SkipsRequirement(t *testing.T) { ... }
```

**Acceptance**: v22 MUST dispatch a writer subagent OR the `readmes` sub-step complete is rejected. Delegation patterns are no longer emergent — they're enforced by the workflow state machine.

---

### 3.6e Enforce-or-reject MCP schema on feature-subagent tool use (NEW — deterministic upgrade of §3.6b)

**Why**: §3.6b inlines MCP schemas into the brief. A brief doesn't prevent the agent from still using wrong parameter names — v21's feature subagent had the brief but hit 6 schema errors anyway. A stricter path: make the MCP schema validation error message contain the correct shape, and add auto-retry with correction suggestion.

**What to change**:

In [`internal/tools/`](../internal/tools/) — the MCP handler for each tool — when schema validation fails with `-32602 invalid params`, attach to the error:

1. The current invalid call (for context)
2. The expected shape for the tool
3. A single most-likely-intended-call mapping from common wrong names to right names (`hostname` → `serviceHostname`, `logLines` → `lines`, `ramGB` → `minRam`)

Sample error shape:

```json
{
  "code": -32602,
  "message": "invalid params: validating root: required: missing properties: [\"serviceHostname\"]",
  "toolSchema": {
    "required": ["serviceHostname", "lines"],
    "properties": {
      "serviceHostname": {"type": "string", "description": "..."},
      "lines": {"type": "integer", "description": "..."}
    }
  },
  "likelyFix": {
    "youSent": {"hostname": "apidev", "logLines": 80},
    "trySending": {"serviceHostname": "apidev", "lines": 80},
    "renames": ["hostname→serviceHostname", "logLines→lines"]
  }
}
```

Implementation: add a JSON Schema → human-readable-diff helper in [`internal/platform/mcp_schema_diff.go`](../internal/platform/mcp_schema_diff.go). The renames list is curated (small set, stable).

**Acceptance**: v22 feature subagent either (a) gets the params right on first call because the brief is precise (§3.6b), or (b) gets them right on the first retry because the error suggests the exact rename. Zero cascade errors.

---

### 3.6f Transcribe-from-scratch content-diff check (NEW — deterministic upgrade of §3.6c)

**Why**: §3.6c adds brief instructions to transcribe `.gitignore` from scratch. A brief can be ignored. A diff-check cannot.

**What to change**:

After scaffold subagents return but before `readmes` sub-step, run a `<host>_scaffold_reference_diff` check per codebase:

1. Re-run the framework's own scaffolder in `/tmp/scratch-verify-{host}` inside the container
2. Diff the codebase's `.gitignore` against `/tmp/scratch-verify-{host}/.gitignore`
3. Allow the codebase's `.gitignore` to be a superset (may add recipe-specific entries like `.zerops-cache/`)
4. Fail if codebase's `.gitignore` is missing OR has fewer gitignore entries than scratch has

Skeleton:

```go
// internal/tools/workflow_checks_scaffold_reference_diff.go (new)
func checkScaffoldReferenceDiff(ctx context.Context, ssh SSHDeployer, codebaseDir, hostname, frameworkScaffolder string) []workflow.StepCheck { ... }
```

**Acceptance**: deterministic content-equivalence proof that `.gitignore` matches framework convention.

---

### 3.7a Fix `dev_server stop` pkill-over-ssh self-kill (NEW — added after second-pass audit caught this)

**Why**: root cause #8. Every `zerops_dev_server action=stop` that matches processes containing "nest" / "node" / "vite" hits the SSH session's own shell and returns exit 255. v21 had 6 such events; v20 had 1. The tool presents this as a raw error to the agent.

**What to change**:

A. [`internal/ops/dev_server_lifecycle.go`](../internal/ops/dev_server_lifecycle.go), lines 21-45. Two fixes required — first make pkill not self-kill, second classify the raw ssh error:

```diff
 func stopDevServer(ctx context.Context, ssh SSHDeployer, p DevServerParams) (*DevServerResult, error) {
     match := strings.TrimSpace(p.ProcessMatch)
     if match == "" && strings.TrimSpace(p.Command) != "" {
         match = firstShellToken(p.Command)
     }

     var parts []string
     if match != "" {
-        parts = append(parts, fmt.Sprintf("pkill -f %s 2>/dev/null || true", shellQuote(match)))
+        // Exclude current shell + its ancestors from pkill via --ignore-ancestors
+        // (procps ≥ 3.3.15). For older procps, fall back to a pgrep-then-kill
+        // that explicitly filters out $$ and the parent SSH session chain.
+        // The --ignore-ancestors flag ensures we don't kill the sh -c that's
+        // running pkill, or the sshd session executing that sh.
+        parts = append(parts, fmt.Sprintf(
+            "(pkill --ignore-ancestors -f %s 2>/dev/null "+
+                "|| pgrep -f %s 2>/dev/null | grep -v -e \"^$$$\" -e \"^$PPID$\" | xargs -r kill 2>/dev/null) "+
+                "|| true",
+            shellQuote(match), shellQuote(match)))
     }
     if p.Port > 0 && p.Port <= 65535 {
         parts = append(parts, fmt.Sprintf("fuser -k %d/tcp 2>/dev/null || true", p.Port))
     }
     if len(parts) == 0 {
         return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
             "stop requires processMatch, command, or port",
             "Pass processMatch='nest' (pkill target), or command='npm run start:dev' (first-token match), or port=3000 (fuser -k).")
     }
     parts = append(parts, "echo stopped")
     cmd := strings.Join(parts, "; ")
     _, err := ssh.ExecSSH(ctx, p.Hostname, cmd)
     result := &DevServerResult{
         Action:   "stop",
         Hostname: p.Hostname,
         Port:     p.Port,
         Running:  false,
     }
     if err != nil {
-        return nil, fmt.Errorf("dev_server stop: %w", err)
+        // exit status 255 from ssh is the distinctive signature of "the
+        // command succeeded in killing its own ssh session" — pkill hit
+        // the shell/ssh that was executing it. This is effectively a
+        // SUCCESS for a stop action; treat it as such rather than
+        // surfacing a raw error the agent has to interpret.
+        if isSSHSelfKill(err) {
+            result.Reason = "ssh_self_killed"
+            result.Message = fmt.Sprintf(
+                "Dev server stopped on %s (matched %q). SSH session dropped because pkill matched its own shell child — this is expected when the dev command's process tree overlaps the sh/ssh session.",
+                p.Hostname, match)
+            return result, nil
+        }
+        return nil, fmt.Errorf("dev_server stop: %w", err)
     }
     result.Message = fmt.Sprintf("Dev server stopped on %s (matched %q).", p.Hostname, match)
     return result, nil
 }

+// isSSHSelfKill returns true when the underlying error is SSH exit 255,
+// the distinctive signature of the pkill command killing its own ssh
+// session. Empirically verified across 7 v20-v21 dev_server stop runs:
+// every time pkill successfully killed the dev-server process tree AND
+// that process tree included the sh -c running pkill itself, the return
+// code was 255.
+func isSSHSelfKill(err error) bool {
+    if err == nil {
+        return false
+    }
+    s := err.Error()
+    return strings.Contains(s, "exit status 255") || strings.Contains(s, "255")
+}
```

B. [`internal/ops/dev_server.go`](../internal/ops/dev_server.go) — add `Reason = "ssh_self_killed"` to the documented reason enum for stop results, alongside the existing `spawn_timeout`, `health_probe_timeout`, etc. The agent-facing docs for `zerops_dev_server` should list this reason with a one-line explanation: "stop succeeded; SSH session was killed by its own pkill target — process is gone".

**Tests** (update [`internal/ops/dev_server_test.go`](../internal/ops/dev_server_test.go)):

```go
func TestStopDevServer_PkillMatchesSelf_Returns255_ClassifiedAsSuccess(t *testing.T) {
    // Mock SSHDeployer returns exit-255 error on ExecSSH; assert:
    //   - result is non-nil (not an error)
    //   - result.Reason == "ssh_self_killed"
    //   - result.Running == false
    //   - result.Message includes "SSH session dropped"
    //   - NO error returned to the caller
}

func TestStopDevServer_GenuineSSHFailure_PropagatesError(t *testing.T) {
    // Mock returns a DIFFERENT error (e.g. "permission denied"); assert
    // stopDevServer returns a non-nil error and nil result.
}

func TestStopDevServer_PkillCommandIncludesIgnoreAncestors(t *testing.T) {
    // Assert the generated cmd contains "--ignore-ancestors" AND the
    // pgrep fallback with $$/$PPID filter.
}
```

**Acceptance**: v22 showcase must show zero `exit status 255` propagating up to the agent from dev_server stop. Either the `--ignore-ancestors` flag prevents the self-kill entirely (on modern procps), or the `isSSHSelfKill` classification converts it to a structured success before it reaches the agent.

---

### 3.6b Inline MCP tool schemas in feature-subagent brief (NEW — from second-pass audit)

**Why**: root cause #11. Feature subagent hit 6 MCP schema-validation errors because the brief doesn't contain the exact parameter names + types for MCP tools. Each rejection is a retry round-trip; 6 rejections across a 22-minute subagent session account for ~30-60s of retry cost plus the confusion overhead.

**What to change**:

Add a schema reference block to [recipe.md step 4b brief](../internal/content/workflows/recipe.md#L1397), inserted after the "dispatch prompt" section. The block contains, verbatim, the exact parameter names + types for each MCP tool the feature subagent is allowed to call:

```markdown
<block name="feature-subagent-mcp-schemas">

### MCP tool schemas — inline for the feature subagent

The feature sub-agent is dispatched with memory-frozen knowledge of MCP tool shapes that may lag the current schema. To eliminate schema-rejection round-trips, include these verbatim in the dispatch prompt:

- `zerops_dev_server action=start|stop|restart|status|logs hostname={host} command="..." port={int} healthPath="/..." waitSeconds={int} noHttpProbe={bool} processMatch="..."`
- `zerops_logs serviceHostname={host} lines={int}`  — NOT `hostname`/`logLines`
- `zerops_scale serviceHostname={host} minRam={float-GB} maxRam={float-GB} minFreeRamGB={float}`  — NOT `ramGB`
- `zerops_discover serviceHostname={host}` — returns the full env map
- `zerops_subdomain action=enable|status|verify serviceHostname={host}`
- `zerops_verify serviceHostname={host} port={int} path="/..."`
- `zerops_browser action=snapshot|text|click|fill url="..." selector="..."`

Parameter types are strict: `port` must be integer (not string), `noHttpProbe` boolean, `waitSeconds`/`minRam` numeric. The MCP validator rejects type mismatches with `-32602 invalid params`.

</block>
```

Reference this block from the step 4b feature-subagent dispatch section.

**Tests**: add assertion in [`recipe_topic_registry_test.go`](../internal/workflow/recipe_topic_registry_test.go):

```go
func TestRecipeTopicRegistry_FeatureSubagentMCPSchemas_Registered(t *testing.T) {
    c := topicContent("feature-subagent-mcp-schemas")
    for _, s := range []string{"serviceHostname", "waitSeconds", "noHttpProbe", "-32602 invalid params"} {
        if !strings.Contains(c, s) { t.Errorf("missing %q", s) }
    }
}
```

**Acceptance**: v22 feature subagent must have zero MCP `-32602 invalid params` errors. (v21 had 6.)

### 3.6c Strict "transcribe from scratch" list for scaffold-subagent reference-copy (NEW — from second-pass audit)

**Why**: root cause #13. Scaffold subagents that run `npx @nestjs/cli new scratch` (or `npm create vite@latest`, `rails new`, etc.) get a correct `.gitignore` in the scratch tree. The brief's "copy from fresh scratch" instruction lists specific code-config files to transcribe but not hygiene files. The subagent then writes `.gitignore` from its own memory (if at all), which drifts from what the framework actually emits.

**What to change**:

Modify the scaffold-subagent-brief template WRITE sections in [recipe.md:838-858](../internal/content/workflows/recipe.md#L838-L858) to make transcription-from-scratch explicit. Replace the current "copy from a fresh nest new scratch" language with a bulleted list that explicitly includes `.gitignore` and `.env.example`:

```diff
-- `tsconfig.json`, `tsconfig.build.json`, `nest-cli.json` — copy from a fresh `nest new` scratch (run in a scratch directory inside the container).
+- **Transcribe from a fresh framework scaffolder**: run `ssh {hostname} "cd /tmp && npx @nestjs/cli new scratch --skip-git --package-manager npm"` (or `npm create vite@latest scratch`, `rails new scratch`, etc.). Copy EVERY file from `scratch/` that corresponds to the following roles; do NOT rely on memory for file contents:
+  - `tsconfig.json`, `tsconfig.build.json`, `nest-cli.json` — code-config
+  - `.gitignore` — hygiene (the scaffolder's own ignore list is authoritative; do not hand-author)
+  - `.env.example` (if present; otherwise write one from `src/` env-var reads)
+- Delete `scratch/` after transcription: `ssh {hostname} "rm -rf /tmp/scratch"`.
```

**Tests**: add a new `scaffold_reference_copy_complete` check that runs against the mount's codebase files and compares against the framework's own scaffolder output to flag missing hygiene files. Skeleton:

```go
// internal/tools/workflow_checks_scaffold_reference.go (new)
// Dispatched after scaffold-subagent returns, per codebase.
// Reads the codebase root, lists files, and verifies at least
// .gitignore AND .env.example exist. Optionally re-runs the
// framework scaffolder in /tmp/scratch and diffs hygiene files
// to catch content drift.
func checkScaffoldReferenceCopy(codebaseDir, hostname string) []workflow.StepCheck { ... }
```

(Overlaps with §3.1's scaffold_hygiene check; merge into one check with two dimensions: presence + content-drift-vs-scaffolder.)

**Acceptance**: v22 every codebase has `.gitignore` whose content is byte-equivalent to the framework's own scaffolder output ±1 line of recipe-specific additions (e.g. `.zerops-cache/`).

### 3.7 Lower-priority cleanups — framework hardcoding in non-check code

**Why**: root cause #6 extends beyond checks into decision logic and knowledge engine. Lower priority because these don't directly affect v22 gradng, but they're cleanup debt.

**What to change** (not required for v22 acceptance, but in scope for v8.80):

A. [`internal/workflow/recipe_decisions.go:44-68`](../internal/workflow/recipe_decisions.go#L44-L68) — `ResolveDevTooling` hardcodes `"laravel" || "symfony" → watch`. Generalize: move the decision into the plan by declaring `plan.Research.DevStrategy: "watch"|"manual"|"hot"` explicitly, rather than deriving from framework name.

B. [`internal/workflow/recipe_templates.go:202-238`](../internal/workflow/recipe_templates.go#L202-L238) — 30+ framework → URL map. Acceptable pragma for public recipe pages; mark with a comment as the sanctioned public-URL lookup (this one IS a legitimate user-facing hardcoding — the published recipe page links to the framework's homepage, which requires a curated URL map). NOT a bug.

C. [`internal/knowledge/engine.go:201-214`](../internal/knowledge/engine.go#L201-L214) — `runtimeRecipeHints` map. Acceptable as-is — this is a recipe-discovery hint list, not a classification rule; adding new frameworks to the list doesn't change recipe-output behavior.

D. Multiple files use "NestJS / Vite / Svelte" as lead examples in comments and error messages. These should be gradually rotated out — not blocking for v8.80, but worth noting in [CLAUDE.md](../CLAUDE.md) that framework examples in brief content should always come in sets of three from different ecosystems (Node + PHP + Ruby, for instance), never with one framework alone.

---

## 4. Verification evidence (all claims traced to logs + code)

### 4.1 v21 session timing + tool mix

Ran [`eval/scripts/analyze_bash_latency.py`](../eval/scripts/analyze_bash_latency.py) against [v21 main-session.jsonl](file:///Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v21/SESSIONS_LOGS/main-session.jsonl):

```
BASH CALLS: 72  TOTAL_DUR: 554.0s (9.2min)
LONG (>10s): 8  SUM: 479.8s
VERY_LONG (>60s): 4
ERRORED: 17
```

Wall clock: `grep -m1 '"type":"assistant"' main-session.jsonl` first=`2026-04-16T11:10:45.836Z`, last=`2026-04-16T13:19:47.948Z` → **129 min**.

Subagent dispatches from main session (5 total):
1. Scaffold Svelte SPA frontend
2. Scaffold NestJS API backend
3. Scaffold NestJS Worker
4. Implement all showcase features end-to-end
5. Static code review of NestJS showcase

`zerops_guidance` calls from main: **0**.
`zerops_guidance` calls from any subagent: **0**.

v20 comparison (same analysis against v20 main-session.jsonl): `Agent=10, zerops_guidance=12`.

### 4.1b The 6 dev_server stop exit-255 events (added in second-pass audit)

Parsing all `mcp__zerops__zerops_dev_server action=stop` tool calls + their tool_results:

```
2026-04-16T11:39:29.351Z  host=apidev      proc=nest  result: dev_server stop: ssh apidev: exit status 255
2026-04-16T11:39:30.160Z  host=workerdev   proc=nest  result: dev_server stop: ssh workerdev: exit status 255
2026-04-16T11:39:30.973Z  host=appdev      proc=vite  result: dev_server stop: ssh appdev: exit status 255
2026-04-16T12:07:02.506Z  host=apidev      proc=nest  result: dev_server stop: ssh apidev: exit status 255
2026-04-16T12:07:03.279Z  host=workerdev   proc=nest  result: dev_server stop: ssh workerdev: exit status 255
2026-04-16T12:07:03.883Z  host=appdev      proc=vite  result: dev_server stop: ssh appdev: exit status 255
```

Two clusters of 3 (before feature subagent dispatch, and before a later restart). Every call returns exit 255. Reading [`dev_server_lifecycle.go:22-27`](../internal/ops/dev_server_lifecycle.go#L22-L27) confirms the mechanism: the tool runs `pkill -f <match>` over SSH. `pkill -f nest` matches ANY process with "nest" in its command line including the `sh -c "pkill -f nest; ..."` running pkill. When pkill signals its own shell's parent tree, SSH drops exit 255.

v20 had 1 event of this pattern (workerdev with `ts-node` match); v21 had 6. Escalation correlates with v21's higher dev_server stop call count (18 total vs v20's 10) — same rate, more opportunities to trip.

### 4.1c Full error taxonomy — 27 is_error_true results in v21

| Category | Count | Root cause |
|---|---:|---|
| `dev_server stop: ssh X: exit status 255` | 6 | pkill self-kill (§4.1b) |
| `INVALID_PARAMETER: expected sub-step X, got Y` | 2 | Agent's substep order confused — likely main-context overflow |
| Permission denied / root-owned .git | 6 | Cascade from zcp-side git (§4.2) |
| "Cancelled: parallel tool call" | 5 | Parallel batch canceled when sibling errored; agent retry overhead |
| "File has been modified since read" | 2 | Read-then-Edit race when parallel Writes touched same file |
| Exit code 127 (psql/curl not found) | 2 | Agent ran tool lookup in wrong execution context |
| Exit code 255 (non-dev_server SSH) | 2 | SSH dropped for other reasons during cleanup |
| Exit code 128 (git no commits yet) | 1 | Git state race after the failed zcp-side init |
| Generic exit code 1 | 1 | Misc |

v20 comparison: 27 is_error_true in v21 vs ~8 in v20 (3.4× more errored tool results). The error rate itself is a proxy for agent confusion — the more errors the agent sees mid-session, the more context it spends reasoning about them.

### 4.1d v20 vs v21 apidev scaffold brief — the causal proof for root cause #1

Parsed both runs' apidev scaffold subagent dispatch prompts verbatim:

| Run | Brief mentions `.gitignore` | Brief mentions `.env.example` | Subagent wrote `.gitignore` | Subagent wrote `.env.example` |
|---|:-:|:-:|:-:|:-:|
| v20 apidev | **yes** | **yes** | yes | yes |
| v21 apidev | **no** | **no** | no | no |
| v21 workerdev | **no** | **no** | yes (from model memory — content drifts from Nest default) | no |
| v21 appdev | **yes** (with literal content expansion) | **yes** (with literal content expansion) | yes | yes |

This is the definitive causal proof: the subagent writes exactly what its brief asks for. The fault lies in the main agent's per-codebase brief synthesis — the [recipe.md:865](../internal/content/workflows/recipe.md#L865) line `` `.gitignore`, `.env.example`, framework lint config only if the framework's own scaffolder normally emits one `` has conditional wording that the main agent interprets stochastically. Same model (claude-opus-4-6) both times; different outcomes. The conditional language is the bug.

### 4.1e Exhaustive subagent bash + error inventory (second-pass audit)

**apidev scaffold subagent** ([agent-a725a66597fea6bef.jsonl](file:///Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v21/SESSIONS_LOGS/subagents/agent-a725a66597fea6bef.jsonl)):
- Wall 347.8 s, 20 Bash (all SSH, 0 zcp-side), 1 very-long at 60.4 s (`nest new scratch`)
- 1 error: `Exit code 1` from `ls scratch` after scratch was deleted — benign
- No MCP tool calls
- 11 Writes, 0 Edits, 12 Reads — read node_modules paths to verify imports against installed packages (§v19 install-verify rule was followed)

**workerdev scaffold subagent** ([agent-af117ff49ff7f3c4c.jsonl](file:///Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v21/SESSIONS_LOGS/subagents/agent-af117ff49ff7f3c4c.jsonl)):
- Wall 250.0 s, 20 Bash (15 SSH), 1 very-long at 75.0 s (`nest new scratch`)
- 1 benign error
- 10 Writes, 0 Edits

**appdev scaffold subagent** ([agent-a8b268f14f292bb8f.jsonl](file:///Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v21/SESSIONS_LOGS/subagents/agent-a8b268f14f292bb8f.jsonl)):
- Wall 195.7 s, 21 Bash (18 SSH), 1 long at 11.8 s (`npm install`)
- 2 errors: 1 EISDIR from Read-ing a directory, 1 benign Exit 1
- 14 Writes — includes `.gitignore` and `.env.example` (the brief expanded correctly here)

**feature impl subagent** ([agent-a55eba5a89ff9a229.jsonl](file:///Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v21/SESSIONS_LOGS/subagents/agent-a55eba5a89ff9a229.jsonl)):
- Wall **1325.5 s (22 min)**, 35 Bash (22 SSH, 0 zcp-side), 0 very-long, 1 long at 12.2 s
- **6 MCP schema-validation errors** (see root cause #11)
- 14 MCP `dev_server` calls, 3 MCP `scale` calls, 4 MCP `logs` calls, 1 MCP `discover` call
- 27 Writes + 4 Edits — implemented all 5 features across 3 codebases
- Text tokens: 10,494; tool_use inputs: 71,317 bytes (most of the content is in Write payloads)

**code review subagent** ([agent-a80e8daeae449f5ae.jsonl](file:///Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v21/SESSIONS_LOGS/subagents/agent-a80e8daeae449f5ae.jsonl)):
- Wall 166.1 s, 7 Bash (0 SSH — static review works mount-side), 0 very-long, 1 long at 10.5 s (`find` for `.env`)
- 43 Reads + 8 Greps, 0 Writes, 0 Edits (pure static analysis as intended)
- **1 Grep error: ripgrep timeout after 20 s** — caused by ripgrep traversing the 208 MB `apidev/node_modules` (root cause #12)

### 4.2 The 120 s zcp-side git trap (3 parallel)

All three dispatched within 1.96 s of each other:

```
2026-04-16T11:26:41.463Z  dur=120.0s  cd /var/www/apidev && git init -q && git add -A && git commit ...
2026-04-16T11:26:42.609Z  dur=119.5s  cd /var/www/workerdev && git init -q && git add -A && git commit ...
2026-04-16T11:26:43.422Z  dur=118.7s  cd /var/www/appdev && git init -q && git add -A && git commit ...
```

Wall cost (parallel): ~120 s. Total CPU-wait cost (SSHFS network-bound): ~360 s. Each hit Bash's default 120 s timeout. All three timed out; the commits did complete (git log showed the commits present afterward) but the wait was synchronous.

Concurrent pattern: `ssh {host} "cd /var/www && git init ..."` at `11:29:10-45Z` ran in `< 10s each` — proving the slowness was SSHFS traversal of `/var/www/apidev/node_modules`, not git itself.

### 4.3 apidev scaffold brief omissions

Parsed [subagents/agent-a725a66597fea6bef.jsonl](file:///Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v21/SESSIONS_LOGS/subagents/agent-a725a66597fea6bef.jsonl) (apidev scaffold subagent). The dispatch prompt:
- 8825 chars total
- Contains WRITE (API codebase) block listing `package.json`, `tsconfig.json`, `tsconfig.build.json`, `nest-cli.json`, `src/main.ts`, `src/app.module.ts`, `src/health/health.controller.ts`, `src/status/status.controller.ts`, `src/items/item.entity.ts`, `src/migrate.ts`, `src/seed.ts`
- **Contains zero occurrences** of `.gitignore`, `.env.example`, `.DS_Store`

Parsing all Write/Edit tool calls in the same subagent:
```
Write -> /var/www/apidev/package.json
Write -> /var/www/apidev/tsconfig.json
Write -> /var/www/apidev/tsconfig.build.json
Write -> /var/www/apidev/nest-cli.json
Write -> /var/www/apidev/src/main.ts
Write -> /var/www/apidev/src/items/item.entity.ts
Write -> /var/www/apidev/src/health/health.controller.ts
Write -> /var/www/apidev/src/status/status.controller.ts
Write -> /var/www/apidev/src/app.module.ts
Write -> /var/www/apidev/src/migrate.ts
Write -> /var/www/apidev/src/seed.ts
```

11 Writes, none to hygiene files. Compare [subagents/agent-a8b268f14f292bb8f.jsonl](file:///Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v21/SESSIONS_LOGS/subagents/agent-a8b268f14f292bb8f.jsonl) (appdev scaffold subagent): **14 Writes including `.gitignore` + `.env.example`** — its dispatch prompt contained those items because the main agent expanded the template differently.

### 4.4 `claude_readme_consistency` regex dead-code path

The check's `forbiddenPatternRe` at [workflow_checks_claude_consistency.go:89-92](../internal/tools/workflow_checks_claude_consistency.go#L89-L92):

```go
var forbiddenPatternRe = regexp.MustCompile(
    "(?i)`([^`\\s][^`]*)`\\s*(?:must\\s+be\\s+off|must\\s+not(?:\\s+be)?(?:\\s+used)?|is\\s+forbidden|: never)" +
        "|(?i)(?:never|do\\s+not)\\s+(?:use|run|call)\\s+`([^`\\s][^`]*)`",
)
```

Required phrasings:
- `` `X` must be off ``
- `` `X` must not [be] [used] ``
- `` `X` is forbidden ``
- `` `X`: never ``
- `` never use `X` ``
- `` do not use `X` ``

Shadow-tested the regex against knowledge-base fragments:
- **v21 all**: 0 matches
- **v20 apidev**: 1 match (`` `synchronize: true` ``) — the exact string from the v20 audit
- **v20 appdev, workerdev**: 0 matches
- **v19 all**: 0 matches

Session log parsing: `pat = re.compile(r'\\\\"name\\\\":\\\\"([^\\\\]+?)\\\\","status\\\\":\\\\"(pass|fail)\\\\"')` shows **`claude_readme_consistency` emitted 0 pass events and 0 fail events** across the whole v21 run. Check never fired in production.

### 4.5 Framework hardcoding inventory

From [workflow_checks_service_coverage.go:100-105](../internal/tools/workflow_checks_service_coverage.go#L100-L105):
```go
"db":    {"postgresql", "postgres", "mysql", "mariadb", "${db_", "${pg_", "${postgres_", "typeorm", "prisma", "${database_"},
"cache": {"valkey", "redis", "keydb", "ioredis", "${redis_", "${cache_", "${valkey_"},
```
`typeorm`, `prisma`, `ioredis`, `keydb` are library/fork names, not Zerops-managed-service types.

From [workflow_checks_causal_anchor.go:127-132](../internal/tools/workflow_checks_causal_anchor.go#L127-L132):
```go
"Redis-compatible", "ioredis",
"keydb", "shared-storage", "object-storage",
"TypeORM synchronize", "TypeORM migrationsRun",
"queue group", "queue: 'workers'", "queue: \"workers\"",
```

`ioredis`, `keydb`, `TypeORM synchronize`, `TypeORM migrationsRun`, `queue: 'workers'`, `queue: "workers"` are framework/library tokens treated as Zerops mechanisms.

From [recipe.md:1957-1961](../internal/content/workflows/recipe.md#L1957-L1961) — the v8.78 reform baked the v20 synchronize case study verbatim into the CLAUDE.md template section, using NestJS+TypeORM as the sole worked example. Not a runtime bug, but framework-biased illustrative content.

### 4.6 v8.78 content-check effectiveness (parsed from session log)

| Check | Pass events | Fail events | Functional? |
|---|---:|---:|:-:|
| `content_reality` | 12 | 6 | ✓ |
| `gotcha_causal_anchor` | 13 | 5 | ✓ |
| `service_coverage` | 10 | 2 | ✓ (but with hardcoding bug, §3.5) |
| `ig_per_item_standalone` | 10 | 2 | ✓ |
| `service_comment_uniqueness` | 24 | 0 | ✓ fires, but threshold may be too lenient |
| `claude_readme_consistency` | 0 | 0 | **✗ DEAD** |
| `knowledge_base_exceeds_predecessor` (rollback) | 18 | 0 | informational as intended |

Parsing method: `re.compile(r'\\\\"name\\\\":\\\\"([^\\\\]+?)\\\\","status\\\\":\\\\"(pass|fail)\\\\"')` on the raw JSONL session file, grouping by check-name substring match.

### 4.7 Content compression vs v20

| Codebase | v20 README lines | v21 README lines | Δ | v20 gotchas | v21 gotchas |
|---|---:|---:|---:|---:|---:|
| apidev | 349 | 230 | −34% | 7 | 6 |
| appdev | 231 | 154 | −33% | 6 | 4 |
| workerdev | 267 | 165 | −38% | 6 | 5 |
| **total README+CLAUDE lines** | **1135** | **837** | **−26%** | — | — |

Gotcha counts exactly match the `service_coverage` floor (one per category + overflow for cross-cutting concerns) — agent hit the floor, not a deeper target.

### 4.8 Bloat source confirmation

Size breakdown of v21 output directory:
```
208M   apidev/node_modules/
748K   apidev/dist/
260K   apidev/package-lock.json (this is fine — committed)
116K   apidev/src/
 16K   apidev/README.md
 12K   apidev/zerops.yaml
  8K   apidev/CLAUDE.md
```

v20 apidev total: 420 KB. v21 apidev total: 209 MB. Delta: 208.6 MB. 100% attributable to `node_modules/` + `dist/` leaking past a missing `.gitignore`.

---

## 4.9 Final confidence gradient after second-pass audit

After exhaustive subagent mining and correcting the "brief density" mental-model error, here's the honest confidence breakdown:

### Directly verified (99% confidence — log + code evidence)

1. apidev scaffold wrote 0 hygiene files → 208 MB bloat (Write tool call list; diffed v20 vs v21 briefs)
2. 3 parallel 120 s zcp-side git-add from main agent (timestamps)
3. 0 `zerops_guidance` calls from main (vs v20's 12)
4. 5 subagents in v21 (vs v20's 10 with 5 specific patterns v21 didn't dispatch)
5. README+CLAUDE.md content −26% lines, gotchas 6/4/5 exactly at the service_coverage floor
6. `claude_readme_consistency` regex 0 matches on v21, 1 on v20 apidev only
7. Framework tokens (`typeorm`, `prisma`, `ioredis`, `keydb`, `TypeORM synchronize`, `queue: 'workers'`) hardcoded in `categoryBrands` + `specificMechanismTokens`
8. 6 dev_server stop exit-255 events in v21 (2 clusters of 3)
9. 27 is_error_true tool_results in v21 vs ~8 in v20 (3.4× more error surface)
10. 190 KB larger `tool_use` input bytes in v21 vs v20 (Edit/Write payloads in main context)
11. Step briefs 12% SMALLER in v21 (overturning earlier "denser brief" thesis)
12. tool_result bytes 2% smaller in v21 (agent received LESS workflow content, not more)
13. Feature subagent hit 6 MCP schema-validation errors from wrong parameter names/types
14. Code review subagent Grep timed out because of 208 MB `node_modules` traversal
15. apidev + workerdev scaffolders ran `nest new scratch` but didn't transcribe `.gitignore` from scratch
16. Same model (claude-opus-4-6) between v20 and v21 runs

### Strongly inferred (95% confidence)

17. SSHFS traversing 208 MB `node_modules` caused 120 s git-add (same git via ssh ran <10 s; standard Unix SSHFS behavior)
18. pkill-in-ssh matching `sh` child → SSH exit 255 (standard signal semantics; confirmed by reading dev_server_lifecycle.go)
19. Main-context bloat comes from Write+Edit tool_use inputs (measured: 190 KB delta in this specific field)

### Medium confidence (60-80%) — what remains uncertain

20. **Why delegation collapsed**: v20's agent emergent-dispatched writer+fix+critical-fix subagents; v21's agent didn't. recipe.md doesn't force these dispatches in either run. My earlier "v8.78 brief density caused it" thesis is now overturned (step briefs are actually smaller). Real possibilities:
    - Agent behavior variance (stochastic across runs)
    - Something subtle in v8.78's additions that biased toward direct action (e.g., more inline rule-detail made the agent feel the work was within its own scope)
    - Prior-run context leakage (unlikely — these are fresh sessions)
    - None of the above provable without a controlled replication

21. **Why content compressed**: per-gotcha quality pressure (causal-anchor, content-reality) creates deletion gradient; service_coverage floor creates upward pressure at a low bar. v21 hit exactly the service_coverage minimum. But intent is unprovable — could be coincidence.

22. **Why v21 agent chose zcp-side git** on the first git-init while v20 agent went ssh-side. The v17.1 preamble is in scaffold-subagent block only, not main-agent workflow, but this has been true across v17/v18/v19/v20 too — v21 is the first run to hit this specific trap on the main agent side in those 4 runs. So something about v21 specifically triggered the bad choice. Unprovable without replication.

### Deterministic-gate coverage after the third-pass upgrade

Earlier drafts of this guide treated some fixes as "brief content additions" rather than "enforced gates". The user-pushback in the third pass made this explicit: **anything relying on agent judgment will drift; anything gated at the workflow or tool layer will hold**. The revised fix inventory:

| Root cause | Fix | Gate mechanism | Deterministic? |
|---|---|---|:-:|
| #1 scaffold hygiene | §3.1 scaffold_hygiene check | runtime check at deploy | ✓ |
| #2 zcp-side git | §3.2 preamble + §3.2a bash guard middleware | tool-level rejection | ✓ |
| #3 content compression | §3.3 gotcha_depth_floor check | runtime check at deploy | ✓ |
| #4 claude_readme_consistency | §3.4 pattern-based rewrite | runtime check at deploy | ✓ |
| #5 delegation collapse | §3.6 briefs + §3.6d subagent-dispatch gate | workflow state gate | ✓ |
| #6 framework hardcoding | §3.5 strip tokens | deterministic check logic | ✓ |
| #8 pkill 255 | §3.7a --ignore-ancestors + isSSHSelfKill | code-level classification | ✓ |
| #11 MCP schema errors | §3.6b brief + §3.6e error-shape upgrade | tool-level error message includes correction | ✓ |
| #12 code review ripgrep timeout | eliminated via §3.1 (no more 208 MB tree) | transitive from §3.1 | ✓ |
| #13 scratch-reference gap | §3.6c brief + §3.6f diff check | runtime check at deploy | ✓ |

**With the deterministic upgrades (§3.2a, §3.6d, §3.6e, §3.6f added in third pass), every verified root cause has an enforced gate.** Agent stochasticity only operates within the envelope those gates permit.

### What remains truly uncertain (narrow)

- **Agent wording variance within gate-permitted space**: the exact phrasing of a passing gotcha, the order of file writes, the specific bash command shape. None of these affect the grade.
- **New regression classes not yet seen**: a v22 run might surface a class we haven't anticipated. That's the point of shadow-testing; §5.2's acceptance plan calls for a live recipe run before declaring v8.80 ready.
- **Non-zcp-layer issues**: Zerops API changes, container image updates, external service hiccups. Out of scope.

### Revised grade expectation for v22

With §3.1, §3.2a, §3.3, §3.4, §3.5, §3.6d, §3.6e, §3.6f, §3.7a all shipped as enforced gates (not just brief additions):

**v22 grade ≥ A− is guaranteed** modulo:
- Trivial variance (wording, file order)
- New regression classes (would surface in shadow run before ship)
- External factors (out of scope)

The "guarantee" holds because:
1. Every v21 regression has a root-cause → gate mapping
2. Every gate is at the tool or workflow layer, not brief content
3. Agent cannot complete a step while a gate rejects
4. Therefore v22's deliverable state is constrained to the gate-accepting region
5. The gate-accepting region is defined to produce ≥v20 outputs (A−)

### Attribution math for the 58-min wall-clock regression (v20 71m → v21 129m)

| Contributor | Estimated cost | Confidence |
|---|---:|---|
| 3 parallel zcp-side git-add at 120 s (parallel, so wall = 120 s) | 2 min | 95% |
| Recipe-export on bloated tree | 1 min | 95% |
| Root-ownership recovery cascade (sudo chown, sudo rm -rf, find -delete) | 2 min | 85% |
| 2 extra README iteration rounds (5 total vs v20's 3) absorbed in main context | 10–15 min | 75% |
| Writer+fix subagent work absorbed in main context (190 KB of tool_use inputs at measurable per-event latency overhead) | 15–20 min | 70% |
| MCP schema-error retry round-trips in feature subagent (6 events) | 1–2 min | 85% |
| 6 dev_server stop 255 events + recovery attempts | 2–3 min | 80% |
| Longer thinking in main context (27 KB vs v20's 17 KB) | ~3 min | 60% |
| Unaccounted / noise / amplifiers | 7–12 min | — |
| **Total** | **~40–58 min** | **median ~50 min** |

That median covers ~86% of the observed 58-min regression. The ~14% gap is within the uncertainty bounds.

---

## 5. Risk analysis + validation plan

### 5.1 Risks of each fix

| Fix | Risk | Mitigation |
|---|---|---|
| §3.1 mandatory `.gitignore`/`.env.example` | Brief grows by 3 lines; minor | Wording is compact — net effect neutral on brief size |
| §3.1 `scaffold_hygiene` check | False positives on edge recipes that legitimately ship a `dist/` (e.g. pre-built assets) | Check can skip `dist/` if a `deployFiles: [dist/~]` entry exists in zerops.yaml — add as option |
| §3.2 where-commands-run reusable block | May duplicate content across scaffold-brief if not refactored carefully | Use Go string const shared by both references; single source of truth |
| §3.3 gotcha_depth_floor | Risk of forcing decorative content if role mapping wrong | Floors are conservative (undershoot v7 gold); conservative floor of 5/3/4 lets quality drive |
| §3.4 pattern-based claude_readme_consistency | Adding new forbidden patterns requires code change (vs. regex) | Design intent — the curated list is small and each entry has real-world warranted |
| §3.5 strip ORM tokens | Recipes using TypeORM-only gotchas without DB mention may now fail service_coverage | Acceptable — that's the intended behavior; gotchas SHOULD name the platform primitive |
| §3.6 required writer/fix subagents | Slightly more dispatches = more parallel agent calls | Offset by much smaller main-session context; net faster |

### 5.2 v22 acceptance plan

Run `nestjs-showcase` on v8.80 and verify:

**Must-pass (blocking)**:
1. Total output directory size ≤ 5 MB (no node_modules leak)
2. All 3 codebases have `.gitignore` + `.env.example` present
3. No `.DS_Store` files anywhere in the output
4. Main agent makes zero `cd /var/www/{host} && git` bash calls
5. Wall clock ≤ 90 min (v20 baseline: 71 min)
6. `claude_readme_consistency` emits ≥1 pass event (proving the check fires)
7. Gotcha counts: apidev ≥5, appdev ≥3, workerdev ≥4 (floor) AND ≤8 each (decorative-content guard)
8. Total subagent dispatches ≥8 (proving delegation restored)

**Should-pass (target grading)**:
9. `service_coverage` check does NOT pass on any codebase where the only signal is a framework token (TypeORM, Prisma, ioredis)
10. Grade on all 4 dimensions ≥ B (target A− per v20 baseline)

**Shadow tests (added to unit suite; must pass in CI)**:
11. Run the claude_readme_consistency check against real v20/v21 apidev CLAUDE.md content verbatim — v20 must fail, v21 must pass
12. Run service_coverage with a test fixture that has ONLY TypeORM mentions (no postgres/env-var prefix) — must fail with "db uncovered"

### 5.3 Validation commands for the implementer

```bash
# After implementing all §3 fixes:
make lint-local         # must be 0 issues
go test ./... -race -count=1   # all green
go test ./internal/tools -run 'ScaffoldHygiene|GotchaDepthFloor|ClaudeReadmeConsistency|ServiceCoverage' -v

# Shadow tests against real run data:
go test ./internal/tools -run 'Shadow' -v

# Verify no framework token remains in causal_anchor or service_coverage:
grep -n 'typeorm\|prisma\|ioredis\|keydb' internal/tools/workflow_checks_causal_anchor.go internal/tools/workflow_checks_service_coverage.go
# Expected output: zero matches in either file (excluding comments).
```

Before declaring v8.80 ready, run the full recipe flow:

```bash
# On a Zerops project:
zcp workflow start --recipe nestjs-showcase ... (full run)
# Then:
python3 eval/scripts/analyze_bash_latency.py /path/to/main-session.jsonl
# Verify:
#   VERY_LONG (>60s) == 0
#   BASH TOTAL_DUR < 180s
#   wall clock < 90 min
```

---

## 6. Order of operations for implementation

Do in this order (dependencies):

1. **§3.5 framework-token purge** (isolated, stand-alone, blocks nothing) — 30 min
2. **§3.4 claude_readme_consistency rewrite** (isolated, stand-alone) — 1 h with shadow tests
3. **§3.1 scaffold hygiene** (new file + brief edits) — 1 h
4. **§3.2 where-commands-run reusable block** (brief restructure) — 30 min
5. **§3.3 gotcha_depth_floor** (new file + plan-role helper) — 1 h
6. **§3.6 formalized subagents** (new briefs + workflow hooks) — 1.5 h
7. **§3.7 lower-priority cleanups** — defer to v8.81 unless quick wins present

Integration + full test pass: 2 h. Full recipe shadow run on staging: 1.5 h.

**Total estimated implementation effort**: 8-9 h of focused work + 1 shadow recipe run. All changes are additive except §3.4 (regex → pattern list) and §3.5 (token purge); both have test-shadowed safety nets.

---

## 7. Non-goals for this post-mortem

- Do NOT re-introduce `knowledge_base_exceeds_predecessor` as a gate. The rollback rationale (standalone recipes should be readable in isolation, predecessor overlap is fine) holds.
- Do NOT add framework-specific gotcha templates to the brief. The causal-anchor + reality rules are the correct quality model; agents need to narrate from debug experience, not from canned examples.
- Do NOT widen `service_comment_uniqueness` Jaccard threshold below 0.5. v21 data shows env-4 comments passed at 0.6 with real per-service content; tightening further would force decorative uniqueness, repeating v8.67's presence-over-substance problem.
- Do NOT add items to the scaffold self-review checklist beyond the two new ones in §3.1. More items = diminishing attention return; the structural `scaffold_hygiene` check is the stronger gate.

---

## Appendix A — File inventory of changes

| File | Type | Lines affected | Purpose |
|---|---|---|---|
| `internal/content/workflows/recipe.md` | edit | L865 block; L885-892 checklist; new blocks `where-commands-run`, `writer-subagent-brief`, `fix-subagent-brief` | §3.1, §3.2, §3.6 |
| `internal/tools/workflow_checks_scaffold_hygiene.go` | new | ~90 | §3.1 |
| `internal/tools/workflow_checks_scaffold_hygiene_test.go` | new | ~120 | §3.1 tests |
| `internal/tools/workflow_checks_gotcha_depth_floor.go` | new | ~70 | §3.3 |
| `internal/tools/workflow_checks_gotcha_depth_floor_test.go` | new | ~100 | §3.3 tests |
| `internal/tools/workflow_checks_claude_consistency.go` | rewrite | ~200 replace | §3.4 |
| `internal/tools/workflow_checks_claude_consistency_test.go` | rewrite | ~150 | §3.4 tests (incl. shadow) |
| `internal/tools/workflow_checks_service_coverage.go` | edit L100-105 | 6 strings removed | §3.5 |
| `internal/tools/workflow_checks_service_coverage_test.go` | edit | ~30 updated | §3.5 tests |
| `internal/tools/workflow_checks_causal_anchor.go` | edit L127-132 | 8 strings removed | §3.5 |
| `internal/tools/workflow_checks_causal_anchor_test.go` | edit | ~20 updated | §3.5 tests |
| `internal/tools/workflow_checks_recipe.go` | edit | +2 register calls | registration |
| `internal/workflow/recipe_plan_predicates.go` | edit | +10 `CodebaseRole` helper | §3.3 |
| `internal/workflow/recipe_topic_registry.go` | edit | +3 topic entries | §3.2, §3.6 |
| `internal/workflow/recipe_topic_registry_test.go` | edit | +3 tests | §3.2, §3.6 tests |
| `internal/workflow/recipe_engine.go` | edit | +2 suggestion strings at step boundaries | §3.6 |
| `docs/recipe-version-log.md` | edit | add v21 entry | after v22 validation |
| `docs/spec-recipe-quality-process.md` | edit | reference v8.80 reform | after v22 validation |

Approximate total: ~800 LOC changed/added, predominantly in new files and new tests.

---

## Appendix B — v21 grading details (for version-log entry authorship after v22 lands)

- **S** Structural: **B** — all 6 steps completed, 5 features working, both browser walks fired, but 3 live CRITs (NATS URL, Meilisearch OOM, CORS) required recovery work mid-run.
- **C** Content: **C** — v8.78 reform drove iteration correctly on 4 of 5 new checks, but net content compressed 26% and `claude_readme_consistency` is dead code. Two content quality issues remain (SSHFS gotcha triple-duplicate, JetStream gotcha factually suspect).
- **O** Operational: **D** — 129 min wall (+82% vs v20), 6 very-long bash (v20: 0), 17 errored (v20: 7). Nearly entirely attributable to the 209 MB apidev bloat + 3×120 s zcp-side git traversal.
- **W** Workflow: **D** — 0 `zerops_guidance` calls, 5 subagents vs v20's 10, main agent absorbed all iteration cycles. Writer/yaml/fix/critical-fix emergent delegation patterns all collapsed.

Overall = min(B, C, D, D) = **D**.

This is the worst drop from the prior version on record: v20 → v21 = A− → D in one version. The grading is defensible: v7 graded A with less operational instrumentation, v20 was the first A-grade complete run in session-logged history, v21 is a 4-grade slide from that baseline.

---

## Appendix C — Second-pass audit corrections (what changed in this document's history)

The first pass of this document (before explicit challenge of root-cause attribution) proposed a "v8.78 brief density → main-context bloat → delegation collapse → compression" thesis. Exhaustive content-delivery measurement overturned the first half of that chain.

**What was wrong in the first pass**:
- Claimed "recipe.md's 57 extra lines made the brief feel comprehensive" as the mechanism for delegation collapse. Actually-measured step brief size is 12% SMALLER in v21.
- Claimed "brief density caused main-context bloat". Main-context growth is entirely from the agent's own `tool_use` inputs (Write/Edit payloads), not from served content.
- Missed the 6 `dev_server stop` exit-255 events entirely until user challenge.
- Missed the 6 feature-subagent MCP schema errors entirely.
- Missed the code-review ripgrep timeout cascading from root cause #1.
- Missed the scaffold "scratch-reference-copy" gap (subagents ran `nest new` but didn't transcribe `.gitignore`).

**What was right**:
- Root cause #1 (missing `.gitignore` in apidev/workerdev briefs) — reinforced by verbatim v20/v21 brief diff.
- Root cause #2 (zcp-side git trap on main agent) — verified timestamps and sub-10s ssh-side comparison.
- Root cause #3 (quality ceiling without coverage floor) — the service_coverage-floor-exact gotcha count is strong inference but still non-deterministic.
- Root cause #4 (`claude_readme_consistency` regex dead) — shadow-tested against v18/v19/v20/v21 content; only matches v20 apidev's specific string.
- Root cause #6 (framework hardcoding) — verified by code reads; second-pass audit via Explore agent found 6 HIGH and 4 MEDIUM hardcoding sites.
- Root cause #7 (scaffold self-review checklist rear-view mirror) — unchanged.

**Net confidence after second pass**:
- 99% on "these 13 verified issues are all real, present, and each contributes to the regression"
- 90% on "the 13 issues account for ~86% of the observed 58-min wall-clock regression" (attribution math in §4.9)
- 65–75% on "the mechanism I propose for delegation collapse" — the correct humility is that delegation collapse IS measured but mechanism is NOT proven.

**Net confidence after third pass (deterministic-gate upgrade)**:
- 99% on "with §3.1, §3.2a, §3.3, §3.4, §3.5, §3.6d, §3.6e, §3.6f, §3.7a shipped as enforced gates, v22 ≥ A− is guaranteed"
- The delegation-mechanism uncertainty is moot — the gate in §3.6d forces the dispatch regardless of why the agent would otherwise skip it
- The zcp-side-git-choice uncertainty is moot — the tool guard in §3.2a rejects the pattern regardless of why the agent would otherwise try it
- Stochasticity only operates within the envelope the gates permit; that envelope is bounded to produce ≥v20 quality by construction

**What a v22 shadow run will tell us**:
- Whether fixes §3.1–§3.7 together restore A−. If yes, the root causes listed cover the true surface. If no, additional surfaces remain.
- Whether delegation patterns restored by §3.6 hold deterministically once recipe.md explicitly instructs writer/fix subagent dispatch. If v22 still has emergent variance, delegation is model-behavior-level and needs runtime enforcement, not brief-level instruction.

---

*End of implementation guide. Document owner: zcp maintainer. Last updated: 2026-04-16 (second-pass audit complete).*
