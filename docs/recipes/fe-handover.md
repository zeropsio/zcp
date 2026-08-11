# Zerops Recipe Authoring — Frontend Render & Deploy Contract (HANDOVER)

**Audience:** an autonomous agent that authors recipes in the `zcp` / recipe-authoring repo (recipe repos with a `.zerops-recipe/` folder, and/or Strapi CMS entries). You do **not** work in the frontend repo. After reading only this doc, you should be able to author a recipe — a CMS like Strapi, a software-OSS like Umami, or a code app — that renders beautifully and deploys correctly on the Zerops recipe-detail page.

This is a **behavior contract**, not a code tour. File:line citations appear only where the implementation detail *is* the rule.

---

## 1. TL;DR + the supply/demand mental model + the seam

**TL;DR.** A recipe is a `.zerops-recipe/` folder of markdown fragments plus one `import.yaml` per environment. The frontend reads those fragments through a fixed vocabulary and **composes the detail page by presence** — a fragment exists → its block/tab appears; it's absent → nothing. The one thing you must *declare* (it can't be inferred) is `shape: app | software`, which picks the platform "take-ownership" guide. Everything else is presence-composed. Get the fragment vocabulary, the env-folder naming (`<order> — <Name>` with an **em dash**), the heading levels (`###`–`#####` only), and the `import.yaml` secret taxonomy right, and the page renders correctly.

**Two layers, one seam.**

```
SUPPLY SIDE (you — zrecipator, NOT this repo)     DEMAND SIDE (this Angular frontend)
─────────────────────────────────────────        ───────────────────────────────────
LLM authoring pipeline →                          /recipes list + detail page
  recipe repos (.zerops-recipe/)                  onboarding pickers, deploy dialog
  zerops.yaml + per-env import.yaml               handoff / OAuth deep-link flow
  Strapi CMS entries                              renders fragments, deploys YAML
                    │                                        ▲
                    └────────────  THE SEAM  ────────────────┘
   Strapi recipes (internalType, sourceData.environments, recipeCategories, extracts)
   + GitHub repos via GET /api/recipe/info?url=<repo>@main
```

The frontend **only consumes**. It never authors. It learns about a recipe through exactly two channels, and these are the only contract you can write against:

- **Strapi CMS** — `Recipe.internalType`, `sourceData.environments[]`, `recipeCategories[]`, and `extracts` (the named fragments).
- **GitHub repos via `GET /api/recipe/info?url=<repo>@main`** — the backend clones the repo, reads `.zerops-recipe/`, extracts the fragments + import YAMLs, and returns them shaped like a `Recipe`.

Both converge on the **same** rendered model and the **same** render code (mode-agnostic). **Whatever you author has to express itself through this seam — fragments, environments, `shape`, and import YAML. There is no other channel into the page. Anything the page can't read off the seam, it can't show.**

---

## 2. The `.zerops-recipe/` layout + env-folder naming

```
.zerops-recipe/
  README.md                      ← RECIPE-LEVEL fragments (markers below)
  <order> — <Name>/              ← one folder per environment (em dash!)
    README.md                    ← PER-ENV fragments
    import.yaml                  ← the deploy manifest for this tier
  <order> — <Name>/
    README.md
    import.yaml
  ...
```

**Per-service fragments** are NOT authored here — they live in each service's own `buildFromGit` repo's `.zerops-recipe/` and arrive via `/api/recipe/info` on `service.extracts`. See §3c.

**Env-folder naming → key → tier (the chain that must resolve):**

| Step | Rule |
|---|---|
| Folder name | `<order> — <Name>`, **EM DASH U+2014** (e.g. `1 — Remote (CDE)`, `5 — HA Production`). Hyphen and `<order>_<name>` are accepted fallbacks; em dash is canonical and what every reference recipe uses. |
| `order` | leading integer — sorts the env selector. No prefix → `order = 999` (sorts last). |
| `key` | `slugifyEnvName(<Name>)` — lowercase, spaces→dashes. `displayName` is `<Name>` without the order prefix. |
| `tier` | the key/name is **fuzzy-matched** to one of 6 canonical guide tiers (§8). Native mode skips fuzzy matching — its keys ARE the tiers. |

Parser: `parseEnvironmentName` (`recipe-base.state.ts:131-160`). Two folders that slugify to the same key are **de-duped** — only one survives.

---

## 3. The COMPLETE fragment vocabulary

Fragments are markdown sections in the relevant `README.md`, extracted by the backend into an `extracts` map keyed by fragment name. All three groups feed the same render computeds; render is mode-agnostic.

