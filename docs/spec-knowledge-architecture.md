# ZCP Knowledge Architecture Specification

> **Scope**: The canonical model for how EVERY curated fact reaches the LLM agent — across all delivery channels (atoms, knowledge engine, tool schemas, workspace-injected context, structured-response guidance). This is the umbrella architecture; `spec-knowledge-distribution.md` (the atom model) and the guide-optimality work are subsystems under it.
>
> **Status**: Authoritative — migration complete. Every delivery channel this spec covers (atoms, knowledge themes, guides, tool-schema enums, structured-response/failure guidance) now has a single fact owner; guide and theme content was cut to remove duplication with zero fact loss, each cut verified against the live platform rather than the docs alone; tool-schema enums are drift-pinned to their owning Go symbols by test so a new value can't go unsurfaced. Open item: publishing the resulting guide rewrites to the public `zeropsio/docs` repo — including the upstream rename to `zerops-yaml-run-features` — remains gated on maintainer review ahead of that irreversible public push.
>
> **Provenance**: derived from an exhaustive discovery (2026-06-03): a 6-area source census (72 channels, 47 fact-sources) + an 8-domain fact-ownership trace (127 facts: 101 duplicated, 6 already conflicting) + a Codex architecture review. Empirically grounded, not theorized.

---

## 1. The Problem — the fact-ownership crisis

ZCP curates platform knowledge for an LLM coding agent. The **atom model** (`spec-knowledge-distribution.md`) established the right principle for ONE slice of that knowledge — runtime guidance:

> *One source per fact; delivery is computed. There is one atom per fact, tagged with the envelope cells it applies to; the synthesizer composes per turn.*

But the knowledge surface **grew into ~10 parallel channels** without extending that principle to them. The result, measured:

- **127 platform facts traced; 101 are authored in 2–5 places; 6 have already DRIFTED** (the copies disagree).
- The same fact lives across: the atom corpus, knowledge themes (5), guides (22), decisions (5), bases (5), recipes (37 `.md` + 10 `.import.yml`), recipe-authoring atoms, **tool jsonschema descriptions (115, of which 60–70 % carry platform facts)**, workspace boot-shims (`agents_*.md`), structured-response guidance (`SuggestedAction`/`NextActions`/recovery hints/blocker messages), **and the design specs themselves (`docs/spec-*.md`)** — and these copies actively CONFLICTED until reconciled (see the `zsc noop` row below: an internal "omit `run.start`" convention had diverged from the platform-authoritative `zsc noop` across ~20 surfaces with no owner to reconcile against — fixed 2026-06-03).
- There is **no single-owner registry and no cross-source drift detection anywhere.** Every channel is independently hand-authored.

Representative drift (the 6 conflicts):

| fact | conflict |
|---|---|
| env-var model section | `core.md` repeats it VERBATIM at lines 141-161 **and** 216-236 — edit one, the other silently diverges |
| dev-dynamic `run.start` | **platform-authoritative = `start: zsc noop --silent`** (public docs `zerops-yaml-advanced.mdx`; live dev container runs it; recipes author it). An internal "omit `run.start`" convention (commit `cdbcc0da`, run-49/52) had diverged across atoms + guide + recipe gate/briefs + tool docstrings. **RESOLVED 2026-06-03** — every ZCP surface restated to `zsc noop --silent`; the wrong-enforcing `gate_dev_runtime_no_run_start` gate deleted. The exemplar of §2: the platform is the source of truth; ZCP's stored copy had drifted and was reconciled TO the platform (not the reverse). |
| object-storage region | `services.md` vs `operations.md` — same fact, two wordings (both true: required by the SDK, ignored by MinIO). Duplicate-with-one-owner, not a true conflict — but still two copies. |
| `build.base` multi-base | `core.md` allows `[php@8.4, nodejs@22]`; `develop-first-deploy-scaffold-yaml` atom says "runtime-only key" |
| `mode` (HA/NON_HA) | `core.md` schema shows it generic (L19); rules say "NEVER for runtimes" (L116) — unscoped in the schema |
| reload-vs-restart, setup-naming, failure-phase taxonomy, … | each authored 3–5× with no shared owner |

This is precisely the drift the atom model was built to kill — but the drift now runs **across** the channel boundaries the atom model never governed.

---

## 2. The Principle — one fact, one owner, computed delivery

**Generalize the atom model's founding principle to the WHOLE knowledge surface:**

