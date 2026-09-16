---
id: deploy-from-commit-stage
description: |
  Existing dev/stage Node pair, both buildFromGit-deployed (real cloned
  repo mounted on appdev), and appdev's working tree is dirty (preseed).
  The prompt only says "ship what's on appdev" and asks to be told later
  exactly which commit is running — it never names `sha`, a parameter or
  a tool. Tests G3 (docs/spec-workflows.md §4.9): the process, not the
  prompt, must lead the agent to `zerops_deploy sha=` deploy-from-commit
  — a plain cross-deploy would ship the dirty working tree's contents
  rather than a provable commit, so "ship what's on appdev, and prove
  which commit later" cannot be answered honestly without naming a
  commit and deploying it without touching the tree. The SHA/appVersionId
  fields in the response are recorded operation evidence, not proof that
  a stale or concurrent platform result belongs to this push: the
  correlation limitation in §4.9 remains open.

  Status `promote: containerCheck`: a follow-up must independently check
  the deployed artifact/source; today's toolResult and liveness checks
  establish response shape and a healthy target only.
  The preseed (plant-repo-cargo.sh) leaves appdev's repository on a
  feature branch with a dirty tracked file, an untracked file, a user
  tag, a custom ref, a user identity and a foreign origin. A
  deploy-from-commit extracts the commit OUTSIDE the working tree (GF-3),
  so the containerCheck proves the source checkout is byte-for-byte
  untouched: same HEAD, same branch, same dirty state, nothing committed
  or stashed on the user's behalf — this is the set that actually forces
  the agent toward `sha=` rather than committing appdev's dirty state to
  make "what's on appdev" a clean answer.
seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
preseedScript: preseed/plant-repo-cargo.sh
tags: [deploy-from-commit, deploy-evidence, git-foundation, cross-deploy, node]
area: develop
retrospective:
  promptStyle: briefing-future-agent
verification:
  mode: required
  spec: spec-workflows.md §4.9
  liveness: {service: appstage, marker: "nodejs"}
  toolArg:
    - {always: "zerops_deploy{targetService=appstage,sha~^[0-9a-f]+$}"}
  toolResult:
    - {tool: zerops_deploy, contains: "\"sha\":\""}
    - {tool: zerops_deploy, contains: "\"appVersionId\":\""}
  containerCheck:
    - {service: appdev, cmd: "git -C /var/www merge-base --is-ancestor refs/preseed/head HEAD && echo history-kept", match: "^history-kept"}
    - {service: appdev, cmd: "git -C /var/www symbolic-ref --short HEAD", match: "^feature/preseed-cargo"}
    - {service: appdev, cmd: "git -C /var/www tag -l v0.1-user-release", match: "^v0.1-user-release"}
    - {service: appdev, cmd: "git -C /var/www rev-parse -q --verify refs/t3/checkpoints/preseed >/dev/null && echo ref-kept", match: "^ref-kept"}
    - {service: appdev, cmd: "grep -q 'v2 uncommitted' /var/www/cargo-tracked.txt && test -f /var/www/cargo-untracked.txt && echo dirty-kept", match: "^dirty-kept"}
    - {service: appdev, cmd: "grep -qxF user-private-dir/ /var/www/.git/info/exclude && echo exclude-kept", match: "^exclude-kept"}
    - {service: appdev, cmd: "git -C /var/www config user.email", match: "^preseed@example\\.com"}
    - {service: appdev, cmd: "git -C /var/www remote get-url origin", match: "^https://example\\.invalid/preseed/repo\\.git"}
    - {service: appdev, cmd: "git -C /var/www tag -l 'zcp/*' | wc -l", match: "^\\s*0"}
    - {service: appdev, cmd: "git -C /var/www rev-parse HEAD refs/preseed/head | uniq | wc -l", match: "^\\s*1"}
    - {service: appdev, cmd: "git -C /var/www status --porcelain -- cargo-tracked.txt cargo-untracked.txt", match: "(?s)^ M cargo-tracked\\.txt.*\\?\\? cargo-untracked\\.txt"}
    - {service: appdev, cmd: "test -f /var/www/user-private-dir/keep && echo ignored-kept", match: "^ignored-kept"}
  noFailedProcesses: true
  never: ["zerops_import{override=true}", "zerops_delete"]
userPersona: |
  Your `appdev` and `appstage` are both healthy Node services, cloned from
  the same GitHub repo. You want to ship the exact commit currently
  checked out on `appdev` to `appstage` — and you want to be able to ask
  later "what commit is running on stage" and get a real answer, not a
  guess from the deploy timestamp. Push back if the agent proposes a plain
  cross-deploy without naming a commit, or claims it can't track which
  commit landed where.
notableFriction:
  - id: sha-param-discovery
    description: |
      Nothing in the prompt names `sha`, a parameter, or a tool — the
      agent must work out, from the persona's insistence on shipping a
      provable commit rather than "whatever's in the working tree" and
      from appdev's dirty/branched/tagged preseeded state, that
      `zerops_deploy` takes a `sha` parameter for exactly this case: a
      named commit deployed without touching the working tree. An agent
      that commits appdev's dirty state to make the deploy "clean", or
      that resolves the commit itself and passes it via `workingDir` or
      another field, has picked the wrong route; the containerCheck's
      untouched-checkout assertions and the `sha~` toolArg row both catch
      it directly.
  - id: ledger-as-the-answer
    description: |
      When the user later asks "what's running on stage", the agent
      should inspect the platform's active appVersion, then use the deploy
      response or recorded attempt's sha/appVersionId as operation evidence.
      Missing or uncorrelated evidence must be stated as uncertainty; the
      agent must not invent a tag ledger or promise a proven source mapping.

---

Ship what's on `appdev` to `appstage`. Afterwards tell me exactly which
commit is running on `appstage` so I can verify it later.
