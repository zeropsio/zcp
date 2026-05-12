# Cross-surface audit checklist

Each section names a defect class, the surfaces involved, the check
to run, and the suggested action. Walk them in order. Findings go
into the JSON findings block defined in the preceding section of
this brief.

**`<host>` placeholder convention (READ FIRST)** — every fragmentId
template in this checklist uses `<host>` to mean the **short
codebase name** from `plan.codebases[].host` (`api`, `app`,
`worker`, etc.) — NOT the SSHFS-mount path or the deployed hostname
(`apidev` / `appdev` / `workerdev`). The fragment store keys against
the short form; `codebase/appdev/knowledge-base` is **not** a valid
fragmentId and would route nowhere. When citing
`evidence.primary` use the deliverable file path
(`/var/www/appdev/README.md:123` is fine — that's the file
location); `fragmentId` itself must be the short-form key like
`codebase/app/knowledge-base`. The main agent's `record-fragment`
fix path needs the short form to resolve the target fragment;
blocker-severity findings with the wrong-form fragmentId would
fail-or-loop the close.

The seven content surfaces:

- S1 — Root README (`root/intro` fragment)
- S2 — Tier README (`env/<N>/intro` fragments)
- S3 — Tier `import.yaml` comments (typed `plan.EnvComments[<N>]`
  store; fragment IDs `env/<N>/import-comments/project` +
  `env/<N>/import-comments/<host>` are accepted by record-fragment but
  routed to the typed store, NOT to `plan.fragments`)
