# ZCP Lifecycle Matrix Simulation

Generated: 2026-04-29T23:06:13Z
Corpus: 80 atoms
Scenarios: 45

---

# 1. Idle entry points

## 1.1 idle/empty (fresh user, no project state)

_Brand-new project — should route the agent into bootstrap._

**Phase**: `idle` &middot; **Env**: `container` &middot; **IdleScenario**: `empty`

**Plan.Primary**: `zerops_workflow` → Create services

**Atoms** (2 unique, 2 render-instances, 2018 bytes total):
- `bootstrap-route-options`
- `idle-bootstrap-entry`

## 1.2 idle/adopt (only unmanaged runtimes)

_Project has runtime services but no ServiceMeta files — adoption path._

**Phase**: `idle` &middot; **Env**: `container` &middot; **IdleScenario**: `adopt`

**Plan.Primary**: `zerops_workflow` → Adopt unmanaged runtimes

**Atoms** (2 unique, 2 render-instances, 2281 bytes total):
- `bootstrap-route-options`
- `idle-adopt-entry`

## 1.3 idle/bootstrapped (managed services exist)

_User finished bootstrap, returning later to start a develop task._

**Phase**: `idle` &middot; **Env**: `container` &middot; **IdleScenario**: `bootstrapped`

**Plan.Primary**: `zerops_workflow` → Start a develop task
**Alternatives**: `Add more services`

**Atoms** (2 unique, 2 render-instances, 1997 bytes total):
- `bootstrap-route-options`
- `idle-develop-entry`

## 1.4 idle/incomplete (partial bootstrap meta exists)

_Prior bootstrap session crashed mid-way; resume should be offered._

**Phase**: `idle` &middot; **Env**: `container` &middot; **IdleScenario**: `incomplete`

**Plan.Primary**: `zerops_workflow` → Adopt unmanaged runtimes

**Atoms** (2 unique, 2 render-instances, 2670 bytes total):
- `bootstrap-resume`
- `bootstrap-route-options`

## 1.5 idle/empty LOCAL env

_Local-machine ZCP without any project — bootstrap entry should adapt._

**Phase**: `idle` &middot; **Env**: `local` &middot; **IdleScenario**: `empty`

**Plan.Primary**: `zerops_workflow` → Create services

**Atoms** (2 unique, 2 render-instances, 2018 bytes total):
- `bootstrap-route-options`
- `idle-bootstrap-entry`

---

# 2. Bootstrap — classic route

## 2.1 classic/discover dynamic standard pair (container)

_Free-form plan: dynamic runtime in standard mode + dev/stage hostnames._

**Phase**: `bootstrap-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Continue bootstrap

**Atoms** (6 unique, 6 render-instances, 5148 bytes total):
- `bootstrap-classic-plan-dynamic`
- `bootstrap-intro`
- `bootstrap-mode-prompt`
- `bootstrap-runtime-classes`
- `develop-api-error-meta`
- `develop-platform-rules-common`

## 2.2 classic/discover static SPA (container)

_Static-runtime path (Vite SPA, etc.) — different deploy/build vocabulary._

**Phase**: `bootstrap-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Continue bootstrap

**Atoms** (6 unique, 6 render-instances, 5308 bytes total):
- `bootstrap-classic-plan-static`
- `bootstrap-intro`
- `bootstrap-mode-prompt`
- `bootstrap-runtime-classes`
- `develop-api-error-meta`
- `develop-platform-rules-common`

## 2.3 classic/discover implicit-webserver (PHP simple)

_PHP implicit-webserver: no `start:` block, real start path._

**Phase**: `bootstrap-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Continue bootstrap

**Atoms** (5 unique, 5 render-instances, 4796 bytes total):
- `bootstrap-intro`
- `bootstrap-mode-prompt`
- `bootstrap-runtime-classes`
- `develop-api-error-meta`
- `develop-platform-rules-common`

## 2.4 classic/provision (container, dev mode)

_Provision step — agent should see import.yaml + auto-mount guidance._

**Phase**: `bootstrap-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Continue bootstrap

**Atoms** (6 unique, 6 render-instances, 6839 bytes total):
- `bootstrap-env-var-discovery`
- `bootstrap-intro`
- `bootstrap-provision-rules`
- `bootstrap-wait-active`
- `develop-api-error-meta`
- `develop-platform-rules-common`

