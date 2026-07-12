# Post-launch teardown: agent bypasses reset via zcli

**Evidence:** flow-eval suite `20260710-113219` / `launch-production-delegated`
(the delegated-mint live verification). Persona explicitly asked for cleanup
"via ZCP's own launch-production reset". The agent instead: tried
`zerops_manage` (service lifecycle only) → read prod-ops action list
(`delete-service`, no `delete-project`) → escaped to `zcli project delete`,
logging in with the staged `ZCP_LAUNCH_TOKEN` it extracted over SSH (careful
shell hygiene — the value never crossed the transcript — but still a RED FLAG
zcli escape per the flow-eval grading rule). `action="reset"` was never
called; with the window still open (staged token present, target project
recorded) reset's orphan-delete path would most likely have handled it
natively.

**Root cause (suspected):** discoverability — the launched response + prod-ops
guidance frame reset as failure recovery, not as the teardown path for a
launched-but-unconfirmed project. The retrospective confirms the agent never
associated reset with "delete the prod project".

**Sketch:** one guidance line in the launched response / prod-ops refusal
("to abandon and delete this production project, `action=\"reset\"`") +
possibly a reset mention in `launch-status-recovery.md`. Verify first what
reset actually does on `Status==launched` + open window (test exists for
failed/no-target; launched+target semantics need a pinning test either way).

**Trigger to promote:** next launch-production guidance pass, or a second
eval run showing the same zcli escape.

**Not doing now:** the branch feat/token-delegation-launch is feature-complete
and this is adjacent guidance polish, not delegation behavior; the teardown
ask is eval-shaped (real users delete prod projects in the dashboard).

**Reproduced 2026-07-12** (suite 20260712-124728, launch-production-delegated, fresh
delegation): agent again never discovered `action="reset"` for post-launch cleanup —
SSH-read the staged `ZCP_LAUNCH_TOKEN` off appdev, `zcli login` with it, `zcli project
delete`. Second reproduction; promotion trigger is firing.