### 3a. RECIPE-LEVEL (`.zerops-recipe/README.md`)

| Fragment | Renders to | Authoring guidance |
|---|---|---|
| `name` | Page title / recipe header (overrides the `owner/repo` slug) | One line. Human product name. |
| `intro` | Short blurb under the title, **ABOVE the deploy button** (the deploy card `desc`) | Short hook (1–2 sentences). This is the pre-deploy pitch. Also suppresses the §8 bare-README fallback. |
| `cover` | Full-width hero image above the deploy card. Also hides the recipe icon in GitHub mode. | First markdown image URL in the fragment is used (regex-extracted). Use a wide banner. |
| `description` | Long-form prose inside the **Overview** section, below deploy/price | The post-deploy depth. Half of the Overview gate (`hasAboutBlock`). |
| `features` | "Features" sub-block (h4 + markdown list) inside **Overview** | A markdown bullet list. Other half of the Overview gate. |
| `shape` | *Nothing visible.* Selects the take-ownership track (§4). `app` → template/fork flow; `software` → self-host operate guide. **Defaults to `app`.** | The one declared lever. Case-insensitive, trimmed; anything not literally `software` → `app`. No validation — a typo silently becomes `app`. |
| `takeover-guide` | "Recipe-specific setup" block in **Get started**. App flow: rendered **FIRST**, before fork/CI-CD. Self-host flow: "Recipe-specific notes" **after** the operate guide. | Keep it **FLAT** — no top-level heading (the page supplies the heading). Recipe-specific first-run notes only. See limits in §10. |
| `knowledge-base` | "General Reference" segment in the **Knowledge base** tab (after per-service KBs) | **Must contain at least one `###`** or it renders nothing (§7). Use `### Architecture`, `### Environment variables`, `### Operations`, etc. |

### 3b. PER-ENVIRONMENT (`.zerops-recipe/<env>/README.md`)

| Fragment | Renders to | Authoring guidance |
|---|---|---|
| `intro` | The env-segment intro in **Get started** (the "Taking ownership of the `<env>`" segment intro / env-selector popover) | **One line.** "When to use this tier" — plan, tradeoffs. This is the *only* per-env authoring lever (see limit 3, §10). |

### 3c. PER-SERVICE (authored in each service's `buildFromGit` repo; arrive on `service.extracts`)

| Fragment | Renders to | Authoring guidance |
|---|---|---|
| `integration-guide` | "Integrate with your apps" flow body in **Get started**. Presence summons the **Integrate track**. | Author sections at `###`. |
| `name` | Service display label (overrides raw service type) in integrate headers/selector and KB section headers | One line. |
| `knowledge-base` | A per-service section in the **Knowledge base** tab (header: name + service chips + repo link, then markdown) | **Must contain at least one `###`** or it's dropped. Services sharing one `gitRepo` collapse into a single segment. |

---

## 4. The shape contract (app vs software)

`shape` is the recipe-level extract (`RecipeShape = 'app' | 'software'`, defaults to `app`). It is the single most important authoring decision and **the one thing the runtime cannot infer**.

> **`shape` describes HOW THE RECIPE FRAMES OPERATION — not the product.** The framework is irrelevant. The same product can be either shape:
> - **Strapi** = `app` if its schema is code-first / pushed; `software` if run admin-configured.
> - **WordPress** ≈ `software`. **Umami** = `software`.

**The two platform take-ownership guides (you do NOT write these — `shape` picks them):**

| `shape` | Operation framing | Platform guide steps (rendered in Get started) |
|---|---|---|
| `app` | *You own the code and push it.* | fork source → connect CI/CD → deploy by push/tag → autoscale → domain → backups. Synthetic lead step "Fork the source code". Auto-excludes `zeropsio/*` platform glue from the fork list. |
| `software` | *You run prebuilt OSS you don't fork.* | persist data → configure → domain → backups → upgrade (bump the version) → scale. **No fork, no CI/CD.** |

- `app` is **not** a bespoke const — it renders the shared per-tier `environmentGuides` CMS content (Strapi `/api/static-content`), so the env *tier* tunes wording (tag-trigger vs branch-push). Your `takeover-guide` layers on top as recipe-specific notes.
- `software` is the only shape with a platform-authored const, `SOFTWARE_TAKEOVER_GUIDE` (`recipe-detail.page.ts:125-141`), six `###` steps. Your `takeover-guide` renders *after* it as "Recipe-specific notes".

