# Guide corpus — LLM-optimality pass (DESIGN + PLAN, no edits)

**Date:** 2026-06-02 · **refined 2026-06-03** (purpose-model reframe + whole-corpus analysis)
**Scope:** `internal/knowledge/guides/*.md` (20) + `internal/knowledge/decisions/*.md` (5), and the THEMES they overlap (`internal/knowledge/themes/*.md`).
**Already shipped (content correctness, do not redo):** commit `a339a9d5` + zeropsio/docs PR #346 (facts), commit `e72070ef` (verify-web-agent-protocol retirement). This pass is FORM/SCOPE for LLMs.
**Deliverable of this doc:** a decision-ready plan. No edits until reviewed.

---

## 0. The model — purpose/scope first (supersedes the old tool-agnostic framing)

A guide EXISTS for the **topic-specific depth that the universal layer deliberately doesn't carry.** The universal layer is: **themes** (`scope="infrastructure"`, pulled on demand) → **atoms** (phase + state-tailored, PUSHED every turn) → **tool schemas**. Classify every block of guide content into four buckets:

| Bucket | Meaning | Action |
|---|---|---|
| **U** universal, owned elsewhere & not needed for self-standing | a theme/atom/tool-schema carries the exact fact | **cut** (name the owner) |
| **F** framing — minimal universal context to stay self-standing | a uri-fetch returns ONLY the guide body, no prelude; the public `llms-full.txt` LLM has NO atoms; the ZCP agent may fetch after compaction | **keep compact** (3–8 line lead + pre-danger reminders) |
| **T** topic-specific depth — the reason to exist | the agent reaches THIS guide for it | **keep + sharpen** |
| **P** pedagogy/scaffolding | teach-from-scratch, multi-language SDK demos, ASCII diagrams, body-restating Gotchas | **cut** |

**Tool-leakage is a DETECTOR, not the disease:** a universal-flow tool in a guide (`zerops_deploy/verify/env/…`) flags U content that strayed in. A no-atom tool (`zerops_scale`, `zerops_manage`) flags T where the tool is part of the specific solution → keep, **portable mechanism leads**, ZCP call as a marked aside. Ownership is decided **per-fact, by reading the candidate owner**, never per-keyword.

**Two empirical results from the whole-corpus pass (9 agents, per-fact verified):**

1. **The atom-promotion "flip" is dead: 0 of 36 universal-but-unowned facts justify a new pushed atom.** Every one is already-owned, belongs in a theme, stays as guide F/T, or is rejected pedagogy. Promoting any would bloat a priority-1/2 atom that fires EVERY develop turn — the exact thing the atom contract forbids. The few worth moving go to **themes** (infra-scope, not pushed), not atoms.
2. **The disease is duplication + guides re-hosting theme/atom content, not missing structure.** The fix is per-guide U-cut + a small structural reshape, and it roughly **halves the corpus line count** (~2125 → ~1100 across guides).

---

## 1. Corpus architecture (the structural moves)

| Move | Guide(s) | What |
|---|---|---|
| **CREATE** | `readiness-health-checks` | absorbs the **triplicated** health/readiness/temporaryShutdown now split across zerops-yaml-advanced + deployment-lifecycle + production-checklist. Owns the BEHAVIOR (themes/core owns the field shape). Highest-value extraction. |
| **CREATE** | `shared-storage-integration` | parallels object-storage-integration; the one managed service whose multi-step import `mount:` + `run.mount` + post-deploy `connect-storage` wiring the dense services-card can't teach on a cold fetch. |
| **SPLIT + RENAME** | `zerops-yaml-advanced` → `zerops-yaml-run-features` | a bucket, not a topic. Loses health/readiness (→ new guide) + init-vs-prepare table (→ deployment-lifecycle) + base-images (→ cut, owned by themes/model + bases/). Residual = crontab / startCommands / envReplace / routing / extends (5 genuine-T sections, ~90 ln, NOT sharded into stubs). |
| **SHRINK to pointers** | `production-checklist` 170→~50 | becomes an ordered go-live **checklist that points at owners** (HA / scale / stateless-storage / sessions / SMTP / remove-dev-tools / health), not a second copy. Framework-prod-settings table → recipe `.md` (Aleš) or terse table. |
| **CONSOLIDATE** | initCommands / `zsc execOnce` / `${appVersionId}` | folds INTO deployment-lifecycle (its lifecycle owner), not a new guide. |
| **DEDUPE, leave-split + cross-ref (NOT merge)** | networking→public-access/cloudflare; firewall↔public-access direct-port (firewall owns, public-access xrefs); local-development→vpn (xref, drop own VPN table); logging↔metrics ELK (one self-hosted-observability owner, both xref); choose-runtime-base→thin pivot linking `bases/` cards | |

