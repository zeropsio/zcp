# Capture Inspector audit — remediation verification addendum

This addendum does not rewrite the frozen audit or its historical `NO-GO`
verdict. It records the subsequent implementation response on
`feat/capture-raw-prototype` and the gates run after the fixes.

## Disposition

All 19 confirmed ledger findings now have a production regression test or an
existing audit probe promoted into the production suite, plus a root-cause fix.
No finding was waived and no security, evidence, architecture, lint, or browser
assertion was weakened.

This is an implementation-complete **GO candidate**, not release authorization.
The frozen audit requires an independent final aggregate re-review before its
formal verdict can be superseded. No release/install was performed and neither
the local nor remote canonical `/usr/local/bin/zcp` was changed.

## Finding-to-remediation map

| Finding | Remediation |
|---|---|
| `CI-001` | `8b9e0770`: build-tagged locking/process/signal adapters; Windows lock and liveness query; safe Windows fallback retains ownership rather than killing an unproven PID. |
| `CI-002` | `324b0ab4`: all full-lint findings closed with code changes or narrowly documented wire-schema/debug-shape exceptions; native and Darwin/Linux full lint are green. |
| `LIFE-001` | `8b9e0770`: readiness ownership is journaled immediately; rollback performs control shutdown, bounded waits, identity-checked TERM/KILL, and retains `BROKEN` state until exit is proven. |
| `LIFE-002` | `bcd1395b`: component errors are sampled after drain/close and downgrade terminal status to partial. |
| `INT-001` | `5d323324`: manifest ID is the expected identity for provider, lifecycle, MCP, and provenance streams. |
| `INT-002` | `5d323324`: provider and MCP streams require their exact start record at seq 1 and one valid terminal record. |
| `INT-003` | `5d323324`: open eval/scenario/invocation scopes propagate annotation incompleteness to top-level integrity. |
| `INT-004` | `5d323324`: core inspection resolves the physical session root and rejects symlinked ancestors/components. |
| `CORR-001` | `5d323324`: propagation compares a complete normalized result structure; display text is derived separately. |
| `CORR-002` | `5d323324`: unfinished SSE blocks remain incomplete and cannot become completed tool proposals. |
| `CORR-003` | `d0eb4558`, `5d323324`: graph basis comes from the actual join; argument mismatch and non-unique same-name candidates are not emitted as proven executions. |
| `PROJ-001` | `d0eb4558`: MCP IDs include direction and stream/decoded coordinates; uniqueness is tested over multi-line records and real captures. |
| `EVID-001` | `fd54d99d`: every known metric carries bounded deterministic file/range evidence coordinates; aggregates collapse to covering ranges. |
| `WEB-001` | `38c80381`: finalized cache identity includes non-restorable file change identity with content-hash fallback; same-size/restored-mtime tampering invalidates `/view`. |
| `WEB-002` | `38c80381`: pinned/root same-ID different-directory collisions fail startup/index/by-ID lookup. |
| `WEB-003` | `d0eb4558`: root scanning rejects a symlinked manifest. |
| `SEC-001` | `d0eb4558`: reveal accepts exactly one JSON object and requires EOF. |
| `ISO-001` | `a9681786`: capture-off user simulation inherits the original environment/HOME. |
| `ISO-002` | `a9681786`: OFF discovery checks for state without creating manager/control directories or a lock. |

`418498b3` split the forensic inspection orchestration into explicit provider,
lifecycle, MCP, provenance, correlation, and final-integrity stages so the
strengthened validators remain maintainable and lint-clean.

## Verification run after remediation

The following gates passed from the remediated worktree:

```text
go test ./... -short -count=1
go test -race ./... -count=1
go vet ./...
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./...
make lint-local
make lint
node --check internal/captureinspector/internal/web/assets/app.js
go test -tags captureinspector_browser \
  ./internal/captureinspector/internal/web -run '^TestBrowserSmoke$' -count=1
git diff --check
```

Focused core/Inspector race suites also passed throughout each remediation
batch. The final real-corpus check opened five immutable container captures with
`Integrity: OK`; all five projections exposed 112 metrics and every known metric
had non-empty bounded evidence coordinates.

## Observed real-container run

A commit-addressed temporary binary from `3bbc3da1` was run in the isolated
`eval-new` Zerops project with scoped raw capture. Local and remote candidate
SHA-256 matched. The run exited zero, the capture finalized valid and complete,
the remote canonical binary remained byte-identical, and direct pre-cleanup
platform evidence was preserved in `platform-snapshot.json`.

That run also exposed a separate eval-harness bug: bare scenario type globs did
not match Zerops composite live identifiers such as `ubuntu/nodejs@22` and
`postgresql:single@18`. `182f1022` fixes the compatibility match while keeping
explicit OS/mode patterns strict. This bug was outside the 19-item Inspector
ledger; its immutable original verification artifact remains unchanged.

Raw capture bodies, prompts, tool data, launch capabilities, cookies, and
credentials are intentionally absent from this tracked addendum.

## Remaining re-review limitations

The original unmeasured areas remain limitations: no Windows runtime exercise,
no destructive power-loss/disk-full campaign, and no 250 MB/1 GB or
decompression-bomb benchmark. A formal verdict change still requires an
independent aggregate review of the remediation range and confirmation that no
new finding was introduced.
