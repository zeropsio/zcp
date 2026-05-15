# Regression goldens — IMMUTABLE

These yaml files capture the **current observable output** of
`BuildLaunchBundle` (launch composer) and `BuildBundle` (export
composer) for known fixture inputs. They serve as **regression
detectors**: any intentional or accidental change to either composer
that alters this output is surfaced as a diff in the corresponding
test.

## Update policy

- **Do not edit by hand.** Run the test with `ZCP_GOLDEN_UPDATE=1` and
  review the diff in the PR before committing.
- **Intentional behavior change** (Phase 2 F19/F20/F21 fix, Phase 1b
  composer rewrite, etc.) requires updating the corresponding
  golden(s) AS PART OF the same PR — the diff is the review surface.
- **Accidental drift** (regression in unrelated code) surfaces as a
  test failure; investigate root cause before regenerating.

## Files

| Fixture | Captures |
|---|---|
| `launch-standard-pair.yaml` | Launch from dev/stage standard pair (app + db postgres) |
| `launch-dev-only.yaml` | Launch from single dev-mode runtime (api + db postgres) |
| `launch-recipe-nodejs.yaml` | Launch from Node recipe (app + db + redis) |
| `launch-recipe-laravel.yaml` | Launch from Laravel-style stack (app + db + redis + storage object-storage) — exercises F20/F21 surface |
| `export-dev.yaml` | Export of dev half from standard pair |
| `export-stage.yaml` | Export of stage half from standard pair |

## Update workflow

```
ZCP_GOLDEN_UPDATE=1 go test ./internal/ops/... -run TestLaunchGolden -count=1
ZCP_GOLDEN_UPDATE=1 go test ./internal/ops/... -run TestExportGolden -count=1
git diff -- internal/ops/testdata/goldens/regression/
# Review every line; commit if expected.
```

When the v1b composer redesign moves the package to `internal/ops/bundle/`
(see plans/workflow-family-architecture-2026-05-14.md §11 Phase 1b),
these goldens move alongside the tests.
