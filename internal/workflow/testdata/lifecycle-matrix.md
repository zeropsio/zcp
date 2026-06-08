# ZCP Lifecycle Matrix Simulation

Generated: 2026-06-08T08:47:44Z
Corpus: 111 atoms
Scenarios: 46

---

# 1. Idle entry points

## 1.1 idle/empty (fresh user, no project state)

_Brand-new project — should route the agent into bootstrap._

**Phase**: `idle` &middot; **Env**: `container` &middot; **IdleScenario**: `empty`

**Plan.Primary**: `zerops_workflow` → Create services

**Atoms** (2 unique, 2 render-instances, 3506 bytes total):
- `bootstrap-route-options`
- `idle-bootstrap-entry`

## 1.2 idle/adopt (only unmanaged runtimes)

_Project has runtime services but no ServiceMeta files — adoption path._

**Phase**: `idle` &middot; **Env**: `container` &middot; **IdleScenario**: `adopt`

**Plan.Primary**: `zerops_workflow` → Adopt unmanaged runtimes

**Atoms** (2 unique, 2 render-instances, 6583 bytes total):
- `bootstrap-route-options`
- `idle-adopt-entry`

## 1.3 idle/bootstrapped (managed services exist)

_User finished bootstrap, returning later to start a develop task._

**Phase**: `idle` &middot; **Env**: `container` &middot; **IdleScenario**: `bootstrapped`

**Plan.Primary**: `zerops_workflow` → Start a develop task
**Alternatives**: `Add more services`

**Atoms** (2 unique, 2 render-instances, 3430 bytes total):
- `bootstrap-route-options`
- `idle-develop-entry`

## 1.4 idle/incomplete (partial bootstrap meta exists)

_Prior bootstrap session crashed mid-way; resume should be offered._

**Phase**: `idle` &middot; **Env**: `container` &middot; **IdleScenario**: `incomplete`

**Plan.Primary**: `zerops_workflow` → Adopt unmanaged runtimes

**Atoms** (2 unique, 2 render-instances, 4036 bytes total):
- `bootstrap-resume`
- `bootstrap-route-options`

## 1.5 idle/empty LOCAL env

_Local-machine ZCP without any project — bootstrap entry should adapt._

**Phase**: `idle` &middot; **Env**: `local` &middot; **IdleScenario**: `empty`

**Plan.Primary**: `zerops_workflow` → Create services

**Atoms** (2 unique, 2 render-instances, 3506 bytes total):
- `bootstrap-route-options`
- `idle-bootstrap-entry`

---

# 2. Bootstrap — classic route

## 2.1 classic/discover dynamic standard pair (container)

_Free-form plan: dynamic runtime in standard mode + dev/stage hostnames._

**Phase**: `bootstrap-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Continue bootstrap

**Atoms** (5 unique, 5 render-instances, 5558 bytes total):
- `bootstrap-classic-plan-dynamic`
- `bootstrap-classic-plan-static`
- `bootstrap-intro`
- `bootstrap-mode-prompt`
- `bootstrap-runtime-classes`

## 2.2 classic/discover static SPA (container)

_Static-runtime path (Vite SPA, etc.) — different deploy/build vocabulary._

**Phase**: `bootstrap-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Continue bootstrap

**Atoms** (5 unique, 5 render-instances, 5558 bytes total):
- `bootstrap-classic-plan-dynamic`
- `bootstrap-classic-plan-static`
- `bootstrap-intro`
- `bootstrap-mode-prompt`
- `bootstrap-runtime-classes`

## 2.3 classic/discover implicit-webserver (PHP simple)

_PHP implicit-webserver: no `start:` block, real start path._

**Phase**: `bootstrap-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Continue bootstrap

**Atoms** (5 unique, 5 render-instances, 5558 bytes total):
- `bootstrap-classic-plan-dynamic`
- `bootstrap-classic-plan-static`
- `bootstrap-intro`
- `bootstrap-mode-prompt`
- `bootstrap-runtime-classes`

## 2.4 classic/provision (container, dev mode)

_Provision step — agent should see import.yaml + auto-mount guidance._

**Phase**: `bootstrap-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Continue bootstrap

**Atoms** (3 unique, 3 render-instances, 6183 bytes total):
- `bootstrap-env-var-discovery`
- `bootstrap-provision-rules`
- `bootstrap-wait-active`

## 2.5 classic/close (container, simple mode)

_Close step — finalize ServiceMeta, no first deploy._

