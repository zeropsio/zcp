# Retest pack: <slug>

Zero-context bar: Karel executes every step below with no other file open,
in minutes. Every step ties to an ACx.

## Run
Exact commands, each with the ONE line that means "pass":

| command | expected line |
|---|---|
| `go test ./<pkg> -run <Name> -short -count=1 -v` | `PASS` |
| `make lint-fast` | (no output = clean) |

## Drive
Steps against `zcp-eval-clean`, each tied to an ACx:

1. AC1 — <exact step: tool call / CLI command / UI click> — expect: <exact observation>
2. AC2 — <exact step> — expect: <exact observation>

## What changed
- S1: <one line>
- S2: <one line>

## Rollback
`git revert <range>` — range from Run State `integration:` field.
<any follow-up needed after revert, or "none">

## Docs
Spec §§ touched (promoted at GATE 1): `docs/spec-<name>.md` §<n>, §<m>