**Net:** guide count ~flat (+2 create, 0 delete — firewall survives as direct-port owner), line count down materially, each guide gets one clear reason-to-exist. **Atom corpus UNCHANGED.**

---

## 2. Per-guide action table (U/F/T/P + line delta)

| Guide | ln→ | buckets (F/T/U/P) | Top cuts (→ owner) | Public-fix |
|---|---|---|---|---|
| environment-variables | 219→~95 | 2/7/6/1 | Precedence, Project-auto-inherit, Self-shadow, Restart, Common-Mistakes → `develop-env-var-*` atoms + themes/model | clean |
| deployment-lifecycle | 169→~75 | keep+absorb init | Gotchas, SSHFS-mount → `develop-first-deploy-write-app`; readiness/health → **new guide** | clean |
| local-development | 130→~55 | 2/2/7/2 | **9 of 13 blocks** → vpn.md xref + `develop-local-*`/`platform-rules-local` atoms | step-1 leads `zcli` (ok) |
| zerops-yaml-advanced | 183→~90 | SPLIT | base-images → cut; init-table → deployment-lifecycle; health → new guide | clean |
| scaling | 202→~95 | 4/6/4/1 | behavior-param internals → cut; threshold-call → `zerops_scale` schema | presets lead `zerops_scale` → fix |
| networking | 142→~70 | 1/2/4/2 | ASCII diagram → cut; IPv4 table → public-access; Cloudflare summary → cloudflare | 502-checklist tools → public framing |
| object-storage-integration | 146→~70 | 1/4/2/1 | **4-language SDK sprawl (L45-103)** → cut; (keep path-style + apiUrl/apiHost = T) | clean |
| public-access | 46→~26 | 1/2/5/2 | auto-enable → `develop-first-deploy-execute`; internal-fallback + DNS → themes | **REGRESSION**: leads `zerops_deploy` → lead `enableSubdomainAccess`/GUI |
| cloudflare | 64→~18 | 0/1/4/1 | DNS/SSL tables → operations.md theme; prepare-steps → atom (keep wildcard/ACME = T) | **REGRESSION** → portable-first |
| cdn | 60→~28 | 1/3/1/2 | region pedagogy, Gotchas → cut | clean |
| firewall | 37→~12 | 1/1/2/1 | port tables → operations.md theme (survives as direct-port owner) | clean |
| vpn | 38→~24 | 0/3/2/1 | Behavior → operations.md; Gotchas → cut (keep troubleshooting = T) | clean |
| production-checklist | 170→~50 | SHRINK→pointer | ~all blocks → themes/operations/model + smtp/object-storage; framework table → recipe | clean |
| php-tuning | 133→~95 | 1/7/0/2 | minor; Extensions section = build/runtime spillover | clean (recipe-adjacent flag) |
| build-cache | 91→~75 | 2/5/0/1 | build-container specs → model.md theme (keep cache:/cascade = T) | clean |
| logging | 62→~38 | 1/5/2/1 | GUI access, Gotchas → cut; ELK → consolidate w/ metrics | clean |
| metrics | 51→~40 | 1/6/0/1 | Gotchas → cut; ELK → consolidate w/ logging (apmserver-subdomain = T) | clean |
| backup | 56→~42 | 1/6/0/1 | Gotchas → cut (keep per-service formats = T) | clean |
| smtp | 53→~36 | 1/3/2/1 | Gotchas → cut (Gmail App-Password = F; keep port-587 + provider = T) | clean |
| ci-cd | 84→~58 | 1/8/1/0 | Gotchas, record-deploy → `develop-record-external-deploy` (keep webhooks/actions = T) | clean |
| choose-database | 38→~18 | 1/1/3/1 | Gotchas → themes (keep selection reasoning = T) | clean |
| choose-cache | 31→~12 | 1/1/2/1 | KeyDB + Gotchas → services card (keep "Valkey not KeyDB" = T) | clean |
| choose-queue | 49→~18 | 1/1/4/0 | Gotchas → services NATS card | clean |
| choose-search | 50→~16 | 1/1/4/1 | Gotchas → services cards | clean |
| choose-runtime-base | 42→~12 | 1/1/3/1 | Alpine/Ubuntu/Docker sections → `bases/` cards (thin pivot) | clean |

**Systemic (per-item, not bulk):** ~17 docs end in a "Gotchas" section that's a MIX — most restate the body (cut), a few carry uniques NOT in the body (**smtp** Gmail-app-password, **object-storage** region-ignored/no-backup/no-autoscale). Diff each item; keep uniques as a `TRIGGER→FAILURE` line.

---

## 3. Theme-edit ledger (the ONLY "promotions" — all to themes, ZERO atoms)

~8 facts across 5 theme sites. Themes are committed in-repo (NOT synced/gitignored) → normal commits, pulled into `scope="infrastructure"`.

