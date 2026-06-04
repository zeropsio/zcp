# Recipe-route plan-shape contract: worker runtimes + dev-only narrowing of a standard recipe

**Source:** Wave-8 flow-eval (2026-06-04) — recipe-laravel-showcase-fullstack + kanban-laravel-minimal-dev-only. The recipe-matcher tokenizer fix (shipped this session) made both Laravel recipes surface correctly; these are the NEXT-layer findings the matcher fix unblocked. **Flagged for Karel** — this is a recipe-route (`internal/workflow/recipe_*.go`) BEHAVIOR change (it changes what gets provisioned), and one symptom is a paid service created against an explicit user instruction. Per CLAUDE.md the bootstrap recipe-route is core (no Aleš coordination), but the corpus FORMAT (worker setups) is shared, so FYI-Aleš + verify recipe unit/sim/flow-eval green.

## The shared root cause

`internal/workflow/recipe_shape.go::InferRecipeShape` models only 0/1/2-runtime recipe shapes (dev / prod / simple) and treats the recipe's baked shape as IMMUTABLE. `recipe_override.go::recipeRuntimeRole` maps any non-dev setup (incl. `worker`) to `recipeRoleStage`; `buildRuntimeSlots` emits only dev/stage slots. Two failures fall out:

### Finding 1a — no plan slot for a 3rd (worker) runtime (HIGH, showcase)

The laravel-showcase recipe has 3 runtime setups: `dev` + `prod` + `worker` (queue worker, php-nginx@8.4, `zeropsSetup:worker`, no HTTP). The agent's first discover-complete plan declared the worker as a second runtime target → `INVALID_PARAMETER: no recipe service matches plan target type "php-nginx@8.4" (role=dev)`. The agent recovered by dropping the worker from the plan (trial-and-error) → the worker imported from the recipe YAML but is NOT registered in session ServiceMeta (`adoptionState=adoptable`, untracked). Downstream: the untracked worker has no `recordedServesHTTP`, which is why verify mis-classifies it (Finding 2).

**Fix sketch:** teach `InferRecipeShape` + `buildRuntimeSlots`/`recipeRuntimeRole` about `zeropsSetup:worker` as a non-paired slot kind, so the worker is declarable in the plan and registered into ServiceMeta with `servesHTTP=false`. At minimum, the discover-step guide for a recipe whose import YAML carries a 3rd runtime must state explicitly "worker/extra runtimes provision from the recipe YAML — do NOT add them as plan targets," so the agent doesn't discover the drop-from-plan workaround by a rejected submission.

### Finding 1b — a standard recipe can't be narrowed to dev-only even on explicit user request (HIGH, kanban)

User: "kanban app, **dev service only (no stage, no production)**". laravel-minimal is a `standard` recipe (dev+stage pair). `ValidateBootstrapRecipeMode` hard-rejected `bootstrapMode=dev`, forcing the agent to `bootstrapMode=standard` → it provisioned `appstage` (a **paid** service the user explicitly did NOT want), then framed it in the summary as "standard mode" as if that were the user's choice. **This is the more serious half** — ZCP provisioned billable infra against an explicit instruction.

**Fix sketch:** make `ValidateBootstrapRecipeMode` accept `bootstrapMode=dev` as a valid NARROWING of a standard recipe — provision the dev setup + managed deps, skip the prod/stage (and worker) setups. Verify the dev container still builds from the recipe `buildFromGit` correctly without its stage sibling. Decide the close-mode/cross-deploy implications (dev-only has no stage to promote to).

## Finding 2 — verify mis-classifies a portless php-nginx worker as implicit-HTTP (downstream of 1a)

`ops/verify_checks.go::classifyRuntime` hard-returns `RuntimeImplicit` for php-nginx/php-apache BEFORE the `!hasPorts→RuntimeWorker` branch. For the showcase worker (no HTTP) this yields `http_root=fail "subdomain access not enabled"` + a WRONG recovery (`zerops_subdomain enable` on a queue worker), dragging an otherwise-healthy 8-service stack to `degraded` (7/8). The agent correctly ignored the bad recovery, but it's a footgun.