## 2.5 classic/close (container, simple mode)

_Close step — finalize ServiceMeta, no first deploy._

**Phase**: `bootstrap-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Continue bootstrap

**Atoms** (5 unique, 5 render-instances, 5179 bytes total):
- `bootstrap-close`
- `bootstrap-intro`
- `bootstrap-verify`
- `develop-api-error-meta`
- `develop-platform-rules-common`

## 2.6 classic/discover (LOCAL env)

_Local-mode bootstrap discover — should suppress mount/SSH guidance._

**Phase**: `bootstrap-active` &middot; **Env**: `local`

**Plan.Primary**: `zerops_workflow` → Continue bootstrap

**Atoms** (7 unique, 7 render-instances, 6041 bytes total):
- `bootstrap-classic-plan-dynamic`
- `bootstrap-discover-local`
- `bootstrap-intro`
- `bootstrap-mode-prompt`
- `bootstrap-runtime-classes`
- `develop-api-error-meta`
- `develop-platform-rules-common`

## 2.7 classic/provision (LOCAL env)

_Local provision — no auto-mount path._

**Phase**: `bootstrap-active` &middot; **Env**: `local`

**Plan.Primary**: `zerops_workflow` → Continue bootstrap

**Atoms** (7 unique, 7 render-instances, 7856 bytes total):
- `bootstrap-env-var-discovery`
- `bootstrap-intro`
- `bootstrap-provision-local`
- `bootstrap-provision-rules`
- `bootstrap-wait-active`
- `develop-api-error-meta`
- `develop-platform-rules-common`

---

# 3. Bootstrap — recipe route

## 3.1 recipe/discover (container, hello-world slug)

_Recipe discover: agent picks slug `nodejs-hello-world`._

**Phase**: `bootstrap-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Continue bootstrap

**Atoms** (4 unique, 4 render-instances, 4015 bytes total):
- `bootstrap-intro`
- `bootstrap-recipe-match`
- `develop-api-error-meta`
- `develop-platform-rules-common`

## 3.2 recipe/provision (container, multi-service Laravel)

_Laravel-minimal recipe: php-apache + db._

**Phase**: `bootstrap-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Continue bootstrap

**Atoms** (4 unique, 4 render-instances, 4083 bytes total):
- `bootstrap-intro`
- `bootstrap-recipe-import`
- `develop-api-error-meta`
- `develop-platform-rules-common`

## 3.3 recipe/close (container)

_Recipe close — finalize meta, hand off to develop._

**Phase**: `bootstrap-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Continue bootstrap

**Atoms** (6 unique, 6 render-instances, 5945 bytes total):
- `bootstrap-close`
- `bootstrap-intro`
- `bootstrap-recipe-close`
- `bootstrap-verify`
- `develop-api-error-meta`
- `develop-platform-rules-common`

---

# 4. Bootstrap — adopt route

## 4.1 adopt/discover (container, single dev runtime)

_Single existing runtime to adopt as dev mode._

**Phase**: `bootstrap-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Continue bootstrap

**Atoms** (5 unique, 5 render-instances, 4408 bytes total):
- `bootstrap-adopt-discover`
- `bootstrap-intro`
- `bootstrap-mode-prompt`
- `develop-api-error-meta`
- `develop-platform-rules-common`

## 4.2 adopt/discover (container, dev+stage pair)

_Two existing runtimes with dev/stage suffix → adopt as standard._

**Phase**: `bootstrap-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Continue bootstrap

**Atoms** (5 unique, 5 render-instances, 4408 bytes total):
- `bootstrap-adopt-discover`
- `bootstrap-intro`
- `bootstrap-mode-prompt`
- `develop-api-error-meta`
- `develop-platform-rules-common`

## 4.3 adopt/provision (pure-adoption fast path)

_Plan all-existing — close should be skippable._

**Phase**: `bootstrap-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Continue bootstrap

**Atoms** (5 unique, 5 render-instances, 6379 bytes total):
- `bootstrap-env-var-discovery`
- `bootstrap-intro`
- `bootstrap-provision-rules`
- `develop-api-error-meta`
- `develop-platform-rules-common`

---

# 5. Develop — first-deploy branch

## 5.1 develop never-deployed dev/dynamic (container)

_Just bootstrapped, dev mode dynamic runtime, first develop iteration._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_deploy` → Deploy appdev