**Phase**: `bootstrap-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Continue bootstrap

**Atoms** (2 unique, 2 render-instances, 2365 bytes total):
- `bootstrap-close`
- `bootstrap-verify`

## 2.6 classic/discover (LOCAL env)

_Local-mode bootstrap discover — should suppress mount/SSH guidance._

**Phase**: `bootstrap-active` &middot; **Env**: `local`

**Plan.Primary**: `zerops_workflow` → Continue bootstrap

**Atoms** (6 unique, 6 render-instances, 6451 bytes total):
- `bootstrap-classic-plan-dynamic`
- `bootstrap-classic-plan-static`
- `bootstrap-discover-local`
- `bootstrap-intro`
- `bootstrap-mode-prompt`
- `bootstrap-runtime-classes`

## 2.7 classic/provision (LOCAL env)

_Local provision — no auto-mount path._

**Phase**: `bootstrap-active` &middot; **Env**: `local`

**Plan.Primary**: `zerops_workflow` → Continue bootstrap

**Atoms** (5 unique, 5 render-instances, 7207 bytes total):
- `bootstrap-env-var-discovery`
- `bootstrap-provision-local`
- `bootstrap-provision-local-finalize`
- `bootstrap-provision-rules`
- `bootstrap-wait-active`

---

# 3. Bootstrap — recipe route

## 3.1 recipe/discover (container, hello-world slug)

_Recipe discover: agent picks slug `nodejs-hello-world`._

**Phase**: `bootstrap-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Continue bootstrap

**Atoms** (1 unique, 1 render-instances, 836 bytes total):
- `bootstrap-intro`

## 3.2 recipe/provision (container, multi-service Laravel)

_Laravel-minimal recipe: php-apache + db._

**Phase**: `bootstrap-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Continue bootstrap

**Atoms** (1 unique, 1 render-instances, 1748 bytes total):
- `bootstrap-recipe-import`

## 3.3 recipe/close (container)

_Recipe close — finalize meta, hand off to develop._

**Phase**: `bootstrap-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Continue bootstrap

**Atoms** (3 unique, 3 render-instances, 2895 bytes total):
- `bootstrap-close`
- `bootstrap-recipe-close`
- `bootstrap-verify`

---

# 4. Bootstrap — adopt route

## 4.1 adopt/discover (container, single dev runtime)

_Single existing runtime to adopt as dev mode._

**Phase**: `bootstrap-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Continue bootstrap

**Atoms** (2 unique, 2 render-instances, 3269 bytes total):
- `bootstrap-adopt-discover`
- `bootstrap-intro`

## 4.2 adopt/discover (container, dev+stage pair)

_Two existing runtimes with dev/stage suffix → adopt as standard._

**Phase**: `bootstrap-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Continue bootstrap

**Atoms** (2 unique, 2 render-instances, 3269 bytes total):
- `bootstrap-adopt-discover`
- `bootstrap-intro`

## 4.3 adopt/provision (pure-adoption fast path)

_Plan all-existing — close should be skippable._

**Phase**: `bootstrap-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Continue bootstrap

**Atoms** (1 unique, 1 render-instances, 2333 bytes total):
- `bootstrap-env-var-discovery`

---

# 5. Develop — first-deploy branch

## 5.1 develop never-deployed dev/dynamic (container)

_Just bootstrapped, dev mode dynamic runtime, first develop iteration._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_deploy` → Deploy appdev

**Atoms** (19 unique, 19 render-instances, 15899 bytes total):
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`
- `develop-checklist-dev-mode`
- `develop-deploy-modes`
- `develop-env-var-channels`
- `develop-env-var-model`
- `develop-first-deploy-env-vars`
- `develop-first-deploy-execute`
- `develop-first-deploy-intro`
- `develop-first-deploy-scaffold-yaml`
- `develop-first-deploy-verify`
- `develop-first-deploy-write-app`
- `develop-http-diagnostic`
- `develop-knowledge-pointers`
- `develop-nodejs-greenfield-buildhint`
- `develop-platform-rules-common`
- `develop-platform-rules-container`
- `develop-reserved-env-names`
- `develop-verify-matrix`

## 5.2 develop never-deployed simple/dynamic (container)

_Simple-mode single service, healthCheck-driven start._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_deploy` → Deploy app