> **Every curated fact has exactly ONE authoritative owner. Every other surface that needs that fact POINTS at the owner or DERIVES from it — never re-authors it. Delivery (push / pull / persistent) is computed from the owner, not duplicated by hand.**

This is the same "one source, computed delivery" rule the atoms already follow internally — applied at the seam between channels, where it currently does not hold.

---

## 3. The Canonical Model — a scope boundary + three orthogonal dimensions

A fact is governed first by **scope/audience** (which pipeline + reader it serves), then along three orthogonal axes within that scope. Keeping these separate is what makes the model coherent — the current mess comes from conflating "who is this for," "who owns it," and "how it's delivered."

### 3.0 SCOPE / AUDIENCE — the prior boundary (governs what "one of X" even means)

Not all curated content serves the same reader. The unification rules (one owner, one pull retrieval) apply **within an audience**, not across all of them. The audiences:

| Audience | Channels | Note |
|---|---|---|
| **Runtime agent — guidance** | atoms (push) | what the operator's agent does this turn |
| **Runtime agent — reference** | knowledge themes/guides/decisions/bases (pull) | platform depth the agent fetches |
| **Recipe-authoring agent** | the v3 engine's embedded brief substrate (`internal/authoring/recipe/content/`, served through `zerops_recipe` responses) | a SEPARATE pipeline (maintainer-only authoring domain — `docs/spec-authoring-boundary.md`). Its own engine-driven delivery is legitimate — NOT folded into the runtime pull. |
| **Persistent workspace context** | `agents_{shared,container,local}.md` (boot-shims) | env-topology + routing facts injected at init — a fact channel, governed |
| **Developer / spec** | `docs/spec-*.md` | authoritative for DESIGN; but they restate platform facts too → governed against drift |

**Consequence:** §4's "exactly one pull retrieval" is scoped to the **runtime audience** (guidance + reference). Recipe-authoring keeps its own engine-driven brief delivery by design.

### 3.1 OWNERSHIP — who authors the fact (exactly one owner)

Owner = **authority class + audience + fact kind** (fact-kind alone is too coarse — `schema.Cache` and a theme can both look like "platform reference," but one is a live authority and the other is doctrine):

| Fact kind | Owner | Examples |
|---|---|---|
| **Existence / catalog** (does this type/base/version exist) | **`schema.Cache`** (live authority) | service-type / build-base / run-base existence, active versions |
| **Enforced classification** | **Go registry / gate** | `topology.FailureClass`, recovery-hint mapping, adoption-state enum, deploy-mode classify |
| **Platform doctrine / reference** | **knowledge themes** (`core.md`/`model.md`/`services.md`/`operations.md`); **guides** for topic-depth | YAML schema, env-var model, deploy=new-container, service specs/wiring, scaling, networking |
| **State-tactical action** — what to do NOW given the live envelope | **atoms** | "you're in first-deploy; run `zerops_deploy targetService=X` no strategy"; close-mode gate; per-service promote |
| **Framework / app-specific procedure** | **recipes** | Laravel `.env` handling, Next.js standalone deployFiles |
| **Tool-parameter contract** — what THIS param accepts | **tool jsonschema** (contract only; platform facts it cites derive from the owners above) | "`atomId` is the id from the pointer"; "`serviceHostname` filters logs" |
| **Env-topology operational facts** | **boot-shims** (`agents_container.md`/`agents_local.md`) | mount paths, dev-server-vs-SSH per environment |

The fact-trace tally confirms the split empirically: of the duplicated facts, the *recommended* single owner was knowledge-theme for 57, an atom for 21, a guide for 16, core.md for 13 — i.e. **most platform FACTS belong in the pull/knowledge tier; atoms own the operational slice and point at the reference.**