**Atoms** (19 unique, 19 render-instances, 20492 bytes total):
- `develop-api-error-meta`
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`
- `develop-checklist-dev-mode`
- `develop-deploy-files-self-deploy`
- `develop-deploy-modes`
- `develop-dynamic-runtime-start-container`
- `develop-env-var-channels`
- `develop-first-deploy-env-vars`
- `develop-first-deploy-execute`
- `develop-first-deploy-intro`
- `develop-first-deploy-scaffold-yaml`
- `develop-first-deploy-verify`
- `develop-first-deploy-write-app`
- `develop-http-diagnostic`
- `develop-knowledge-pointers`
- `develop-platform-rules-common`
- `develop-platform-rules-container`
- `develop-verify-matrix`

## 5.2 develop never-deployed simple/dynamic (container)

_Simple-mode single service, healthCheck-driven start._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_deploy` → Deploy app

**Atoms** (18 unique, 18 render-instances, 19092 bytes total):
- `develop-api-error-meta`
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`
- `develop-checklist-simple-mode`
- `develop-deploy-files-self-deploy`
- `develop-deploy-modes`
- `develop-env-var-channels`
- `develop-first-deploy-env-vars`
- `develop-first-deploy-execute`
- `develop-first-deploy-intro`
- `develop-first-deploy-scaffold-yaml`
- `develop-first-deploy-verify`
- `develop-first-deploy-write-app`
- `develop-http-diagnostic`
- `develop-knowledge-pointers`
- `develop-platform-rules-common`
- `develop-platform-rules-container`
- `develop-verify-matrix`

## 5.3 develop never-deployed standard dev half (container)

_Standard-mode dev half, stage entry not yet written._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_deploy` → Deploy appdev

**Atoms** (19 unique, 19 render-instances, 20466 bytes total):
- `develop-api-error-meta`
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`
- `develop-deploy-files-self-deploy`
- `develop-deploy-modes`
- `develop-dynamic-runtime-start-container`
- `develop-env-var-channels`
- `develop-first-deploy-env-vars`
- `develop-first-deploy-execute`
- `develop-first-deploy-intro`
- `develop-first-deploy-promote-stage`
- `develop-first-deploy-scaffold-yaml`
- `develop-first-deploy-verify`
- `develop-first-deploy-write-app`
- `develop-http-diagnostic`
- `develop-knowledge-pointers`
- `develop-platform-rules-common`
- `develop-platform-rules-container`
- `develop-verify-matrix`

## 5.4 develop never-deployed PHP simple (implicit-webserver)

_PHP simple — no `start:`; healthCheck on `/`._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_deploy` → Deploy site

**Atoms** (20 unique, 20 render-instances, 21579 bytes total):
- `develop-api-error-meta`
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`
- `develop-checklist-simple-mode`
- `develop-deploy-files-self-deploy`
- `develop-deploy-modes`
- `develop-env-var-channels`
- `develop-first-deploy-asset-pipeline-container`
- `develop-first-deploy-env-vars`
- `develop-first-deploy-execute`
- `develop-first-deploy-intro`
- `develop-first-deploy-scaffold-yaml`
- `develop-first-deploy-verify`
- `develop-first-deploy-write-app`
- `develop-http-diagnostic`
- `develop-implicit-webserver`
- `develop-knowledge-pointers`
- `develop-platform-rules-common`
- `develop-platform-rules-container`
- `develop-verify-matrix`

## 5.5 develop never-deployed static SPA

_Static runtime — buildCommands generate dist; deployFiles selects ./dist._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_deploy` → Deploy spa

**Atoms** (19 unique, 19 render-instances, 20042 bytes total):
- `develop-api-error-meta`
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`
- `develop-checklist-simple-mode`
- `develop-deploy-files-self-deploy`
- `develop-deploy-modes`
- `develop-env-var-channels`
- `develop-first-deploy-env-vars`
- `develop-first-deploy-execute`
- `develop-first-deploy-intro`
- `develop-first-deploy-scaffold-yaml`
- `develop-first-deploy-verify`
- `develop-first-deploy-write-app`
- `develop-http-diagnostic`
- `develop-knowledge-pointers`
- `develop-platform-rules-common`
- `develop-platform-rules-container`
- `develop-static-workflow`
- `develop-verify-matrix`

## 5.6 develop never-deployed dev/dynamic (LOCAL env)

_Local-machine first deploy — local workflow atom path._

**Phase**: `develop-active` &middot; **Env**: `local`

**Plan.Primary**: `zerops_deploy` → Deploy app

**Atoms** (16 unique, 16 render-instances, 17028 bytes total):
- `develop-api-error-meta`
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`
- `develop-deploy-files-self-deploy`
- `develop-deploy-modes`
- `develop-env-var-channels`
- `develop-first-deploy-env-vars`
- `develop-first-deploy-intro`
- `develop-first-deploy-scaffold-yaml`
- `develop-first-deploy-verify`
- `develop-http-diagnostic`
- `develop-knowledge-pointers`
- `develop-local-workflow`
- `develop-platform-rules-common`
- `develop-platform-rules-local`
- `develop-verify-matrix`