**Atoms** (19 unique, 19 render-instances, 15620 bytes total):
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`
- `develop-checklist-simple-mode`
- `develop-deploy-modes`
- `develop-env-var-channels`
- `develop-env-var-model`
- `develop-first-deploy-env-vars`
- `develop-first-deploy-execute`
- `develop-first-deploy-intro`
- `develop-first-deploy-scaffold-yaml`
- `develop-first-deploy-verify`
- `develop-first-deploy-write-app`
- `develop-http-diagnostic`
- `develop-knowledge-pointers`
- `develop-nodejs-greenfield-buildhint`
- `develop-platform-rules-common`
- `develop-platform-rules-container`
- `develop-reserved-env-names`
- `develop-verify-matrix`

## 5.3 develop never-deployed standard dev half (container)

_Standard-mode dev half, stage entry not yet written._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_deploy` → Deploy appdev

**Atoms** (19 unique, 19 render-instances, 15518 bytes total):
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`
- `develop-deploy-modes`
- `develop-env-var-channels`
- `develop-env-var-model`
- `develop-first-deploy-env-vars`
- `develop-first-deploy-execute`
- `develop-first-deploy-intro`
- `develop-first-deploy-promote-stage`
- `develop-first-deploy-scaffold-yaml`
- `develop-first-deploy-verify`
- `develop-first-deploy-write-app`
- `develop-http-diagnostic`
- `develop-knowledge-pointers`
- `develop-nodejs-greenfield-buildhint`
- `develop-platform-rules-common`
- `develop-platform-rules-container`
- `develop-reserved-env-names`
- `develop-verify-matrix`

## 5.4 develop never-deployed PHP simple (implicit-webserver)

_PHP simple — no `start:`; healthCheck on `/`._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_deploy` → Deploy site

**Atoms** (19 unique, 19 render-instances, 18194 bytes total):
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`
- `develop-checklist-simple-mode`
- `develop-deploy-modes`
- `develop-env-var-channels`
- `develop-env-var-model`
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
- `develop-reserved-env-names`
- `develop-verify-matrix`

## 5.5 develop never-deployed static SPA

_Static runtime — buildCommands generate dist; deployFiles selects ./dist._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_deploy` → Deploy spa

**Atoms** (14 unique, 14 render-instances, 13059 bytes total):
- `develop-auto-close-semantics`
- `develop-deploy-modes`
- `develop-env-var-channels`
- `develop-env-var-model`
- `develop-first-deploy-env-vars`
- `develop-first-deploy-execute`
- `develop-first-deploy-intro`
- `develop-first-deploy-scaffold-yaml`
- `develop-http-diagnostic`
- `develop-knowledge-pointers`
- `develop-platform-rules-common`
- `develop-reserved-env-names`
- `develop-static-workflow`
- `develop-verify-matrix`

## 5.6 develop never-deployed dev/dynamic (LOCAL env)

_Local-machine first deploy — local workflow atom path._

**Phase**: `develop-active` &middot; **Env**: `local`

**Plan.Primary**: `zerops_deploy` → Deploy app

**Atoms** (20 unique, 20 render-instances, 18904 bytes total):
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`
- `develop-deploy-modes`
- `develop-dynamic-runtime-start-local`
- `develop-env-var-channels`
- `develop-env-var-model`
- `develop-first-deploy-env-vars`
- `develop-first-deploy-intro`
- `develop-first-deploy-scaffold-yaml`
- `develop-first-deploy-verify`
- `develop-http-diagnostic`
- `develop-knowledge-pointers`
- `develop-local-env-channels`
- `develop-local-env-troubleshoot`
- `develop-local-workflow`
- `develop-nodejs-greenfield-buildhint`
- `develop-platform-rules-common`
- `develop-platform-rules-local`
- `develop-reserved-env-names`
- `develop-verify-matrix`

---

# 6. Develop — iteration after first deploy

## 6.1 develop deployed unset close-mode (post-first-deploy review)

_First deploy succeeded; close-mode still unset → review prompt should fire._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Close develop session

**Atoms** (12 unique, 12 render-instances, 12629 bytes total):
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`
- `develop-checklist-dev-mode`
- `develop-dev-server-reason-codes`
- `develop-dev-server-triage`
- `develop-dynamic-runtime-start-container`
- `develop-env-var-shell-usage`
- `develop-intro`
- `develop-knowledge-pointers`
- `develop-mode-expansion`
- `develop-strategy-review`
- `develop-verify-matrix`

## 6.2 develop deployed CloseMode=auto (steady-state iteration)

_Iteration after picking auto close-mode — strategy-review should NOT fire._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Close develop session

