# Deploy Per-Target Tracking — Superseded

**Date**: 2026-03-21
**Status**: SUPERSEDED by `analysis-deploy-workflow-redesign.md`

---

The per-target tracking cleanup (delete dead UpdateTarget/DevFailed/Error/LastAttestation) is now Phase 1 of the broader deploy workflow redesign. The dead code deletion is unchanged, but the dev→stage gating and checker work are part of a larger effort to bring the standalone deploy workflow to production quality.

See: `plans/analysis-deploy-workflow-redesign.md`