**Service identity is the `base` string, and the platform `/settings` schema is its catalog — both confirmed by the BE direction (2026-06):** Zerops is collapsing `serviceStackType` + `serviceStackTypeVersion` (+ the arbitrary `serviceStackTypeVersionId`) into one self-describing `base` string — `[os/]software[:mod][@version]` (runtimes carry an OS prefix `alpine/nodejs@22`; managed services carry a mode `postgresql:ha@17`; the two are mutually exclusive, never both). Clients are to stop hard-coding type/version lists and read them from `/settings` instead. ZCP **already** matches this: `schema.Cache` reads the `/settings` JSON schema (no hard-coded catalog — `StackTypeCache` was deleted), and `topology.type_equivalence.go::CanonicalBareForm` parses exactly this base-string grammar (OS-prefix + `:mode` decorations, with legacy sibling `os:`/`mode:` fallback). This is why **existence/catalog is owned by `schema.Cache` and classification by `topology`** (§3.1) and not by any stored constant: the `base` string IS the identity, the live schema IS the catalog. The BE's deprecation of `serviceStackTypeVersionId` therefore needs no ZCP change beyond following the DTO when the field is finally removed (it stays BC-deprecated meanwhile). Note: the base-string `:mod` (`:ha`/`:single`) is a managed-service-only decoration — distinct from the import.yaml `mode: HA|NON_HA` scaling field, which the platform also accepts on runtimes.

**ZCP speaks the composite form everywhere it speaks; legacy forms are explained once.** Every agent-facing surface (atoms, themes, bases, tool jsonschema examples, the live catalog) writes runtime identifiers as `<os>/<tech>@<ver>` and managed ones as `<tech>:single|ha@<ver>`; the deprecated sibling fields (`os:`, `mode:`) and bare `<tech>@<ver>` appear only inside the single block labelled *Legacy forms* in `bootstrap-provision-rules.md`, which maps each legacy spelling to its equivalent (`TestNoLegacyTypeFormInAgentContent`). The block exists because the two legacy defaults are ASYMMETRIC and `run.base` is authoritative — live-verified on prg1 (2026-08-28): a bare import `type` materializes as `ubuntu/…` (only `static` → `alpine/`), a bare zerops.yaml `base` resolves to `alpine/…` in both build and run, `run.base` rewrites the service OS on every deploy (both directions), omitting it keeps the current OS, and a bare `build.base` under an `ubuntu/` run builds on alpine (mixed-OS). The catalog rendering (`knowledge.FormatStackList`/`FormatServiceStacks`) therefore leads with an identifier legend whose single-OS exceptions are DERIVED from the schema enum (`catalogView.osExceptions`), never hard-coded (`TestFormatStackList_OSLegend`). The platform facts themselves have no ZCP test — their home is the platform docs (upstream, mid-migration: only the Ruby pages use the composite form as of docs `4899cf0b`) and live verification.

### 3.2 DELIVERY — how the owner's fact reaches the agent

| Mode | Mechanism | When |
|---|---|---|
| **PUSH** | atom `Synthesize` composes it into the workflow response, filtered by live `StateEnvelope` | state-tactical guidance the agent needs every relevant turn |
| **PULL** | `zerops_knowledge` (query / briefing / scope / recipe / **uri**) — the ONE retrieval path | reference depth the agent fetches on demand |
| **PERSISTENT** | written to the workspace at `zcp init` (`agents_*.md`) | env-topology context that must survive across sessions |