**Atoms** (16 unique, 16 render-instances, 19436 bytes total):
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`
- `develop-checklist-dev-mode`
- `develop-close-mode-auto`
- `develop-close-mode-auto-deploy-container`
- `develop-close-mode-auto-dev`
- `develop-close-mode-auto-workflow-dev`
- `develop-dev-server-reason-codes`
- `develop-dev-server-triage`
- `develop-dynamic-runtime-start-container`
- `develop-env-var-shell-usage`
- `develop-intro`
- `develop-knowledge-pointers`
- `develop-mode-expansion`
- `develop-strategy-awareness`
- `develop-verify-matrix`

---

# 7. Develop — close-mode variants

## 7.1 close-mode=auto + dev mode (container)

_Default close path — auto = run zerops_deploy at close._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Close develop session

**Atoms** (16 unique, 16 render-instances, 19436 bytes total):
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`
- `develop-checklist-dev-mode`
- `develop-close-mode-auto`
- `develop-close-mode-auto-deploy-container`
- `develop-close-mode-auto-dev`
- `develop-close-mode-auto-workflow-dev`
- `develop-dev-server-reason-codes`
- `develop-dev-server-triage`
- `develop-dynamic-runtime-start-container`
- `develop-env-var-shell-usage`
- `develop-intro`
- `develop-knowledge-pointers`
- `develop-mode-expansion`
- `develop-strategy-awareness`
- `develop-verify-matrix`

## 7.2 close-mode=git-push + GitPushState=configured + webhook

_Full git-push setup with webhook integration._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Close develop session

**Atoms** (9 unique, 9 render-instances, 13909 bytes total):
- `develop-auto-close-semantics`
- `develop-build-observe`
- `develop-change-drives-deploy`
- `develop-close-mode-git-push`
- `develop-env-var-shell-usage`
- `develop-intro`
- `develop-knowledge-pointers`
- `develop-strategy-awareness`
- `develop-verify-matrix`

## 7.3 close-mode=manual (yield to user)

_Manual close — ZCP records evidence but user owns deploys._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Close develop session

**Atoms** (13 unique, 13 render-instances, 16434 bytes total):
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`
- `develop-checklist-dev-mode`
- `develop-close-mode-manual`
- `develop-dev-server-reason-codes`
- `develop-dev-server-triage`
- `develop-dynamic-runtime-start-container`
- `develop-env-var-shell-usage`
- `develop-intro`
- `develop-knowledge-pointers`
- `develop-mode-expansion`
- `develop-strategy-awareness`
- `develop-verify-matrix`

## 7.4 close-mode=git-push BUT FirstDeployedAt empty (D2a edge)

_Agent set close-mode before first deploy — atoms must explain D2a (default self-deploy still applies)._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_deploy` → Deploy appdev

**Atoms** (20 unique, 20 render-instances, 18586 bytes total):
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`
- `develop-deploy-modes`
- `develop-env-var-channels`
- `develop-env-var-model`
- `develop-first-deploy-env-vars`
- `develop-first-deploy-execute`
- `develop-first-deploy-intro`
- `develop-first-deploy-promote-stage`
- `develop-first-deploy-scaffold-yaml`
- `develop-first-deploy-verify`
- `develop-first-deploy-write-app`
- `develop-http-diagnostic`
- `develop-knowledge-pointers`
- `develop-nodejs-greenfield-buildhint`
- `develop-platform-rules-common`
- `develop-platform-rules-container`
- `develop-reserved-env-names`
- `develop-strategy-awareness`
- `develop-verify-matrix`

---

# 8. Develop — git-push capability matrix

## 8.1 auto / unconfigured / none

_Default — git push capability not provisioned._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Close develop session

**Atoms** (10 unique, 10 render-instances, 11891 bytes total):
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`
- `develop-close-mode-auto`
- `develop-close-mode-auto-standard`
- `develop-dynamic-runtime-start-container`
- `develop-env-var-shell-usage`
- `develop-intro`
- `develop-knowledge-pointers`
- `develop-strategy-awareness`
- `develop-verify-matrix`

## 8.2 auto / configured / none

_Capability provisioned; close still does zcli (auto)._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Close develop session

