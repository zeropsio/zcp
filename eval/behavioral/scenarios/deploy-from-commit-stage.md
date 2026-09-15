---
id: deploy-from-commit-stage
description: |
  Existing dev/stage Node pair, both buildFromGit-deployed (real cloned
  repo mounted on appdev). User wants a SPECIFIC commit — not necessarily
  the current HEAD — shipped to appstage, and wants to be able to prove
  later exactly which commit is running there. Tests G3 (docs/
  spec-workflows.md §4.9): `zerops_deploy sha=` deploy-from-commit and the
  SHA/appVersionId fields in the response. These are recorded operation
  evidence, not proof that a stale or concurrent platform result belongs
  to this push: the correlation limitation in §4.9 remains open.

  Status `promote: containerCheck`: a follow-up must independently check
  the deployed artifact/source; today's toolResult and liveness checks
  establish response shape and a healthy target only.
  The preseed (plant-repo-cargo.sh) leaves appdev's repository on a
  feature branch with a dirty tracked file, an untracked file, a user
  tag, a custom ref, a user identity and a foreign origin. A
  deploy-from-commit extracts the commit OUTSIDE the working tree (GF-3),
  so the containerCheck proves the source checkout is byte-for-byte
  untouched: same HEAD, same branch, same dirty state, nothing committed
  or stashed on the user's behalf.
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
    - {always: "zerops_deploy{targetService=appstage}"}
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
      Agent must discover `zerops_deploy` takes a `sha` parameter (rather
      than resolving the commit itself and passing it as `workingDir` or
      some other field) and pass the commit reachable in appdev's mounted
      repo.
  - id: ledger-as-the-answer
    description: |
      When the user later asks "what's running on stage", the agent
      should inspect the platform's active appVersion, then use the deploy
      response or recorded attempt's sha/appVersionId as operation evidence.
      Missing or uncorrelated evidence must be stated as uncertainty; the
      agent must not invent a tag ledger or promise a proven source mapping.

---

I want to ship the exact commit currently checked out on `appdev` to
`appstage` — not just "whatever's in the working tree right now" in some
vague sense, the actual commit, by its sha. Can you do that, and
afterwards tell me exactly what's now running on appstage so I can check
it later?