**How to CHOOSE shape — it's about operation framing, not product:**
- Code you fork-and-push (hello-worlds, frameworks, starter kits, scaffolded CMS where the schema is in the repo) → **`app`**.
- Prebuilt OSS run admin-configured, where the thing you really own is its **data** (Umami, WordPress, admin-configured Strapi) → **`software`**.

**The `internalType` note.** Historically the native detail page branched on `recipe.internalType` (`'example'` vs `'project-utility'`, the old `isUtilityRecipe`). **That branch was removed from the detail page** — the only surviving mentions are explanatory comments (`recipe-detail.page.ts:940-941`). The page now routes on `shape` (GitHub) and **content presence** (native). `internalType` is still a real Strapi field used elsewhere (e.g. the import/export dialog's `filters[internalType][$in]` catalog query) and the 7 authoring types still collapse to its 2 buckets — but it no longer arbitrates detail-page rendering. **`app|software` is the true cardinality of the take-ownership problem.**

---

## 5. Compose-by-presence → tabs/tracks + the page funnel

**Which fragment summons which element:**

| Page element | Appears when… |
|---|---|
| **Hero cover** | `cover` authored |
| **Overview tab/section** | `description` OR `features` authored |
| **Get started tab** | always (when the recipe is present) |
| → **Template / take-ownership track** | GitHub: `shape:app` AND a clone affordance. Native: a CMS env guide exists OR ≥1 service has a git repo. |
| → **Integrate track** | ≥1 per-service `integration-guide` present |
| → **Flow chooser** (Template ⇄ Integrate) | **both** tracks have content |
| → **Self-host (operate) flow** | GitHub: `shape:software` AND ownable. Native: `takeover-guide` exists AND no integrate track. |
| → **Recipe-specific setup block** | `takeover-guide` authored (FIRST in app flow, after operate guide in self-host) |
| → **Fork/Clone source step** | `shape:app` AND a clone affordance |
| → **"Deploy more environments"** | >1 env folder |
| **Knowledge base tab** | any per-service `knowledge-base` OR recipe-level `knowledge-base` OR CMS service-type quicklinks exist |
| **Env selector** (deploy card + inline TOC popover) | >1 env folder |
| **Config wizard** (deploy-time) | `import.yaml` has bare-`null` secrets (§6) — handled in the deploy dialog, not this page |
| **Related Recipes tab** | native mode only (GitHub has no Strapi slug) |

**KB layers are stacked-additive** (every layer with content renders), in order: per-service `knowledge-base` → recipe-level `knowledge-base` ("General Reference") → CMS platform/service-type quicklinks (last, not author-written). If you ship *only* quicklinks (no authored `###` KB), the section is honestly retitled "Related platform & services docs" / nav "Platform docs" — it never falsely claims authored reference material.

**Page funnel, top → bottom** (where each fragment lands):

1. Logo claim (unauthed GitHub only) — no fragment
2. **Hero cover** ← `cover`
3. **Deploy card**: title ← `name`, blurb ← `intro`, categories/icon (native CMS), **env selector** (>1 env), **Deploy button**, resources + price
4. Section nav row (Overview · Get started · Knowledge base · Related)
5. **Overview** ← `description` then `features`
6. **Get started**: path intro / flow chooser → inline TOC (env/service popovers ← per-env `intro`, per-service `name`) → flow body:
   - *template flow:* "Taking ownership of `<env>`" → **Recipe-specific setup** ← `takeover-guide` (FIRST) → Fork/Clone → shared per-tier guide → integrate CTA → "Deploy more environments"
   - *self-host flow:* operate guide → `takeover-guide` notes → deploy-more-envs
   - *integrate flow:* per-service `integration-guide` → source repos → env guide → deploy-more-envs
7. **Knowledge base** ← per-service KBs → recipe KB ("General Reference") → CMS quicklinks
8. **Related recipes** (native only)
9. **§8 supplementary raw README** — GitHub only, *and only when none of* `intro` / `takeover-guide` / `integration-guide` / template-guide is present (thinly-authored fallback). Authoring any real fragment suppresses it.

`intro` deliberately sits *above* the deploy button (short hook); `description` is the post-deploy depth.

---

## 6. `import.yaml` & the secret taxonomy

One `import.yaml` per env folder. The frontend **has no YAML parser** — secret discovery is a pure line-scan (`scanNullSecrets`, `recipe-secret.util.ts`), and the deploy injection is a per-line rewrite that preserves every untouched line byte-for-byte.

**The secret taxonomy — what triggers the deploy-time config wizard.** A key is *eligible* only when it sits directly inside an `envVariables:` or `envSecrets:` block, one indent below the header (block entry/exit tracked by indentation). Within an eligible block:

| YAML value | Prompted at deploy? | Meaning |
|---|---|---|
| `KEY:` (bare) / `KEY: null` / `KEY: ~` | **YES** — config wizard asks, blocks deploy until filled | required user input |
| `KEY: ""` or `KEY: ''` | **NO** — never prompted | intentionally-empty / optional |
| `KEY: <@generateRandomString(…)>` | **NO** | backend generates it |
| `KEY: ${someRef}` | **NO** | derivable reference |
| `KEY: any-literal` | **NO** | already set |

So the **secret taxonomy** is: **bare/`null`/`~` → required (wizard prompts); `""` → optional (silent); generated/`${ref}`/literal → not user input.**

**Hard rules (enforced by the scanner — respect or the wizard misbehaves):**
1. **UPPER_SNAKE keys only.** The scan anchors on `[A-Z][A-Z0-9_]*`. Lowercase/mixed-case keys are never prompted (structural keys like `hostname`, `type`, `mode` are lowercase by convention — intentionally invisible).
2. **No block scalars for secrets.** `KEY: |` / `KEY: >` bodies are skipped entirely; multi-line secrets can't be prompted or injected.
3. Comment lines and blanks are skipped.
4. A bare `NESTED_CONFIG:` whose next line is *more-indented* is treated as a parent mapping and skipped (won't be mis-filled).

**`envVariables` vs `envSecrets`:**
- `envVariables:` → **project scope** (project-wide variable). Use for per-project knobs (DB name, region).
- `envSecrets:` → **service scope**, attributed to the nearest preceding `- hostname: <value>` list item. Use for per-service credentials. The same key name across two services stays unambiguous (injection targets by line index, never by global key name) — they render as two distinct fields.

**Masked input for free.** A key containing `PASSWORD|PASSWD|PWD|SECRET|TOKEN|API_?KEY|ACCESS_?KEY|PRIVATE|CREDENTIAL` renders as a `type=password` field, with a humanized label (`ADMIN_PASSWORD` → "Admin password"). Name sensitive vars accordingly. Every discovered null defaults to `required: true`; optionality/help text is enriched from Strapi metadata, not the YAML.

**Deploy path (FYI).** Authed in-page: edited YAML → `scanNullSecrets` → if secrets, the config-wizard dialog collects them and `injectSecrets` fills only those lines (always double-quoted/escaped for the strict Go parser) → `POST /project/import`. No secrets → one-click import. The confirm dialog is mounted **only** on the detail page, so the OAuth/deep-link path redirects there (`deploy=true`) when a recipe has nulls.

---

## 7. Heading-level rules

The TOC parser is called everywhere as `parseMarkdownHeadingsWithMeta(content, 3, 5)` — **only `###`–`#####` become navigable TOC anchors.**

- **Sections must start at `###`.** `#`/`##` are ignored by the TOC; `######` (h6) is out of range.
- **A `knowledge-base` (recipe-level OR per-service) with no `###` renders NOTHING** — it hits a `length === 0` guard and is dropped. Use `### Architecture`, `### Environment variables`, `### Operations`, `### Troubleshooting`. (`features` uses h4 internally — that's the page's own rendering, not a TOC rule.)
- **`integration-guide`** renders at any level, but author sections at `###`–`#####` to get TOC entries.
- **`takeover-guide` stays FLAT** — no top-level heading. The page heads it "Recipe-specific setup" itself. (The platform's `SOFTWARE_TAKEOVER_GUIDE` uses `###` because it's a full standalone guide — but *your* `takeover-guide` must not.)
- Reserve `#`/`##` for the human README body outside the fragment markers.

---

## 8. The 6 canonical env tiers + fuzzy matching

`EnvironmentGuideKey` — the only legal guide-tier keys (`constants/environment-guides.constant.ts`):

| key | display name | typical `import.yaml` topology |
|---|---|---|
| `ai-agent` | AI Agent | dev+stage pair: `appdev` (zeropsSetup `dev`, idles for an agent) + `appstage` (`prod`). LIGHT, NON_HA. |
| `remote-cde` | Remote CDE | one app container, zeropsSetup `dev`, idles for a cloud IDE. LIGHT, NON_HA. |
| `local` | Local | **backing services only** (db/storage/cache) — no app; run the app locally over `zcli vpn`. LIGHT. |
| `stage` | Stage | single `app`, zeropsSetup `prod`. LIGHT, NON_HA. |
| `small-production` | Small Production | NON_HA services + `app` on **shared** CPU, `minContainers: 1` / `maxContainers: 2`. SERIOUS. |
| `highly-available-production` | HA Production | **HA** services (`mode: HA`) + `app` on **dedicated** CPU (`cpuMode: DEDICATED`), `minContainers: 2` / `maxContainers: 4`. SERIOUS. |

The **service set is identical across tiers** (same services, hostnames, secrets); only topology (package size, `mode`, `cpuMode`, container counts) changes.

**Name → tier fuzzy matching** (`#mapEnvToGuideTier`, GitHub mode only — native keys ARE tiers). Tests `"<key> <name>"` lowercased, **in this exact order** (order is load-bearing — looser patterns overlap):

```
/ai.?agent/                                  → ai-agent
/remote|cde/                                 → remote-cde
/highly.?available|ha[-\s]?prod|\bha\b/      → highly-available-production
/prod/                                       → small-production
/stag/                                       → stage
/dev|local/                                  → local
(no match)                                   → small-production   (fallback)
```

So name your env folders to land on the right tier: `5 — HA Production` → HA; `4 — Small Production` → small-production; `0 — Development` → local. **Misname → wrong take-ownership guide renders.**

**Default landing tier** (`getDefaultEnvironment`, `recipes.model.ts:190-214`): walks `PREFERRED_ENV_KEYS = ['small-production','production','highly-available-production']` and selects the first exact key match (production-first bias); else the **middle** env (`floor((len-1)/2)`). The `?yaml=<envKey>` URL param overrides this when it matches an env's key.

---

## 9. QUALITY BAR + recipe skeleton + checklist

**Quality bar.** A recipe renders *beautifully* when: it has a `cover` and a tight `intro`; an Overview worth reading (`description` + `features`); a correct `shape`; a `takeover-guide` that adds recipe-specific value beyond the platform guide; a `knowledge-base` with real `###` sections; envs named for their tiers with one-line `intro`s; and `import.yaml` secrets typed so the wizard prompts exactly the required inputs (masked where sensitive) and nothing more.

**Copy-pasteable skeleton:**

`.zerops-recipe/README.md` (fragments are delimited by `<!-- name -->` markers; emit each one once):
```markdown
<!-- name -->
My CMS

<!-- intro -->
Self-hosted headless CMS on Zerops — content API + admin in one click.

<!-- cover -->
![cover](https://raw.githubusercontent.com/<owner>/<repo>/main/.zerops-recipe/cover.png)

<!-- shape -->
software

<!-- description -->
A longer pitch shown in Overview, after the deploy card. What it is, what you
get, why run it on Zerops.

<!-- features -->
- Headless content API
- Admin UI with role-based access
- Postgres + object storage wired in

<!-- takeover-guide -->
After first boot, open the admin at the generated subdomain and create your
first admin user. Set `APP_KEYS` and `ADMIN_JWT_SECRET` as service env secrets
(prompted at deploy). To upgrade, bump the image tag in `zerops.yaml`, redeploy,
then run the built-in migration on first request.

<!-- knowledge-base -->
### Environment variables
`DATABASE_URL`, `APP_KEYS`, `ADMIN_JWT_SECRET`, ...

### Operations
Backups run nightly via the managed database. To restore, ...

### Upgrading
Bump the upstream version in `zerops.yaml`, redeploy, run migrations.
```

`.zerops-recipe/4 — Small Production/README.md`:
```markdown
<!-- intro -->
Production-ready single-container setup with shared CPU and autoscaling to 2.
```

`.zerops-recipe/4 — Small Production/import.yaml` (minimal):
```yaml
project:
  name: my-cms
  envVariables:
    REGION: prg1
services:
  - hostname: db
    type: postgresql@16
    mode: NON_HA
  - hostname: app
    type: nodejs@20
    buildFromGit: https://github.com/<owner>/<repo>
    minContainers: 1
    maxContainers: 2
    envSecrets:
      APP_KEYS: <@generateRandomString(<64>)>
      ADMIN_JWT_SECRET: <@generateRandomString(<64>)>
      ADMIN_PASSWORD:            # bare → prompts at deploy, masked
      SMTP_API_KEY: ""           # optional → never prompted
```

**Authoring checklist:**
- [ ] `shape` chosen by **operation framing** (`software` = run-prebuilt/data-owned; `app` = fork-and-push).
- [ ] `cover` + tight `intro` (above deploy) + `description` + `features` (Overview).
- [ ] `takeover-guide` is **flat** (no top-level heading), recipe-specific.
- [ ] `knowledge-base` sections all start at **`###`** (or it renders nothing).
- [ ] Env folders named `<order> — <Name>` with **em dash**, names landing on the intended tier (§8).
- [ ] Each env has a one-line `intro` ("when to use this tier").
- [ ] `import.yaml` per env; required secrets as **bare/null** (UPPER_SNAKE), optional as `""`, generated via `<@generateRandomString>`; sensitive names contain `PASSWORD`/`SECRET`/`TOKEN`/etc. for masking.
- [ ] Repo on **`main`** branch; each `buildFromGit` target has a `zerops.yaml`.
- [ ] Preview before publishing (URL in §11).

---

## 10. KNOWN LIMITS / GAPS (be candid) + workarounds

A CMS is the case the chassis fits worst, because a CMS has **ongoing operational concerns** the recipe surface has no clean home for.

**Limit 1 — No "operations" outlet positioned AFTER the platform mechanics.** `takeover-guide` is semantically "first-run setup, shown FIRST." There is **no fragment that trails the platform steps**. Ongoing CMS ops (backup/restore, upgrade-and-migrate) must go in `knowledge-base` (reference) or be crammed into setup.
→ **Workaround:** put ongoing ops in `knowledge-base` under `### Operations` / `### Upgrading`.

**Limit 2 — You cannot augment a SPECIFIC platform step.** The platform guide is monolithic and author-opaque. For a CMS authored as `app`, the guide's "upgrade = push your code" is subtly wrong (a CMS upgrade = bump upstream + run migrations). The only lever is `takeover-guide`, which renders as a **separate leading block, spatially divorced** from the step it would correct.
→ **Workaround:** leave a clear note at the top of `takeover-guide` ("Upgrades for this recipe work differently — see Knowledge base › Upgrading") and put the real procedure in `knowledge-base`. Or choose `software` shape so the guide's upgrade step is already correct.

**Limit 3 — Per-environment authoring is ONE popover line (`intro`).** Per-tier setup nuance (media-bucket size, HA migration caveats, backup cadence) has nowhere to live — the tier guide is platform-provided and not per-recipe-augmentable.
→ **Workaround:** put cross-tier nuance in `knowledge-base`; keep `intro` to the one-line "when to use this tier."

**The trade you must consciously make (CMS):**
- **As `app`:** good code-ownership/CI-CD narrative; accept `knowledge-base` as the home for ongoing ops; the guide's "upgrade = push" is imprecise and only soft-correctable.
- **As `software`:** the platform guide carries persist → configure → domain → backups → **upgrade (bump version)** → scale (operationally honest); **loses** the code-ownership/CI-CD narrative.
- **You cannot have both.** Decide which story is more true for *this* recipe.

---

## 11. GOTCHAS

- **`@main` only.** GitHub recipe content is fetched from `/api/recipe/info?url=<repo>@main`. **Commit-SHA and branch URLs are NOT supported.** The repo must have a `main` branch with the recipe on it.
- **Per-URL backend cache + never-used-rename rule.** `recipe-info` is cached per-URL on the backend (and per-session client-side). Editing fragments on an existing repo often won't surface. The reliable cache-bust is renaming to a **brand-new, never-before-used** repo name (new URL = miss). Renaming *back* to a prior name hits **that name's stale cache** — so never rename back; always go forward to a fresh name while iterating.
- **Em-dash env folders.** `<order> — <Name>` with U+2014. The name slugifies to the key and fuzzy-maps to a tier (§8). Two folders that slug to the same key are de-duped.
- **`zerops.yaml` required per build target.** Each `buildFromGit` repo needs a `zerops.yaml` (missing → a benign `zeropsYamlSetupNotFound`, deploy-validation only).
- **Fail-loud loader.** A malformed/empty `/api/recipe/info` response throws (isn't cached), so the retry button works; a legitimately empty result (no env folders) falls back to the §8 bare-README dump.
- **Worked examples that render correctly today** (6-env, `app`-shape): **`fxck/zrno-roastery`** and **`fxck/lumen`**. Use them as the structural reference for a multi-env recipe.
- **No-auth preview URL:**
  ```
  https://app.zerops.io/recipes/detail?github=https://github.com/<owner>/<repo>&yaml=production
  ```
  (`?yaml=<envKey>` selects the landing env.)