---

# 6. Develop — iteration after first deploy

## 6.1 develop deployed unset close-mode (post-first-deploy review)

_First deploy succeeded; close-mode still unset → review prompt should fire._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Close develop session

**Atoms** (18 unique, 18 render-instances, 19198 bytes total):
- `develop-api-error-meta`
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`
- `develop-checklist-dev-mode`
- `develop-deploy-files-self-deploy`
- `develop-deploy-modes`
- `develop-dev-server-reason-codes`
- `develop-dev-server-triage`
- `develop-dynamic-runtime-start-container`
- `develop-env-var-channels`
- `develop-http-diagnostic`
- `develop-intro`
- `develop-knowledge-pointers`
- `develop-mode-expansion`
- `develop-platform-rules-common`
- `develop-platform-rules-container`
- `develop-strategy-review`
- `develop-verify-matrix`

## 6.2 develop deployed CloseMode=auto (steady-state iteration)

_Iteration after picking auto close-mode — strategy-review should NOT fire._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Close develop session

**Atoms** (22 unique, 22 render-instances, 24061 bytes total):
- `develop-api-error-meta`
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`
- `develop-checklist-dev-mode`
- `develop-close-mode-auto`
- `develop-close-mode-auto-deploy-container`
- `develop-close-mode-auto-dev`
- `develop-close-mode-auto-workflow-dev`
- `develop-deploy-files-self-deploy`
- `develop-deploy-modes`
- `develop-dev-server-reason-codes`
- `develop-dev-server-triage`
- `develop-dynamic-runtime-start-container`
- `develop-env-var-channels`
- `develop-http-diagnostic`
- `develop-intro`
- `develop-knowledge-pointers`
- `develop-mode-expansion`
- `develop-platform-rules-common`
- `develop-platform-rules-container`
- `develop-strategy-awareness`
- `develop-verify-matrix`

---

# 7. Develop — close-mode variants

## 7.1 close-mode=auto + dev mode (container)

_Default close path — auto = run zerops_deploy at close._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Close develop session

**Atoms** (22 unique, 22 render-instances, 24061 bytes total):
- `develop-api-error-meta`
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`
- `develop-checklist-dev-mode`
- `develop-close-mode-auto`
- `develop-close-mode-auto-deploy-container`
- `develop-close-mode-auto-dev`
- `develop-close-mode-auto-workflow-dev`
- `develop-deploy-files-self-deploy`
- `develop-deploy-modes`
- `develop-dev-server-reason-codes`
- `develop-dev-server-triage`
- `develop-dynamic-runtime-start-container`
- `develop-env-var-channels`
- `develop-http-diagnostic`
- `develop-intro`
- `develop-knowledge-pointers`
- `develop-mode-expansion`
- `develop-platform-rules-common`
- `develop-platform-rules-container`
- `develop-strategy-awareness`
- `develop-verify-matrix`

## 7.2 close-mode=git-push + GitPushState=configured + webhook

_Full git-push setup with webhook integration._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Close develop session

**Atoms** (17 unique, 17 render-instances, 19638 bytes total):
- `develop-api-error-meta`
- `develop-auto-close-semantics`
- `develop-build-observe`
- `develop-change-drives-deploy`
- `develop-close-mode-git-push`
- `develop-deploy-files-self-deploy`
- `develop-deploy-modes`
- `develop-dynamic-runtime-start-container`
- `develop-env-var-channels`
- `develop-http-diagnostic`
- `develop-intro`
- `develop-knowledge-pointers`
- `develop-platform-rules-common`
- `develop-platform-rules-container`
- `develop-record-external-deploy`
- `develop-strategy-awareness`
- `develop-verify-matrix`

## 7.3 close-mode=manual (yield to user)

_Manual close — ZCP records evidence but user owns deploys._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Close develop session

**Atoms** (19 unique, 19 render-instances, 21278 bytes total):
- `develop-api-error-meta`
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`
- `develop-checklist-dev-mode`
- `develop-close-mode-manual`
- `develop-deploy-files-self-deploy`
- `develop-deploy-modes`
- `develop-dev-server-reason-codes`
- `develop-dev-server-triage`
- `develop-dynamic-runtime-start-container`
- `develop-env-var-channels`
- `develop-http-diagnostic`
- `develop-intro`
- `develop-knowledge-pointers`
- `develop-mode-expansion`
- `develop-platform-rules-common`
- `develop-platform-rules-container`
- `develop-strategy-awareness`
- `develop-verify-matrix`

