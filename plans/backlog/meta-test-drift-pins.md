# Deferred meta-test hardening pins (from P5 audit-fixes, 2026-06-10)

Two class-prevention pins from the audit-fixes plan were deferred — they harden
existing lints rather than fix a defect, and are lower-risk than the P5 fixes they
would guard (which all shipped + carry their own tests).

## 1. description-drift lint: fold BinaryExpr / Ident descriptions

`internal/tools/description_drift_test.go::scanGoForDriftPatterns` only matches
`Description: <BasicLit>`. It silently skips `Description: desc` (Ident built by
concat — deploy_ssh.go, deploy_batch.go) and concatenated-literal BinaryExpr
descriptions (browser.go). The two deploy tools — highest drift risk — are exactly
the unscanned ones. Fix: in `stringLitValue`, fold `*ast.BinaryExpr` string concat;
for `Description: <Ident>` resolve the local assignment (or scan all top-level
string literals in registration funcs).

## 2. schema-action-enum vs dispatcher pin

Nothing pins that every action handled by `handleWorkflowAction`'s dispatch (the
switch cases + the pre-switch specials: dispatch-brief-atom, build-subagent-brief,
verify-subagent-dispatch, record-deploy) appears in the `Action` field's jsonschema
description — this is how `set-default-setup` drifted out (B18). Fix: an AST test
collecting the dispatched action strings and asserting each appears in the Action
tag (and vice-versa). Extend the existing `TestAtomLintAcceptedActionsMatchDispatcher`
pattern to cover the jsonschema text too.

## 3. FlexBool guard via ListTools

`TestInputStructsUseFlexBoolForBooleans` pins field TYPES, not the published schema.
Extend it (or add a sibling) to drive `ListTools` and assert every FlexBool field's
published property is `oneOf[boolean,string]` — this is what would have caught the
B4 zerops_workflow/browser drift structurally rather than by audit. (The two specific
tools are now pinned by TestWorkflowInputSchema_FlexBoolPublished; this generalizes it.)

## Trigger to promote

Next time `description_drift_test.go` or the workflow Action schema is touched, or a
new tool is added — fold these in then (they share the meta-test surface).