- S4 — Per-codebase Integration Guide
  (`codebase/<host>/integration-guide/<N>` fragments; IG #1 is
  ENGINE-EMITTED from the codebase's zerops.yaml — cite the underlying
  yaml fragment via `codebase/<host>/zerops-yaml` (S7) when flagging
  IG #1 content)
- S5 — Per-codebase Knowledge Base (`codebase/<host>/knowledge-base`)
- S6 — Per-codebase CLAUDE.md (`codebase/<host>/claude-md`)
- S7 — Per-codebase `zerops.yaml` comments
  (`codebase/<host>/zerops-yaml`) — the WHOLE yaml is one fragment;
  IG #1 on S4 is engine-rendered FROM this fragment, not authored
  separately

**fragmentId routing**: for codebase-bound surfaces (S4-S7) the
fragment IDs above are canonical keys in `plan.fragments`. For tier
surfaces S2 and S3, the engine uses a typed store
(`plan.EnvComments[<tier-index>]`); cite the conventional fragment
ID in findings (`env/<N>/import-comments/<host>` etc.), but be aware
the main agent's fix path may use `record-fragment` or a typed-store
write depending on surface. Cite the SHIPPED file path
(`environments/<tier-folder>/import.yaml`) alongside the fragment ID
so the main agent can navigate either way. Recall the `<host>`
short-form rule from the preamble — `codebase/<host>/...` keys use
`api`/`app`/`worker`, not `apidev`/`appdev`/`workerdev`.

---

## Per-surface caps + floors (line-budget contract)

These are the hard per-surface caps. Cross-surface audit cares
about FLOORS (under-population) more than caps; the structural cap
validators in refinement-1 already gate over-cap.

| Surface | Bullet floor | Bullet cap | Defect-class on miss |
|---|---|---|---|
| S1 Root README | n/a | 35 lines | refinement-1 catches over-cap |
| S2 Tier README extract | 1 sentence | 2 sentences ≤ 350 chars | refinement-1 catches |
| S3 Tier yaml | 3 lines/svc | 8 lines/svc | refinement-1 catches |
| **S4 IG items / codebase** | **4** | **5** (incl. engine-emitted IG #1) | `kb-over-cap` if > 5 (IG floor enforced by refinement-1, not here) |
| **S5 KB bullets / codebase** | **no floor** | **8** | `kb-over-cap` if > 8 (no floor — bullets stand on their own editorial-test merit, not on count; spec §S5) |
| S6 CLAUDE.md | ~30 lines | 50 lines (soft) | refinement-1 catches |

---

## Per-surface tests (one-question editorial test)

- **S1**: *"Can a reader decide in 30 seconds whether this deploys what they need?"*
- **S2**: *"Does this 1–2 sentence card description tell a porter which tier to click?"*
- **S3**: *"Does each service block explain a decision (why this scale / mode / presence), not narrate what the field does?"*
- **S4 (IG)**: *"Would a porter bringing their own code need to copy THIS exact content into their own app?"*
- **S5 (KB)**: *"Would a developer who read the Zerops docs AND the framework docs STILL be surprised by this?"*
- **S6**: *"Is this useful for operating THIS repo — not for deploying or porting?"*
- **S7**: *"Does each comment explain a trade-off the reader couldn't infer from the field name?"*

Items that fail their surface's test go in findings with
`suggestedAction: "drop"` or `"move-to-<surface-id>"`.

---

## Defect class: kb-ig-duplication

**What**: A KB bullet in a codebase teaches the SAME trap + SAME fix
as an IG item in the SAME codebase, with no additional symptom
dimension. Both surfaces become noise.

**Check**: For each codebase {api, app, worker, …}:

1. Read `codebase/<host>/integration-guide/*` fragments.
2. Read `codebase/<host>/knowledge-base` fragment.
3. For each KB bullet ## H3 heading, scan the IG items for one whose
   body addresses the same trap. Indicators of duplication:
   - Same error string quoted (`"Authorization Violation"`, `"Blocked request. This host is not allowed."`).
   - Same code fix shown (`servers + user + pass`, `allowedHosts: true`, `dist/~`).
   - Same env var or yaml field as the central artifact.
4. If both surfaces teach the FIX, flag as duplication.

**Pass condition** for a KB bullet that overlaps with an IG item:
the KB bullet must add the SYMPTOM dimension (porter-observable
failure mode beyond the fix) that the IG didn't cover. KB-first
phrasing (`### <Symptom> — <one-line cause>`) is the signal.

**Action**: `rewrite-as-symptom` (preserve KB, rewrite to lead with
symptom + back-reference IG for the fix) OR `drop` (when IG already
covers the symptom too).

**Severity**: advisory unless > 2 duplications in the same codebase
KB, then blocker (the KB has become an IG echo).

**Run-44 G4 — per-bullet blocker promotion clause** (pure-IG-echo
detection): when a single KB bullet's stem symptom AND the
fix mechanism BOTH appear in the matching IG body (the IG's prose names
the same observable failure AND its code block ships the same fix),
promote that finding to **blocker** with `suggestedAction: "drop"`.
The KB bullet is a pure IG echo — there is no symptom dimension to
preserve, and the porter has already read the IG by the time they
hit the KB. Run-43 dogfood evidence: appdev KB #1 + #2 against
appdev IG #2 + #3 — IG #3 quoted *"Blocked request. This host is
not allowed."* AND shipped the `allowedHosts: true` fix in a code
block; KB #1 restated the same symptom + fix with no added depth.
Single-bullet promotion fires before the `> 2 duplications` whole-KB
escalation; both rules can coexist on the same KB.

---

## Defect class: kb-over-cap (+ S4 IG counts)

**Run-43 F2** — the floor side of the prior count-based class is
REMOVED. Spec §S5 now declares "no floor; cap 8" — KB bullets stand
on their own editorial-test merit, not on count. The empirical span
across the two reference recipes is 2 (jetstream) to 7 (showcase);
the prior 5-bullet floor was an invented number that the goldens
contradicted. Refinement-2 no longer flags KBs by count below the
cap. The cap side remains — only `kb-over-cap` fires here.

**Check S5 KB**: For each codebase, count `### H3` headings inside
the `codebase/<host>/knowledge-base` fragment.

- > 8 bullets → `kb-over-cap` (blocker — refinement-1 should have
  caught this; double-check).

**Check S4 IG**: For each codebase, count IG items (IG #1 is
engine-emitted from the codebase's `zerops-yaml` fragment; items
#2+ are `codebase/<host>/integration-guide/<N>` fragments).

- > 5 items → `kb-over-cap` (blocker).

**suggestedAction enum**: `"drop"` is the only applicable enum value
when no concrete fix exists at this boundary (over-cap needs
selection). Emit findings with `suggestedAction: "drop"` and
explain in `rationale` that the main agent must rank-and-cut. The
`suggestedAction` field is required by the JSON schema defined
earlier in this brief; "drop" is the conservative fallback when no
specific action applies.

---

## Defect class: surface-misplacement

**What**: An item on one surface should live on a different surface
per the seven-surface contract. Distinct from `scaffold-code-in-kb`
(below) — that's a specific sub-case where recipe-internal scaffold
prose lands in KB. Surface-misplacement is the broader class:

- A KB bullet that teaches generic framework setup (Vite `mount()`,
  Nest CLI, `php artisan serve`) — framework setup, not platform
  trap. Move to S6 (CLAUDE.md) or drop.
- An IG item that explains a recipe-internal convention the porter
  inherits already-done (e.g., a `## Use this api.ts wrapper`) —
  not Zerops-forced. Move to code comments or drop.
- A CLAUDE.md `## Zerops <topic>` section — Zerops content belongs
  on IG/KB/yaml-comments, not CLAUDE.md. Move to S4/S5/S7.

**Check**: For each item across S4-S7, apply the surface's
one-question editorial test from §"Per-surface tests" above. If the
item fails its surface's test BUT would pass on a different surface,
flag with `suggestedAction: "move-to-<correct-surface-id>"`. If it
fails everywhere, flag with `suggestedAction: "drop"`.

**Severity**: blocker (surface placement defines reader contract).

## Defect class: scaffold-code-in-kb

**What**: A KB bullet cites a recipe-authored source file
(`src/lib/bus.js`, `src/components/X.svelte`,
`src/server/api.ts`) AND describes that file's behavior in present
tense. This is scaffold-code, not a platform trap. Sub-case of
`surface-misplacement` with a concrete signature.

**Check**: For each KB bullet, scan the body for any of these
shapes naming a recipe-internal source file:

- Markdown link form: `[src/<path>]`, `[<filename>](src/<path>)`,
  `[\`src/<path>\`](src/<path>)`.
- Backtick prose form: `` `src/<path>` ``, `` `<recipe-file>.svelte` ``,
  `` `<recipe-file>.ts` ``.
- Bare path mention in prose: `"the recipe wires a small refresh
  bus in src/lib/bus.js"`, `"each card registers a refresh function
  ... see src/components/StatusStrip.svelte"`.

If the body further describes "what this codebase does"
(recipe-internal pattern: poll intervals, refresh-bus shape, event-
bus design) — flag. The KB surface is for platform traps the
porter would hit on ANY codebase, not for documenting the recipe's
own scaffold-time decisions.

**Action**: `move-to-S6` (CLAUDE.md `## Adding a feature panel`-style
section) OR `drop`.

**Severity**: blocker (defines surface placement).

---

## Defect class: aspirational-as-current

**What**: Porter prose asserts the recipe wires up X in present
tense (`"the SPA build receives only `${search_defaultSearchKey}`"`,
`"the api signs JWTs with `${APP_SECRET}`"`), but the actual zerops.yaml
+ source doesn't implement X.

**Check across all prose surfaces (S2 + S3 + S4 + S5 + S6 + S7)**:

KB/IG/CLAUDE.md prose claims (S4 + S5 + S6 + S7) — for each bullet
that names a specific named constant or env var:

1. Identify the named constant (`${search_defaultSearchKey}`,
   `MEILI_SEARCH_KEY`, `APP_SECRET` for JWT signing, etc.).
2. Read the relevant codebase's `zerops.yaml run.envVariables` (and
   `build.envVariables` for SPA codebases).
3. If the constant isn't declared, flag — the prose is aspirational
   but framed as current state.

Tier yaml prose (S3) — the tier `import.yaml`'s `project` block
preamble + per-service comments. Run-40 N-2 worked example: tier-0
import.yaml line 4 says "APP_SECRET is generated once at import and
shared across api + worker so JWT verification holds across
containers." For each tier-yaml prose mention of a framework feature
or named-constant claim:

1. Identify the feature (JWT verification, session sharing, magic-
   link auth, queue-group splitting, etc.) OR the named constant.
2. Cross-check: for feature claims, scan the relevant codebases'
   `package.json` / `composer.json` for the implementing dependency
   (`@nestjs/jwt`, `jsonwebtoken`, `passport-jwt`, etc.). For
   named-constant claims, check the codebases' yaml + source.
3. If absent, flag — tier-yaml prose ships in every deployed
   instance and porters trust it as the canonical "what this tier
   does".

**Framework-feature manifest scan** — read each codebase's manifest:
`<host>dev/package.json` (Node) OR `<host>dev/composer.json` (PHP)
OR `<host>dev/pyproject.toml` / `requirements.txt` (Python). The
brief's stitched-output pointer block enumerates per-codebase
README + zerops.yaml + CLAUDE.md; for `aspirational-as-current` you
also read the same codebase directory's manifest file.

**Action**: `reword-conditional` (rewrite as "if you expose X to the
SPA, here's the trap" rather than "the SPA receives X") OR `drop`.

**Severity**: blocker — recipe lies to porter.

---

## Defect class: yaml-comment-content-drift

**What**: A yaml comment names a Zerops cross-service alias
(`${<host>_<key>}`) that doesn't exist in the same yaml's
envVariables block AND isn't a known cross-service alias.

**Check**: For each tier `import.yaml` AND each codebase
`zerops.yaml`:

1. Scan COMMENTS for `${<host>_<key>}` tokens.
2. For each token, identify the host's **service type** from
   `plan.services[].type` in plan.json (e.g. `db` → `postgresql@18`,
   `cache` → `valkey@7.2`, `broker` → `nats@2.12`, `storage` →
   `object-storage`, `search` → `meilisearch@1.20`). Cross-service
   aliases are SERVICE-TYPE-SPECIFIC — `password` is valid for
   postgres/valkey but NOT for meilisearch (meilisearch publishes
   `masterKey` + `defaultSearchKey`, not `password`).
3. Check whether the SAME yaml file declares an env that resolves
   to the token, OR whether the token is a documented alias VALID
   FOR THE HOST'S SERVICE TYPE per the table below.
4. If the token doesn't appear in envVariables AND isn't a valid
   alias for the host's service type, flag.

**Per-service-type alias allowlist** (only these are documented
Zerops aliases for the named service type):

| Service type | Valid `${<host>_<key>}` suffixes |
|---|---|
| postgresql@* / valkey@* | hostname, port, user, password, dbName (postgres only), connectionString |
| nats@* | hostname, port, portManagement, user, password, connectionString |
| object-storage | apiUrl, apiHost, bucketName, accessKeyId, secretAccessKey |
| meilisearch@* | hostname, port, masterKey, defaultSearchKey, connectionString |
| static (build-from-git frontend slot) | zeropsSubdomain, zeropsSubdomainHost |
| any runtime service | zeropsSubdomain, zeropsSubdomainHost |

Hosts WITHOUT a matching entry in `plan.services[]` are runtime
codebases (api/app/worker); their cross-service tokens reference
peer services and follow the peer-service-type's allowlist.

**Worked example**: a tier import.yaml comment says
`"shared with both services via ${search_password}"`. Host `search`
has service type `meilisearch@1.20`. Meilisearch allowlist publishes
`masterKey` + `defaultSearchKey` — NOT `password`. The yaml content's
envVariables uses `${search_masterKey}`. Flag with
`suggestedAction: "fix-named-constant"`, `suggestedReplacement:
"${search_masterKey}"`.

**Action**: `fix-named-constant` with `suggestedReplacement` set to
the correct alias if obvious from sibling yaml content.

**Severity**: blocker.

---

## Defect class: cross-codebase-named-constant-drift

**What**: A named constant (queue group string, env var name, port
number) appears with different values in different surfaces.

**Check**: Read the `## Canonical-latest constants` block in this
brief (engine-rendered from `plan.namedConstants` + canonical-topic
facts). For each constant:

1. Scan every fragment for the constant name.
2. If any surface uses a value different from the canonical, flag.

**Worked example**: source code uses `'workers'` as a queue-group
name while tier yamls use `'showcase-workers'` in env-var defaults;
canonical (per `plan.NamedConstants` or canonical-topic facts) is
`worker-indexer`. Find any place still using a non-canonical value.

**Action**: `fix-named-constant`.

**Severity**: blocker.

---

## Defect class: ig-cites-recipe-internal-file

**What**: An IG item cites a recipe-authored file path
(`src/lib/api.js`, `src/components/StatusStrip.svelte`) as if the
porter has it. The IG is for porters bringing their own code —
referencing recipe-internal files is dead weight.

**Check**: For each IG item body:

1. Scan for `[src/<path>]` or `` `src/<path>` `` references to files
   that exist only in the recipe's scaffold.
2. Flag any. Exception: when the IG item is teaching a PATTERN and
   uses the recipe's file as a worked example, the body must
   explicitly frame it as "the recipe's example" — without that
   framing, flag.

**Action**: `rewrite-as-symptom` (rephrase to general pattern) or
`drop`.

**Severity**: advisory.

---

## Defect class: framework-quirk-as-gotcha

**What**: A KB bullet teaches a framework's own documented behavior
as if it were a Zerops trap. The content-surface contract classifies
these as `framework-quirk` (npm registry metadata, framework
bootstrap APIs, framework-specific console warnings, library-version
peer-dep errors) and routes them to **DISCARD** — they belong in
framework docs, not on any Zerops recipe surface.

**Check**: For each KB bullet, identify the bullet's underlying
mechanism. **All three tests run together; Check #1 is the decisive
gate — Checks #2 and #3 are signal collected to confirm Check #1's
verdict, not independent triggers.**

1. **The "Zerops side material" test (DECISIVE)**: does the Zerops
   platform contribute to producing the failure? If Zerops's
   container model, env-injection, SIGTERM-on-rolling-deploy timing,
   L7 routing, subdomain shape, build/run-asymmetric runtime, etc.
   materially CAUSES the trap (not just provides the runtime), it's
   `intersection` and stays. If Zerops only provides where the code
   runs (any container platform — Docker locally, Heroku, fly.io —
   would behave identically), it's framework-quirk. This is the
   primary test; an `intersection` verdict here stops the rule from
   firing regardless of the other tests.
2. **The "different scaffold" test (CONFIRMS)**: would a porter
   using the SAME framework with DIFFERENT scaffold code (different
   file layout, different deps choice, different config) hit the
   same trap with the same fix? If yes AND the fix has no Zerops
   component, the bullet is framework-quirk. If the answer is "yes
   but the trap fires because Zerops's runtime invariant forces the
   shape," it's intersection — defer to Check #1.
3. **The "documented elsewhere" test (CONFIRMS — never triggers
   alone)**: is this behavior covered in the framework's own docs,
   the library's README, or the npm/composer/pypi page? If yes AND
   the bullet adds no Zerops-specific interaction beyond restating,
   it's framework-quirk. **Note**: legitimate `intersection` bullets
   often DO appear in framework docs (nats.js's README documents the
   URL-credential double-auth issue) — the rule must NOT fire on
   those because Check #1's "Zerops side material" verdict overrides.
   This test only confirms a Check #1 framework-quirk verdict; it
   never elevates an intersection bullet to framework-quirk.

**Worked example**: a KB bullet titled *"CDN-loaded utility-CSS
shows a 'do not use in production' console warning"* describing
how the SPA loads its CSS framework from a third-party CDN
domain. Mechanism is the CDN's documented runtime behavior;
Zerops's side: zero involvement (the CDN logs to the browser
console identically on Docker locally, on Heroku, on Vercel).
"Different scaffold" test: a porter who uses a different CSS
framework hits zero of this; a porter who installs the framework
locally instead of the CDN hits zero of this. Discard.

**Counter-example (NOT framework-quirk)**: a KB bullet titled
*"Missing queue option drops zero rows but doubles every write at
minContainers ≥ 2"* — queue groups are a broker-library feature,
but the trap fires because **Zerops's `minContainers ≥ 2` brings a
second replica online during rolling deploys**. The Zerops side
materially causes the failure mode. Keep.

**Action**: `drop`.

**Severity**: **blocker** — spec classification taxonomy is
unambiguous on this routing.

---

## Defect class: self-inflicted-as-gotcha

**What**: A KB bullet documents a scaffold-time mistake the recipe
author hit and fixed before shipping — the trap fires only when the
porter deviates from IG #1's shipped configuration. The content-
surface contract classifies these as `self-inflicted` and routes
them to **DISCARD** entirely — they are not content material per
[spec-content-surfaces.md §"Fact classification taxonomy" litmus
#4](docs/spec-content-surfaces.md#fact-classification-taxonomy):
*"Could this observation be summarized as 'our code did X, we fixed
it to do Y'? If yes, discard."*

The trap class slips past `framework-quirk-as-gotcha` because there
IS a shallow Zerops-side mechanism the bullet can anchor on
(MinIO gateway redirect, per-service subdomain, cross-origin
headers). The new rule asks a sharper question: would a porter
copying IG #1's shipped envVariables verbatim hit this on a clean
deploy?

**Check**: For each KB bullet, run the porter-following-shipped-
config test.

1. **Decisive Check #1 — porter-following-IG#1-verbatim test**:
   identify the platform env var(s) the trap's mechanism cites
   (`${storage_apiHost}`, `${storage_apiUrl}`, `${db_*}`,
   `${broker_*}`, etc.). Cross-reference against IG #1's shipped
   envVariables block in the SAME codebase. If the trap fires only
   when the porter uses a different env var than what IG #1 ships
   (e.g. composes `http://${storage_apiHost}` when IG #1 ships
   `S3_ENDPOINT: ${storage_apiUrl}`), → `self-inflicted` → fire.
   If the trap fires for a porter copying IG #1's shipped block
   verbatim, → legitimate intersection → do NOT fire (defer to
   `framework-quirk-as-gotcha` and the existing classification).

   **Run-44 G3 — named-artifact patterns**. Check #1's env-var-only
   form caught the apidev KB #2 trap (`S3_ENDPOINT` deviation) but
   missed apidev KB #3 X-Cache cross-origin (the trap's deviation
   point is CORS code config, NOT a yaml env var). Extend the check
   with a SECOND step — scan for these named artifacts in IG #1's
   shipped code blocks (NOT a generic fenced-code scan; the explicit
   pattern list keeps the audit narrow). The list comes from
   `briefs/codebase-content/synthesis_workflow.md` §"DISCARD —
   `self-inflicted`":

   - **`${storage_apiHost}` / `${storage_apiUrl}` confusion** — if
     IG #1 ships `S3_ENDPOINT: ${storage_apiUrl}` AND a KB bullet
     narrates `http://${storage_apiHost}` produced a 301 redirect →
     `self-inflicted` (the porter following the shipped block never
     composes the broken URL).
   - **`exposedHeaders` / CORS custom response headers** — if IG #1
     ships an `exposedHeaders: [...]` allowlist in a code block AND a
     KB bullet narrates "browsers hide custom headers cross-origin"
     or "`fetch().headers.get('X-Cache')` returns null from the SPA"
     → `self-inflicted` (the shipped IG block encodes the fix; porter
     following it hits zero of this).

   These are the ONLY two named-artifact patterns currently anchored
   in the synthesis_workflow.md DISCARD list. Do NOT extend the list
   without an explicit spec source — a fenced-code scan against
   arbitrary IG content over-fires on legitimate intersections (the
   NATS Pattern A vs Pattern B counter-example below would trip a
   generic scanner). If you encounter a KB bullet whose trap
   matches one of these two patterns → fire. If it does NOT match
   the env-var-only test (step 1 above) AND does NOT match either
   named-artifact pattern → defer to legitimate intersection.
2. **Author-fix signal (CONFIRMS)**: bullet body narrates the
   recipe's fix sequence — "the recipe scaffolded without X, hit Y,
   added Z" — first-person scaffold-history voice. This signal alone
   does not fire the rule; legitimate intersections sometimes
   describe the fix shape too. Check #1's porter-following-shipped
   verdict overrides.
3. **Generic-platform-affordance test (CONFIRMS)**: the fix the
   bullet documents is generic platform configuration any application
   would set up the same way (CORS exposed headers, S3 endpoint
   URL, basic env-var aliasing) — there is no Zerops-specific
   teaching once the shipped IG #1 yaml encodes the fix. Confirms
   Check #1; does NOT elevate a legitimate intersection.

**Worked example**: a KB bullet titled *"`UnknownError` on first
`GetObject`"* describing how `S3_ENDPOINT` resolved to
`http://${storage_apiHost}` and produced a 301 redirect from the
MinIO gateway. IG #1's shipped envVariables block now ships
`S3_ENDPOINT: ${storage_apiUrl}` directly. A porter copying that
block verbatim never composes the broken URL — the trap only fires
if the porter un-does the shipped fix. Self-inflicted; DISCARD.
(Run-42 dogfood: apidev KB #2 shipped this bullet; spec routes to
DROP, not KB.)

**Counter-example (NOT self-inflicted)**: a KB bullet titled *"NATS
`connect()` crashes with Invalid URL on the first boot"* describing
nats.js v2's `hostPort()` parser stripping URL-embedded credentials.
IG #1 ships Pattern A (four separate `${broker_*}` aliases:
`NATS_HOST`/`NATS_PORT`/`NATS_USER`/`NATS_PASS`). The trap fires
when the porter deviates to Pattern B (connection-string assembly
like `nats://${broker_user}:${broker_password}@${broker_hostname}`)
— but Pattern B is a legitimate alternative the porter could
reasonably reach for; it's a real platform-library intersection,
not a recipe scaffold-time mistake. Keep.

**Action**: `drop`.

**Severity**: **blocker** — spec §"Fact classification taxonomy"
routes `self-inflicted` to DISCARD unambiguously.

---

## Defect class: scaffold-decision-as-gotcha

**What**: A KB bullet documents a choice the recipe made (CDN over
build pipeline, polling interval, file layout, helper module shape)
as if it were a Zerops trap. Scaffold decisions of the recipe-
internal flavor route to **DISCARD** (or move the underlying
principle, stripped of implementation, to IG). Distinct from
`scaffold-code-in-kb` — that fires on `src/<path>` file references;
this fires on prose framing ("the recipe accepts this trade", "this
recipe chose X over Y", "the SPA polls every 700ms").

**Check**: For each KB bullet, run THREE tests. **Check #1 is the
decisive gate; Checks #2 and #3 are signal that confirm Check #1's
verdict, never independent triggers.** First-person framing alone
does NOT fire the rule — legitimate intersection bullets often use
phrases like "this recipe sets X" when teaching a Zerops-forced
mechanic. The "remove the bullet" test is the load-bearing
decision; first-person framing only signals where to look.

1. **The "remove the bullet, recipe still works" test (DECISIVE)**:
   if the teaching describes a recipe-internal choice AND a porter
   could make a DIFFERENT choice without hitting any Zerops trap,
   the bullet is scaffold-decision → fire. If a porter making a
   different choice WOULD hit a Zerops trap (e.g., omitting the
   queue group at `minContainers ≥ 2` produces duplicated state),
   the bullet is intersection → do not fire, regardless of how it's
   framed.
2. **First-person scaffold framing (SIGNAL)**: "the recipe accepts
   this", "we chose this trade", "this recipe sets X for Y", "the
   recipe wires …" — the author signalling they're documenting
   their own scaffold. This signal IS NOT the trigger; legitimate
   intersection bullets use the same phrasing — e.g., a bullet that
   says *"This recipe sets `queue: 'worker'` for the items.events
   subscription so two NestJS replicas split the load"* is
   `intersection` because removing it at `minContainers ≥ 2`
   produces duplicated state per Check #1.
3. **Recipe-specific values without platform forcing (CONFIRMS)**:
   poll intervals, file paths, UI design choices, CSS frameworks —
   values the porter would replace with their own choices, that
   Zerops doesn't force. Confirms a Check #1 scaffold-decision
   verdict; does NOT elevate a Check #1 intersection.

**Worked example**: a KB bullet that includes *"...the recipe
accepts this trade as a build-pipeline simplification (no PostCSS,
no `tailwind.config.js`, no separate build step), since the goal is
showcasing the Zerops integration..."* — the bullet body literally
narrates the scaffold decision. Drop.

**Worked example**: a KB bullet titled *"Queue panel polls the api
every ~700ms"* — recipe-internal polling implementation. Not a
platform trap. The porter would replace the polling cadence with
their own choice; nothing about Zerops forces 700ms.

**Action**: `drop`. If the underlying principle is genuinely
platform-relevant (e.g., the "Nginx SPA fallback returns 200 on
/api/*" mechanic underneath an api.ts content-type check), the main
agent may move the principle (stripped of recipe specifics) to IG
— but the audit emits `drop`; the main agent makes the move
decision.

**Severity**: **blocker** — the classification taxonomy routes
recipe-internal scaffold decisions away from KB unambiguously.

---

## Defect class: cross-codebase-content-duplication

**What**: A KB bullet OR IG item teaches the same platform trap +
the same fix on more than one codebase's README, with full re-
authoring on each. The seven-surface content rule is **each fact
lives on one surface; other surfaces that need it cross-reference
— they do not re-author**. This applies within a single codebase
(don't author the same fact on IG + KB + yaml comment of one
codebase) AND across codebases (don't fully author the same fact
on two codebases' READMEs).

**Check**: For each KB bullet and each IG item, scan ACROSS all
other codebases' KB+IG fragments for:

1. **Same error string quoted** (`"Authorization Violation"`,
   `getaddrinfo ENOTFOUND ${db_hostname}`,
   `"Blocked request. This host is not allowed."`).
2. **Same code fix shown** (`servers + user + pass`, `allowedHosts:
   true`, `dist/~`, the "self-shadow" yaml example).
3. **Same env-var / yaml-field as the central artifact**.

If two surfaces (across different codebases) author the SAME
teaching with substantially the same depth + body, flag the LATER-
read one — the one the porter would land on second. Pass condition:
one codebase carries the full teaching; the other says *"See
[apidev README KB #N] — same trap on this codebase too."* That's
cross-reference, not duplication.

**Worked examples**:
- A multi-codebase recipe where the api codebase teaches the
  same-key shadow trap as a full IG item (with the yaml self-shadow
  example AND the `getaddrinfo ENOTFOUND ${db_hostname}` symptom)
  AND the worker codebase teaches the same trap as a full IG item
  (near-identical body, same symptom). Same-key shadow is a
  platform-invariant teaching that applies to any codebase using
  cross-service aliases; one codebase carries the canonical
  teaching, the other cross-references.
- Two codebases each carrying a full KB bullet titled "NATS
  Authorization Violation on boot with `${broker_connectionString}`"
  with the same nats.js double-auth explanation. Same fact, two
  surfaces, no cross-reference.

**Action**: `cross-reference-canonical-surface`. The suggested fix
is to keep the canonical (typically the codebase that uses the
trap-causing feature first or most centrally) and replace the
duplicate with a 1-2 sentence pointer.

**Severity**: **blocker** — same fact on multiple surfaces is
noise.

---

## Defect class: missing-citation

**What**: A KB bullet (S5) OR an IG item (S4) covers a topic that has
a dedicated `zerops_knowledge` guide, but the body doesn't cite the
guide by name.

**Run-44 G2** — IG citation enforcement. The prior rule walked only
KB bullets; run-43 evidence showed IG citation coverage held at 0/12
H3 items across three codebases despite the writer-brief contract
demanding Citation-Map-matching IG items to cite (`apidev` IG #2/#3
→ `http-support`; apidev IG #4 → `rolling-deploys`; appdev IG #4 →
`deploy-files`). Refinement-2 now walks both surfaces. The audit's
emitted `surface` field distinguishes them: `CODEBASE_KB` for an S5
hit, `CODEBASE_IG` for an S4 hit. The `fragmentId` follows: short-form
`codebase/<host>/knowledge-base` for KB findings,
`codebase/<host>/integration-guide/<N>` for IG findings.

**Check**: Walk BOTH surfaces — every KB bullet AND every IG H3 item
body. The check logic is identical across surfaces. The authoritative
list of {topic → required-citation} pairs is the
`## Citation map — topics requiring zerops_knowledge citation`
section rendered into this brief by the engine composer (below the
audit checklist). Walk THAT map, not a hardcoded list — the brief's
citation map is engine-versioned and will evolve.

For each KB bullet AND for each IG H3 item body:

1. Identify the topic family the item covers (rolling-deploys,
   init-commands, object-storage, env-var-model, http-support,
   deploy-files, readiness-health-checks, managed-nats,
   managed-meilisearch, etc.).
2. Match against the citation map below. If the item's topic has
   a required citation, scan the item body for ANY of the three
   acceptable forms named in the citation-map block's "Acceptable
   citation forms" paragraph: (a) canonical guide ID IN CITATION
   FRAMING — `` `the \`<guide-id>\` guide covers …` `` or
   equivalent (not a bare backtick mention used as a noun phrase),
   (b) friendly display name as markdown link text
   (`` `[friendly name](URL)` ``), or (c) the literal docs URL.
   The scan passes on any of the three.
3. If none of the three forms appears, flag. Set `surface` to
   `CODEBASE_KB` for an S5 hit, `CODEBASE_IG` for an S4 hit; set
   `fragmentId` to the matching short-form key.

**Worked counter-example (KB)**: a bullet says *"Zerops's
`init-commands` feature stamps each `key:` value into a per-deploy
ledger"* — the literal backticked token `init-commands` appears,
but the framing is descriptive prose ("the feature stamps…"), not
citation framing ("the guide covers…"). Form (a) does NOT match.
If the bullet covers the init-commands topic (it does) and contains
no form-(b) link / form-(c) URL either, flag as `missing-citation`.

**Worked counter-example (IG)**: an IG item titled *"Trust the L7
proxy"* teaches `app.set('trust proxy', true)` for Express — topic
is `http-support`. The item body explains why (the L7 balancer
terminates TLS and forwards the client IP via `X-Forwarded-For`) but
never cites the `http-support` guide, the friendly label
`Zerops L7 balancer + subdomain access`, or the canonical URL
(`docs.zerops.io/features/access`). Flag with `surface: "CODEBASE_IG"`
and `fragmentId: "codebase/<host>/integration-guide/<N>"`.

**Worked tolerance**: a bullet or IG item may legitimately reference
multiple guide topics. The citation is required ONCE per bullet/item,
not once per topic mention. If the body cites the guide for any of
its topics, the item passes.

**Keyword-over-match guard**: topic matching is topic-family, not raw
substring. A bare keyword mention does NOT trigger the citation
requirement unless the item's TOPIC is the matched family. Worked
example: `SIGTERM` appears in the rolling-deploys row keywords
(`SIGTERM-before-teardown`), but a Node-stdout-buffering bullet
that merely mentions `SIGTERM` as the trigger for log loss is NOT
about rolling deploys — its topic is generic Node process-exit +
stdout flushing. Don't fire `missing-citation` on it. The check is
"is this item's PRIMARY teaching covered by the guide?" — not
"does this item contain any keyword from the guide's row?". When
the keyword fires but the topic is foreign, pass.

**Action**: `add-citation`. Emit `suggestedReplacement` as a concrete
form-(b) markdown link the main agent can copy verbatim — the citation
map's friendly-display-name + canonical URL pair forms the canonical
shape (e.g. `[zero-downtime deploys with multi-container setups](https://docs.zerops.io/features/scaling-ha)`).
Pre-resolving the citation prose closes the main-agent compose-and-
match cycle that ran into slug-stem-leak / wrong-URL-form regressions
in prior runs (Run-44 G6).

**Severity**: advisory.

---

## Findings emission

Walk EVERY defect class in this checklist in order — the full
set is `kb-ig-duplication`, `kb-over-cap`,
`surface-misplacement`, `scaffold-code-in-kb`,
`aspirational-as-current`, `yaml-comment-content-drift`,
`cross-codebase-named-constant-drift`,
`ig-cites-recipe-internal-file`, `framework-quirk-as-gotcha`,
`self-inflicted-as-gotcha`, `scaffold-decision-as-gotcha`,
`cross-codebase-content-duplication`, `missing-citation`. For each
hit, emit ONE finding. Empty findings list = pass.

After the walk, emit the single fenced JSON block in the shape
defined at the top of this brief (the `findings` array). No prose
around it.