## 7.4 close-mode=git-push BUT FirstDeployedAt empty (D2a edge)

_Agent set close-mode before first deploy — atoms must explain D2a (default self-deploy still applies)._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_deploy` → Deploy appdev

**Atoms** (20 unique, 20 render-instances, 21891 bytes total):
- `develop-api-error-meta`
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`
- `develop-deploy-files-self-deploy`
- `develop-deploy-modes`
- `develop-dynamic-runtime-start-container`
- `develop-env-var-channels`
- `develop-first-deploy-env-vars`
- `develop-first-deploy-execute`
- `develop-first-deploy-intro`
- `develop-first-deploy-promote-stage`
- `develop-first-deploy-scaffold-yaml`
- `develop-first-deploy-verify`
- `develop-first-deploy-write-app`
- `develop-http-diagnostic`
- `develop-knowledge-pointers`
- `develop-platform-rules-common`
- `develop-platform-rules-container`
- `develop-strategy-awareness`
- `develop-verify-matrix`

---

# 8. Develop — git-push capability matrix

## 8.1 auto / unconfigured / none

_Default — git push capability not provisioned._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Close develop session

**Atoms** (17 unique, 17 render-instances, 17991 bytes total):
- `develop-api-error-meta`
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`
- `develop-close-mode-auto`
- `develop-close-mode-auto-deploy-container`
- `develop-close-mode-auto-standard`
- `develop-deploy-files-self-deploy`
- `develop-deploy-modes`
- `develop-dynamic-runtime-start-container`
- `develop-env-var-channels`
- `develop-http-diagnostic`
- `develop-intro`
- `develop-knowledge-pointers`
- `develop-platform-rules-common`
- `develop-platform-rules-container`
- `develop-strategy-awareness`
- `develop-verify-matrix`

## 8.2 auto / configured / none

_Capability provisioned; close still does zcli (auto)._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Close develop session

**Atoms** (17 unique, 17 render-instances, 17991 bytes total):
- `develop-api-error-meta`
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`
- `develop-close-mode-auto`
- `develop-close-mode-auto-deploy-container`
- `develop-close-mode-auto-standard`
- `develop-deploy-files-self-deploy`
- `develop-deploy-modes`
- `develop-dynamic-runtime-start-container`
- `develop-env-var-channels`
- `develop-http-diagnostic`
- `develop-intro`
- `develop-knowledge-pointers`
- `develop-platform-rules-common`
- `develop-platform-rules-container`
- `develop-strategy-awareness`
- `develop-verify-matrix`

## 8.3 git-push / unconfigured / none

_Mode flipped to git-push but capability missing — must chain to setup._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Close develop session

