// Package seed is the Data Console's island-safe fixture/seeder library: it
// writes (Service) and tears down (Cleanup) sample data across every family
// provider's engine (tabular, kv, object, document, stream), descriptor-
// driven exactly like the rest of console/ — no Zerops REST discovery, no
// core import (plans/dataconsole-excellence-program-2026-07-16.md §4 S10b
// names the slice).
//
// # Island
//
// This package imports ONLY stdlib + internal/dataconsole/console/provider
// (for the ConnectionDescriptor/Family/BaseType vocabulary) + the same
// already-vendored third-party drivers the rest of console/ uses (pgx,
// go-sql-driver/mysql, ClickHouse/clickhouse-go, redis/go-redis,
// minio/minio-go, segmentio/kafka-go, nats-io/nats.go) — never zcpadapter,
// auth, ops, or platform. It dials engines directly with those drivers, the
// same way cmd/dcseed always has (seeding/cleanup is setup/teardown
// plumbing, not a family provider's read/write surface, so it does not go
// through the provider.*Provider interfaces). Pinned alongside the rest of
// console/ by TestDataConsoleBoundary_CoreIsolated + depguard
// dataconsole-core-isolated.
//
// # Static vs namespaced
//
// Every exported seed/cleanup entry point is namespace-parameterized via
// Options.Namespace:
//
//   - Empty Namespace ("static mode") reproduces the exact fixture set
//     cmd/dcseed has always written — same table/key/object/index/topic
//     names, byte-for-byte. cmd/dcseed is the only intended caller of
//     static mode; it is the human-browsable dataset a developer opens the
//     console against.
//   - Non-empty Namespace prefixes every artifact this package creates —
//     see the per-family Name functions in namespace.go for the exact,
//     documented rule per family (tabular/document/stream join with "_",
//     kv joins with ":" matching the provider's own keyspace-tree
//     convention, object joins with "/" matching S3 prefix semantics).
//     Cleanup(ctx, type, desc, namespace) removes everything a namespace
//     owns and is idempotent — safe to call when nothing exists — but
//     refuses an empty namespace outright, so a caller can never
//     accidentally target the static dataset.
//
// The intended consumer of namespaced mode is
// internal/dataconsole/console/provider/conformance: each live run seeds
// its own namespace, asserts against it, and tears it down, and sweeps
// (Cleanup) that namespace at suite start as a recovery step after an
// interrupted prior run — see conformance/doc.go and conformance/harness_test.go.
package seed
