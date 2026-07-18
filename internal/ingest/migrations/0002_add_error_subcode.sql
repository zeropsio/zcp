-- Adds error_subcode (docs/spec-telemetry.md §4.2, telemetry-production-
-- readiness plan S4): a stable, more diagnosable narrowing of error_code,
-- populated at the worst INVALID_PARAMETER/API_ERROR conflations first.
-- Positioned right after error_code to mirror the wire/schema field order.
-- IF NOT EXISTS makes this migration's own DDL idempotent (spec §8
-- authoring convention) on top of the runner's applied-id bookkeeping.
ALTER TABLE __DB__.events ADD COLUMN IF NOT EXISTS error_subcode LowCardinality(String) AFTER error_code;
