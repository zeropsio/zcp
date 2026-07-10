# sync pull: per-item fetch error exits 0 and masks itself until a test fails

**Evidence:** Release CI for v9.125.0 (run 29091169749, 2026-07-10). The test
job's `go run ./cmd/zcp sync pull` step hit a transient GitHub read failure:

```
ERROR zerops-yaml-advanced: read from GitHub: read file apps/docs/content/guides/zerops-yaml-advanced.mdx: exit status 1
```

The pull continued past the error and the STEP exited 0, so the failure
surfaced two steps later as `TestStore_GuidesEmbedded` failing on a missing
guide — a confusing symptom far from the cause (the lint job's pull of the
same guide, one minute earlier, succeeded — pure flake). Cost: one failed
release tag (v9.125.0 re-shipped as v9.125.1 with identical content).

**Root cause:** per-item error tolerance in `zcp sync pull` prints ERROR and
keeps exit 0 — a masking fallback (Information Contract: no masking
fallbacks). Reasonable interactively; wrong for CI where a partial corpus is
never acceptable.

**Sketch:** `sync pull` collects per-item errors and exits non-zero if any
occurred (or add `--strict` used by the release workflow). Optionally a
bounded retry per item for transient GitHub reads. Either way the release
workflow's pull step must FAIL FAST, so the job stops at "pull failed", not
at an embed test.

**Trigger to promote:** next touch of `internal/sync` / the release workflow,
or a second flake-caused release failure.