**Atoms** (15 unique, 15 render-instances, 16762 bytes total):
- `develop-api-error-meta`
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`
- `develop-close-mode-git-push`
- `develop-deploy-files-self-deploy`
- `develop-deploy-modes`
- `develop-dynamic-runtime-start-container`
- `develop-env-var-channels`
- `develop-http-diagnostic`
- `develop-intro`
- `develop-knowledge-pointers`
- `develop-platform-rules-common`
- `develop-platform-rules-container`
- `develop-strategy-awareness`
- `develop-verify-matrix`

## 8.4 git-push / configured / webhook

_Full webhook CI._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Close develop session

**Atoms** (17 unique, 17 render-instances, 19638 bytes total):
- `develop-api-error-meta`
- `develop-auto-close-semantics`
- `develop-build-observe`
- `develop-change-drives-deploy`
- `develop-close-mode-git-push`
- `develop-deploy-files-self-deploy`
- `develop-deploy-modes`
- `develop-dynamic-runtime-start-container`
- `develop-env-var-channels`
- `develop-http-diagnostic`
- `develop-intro`
- `develop-knowledge-pointers`
- `develop-platform-rules-common`
- `develop-platform-rules-container`
- `develop-record-external-deploy`
- `develop-strategy-awareness`
- `develop-verify-matrix`

## 8.5 git-push / configured / actions

_GitHub Actions CI._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Close develop session

**Atoms** (17 unique, 17 render-instances, 19638 bytes total):
- `develop-api-error-meta`
- `develop-auto-close-semantics`
- `develop-build-observe`
- `develop-change-drives-deploy`
- `develop-close-mode-git-push`
- `develop-deploy-files-self-deploy`
- `develop-deploy-modes`
- `develop-dynamic-runtime-start-container`
- `develop-env-var-channels`
- `develop-http-diagnostic`
- `develop-intro`
- `develop-knowledge-pointers`
- `develop-platform-rules-common`
- `develop-platform-rules-container`
- `develop-record-external-deploy`
- `develop-strategy-awareness`
- `develop-verify-matrix`

## 8.6 git-push / broken / webhook

_Push capability previously broken; recovery atom expected._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Close develop session

**Atoms** (17 unique, 17 render-instances, 19638 bytes total):
- `develop-api-error-meta`
- `develop-auto-close-semantics`
- `develop-build-observe`
- `develop-change-drives-deploy`
- `develop-close-mode-git-push`
- `develop-deploy-files-self-deploy`
- `develop-deploy-modes`
- `develop-dynamic-runtime-start-container`
- `develop-env-var-channels`
- `develop-http-diagnostic`
- `develop-intro`
- `develop-knowledge-pointers`
- `develop-platform-rules-common`
- `develop-platform-rules-container`
- `develop-record-external-deploy`
- `develop-strategy-awareness`
- `develop-verify-matrix`

---

# 9. Develop — failure tiers

## 9.1 iteration tier 1 (1 failed)

_First failure — DIAGNOSE tier._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_deploy` → Deploy appdev

**Atoms** (20 unique, 20 render-instances, 21917 bytes total):
- `develop-api-error-meta`
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`
- `develop-checklist-dev-mode`
- `develop-deploy-files-self-deploy`
- `develop-deploy-modes`
- `develop-dynamic-runtime-start-container`
- `develop-env-var-channels`
- `develop-first-deploy-env-vars`
- `develop-first-deploy-execute`
- `develop-first-deploy-intro`
- `develop-first-deploy-scaffold-yaml`
- `develop-first-deploy-verify`
- `develop-first-deploy-write-app`
- `develop-http-diagnostic`
- `develop-knowledge-pointers`
- `develop-platform-rules-common`
- `develop-platform-rules-container`
- `develop-strategy-awareness`
- `develop-verify-matrix`

## 9.2 iteration tier 3 (3 failed)

_After 3 failures — SYSTEMATIC tier kicks in._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_deploy` → Deploy appdev

**Atoms** (20 unique, 20 render-instances, 21917 bytes total):
- `develop-api-error-meta`
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`
- `develop-checklist-dev-mode`
- `develop-deploy-files-self-deploy`
- `develop-deploy-modes`
- `develop-dynamic-runtime-start-container`
- `develop-env-var-channels`
- `develop-first-deploy-env-vars`
- `develop-first-deploy-execute`
- `develop-first-deploy-intro`
- `develop-first-deploy-scaffold-yaml`
- `develop-first-deploy-verify`
- `develop-first-deploy-write-app`
- `develop-http-diagnostic`
- `develop-knowledge-pointers`
- `develop-platform-rules-common`
- `develop-platform-rules-container`
- `develop-strategy-awareness`
- `develop-verify-matrix`

## 9.3 iteration tier 5 (5 failed, STOP)

_Hit iteration cap — STOP tier should surface._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_deploy` → Deploy appdev