**NOT a clean standalone fix:** `hasPorts = len(svc.Ports) > 0`, and a normal php-nginx WEB app reports port 80 in `svc.Ports` (landing-page run showed `ports:[{port:80}]`), so a naive `!hasPorts→worker` for php-nginx would misclassify web apps. The php-nginx→implicit hard-return is deliberate (pinned by `verify_classify_bc_test.go`). The reliable discriminator is `recordedServesHTTP` from ServiceMeta — which is nil ONLY because Finding 1a left the worker unregistered. **So fixing 1a (register the worker with servesHTTP=false) fixes this automatically** via the existing `classifyRuntime` line-78 `recordedServesHTTP==false→RuntimeWorker` path. Do NOT change classifyRuntime standalone. (If a belt-and-suspenders is wanted later, thread `zeropsSetup` into classifyRuntime as the discriminator — not hasPorts.)

## Finding 3 — zerops_knowledge recipe= honors a hand-typed slug, not the session's committed recipeSlug (MEDIUM, handler)

In the showcase session (committed `recipeSlug=laravel-showcase`), the provision guide says `Check gotchas via zerops_knowledge recipe="<slug>"`; the agent called `recipe="laravel-minimal"` and got the MINIMAL gotchas — missing the showcase-only traps (predis-over-phpredis, `AWS_USE_PATH_STYLE_ENDPOINT` for MinIO) the scenario probes. It presented minimal-recipe guidance as authoritative.

**Fix sketch:** `tools/knowledge.go` recipe-mode — when an active bootstrap session has a committed `recipeSlug`, default `recipe=` to it, or warn loudly when the requested slug differs. Better: the provision-step guide should interpolate the actual committed slug into the example instead of a literal `<slug>` placeholder so the agent copies the right value. (Single-owner: the session already knows the recipe.)

## Lower-confidence Wave-8 items

- **close-mode=git-push chained pointer** (medium, handler, workflow_close_mode.go:226): the `nextSteps` pointer pre-fills the confirm shape (remoteUrl+gitToken), routing the agent AROUND the git-push-setup walkthrough that carries the CICD integration picker + PAT-scope guidance. Change the default tell to recommend the walkthrough-first (no remoteUrl) call. NOTE: the git-push-setup-with-cicd-method-prompt scenario was WALL-BLOCKED by a fake-PAT fixture (`github_pat_11A...` placeholder) — harness needs a real reachable repo + valid PAT, OR a reframe, before §2.5 (the co-emitted cicd-method prompt the scenario expects, which may itself be unimplemented — scenario-vs-impl drift) can run end-to-end. Eval-author.
- **Recipe gotchas never relayed to the user** (low, guide): the agent retrieves recipe gotchas as internal trivia but never surfaces them as pre-emptive warnings, though personas expect them. Guidance nudge: "relay framework gotchas to the user before they bite."
- **Fit=exact framework-blind / over-provisions on a REQUIRED dep** (low): kanban labeled laravel-minimal's REQUIRED postgres as an `over-provisions` extra. Same owner as the Fit-framework item already in `plans/backlog/develop-worksession-pid-claim-and-fit-framework.md` (Finding 2 there) — consolidate.
- **Out-of-scope service in descriptive prose** (low, atom, reg:possible): kanban — scope-exclusion correctly governs the gate math (shipped this session), but an env-var EXAMPLE in prose still named the out-of-scope appstage. Whoever fixes this must confirm it's additive to the workSessionScopeSet work, not a conflict.
- **Unconditional "confirm with the user" guidance** (low, guide): trains false "User confirmed" attestations when intent is unambiguous (landing-page). Scope the imperative to genuine ambiguity. (Same as a Wave-6 low item — consolidate.)
- **discover echoes pre-deploy httpSupport:false DTO as capability** (low, ops): landing-page — pre-deploy discover for an empty nginx returns `ports:[{httpSupport:false,port:80}]`; don't present pre-deploy port DTO as settled HTTP capability.
