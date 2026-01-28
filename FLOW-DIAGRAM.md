# ZCP Flow Diagrams

## Main Workflow

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           ZCP MAIN WORKFLOW                                  │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│   INIT ──────► DISCOVER ──────► DEVELOP ──────► DEPLOY ──────► VERIFY ──► DONE
│     │             │                │               │             │          │
│   Gate 0       Gate 1           Gate 2          Gate 3        Gate 4        │
│   recipe       discovery        dev_verify      deploy        stage         │
│   review.json  .json            .json           evidence      verify.json   │
│                                 + config        .json                       │
│                                                                     ▲       │
│                                      ◄────────── iterate ───────────┘       │
│                                                                              │
├─────────────────────────────────────────────────────────────────────────────┤
│  Alternative Modes:                                                          │
│  • dev-only:  INIT → DISCOVER → DEVELOP → DONE (no deploy/verify)           │
│  • hotfix:    INIT → DEVELOP → DEPLOY → VERIFY → DONE (skips discover)      │
│  • quick:     No phases, no gates (exploration mode)                         │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Gate Requirements

| Gate | Transition | Evidence Required |
|------|------------|-------------------|
| 0 | INIT → DISCOVER | `recipe_review.json` |
| 1 | DISCOVER → DEVELOP | `discovery.json` |
| 2 | DEVELOP → DEPLOY | `dev_verify.json` + config validation |
| 3 | DEPLOY → VERIFY | `deploy_evidence.json` |
| 4 | VERIFY → DONE | `stage_verify.json` |

---

## Bootstrap Flow (New Projects)

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         BOOTSTRAP FLOW (New Projects)                        │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│   .zcp/workflow.sh bootstrap --runtime X --services Y                        │
│                            │                                                 │
│   ┌────────────────────────┼────────────────────────────────────────────┐   │
│   │                        ▼                                             │   │
│   │   ┌──────┐    ┌───────────────┐    ┌─────────────────┐              │   │
│   │   │ plan │───►│ recipe-search │───►│ generate-import │              │   │
│   │   └──────┘    └───────────────┘    └────────┬────────┘              │   │
│   │                                              │                       │   │
│   │   ┌──────────────────────────────────────────┘                      │   │
│   │   │                                                                  │   │
│   │   ▼                                                                  │   │
│   │   ┌─────────────────┐    ┌───────────────┐    ┌───────────┐         │   │
│   │   │ import-services │───►│ wait-services │───►│ mount-dev │         │   │
│   │   └─────────────────┘    │   (polling)   │    └─────┬─────┘         │   │
│   │                          └───────────────┘          │               │   │
│   │                                                     │               │   │
│   │   ┌─────────────────────────────────────────────────┘               │   │
│   │   │                                                                  │   │
│   │   ▼                                                                  │   │
│   │   ┌──────────┐    ┌─────────────────┐                               │   │
│   │   │ finalize │───►│ spawn-subagents │                               │   │
│   │   └──────────┘    └────────┬────────┘                               │   │
│   │                            │                                         │   │
│   └────────────────────────────┼─────────────────────────────────────────┘   │
│                                ▼                                             │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │  PARALLEL SUBAGENTS (one per service pair)                           │   │
│   │  Each: zerops.yml → deploy dev → write code → test → deploy stage   │   │
│   └───────────────────────────────┬─────────────────────────────────────┘   │
│                                   │                                          │
│                                   ▼                                          │
│   ┌───────────────────┐    ┌──────────────────────────────────────────┐     │
│   │ aggregate-results │───►│  Standard Workflow (DEVELOP phase)       │     │
│   │     (polling)     │    │                                          │     │
│   └───────────────────┘    │  DEVELOP → DEPLOY → VERIFY → DONE        │     │
│                            └──────────────────────────────────────────┘     │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Bootstrap Steps

| Step | Description |
|------|-------------|
| `plan` | Create bootstrap plan (instant) |
| `recipe-search` | Fetch runtime patterns (2-3 sec) |
| `generate-import` | Create import.yml (instant) |
| `import-services` | Send to Zerops API (instant) |
| `wait-services` | Poll until RUNNING |
| `mount-dev` | SSHFS mount (instant) |
| `finalize` | Create handoff data (instant) |
| `spawn-subagents` | Output subagent instructions |
| `aggregate-results` | Wait for completion, create discovery |

---

## Entry Decision Tree

```
┌──────────────────┐
│ zcli service list │
└────────┬─────────┘
         │
   ┌─────┴─────┬─────────────────┐
   ▼           ▼                 ▼
FRESH      CONFORMANT      NON_CONFORMANT
   │           │                 │
   ▼           ▼                 ▼
BOOTSTRAP → STANDARD         BOOTSTRAP
(create)     (init)         (add missing)
```

---

## Key Concepts

- **Gates**: Evidence files must exist before transitioning (prevents skipping steps)
- **Main flow**: Linear phase progression with gates enforcing evidence at each step
- **Bootstrap**: Agent-orchestrated step-by-step service creation, ending at DEVELOP phase
- **Subagents**: Run in parallel during bootstrap to configure each service pair
- **Iterate**: After DONE, use `iterate` to return to DEVELOP for additional work
