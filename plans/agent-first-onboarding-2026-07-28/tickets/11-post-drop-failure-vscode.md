# 11 — Post-drop failure presentation in vscode

- `status:` closed
- `type:` grilling
- `assignee:` krls2020 (session 2026-07-28)
- `blocked-by:` —

## Question

Graduated from the map's fog after tickets 06 + 08 closed. The overlay drops on `agent-ready`
(S2 — command dispatched to a live terminal); the launch can still fail *after* that, visibly
only inside vscode (`command not found`, agent crash, agent's own login screen). Settled
context: the FE is deliberately not told post-dispatch and shows nothing (tickets 02 §6, 08 §7);
the terminal is the error surface; panel rows derive from envs, so a crashed-after-launch agent
still reads `authorized` → `Open terminal`.

Decide the container-side presentation: is the maximized terminal showing the error the WHOLE
answer, or does the container add anything — e.g. panel auto-open when the launched terminal
exits early, a transient row state, a notification? And confirm the steady state: after such a
failure the panel's plain `authorized` row (no special "launch failed" state) is the desired
resting point.

## Answer

Resolved 2026-07-28 (grilling with owner).

### 1. The terminal IS the whole answer — no container-side reaction

Post-dispatch, the container adds **nothing**: no panel auto-open on early exit, no
notification, no transient row state. A best-effort early-exit reaction (shell-integration
`onDidEndTerminalShellExecution` within a time window) was considered and rejected — more
mechanism than the rarity of the failure deserves.

Facts that bound the decision (research 03, verified in code):

- The launch is `sendText` of a command line into a live shell — a failed agent leaves the
  shell alive, the terminal never closes, so `onDidCloseTerminal` never fires. The only
  "command ended" signal is shell-integration-gated, race-prone, and may never activate:
  any reaction would sit on a best-effort foundation.
- The dispatched command line lands in shell history (`sendText` writes the pty as typed
  text) — up-arrow + enter is the native retry, already in the user's hands.
- The "agent shows its own login screen" case (zembed env lag, ticket 02 §5) never ends the
  command, so exit-based detection cannot catch the most likely post-dispatch surprise anyway;
  the terminal is the correct surface for it.
- Failure is rare by construction: agents are preinstalled in the image and the agentId gate
  rejects anything outside the registry pre-dispatch (`launch-failed` covers that half).

### 2. Steady state confirmed — the plain `authorized` row

Panel rows derive from envs only; a crashed-after-launch agent is indistinguishable from a
running one and reads `authorized` → `Open terminal`. No "launch failed" row state exists —
it would require exactly the detection rejected above. `Open terminal` is the recovery
affordance; the return-visit branch (ticket 01 §3) is the recovery path.

**Accepted sharp edge** (owner, explicit): after a reload with the dead-agent terminal
restored, `hasEditors` is true, so the panel does not force itself open — the user again sees
only the terminal with the error. This is a deliberate consequence of the container-owned
restored-editors rule, not an oversight.

### 3. Universal posture — one rule for every launch path

Post-dispatch, the terminal is the sole error surface for **every** launch path — the
onboarding launch and later panel-initiated `Open terminal` launches alike. No path gets a
special follow-up; the spec carries one sentence, not two regimes.