**An atom may carry its body inline (PUSH) or as a pointer (PULL).** The `delivery = inline | pointer` attribute (today's `reference:true`) chooses. A pointer-rendered atom is delivered *via the pull tier* — see §3.3 + §4.

### 3.3 REFERENCE — how non-owners point at a fact

A surface that needs a fact it does NOT own must use one of exactly three sanctioned pointer forms — never a hand-copy:

| Pointer form | Used by | Mechanism |
|---|---|---|
| **content cross-ref** (`references-atoms`) | atom → inline atom in the same payload | the cited body co-renders |
| **depth pointer** (`pointer-atoms`) | atom → deferred reference | a stub + the canonical pull URI |
| **derive-from-code** | tool-schema / response-prose → a code-owned fact (enum, registry) | generated or compile-checked against the owner, not retyped |

The `references-atoms` / `pointer-atoms` contract (`atom_crossref_contract_test.go`) is the atom-tier instance of this rule and is a **fundament** — it stays. §3.3 generalizes it to the other channels.

---

## 4. The unified retrieval — one pull path (runtime audience)

Within the **runtime audience** (§3.0) there is exactly **one** "fetch a curated document by key" operation: `zerops_knowledge`. Before this unification there were two — `zerops_knowledge uri=` for knowledge docs and a bespoke develop-atom action for reference atoms — the same operation on two corpora. **(Achieved, Phase 1.)**

**Model:** `zerops_knowledge` is the single runtime pull retrieval. Reference atoms are addressable as `zerops://atoms/<id>` and fetched through it. The tool-layer adapter resolves `zerops://atoms/` against the atom corpus — and **only for `reference:true` atoms**: inline atoms carry `{hostname}`/`{stage-hostname}` placeholders that are substituted at synthesis time, so exposing them via raw fetch would leak unsubstituted tokens. The bespoke develop-atom action is deleted. The atom `pointer` stub emits `zerops_knowledge uri="zerops://atoms/<id>"`.

This keeps **separate authoring models** (atoms hand-authored + state-composed; knowledge docs synced/embedded + searched) while unifying the **pull surface** the runtime agent sees.

**Out of scope:** recipe-authoring brief delivery (the v3 engine's embedded substrate) is a different audience (§3.0) and keeps its own engine-driven delivery by design — this is NOT the duplicate retrieval the rule targets.

---

## 5. Governance — the missing fundament

The reason the surface fragmented is that nothing **enforces** one-owner. The architecture adds two enforcement mechanisms:

1. **Single-owner drift lint** (cross-source) — a **high-value tripwire registry**, NOT an attempt to register all 127 facts (that would be a maintenance sink). Each entry: `{fact-id, canonical owner, forbidden-duplicate fingerprint(s), allowed reference surfaces}`. A test asserts the fingerprint appears only in the owner. Modeled on the existing `tell == check` lints. Seed with the acute drift + the highest-fan-out facts (dev-dynamic `run.start`-omitted, env precedence, setup naming, failure phase); grows only as real drift is found.
2. **Owner registries for code-owned facts**: failure-phase taxonomy, recovery-hint mapping, adoption-state enum, setup-naming — each a single Go owner (enum/table/registry) that tool-schemas + response-prose + atoms derive from or cite, never retype. (Several already exist, e.g. `topology.FailureClass`; the work is routing every restating site through them.)

Drift becomes a build failure, the way layer violations do (`architecture_test.go`).

---

## 6. Don't-touch — deliberately separate (NOT fragmentation)

These are distinct concerns, not duplication to consolidate:

- **Recipe-authoring brief substrate** (`internal/authoring/recipe/content/`) + its engine-driven delivery (`zerops_recipe`): a separate v3 pipeline + audience (maintainer-only authoring domain — `docs/spec-authoring-boundary.md`). Orthogonal — explicitly carved out of the §4 one-retrieval rule.
- **Workspace plumbing**: `.mcp.json`, `.claude.json`, `.claude/settings.json`, SSH config, VS Code extension, shell aliases. Configuration, authors no facts. (NOTE: the `agents_*.md` boot-shims are NOT plumbing — they author routing + env-topology facts → governed per §3.0.)
- **REFLOG** in AGENTS.md: historical record, explicitly disclaimed ("verify current state via `zerops_discover`"). Not a knowledge owner.
- **Live schema** (`schema.Cache`): NOT exempt-and-ignored — it is the **named owner** of existence/catalog facts (§3.1). Out of *content de-dup*, but tool-schema/atom restatements of "what types exist" must derive from it.
- **Separate authoring models themselves**: do NOT merge `internal/knowledge` into `internal/content/atoms` or vice-versa. The push/pull split is principled (state-composed vs synced/searched). Unify the *retrieval surface* and the *fact ownership*, not the stores.

---

## 7. Relationship to other specs

- `spec-knowledge-distribution.md` (atom model) — the PUSH tier's authoring contract; a subsystem under this spec. Gains the `delivery=inline|pointer` attribute + the §3.3 reference rule.
- `plans/guide-llm-optimality-2026-06-02.md` — the internal de-dup of the PULL tier (themes/guides), which independently reached the same "themes own facts; don't promote to atoms" conclusion. It is **Phase 4 of this program's PULL-tier slice**; this spec is its umbrella.

---

## 8. Invariants (to pin with tests as phases land)

- **I1** Every registered platform fact has exactly one authoring owner; other surfaces reference/derive. (drift lint)
- **I2** `references-atoms` targets render inline; `pointer-atoms` targets are deferred + reachable via the canonical pull URI. (`atom_crossref_contract_test.go`)
- **I3** Exactly one pull-retrieval operation; reference atoms reachable via `zerops_knowledge uri=zerops://atoms/<id>`; no second fetch API.
- **I4** Code-owned facts (failure phase, recovery, adoption state, setup naming) have one Go owner; tool-schema/response-prose derive or cite.
- **I5** The push/pull store split is preserved; no cross-store content merge.
