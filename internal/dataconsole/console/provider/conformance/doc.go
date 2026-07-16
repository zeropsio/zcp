// Package conformance is the Data Console's live data-plane conformance
// harness: a mutating suite that drives every family provider (tabular, kv,
// object, document, stream) against REAL managed-service engines — the audit
// + regression instrument that proves capability is EARNED, never asserted
// (plans/dataconsole-excellence-program-2026-07-16.md §4 S10a names the
// slice; the durable tier rule this package follows lives at
// docs/spec-testing-architecture.md §2).
//
// # Island
//
// This package imports ONLY stdlib + internal/dataconsole/console/provider
// (+ its family subpackages, + the third-party drivers they already vendor:
// minio-go, pgx, the mysql/clickhouse drivers, go-redis, nats.go, kafka-go).
// It never imports zcpadapter, auth, ops, or platform — providers are built
// the same way cmd/zcp/studio_console.go's factories build them, replicated
// here so the island stays core-free. Pinned alongside the rest of console/
// by TestDataConsoleBoundary_CoreIsolated + depguard dataconsole-core-isolated.
//
// # Build tag
//
// Every file that dials a live engine carries `//go:build e2e`. config.go and
// summary.go are untagged (plain package code — the loader + the run-summary
// recorder are usable, and unit-tested, without a build tag), and so is
// config_test.go, which pins the loader + profile/manifest decision logic
// OFFLINE, against fake descriptors, with no network. `go vet -tags e2e
// ./...` (the `vet-tags` Makefile target) already compile-checks the tagged
// files, so a signature change here cannot rot silently between live runs —
// see the docs/spec-testing-architecture.md §2 amendment for why the e2e tag
// covers this data-plane suite alongside the control-plane one.
//
// # Config
//
// Nothing here talks to the Zerops REST API to discover services — that
// would be a core import. Instead DC_LIVE_CONFIG points at a gitignored JSON
// file naming the live services to drive, keyed by hostname + Zerops service
// type (classified via provider.Classify, exactly like the real adapter) + a
// family-specific descriptor block matching that classification — see
// config.go for the exact field set. Example:
//
//	{
//	  "services": [
//	    {"hostname": "db", "type": "postgresql",
//	     "tabular": {"host": "db", "port": "5432", "user": "postgres", "password": "...", "database": "db"}},
//	    {"hostname": "cache", "type": "valkey",
//	     "kv": {"host": "cache", "port": "6379", "password": "..."}},
//	    {"hostname": "storage", "type": "object-storage",
//	     "object": {"endpoint": "storage.zerops.io", "accessKey": "...", "secretKey": "...", "bucket": "...", "secure": true}},
//	    {"hostname": "es", "type": "elasticsearch",
//	     "document": {"baseUrl": "http://es:9200", "user": "elastic", "apiKey": "..."}},
//	    {"hostname": "queue", "type": "kafka",
//	     "stream": {"host": "queue", "port": "9092"}}
//	  ]
//	}
//
// DC_LIVE_PROFILE selects the run's strictness (default "partial" when unset):
//
//   - partial — a service missing from DC_LIVE_CONFIG, unreachable, or with
//     nothing to read SKIPS with a logged reason; the run stays green. For
//     local dev against whatever subset of the testbed happens to be up.
//   - full — every hostname listed in DC_LIVE_MANIFEST (comma-separated) MUST
//     be present in DC_LIVE_CONFIG, reachable, and pass its case, or the run
//     FAILS. The release-gate shape (the S24 close-out pin runs this against
//     the release manifest).
//
// TestMain always prints a run summary (pass/skip/fail per hostname) after
// m.Run(), independent of -v, so a partial-profile skip is never silent.
//
// # Fixtures (S10b)
//
// The kv, document, and stream smoke cases no longer depend on cmd/dcseed
// having run out-of-band: each seeds its own fixture via
// internal/dataconsole/console/seed (the island-safe fixture/seeder
// library), under a namespace so it can never collide with — or need to be
// cleaned up alongside — the static, human-browsable dataset dcseed itself
// writes. DC_LIVE_NAMESPACE selects that namespace (default "dcconf",
// deliberately STABLE across runs, not randomized — see config.go's
// DefaultNamespace and harness_test.go's TestMain). At startup, before any
// test runs, TestMain sweeps (deletes) every configured service's
// activeNamespace fixtures — recovery after an interrupted prior run left
// partial fixtures behind; a namespace with nothing to sweep is a fast
// no-op (seed.Cleanup is idempotent by contract). Each smoke test then
// seeds fresh fixtures and tears them down (deferred) after asserting.
// tabular's smoke case stays catalog-based (information_schema and
// equivalents are always present, no seeding needed); object's case keeps
// its own pre-existing self-seeding "_conformance/" pattern rather than
// switching to the namespace convention (self-seed-and-delete-by-exact-path
// already proves the write surface directly, which is that case's point).
//
// # Retry policy
//
// Reachability/setup (Health, the initial dial) may retry briefly — VPN or
// dial jitter is not a finding. Semantic assertions (List/ReadTable/ReadBlob
// content, row counts, version strings) never retry: a flaky assertion is a
// real finding, not noise to paper over.
//
// # Runbook
//
// From a machine with zcli + VPN access to the project (see the zerops MCP
// server's adopted-project instructions for the project id):
//
//	zcli vpn up <projectId>              # bring up the private network
//	# start any STOPPED managed service this run needs (e.g. postgresql)
//	go run ./cmd/dcseed                  # seed the static, human-browsable dataset (tabular needs it; kv/document/stream self-seed per-run)
//	DC_LIVE_CONFIG=/path/to/live-config.json \
//	  go test -tags e2e -count=1 ./internal/dataconsole/console/provider/conformance/
//
// Add DC_LIVE_PROFILE=full DC_LIVE_MANIFEST=db,cache,storage,es,queue (the
// release manifest) for the release-gate run. `make dc-live` / `make
// dc-live-full` wrap these two shapes.
package conformance
