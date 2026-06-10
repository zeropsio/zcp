# P6 residual cleanup (deferred from audit-fixes, 2026-06-10)

Lowest-value items from the audit-fixes P6 (dead-code/test-infra) phase, deferred
because each is cosmetic or a design choice and lower-risk than everything already
shipped. None is a correctness defect.

## C4a — mock ImportServices does not materialize services

`platform.Mock.ImportServices` records the YAML + returns a canned `ImportResult`
but never appends the new services to `m.services`, so `TestIntegration_ImportThenDiscover`'s
discover step asserts the pre-seeded fixture (passes byte-identically if the import
call is deleted). Fix: materialize from `ImportResult.ServiceStacks` (ID+Name) behind
a `WithImportMaterializes` toggle (mirroring `WithDeleteRemovesService`), and flip the
integration test to expect 3 services incl. `web`. (C4b typed miss-path errors + C4c
Search sort/limit shipped; this is the remaining mock-fidelity gap.)

## C7 — test-only "orphan twin" wrappers

Three refactors left the pre-refactor entry point as a test-only 1-line delegation:
- `ops.Verify` / `ops.VerifyAll` → production uses `VerifyWithRuntimeMeta` / `VerifyAllWithRuntimeMeta`
- `workflow.HasSuccessfulDeploy` → production uses `HasSuccessfulDeployFor`
- `workflow.AutoCloseProgressFor` → production uses `AutoCloseProgressOf`

The clean-code rule forbids internal back-compat shims. Fix: repoint the tests at the
`*WithRuntimeMeta`/`*For`/`*Of` successors and delete the wrappers. Deferred because
repointing requires constructing the successor args in each test (behavior-preserving
but touches several test files) — low value, do it next time those areas are edited.

## CreateOpts — accepted, documented, ignored (delete-or-wire)

`platform.CreateOpts{Location, Tags}` is passed by the launch caller
(`Location: input.Region`, prod tags) but `CreateAndImportProject` does `_ = opts`.
This is a genuine design choice, NOT a clean deletion: the caller passes a region that
is currently dropped. Either (a) WIRE it — actually apply Location/Tags at project
creation (if the region is not already in the import YAML, this is a latent bug worth
fixing), or (b) DELETE CreateOpts and confirm the region rides in the YAML. Needs a
look at whether the launch import YAML already carries the region before deciding —
flagged rather than risk silently dropping prod region config.

## Trigger to promote

Touch the relevant area (mock, verify/work-session, launch project creation) → fold the
matching item in then.
