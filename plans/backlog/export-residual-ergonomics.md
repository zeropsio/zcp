# Export + chain residual ergonomics (post-production-transition leftovers)

**Status:** open deferral — the explicitly-not-shipped tail of
`plans/production-transition-2026-06-10.md` (F8 shipped the compose-ready atom + intro/needs-setup
truth fixes; these remain).

1. **Export reads recorded setup names from the ledger** — export's probe still re-derives setup
   identity via the heuristic suffix cascade (`workflow_export_probe.go`) instead of reading
   `meta.PrimarySetupName/StageSetupName/ProdSetupName` (parallel-path with the launch cascade; a
   multi-setup yaml with non-conventional names errors out even when the meta knows the answer).
2. **EX-2 per-file error provenance** — bundle.errors merges import.yaml + zerops.yaml errors into
   one slice; tag each ValidationError with its source file.
3. **EX-3 strict-vs-lenient zerops.yaml validator seam** — deploy-time live validation never rejects
   what export's structure schema rejects; defects ship through the dev loop and surface at export.
   Validator-owner contract change; coordinate with the B2 lint work.
4. **EX-4 soften `meta.IsComplete()` export gate** — allow export when live discover proves the
   service real (verify Mode still resolves).
5. **LP-5 push-unsupported-mode blocker** — `meta.PushSourceCheckFor` at the top of
   `validateLaunchSourceControl`; emit `mode-unsupported-<host>` instead of unsatisfiable git-push
   guidance.
6. **LP-8/J4 adopt redirect carries scope** — service-not-bootstrapped Recovery.Args gains the
   dev/stage pair so the redirected adopt skips its pairing question (decide []string encoding in
   RecoveryHint.Args first).
7. **BI-2 leftovers** — build-integration: PushSourceCheckFor stage-half gate (parity with
   git-push-setup); omit `alternateWorkflowFiles` when setupMandatory. (Noop full recompute +
   needsGitPushSetup Recovery + drift warning shipped in F1.)
8. **J6 validation-failed cheap exit** — name the config-only path (edit → action=close → re-call).
9. **EX-6 classify-prompt trim** — auto-classify `IsClassifyInfrastructure` keys server-side to cut
   the 23.6 KB prompt.

**Promote when:** flow-eval retrospectives on the export/launch scenarios surface any of these as
recurring friction, or the next export-focused plan opens.