**Atoms** (20 unique, 20 render-instances, 21917 bytes total):
- `develop-api-error-meta`
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`
- `develop-checklist-dev-mode`
- `develop-deploy-files-self-deploy`
- `develop-deploy-modes`
- `develop-dynamic-runtime-start-container`
- `develop-env-var-channels`
- `develop-first-deploy-env-vars`
- `develop-first-deploy-execute`
- `develop-first-deploy-intro`
- `develop-first-deploy-scaffold-yaml`
- `develop-first-deploy-verify`
- `develop-first-deploy-write-app`
- `develop-http-diagnostic`
- `develop-knowledge-pointers`
- `develop-platform-rules-common`
- `develop-platform-rules-container`
- `develop-strategy-awareness`
- `develop-verify-matrix`

---

# 10. Develop — multi-service orchestration

## 10.1 standard mode dev+stage halves both never-deployed

_Standard pair — atoms should fire per-half with correct hostnames._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_deploy` → Deploy appdev

**Atoms** (19 unique, 19 render-instances, 20507 bytes total):
- `develop-api-error-meta`
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`
- `develop-deploy-files-self-deploy`
- `develop-deploy-modes`
- `develop-dynamic-runtime-start-container`
- `develop-env-var-channels`
- `develop-first-deploy-env-vars`
- `develop-first-deploy-execute`
- `develop-first-deploy-intro`
- `develop-first-deploy-promote-stage`
- `develop-first-deploy-scaffold-yaml`
- `develop-first-deploy-verify`
- `develop-first-deploy-write-app`
- `develop-http-diagnostic`
- `develop-knowledge-pointers`
- `develop-platform-rules-common`
- `develop-platform-rules-container`
- `develop-verify-matrix`

## 10.2 mixed runtimes (api + web + db)

_Two runtimes + managed dep — per-service rendering correctness._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Close develop session

**Atoms** (22 unique, 30 render-instances, 34171 bytes total):
- `develop-api-error-meta`
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`
- `develop-checklist-dev-mode`
- `develop-close-mode-auto`
- `develop-close-mode-auto-deploy-container`
- `develop-close-mode-auto-dev`
- `develop-close-mode-auto-workflow-dev`
- `develop-deploy-files-self-deploy`
- `develop-deploy-modes`
- `develop-dev-server-reason-codes`
- `develop-dev-server-triage`
- `develop-dynamic-runtime-start-container`
- `develop-env-var-channels`
- `develop-http-diagnostic`
- `develop-intro`
- `develop-knowledge-pointers`
- `develop-mode-expansion`
- `develop-platform-rules-common`
- `develop-platform-rules-container`
- `develop-strategy-awareness`
- `develop-verify-matrix`

---

# 11. Strategy-setup synthesis

## 11.1 strategy-setup container (git-push setup)

_action=git-push-setup synthesizes setup-git-push-container._

**Phase**: `strategy-setup` &middot; **Env**: `container`

**Plan.Primary**: `` → 

**Atoms** (1 unique, 1 render-instances, 2369 bytes total):
- `setup-git-push-container`

## 11.2 strategy-setup local

_Local-env git-push setup atom._

**Phase**: `strategy-setup` &middot; **Env**: `local`

**Plan.Primary**: `` → 

**Atoms** (1 unique, 1 render-instances, 1948 bytes total):
- `setup-git-push-local`

---

# 12. Export workflow

## 12.1 export-active container

_Export workflow synthesizes export-* atoms._

**Phase**: `export-active` &middot; **Env**: `container`

**Plan.Primary**: `` → 

**Atoms** (6 unique, 6 render-instances, 27945 bytes total):
- `export-classify-envs`
- `export-intro`
- `export-publish`
- `export-publish-needs-setup`
- `export-validate`
- `scaffold-zerops-yaml`

---

# 13. Develop closed (auto)

## 13.1 develop-closed-auto after green run

_All services deployed+verified, session auto-closed._

**Phase**: `develop-closed-auto` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Close current develop session

**Atoms** (2 unique, 2 render-instances, 1403 bytes total):
- `develop-auto-close-semantics`
- `develop-closed-auto`

---

# Anomalies (5)

## ERROR (3)

- **11.1 strategy-setup container (git-push setup)** — Plan.Primary is zero (empty Plan)
- **11.2 strategy-setup local** — Plan.Primary is zero (empty Plan)
- **12.1 export-active container** — Plan.Primary is zero (empty Plan)

## WARN (2)

- **10.2 mixed runtimes (api + web + db)** — briefing 34171 bytes > 25KB soft cap
- **12.1 export-active container** — briefing 27945 bytes > 25KB soft cap