**Atoms** (10 unique, 10 render-instances, 11891 bytes total):
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`
- `develop-close-mode-auto`
- `develop-close-mode-auto-standard`
- `develop-dynamic-runtime-start-container`
- `develop-env-var-shell-usage`
- `develop-intro`
- `develop-knowledge-pointers`
- `develop-strategy-awareness`
- `develop-verify-matrix`

## 8.3 git-push / unconfigured / none

_Mode flipped to git-push but capability missing — must chain to setup._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Close develop session

**Atoms** (8 unique, 8 render-instances, 8922 bytes total):
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`
- `develop-close-mode-git-push-needs-setup`
- `develop-env-var-shell-usage`
- `develop-intro`
- `develop-knowledge-pointers`
- `develop-strategy-awareness`
- `develop-verify-matrix`

## 8.4 git-push / configured / webhook

_Full webhook CI._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Close develop session

**Atoms** (9 unique, 9 render-instances, 13909 bytes total):
- `develop-auto-close-semantics`
- `develop-build-observe`
- `develop-change-drives-deploy`
- `develop-close-mode-git-push`
- `develop-env-var-shell-usage`
- `develop-intro`
- `develop-knowledge-pointers`
- `develop-strategy-awareness`
- `develop-verify-matrix`

## 8.5 git-push / configured / actions

_GitHub Actions CI._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Close develop session

**Atoms** (9 unique, 9 render-instances, 13909 bytes total):
- `develop-auto-close-semantics`
- `develop-build-observe`
- `develop-change-drives-deploy`
- `develop-close-mode-git-push`
- `develop-env-var-shell-usage`
- `develop-intro`
- `develop-knowledge-pointers`
- `develop-strategy-awareness`
- `develop-verify-matrix`

## 8.6 git-push / broken / webhook

_Push capability previously broken; recovery atom expected._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Close develop session

**Atoms** (9 unique, 9 render-instances, 9912 bytes total):
- `develop-auto-close-semantics`
- `develop-build-observe`
- `develop-change-drives-deploy`
- `develop-close-mode-git-push-needs-setup`
- `develop-env-var-shell-usage`
- `develop-intro`
- `develop-knowledge-pointers`
- `develop-strategy-awareness`
- `develop-verify-matrix`

---

# 9. Develop — failure tiers

## 9.1 iteration tier 1 (1 failed)

_First failure — DIAGNOSE tier._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_deploy` → Deploy appdev

**Atoms** (22 unique, 22 render-instances, 21473 bytes total):
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`
- `develop-checklist-dev-mode`
- `develop-deploy-files-self-deploy`
- `develop-deploy-modes`
- `develop-dynamic-runtime-start-container`
- `develop-env-var-channels`
- `develop-env-var-model`
- `develop-first-deploy-env-vars`
- `develop-first-deploy-execute`
- `develop-first-deploy-intro`
- `develop-first-deploy-scaffold-yaml`
- `develop-first-deploy-verify`
- `develop-first-deploy-write-app`
- `develop-http-diagnostic`
- `develop-knowledge-pointers`
- `develop-nodejs-greenfield-buildhint`
- `develop-platform-rules-common`
- `develop-platform-rules-container`
- `develop-reserved-env-names`
- `develop-strategy-awareness`
- `develop-verify-matrix`

## 9.2 iteration tier 3 (3 failed)

_After 3 failures — SYSTEMATIC tier kicks in._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_deploy` → Deploy appdev

**Atoms** (22 unique, 22 render-instances, 21473 bytes total):
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`
- `develop-checklist-dev-mode`
- `develop-deploy-files-self-deploy`
- `develop-deploy-modes`
- `develop-dynamic-runtime-start-container`
- `develop-env-var-channels`
- `develop-env-var-model`
- `develop-first-deploy-env-vars`
- `develop-first-deploy-execute`
- `develop-first-deploy-intro`
- `develop-first-deploy-scaffold-yaml`
- `develop-first-deploy-verify`
- `develop-first-deploy-write-app`
- `develop-http-diagnostic`
- `develop-knowledge-pointers`
- `develop-nodejs-greenfield-buildhint`
- `develop-platform-rules-common`
- `develop-platform-rules-container`
- `develop-reserved-env-names`
- `develop-strategy-awareness`
- `develop-verify-matrix`

## 9.3 iteration tier 5 (5 failed, STOP)

_Hit iteration cap — STOP tier should surface._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_deploy` → Deploy appdev

**Atoms** (22 unique, 22 render-instances, 21473 bytes total):
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`
- `develop-checklist-dev-mode`
- `develop-deploy-files-self-deploy`
- `develop-deploy-modes`
- `develop-dynamic-runtime-start-container`
- `develop-env-var-channels`
- `develop-env-var-model`
- `develop-first-deploy-env-vars`
- `develop-first-deploy-execute`
- `develop-first-deploy-intro`
- `develop-first-deploy-scaffold-yaml`
- `develop-first-deploy-verify`
- `develop-first-deploy-write-app`
- `develop-http-diagnostic`
- `develop-knowledge-pointers`
- `develop-nodejs-greenfield-buildhint`
- `develop-platform-rules-common`
- `develop-platform-rules-container`
- `develop-reserved-env-names`
- `develop-strategy-awareness`
- `develop-verify-matrix`