| Theme | Add |
|---|---|
| `themes/core.md` (initCommands cluster) | **initCommands non-zero exit ABORTS the deploy** (`RUN.INIT COMMANDS FINISHED WITH ERROR`, appVersion not activated) — highest-value, also a live-verified correctness fact; one-sentence extension. |
| `themes/model.md` (Build/Deploy Lifecycle) | build-container resource envelope (CPU 1-5, RAM 8GB fixed, Disk 1-100GB, 60-min timeout). |
| `themes/services.md` (PG + MariaDB cards) | per-service RAM minimum override (0.25GB vs 0.125 default); PG `pg_stat_statements` needs superuser + restart. |
| `themes/operations.md` (Public Access + Firewall) | direct TCP/UDP port-access + per-port blacklist/whitelist (consolidate the firewall direct-port topic). |

Everything else: 18 keep-as-framing (guide F/T or already-owned pointer), 2 reject (per-language `.env` loaders, MCP/API-key setup — pedagogy).

---

## 4. Phasing

- **P1 — Guide content (sync-pushable).** All U-cuts + F-leads + scaffolding strips + Gotchas per-item + the 5 cross-ref dedupes. ~25 docs. Verify by grep (gitignored), `sync push --dry-run` preview before any push.
- **P2 — Structural reshape (sync-pushable + process care).** 2 new guides (readiness-health-checks, shared-storage-integration), 1 split+rename (zerops-yaml-advanced → zerops-yaml-run-features), production-checklist → pointer. **Process wrinkle:** guides are synced from zeropsio/docs and `sync pull` is additive (never deletes) — a RENAME/SPLIT leaves the old upstream file, and a new guide must land upstream to survive a pull. Needs: author upstream + update the embedded URI list (`engine_doc_test.go`) + manual upstream cleanup of the renamed file. **This is the one move with non-trivial sync mechanics — see Gate.**
- **P3 — Theme edits (in-repo commit).** The 5 sites in §3. Themes are committed Go-embedded content; normal RED→GREEN if any test pins them.
- **P4 — Engine (OPTIONAL, ~20-40 LOC).** `section=`/`tldr` param on MODE 5 (parser already exists) — only if big guides still flood after P1/P2. **Free win regardless:** add `local-development` + `php-tuning` to `TestStore_GuidesEmbedded` (fetchable but unasserted, 18 of 20).
- **Aleš-scope (FLAG, no edits):** production-checklist framework-prod-settings, php-tuning FPM/extensions, build-cache per-runtime paths, choose-queue Kafka, ci-cd build-integration overlap.

---

## 5. Effort / risk

- **P1:** ~25 docs, content-only, ~1 day. **Risk low** (prose on corrected facts, `--dry-run` gate). Watch: don't strip a "P" item that's actually F/T (object-storage path-style, env-vars `${RUNTIME_x}` — marked keep).
- **P2:** the structural move. **Risk medium** — purely from the sync-rename mechanics, not the content. Mitigate: do new-guide creation first (additive, safe), do the rename last with explicit upstream cleanup.
- **P3:** 5 theme edits, in-repo. **Risk low**, but themes feed `scope=infrastructure` + may have pin tests — verify.
- **P4:** opt-in, low.
- **Backward-compat:** guide content is fetched-on-demand + gitignored → transparent to users. New/renamed guide URIs: only break a caller that hardcoded `zerops://guides/zerops-yaml-advanced` — internal only; update the embed test + any atom cross-ref.

---

## 6. Gate questions

1. **Approve the U/F/T/P model + the empirical result** (0 atom edits; ~5 theme edits; ~half the guide lines)? This supersedes the old tool-agnostic boundary policy. Or challenge the bucketing first.
2. **Corpus reshape (§1)** — green-light: create readiness-health-checks + shared-storage-integration, split+rename zerops-yaml-advanced, shrink production-checklist? This is the structural decision (vs. P1-only "just cut/sharpen in place, no new/renamed files").
3. **P2 sync-rename mechanics** — the split/rename interacts with additive `sync pull`. Want me to (a) do P1 + new-guide creation now and **defer the rename** until we confirm the upstream-cleanup flow, or (b) handle the full reshape including rename in one pass?
4. **P4 engine `section=` param** — build now or defer (test-gap close happens regardless)?

---

## Provenance

Three workflows + one Codex review, no edits to guides/atoms/themes: **inventory** (9 agents), **plan critic** (3 agents), **Codex purpose-model review** (added the F bucket + flip-as-bias + per-fact rule + 5 ownership-sensitive guides + 3 missing-guide gaps), **whole-corpus U/F/T/P analysis** (9 agents: 7 per-guide ownership-by-reading + 2 synthesis — promotion/atom-bloat ledger returned 0/36 atom promotions; architecture returned the §1 reshape). Fetch model independently verified against `internal/knowledge/{documents,sections,engine}.go` + `internal/tools/knowledge.go`.
