# App-code substep — completion predicate

The app-code substep counts as complete when:

- Every tier-matching artifact named in the tier atom (`execution-order-minimal` for minimal, `dashboard-skeleton-showcase` for showcase) is present on the mount.
- Service client initialization code reads env-var names exactly as declared in `plan.SymbolContract.EnvVarsByKind` for the managed services in the plan — no drift between what the code references and the platform-provided names.
- HTTP route paths match `plan.SymbolContract.HTTPRoutes` byte-for-byte.
- No feature-section code has landed for showcase (that belongs to the feature sub-agent at deploy); for minimal the inline feature code matches the tier's shape table.
- READMEs have not been drafted at this phase — they belong to the writer sub-agent at the deploy step.

Attest only when these predicates hold across every codebase mount.