---

# 10. Develop — multi-service orchestration

## 10.1 standard mode dev+stage halves both never-deployed

_Standard pair — atoms should fire per-half with correct hostnames._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_deploy` → Deploy appdev

**Atoms** (19 unique, 19 render-instances, 15559 bytes total):
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`
- `develop-deploy-modes`
- `develop-env-var-channels`
- `develop-env-var-model`
- `develop-first-deploy-env-vars`
- `develop-first-deploy-execute`
- `develop-first-deploy-intro`
- `develop-first-deploy-promote-stage`
- `develop-first-deploy-scaffold-yaml`
- `develop-first-deploy-verify`
- `develop-first-deploy-write-app`
- `develop-http-diagnostic`
- `develop-knowledge-pointers`
- `develop-nodejs-greenfield-buildhint`
- `develop-platform-rules-common`
- `develop-platform-rules-container`
- `develop-reserved-env-names`
- `develop-verify-matrix`

## 10.2 mixed runtimes (api + web + db)

_Two runtimes + managed dep — per-service rendering correctness._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Close develop session

**Atoms** (16 unique, 19 render-instances, 24402 bytes total):
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`
- `develop-checklist-dev-mode`
- `develop-close-mode-auto`
- `develop-close-mode-auto-deploy-container`
- `develop-close-mode-auto-dev`
- `develop-close-mode-auto-workflow-dev`
- `develop-dev-server-reason-codes`
- `develop-dev-server-triage`
- `develop-dynamic-runtime-start-container`
- `develop-env-var-shell-usage`
- `develop-intro`
- `develop-knowledge-pointers`
- `develop-mode-expansion`
- `develop-strategy-awareness`
- `develop-verify-matrix`

## 10.3 four runtimes scope=1 (Lever B narrow)

_Project has 3 dev runtimes + 1 managed; scope is just appdev. Per-service atoms must fire only for appdev._

**Phase**: `develop-active` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Close develop session

**Atoms** (16 unique, 16 render-instances, 19436 bytes total):
- `develop-auto-close-semantics`
- `develop-change-drives-deploy`
- `develop-checklist-dev-mode`
- `develop-close-mode-auto`
- `develop-close-mode-auto-deploy-container`
- `develop-close-mode-auto-dev`
- `develop-close-mode-auto-workflow-dev`
- `develop-dev-server-reason-codes`
- `develop-dev-server-triage`
- `develop-dynamic-runtime-start-container`
- `develop-env-var-shell-usage`
- `develop-intro`
- `develop-knowledge-pointers`
- `develop-mode-expansion`
- `develop-strategy-awareness`
- `develop-verify-matrix`

---

# 11. Strategy-setup synthesis

## 11.1 strategy-setup container (git-push setup)

_action=git-push-setup synthesizes setup-git-push-container._

**Phase**: `strategy-setup` &middot; **Env**: `container`

**Plan.Primary**: `` → 

**Atoms** (1 unique, 1 render-instances, 4221 bytes total):
- `setup-git-push-container`

## 11.2 strategy-setup local

_Local-env git-push setup atom._

**Phase**: `strategy-setup` &middot; **Env**: `local`

**Plan.Primary**: `` → 

**Atoms** (1 unique, 1 render-instances, 2140 bytes total):
- `setup-git-push-local`

---

# 12. Export workflow

## 12.1 export-active container

_Export workflow synthesizes export-* atoms._

**Phase**: `export-active` &middot; **Env**: `container`

**Plan.Primary**: `` → 

**Atoms** (1 unique, 1 render-instances, 2185 bytes total):
- `export-intro`

---

# 13. Develop closed (auto)

## 13.1 develop-closed-auto after green run

_All services deployed+verified, session auto-closed._

**Phase**: `develop-closed-auto` &middot; **Env**: `container`

**Plan.Primary**: `zerops_workflow` → Close current develop session

**Atoms** (2 unique, 2 render-instances, 1934 bytes total):
- `develop-auto-close-semantics`
- `develop-closed-auto`

