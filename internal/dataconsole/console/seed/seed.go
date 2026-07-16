package seed

import (
	"context"
	"fmt"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// Service seeds one managed service with sample fixtures, dispatched by its
// Zerops service type (e.g. "postgresql", "valkey@7.2", "object-storage",
// "elasticsearch", "kafka" — the same raw type string ConnectionInfo/
// conformance.ServiceEntry carry) and its already-built ConnectionDescriptor.
// Dispatch mirrors cmd/dcseed's original switch (postgresql / mariadb+mysql
// / clickhouse / valkey / object-storage / elasticsearch / meilisearch /
// typesense / qdrant / kafka / nats), now keyed explicitly by svcType rather
// than by a descriptor field the caller might not have populated.
//
// opts.Namespace == "" reproduces the static, human-browsable dataset
// dcseed has always written, byte-for-byte. A non-empty opts.Namespace
// (which MUST satisfy ValidNamespace) prefixes every artifact name this
// call creates — see the per-family Name functions in namespace.go — so
// concurrent callers (or repeated conformance runs) never collide with the
// static dataset or with each other.
func Service(ctx context.Context, svcType string, desc provider.ConnectionDescriptor, opts Options) error {
	if opts.Namespace != "" && !ValidNamespace(opts.Namespace) {
		return fmt.Errorf("seed: invalid namespace %q: must match %s", opts.Namespace, validNamespacePattern.String())
	}
	base := provider.BaseType(svcType)
	switch conn := desc.(type) {
	case provider.SQLConn:
		switch base {
		case "postgresql":
			return seedPostgres(ctx, conn, opts)
		case "mariadb", "mysql":
			return seedMySQL(ctx, conn, opts)
		case "clickhouse":
			return seedClickhouse(ctx, conn, opts)
		}
	case provider.KVConn:
		return seedValkey(ctx, conn, opts)
	case provider.ObjectConn:
		return seedObject(ctx, conn, opts)
	case provider.DocumentConn:
		switch base {
		case "elasticsearch":
			return seedElastic(ctx, conn, opts)
		case "meilisearch":
			return seedMeili(ctx, conn, opts)
		case "typesense":
			return seedTypesense(ctx, conn, opts)
		case "qdrant":
			return seedQdrant(ctx, conn, opts)
		}
	case provider.StreamConn:
		switch base {
		case "kafka":
			return seedKafka(ctx, conn, opts)
		case "nats":
			return seedNats(ctx, conn, opts)
		}
	}
	return fmt.Errorf("seed: no seeder for service type %q (descriptor %T)", svcType, desc)
}

// Cleanup removes every artifact namespace owns for the given service, per
// family (see the per-family Owns functions in namespace.go). It refuses an
// empty namespace outright — Cleanup must never be able to target the
// static dataset — and is idempotent: calling it on a namespace with
// nothing left to remove (already clean, or never seeded) succeeds without
// error, which is what makes a "sweep my namespace before seeding" recovery
// step safe after an interrupted run.
func Cleanup(ctx context.Context, svcType string, desc provider.ConnectionDescriptor, namespace string) error {
	if namespace == "" {
		return fmt.Errorf("seed: Cleanup refuses an empty namespace (would target the static dataset)")
	}
	if !ValidNamespace(namespace) {
		return fmt.Errorf("seed: invalid namespace %q: must match %s", namespace, validNamespacePattern.String())
	}
	base := provider.BaseType(svcType)
	switch conn := desc.(type) {
	case provider.SQLConn:
		switch base {
		case "postgresql":
			return cleanupPostgres(ctx, conn, namespace)
		case "mariadb", "mysql":
			return cleanupMySQL(ctx, conn, namespace)
		case "clickhouse":
			return cleanupClickhouse(ctx, conn, namespace)
		}
	case provider.KVConn:
		return cleanupValkey(ctx, conn, namespace)
	case provider.ObjectConn:
		return cleanupObject(ctx, conn, namespace)
	case provider.DocumentConn:
		switch base {
		case "elasticsearch":
			return cleanupElastic(ctx, conn, namespace)
		case "meilisearch":
			return cleanupMeili(ctx, conn, namespace)
		case "typesense":
			return cleanupTypesense(ctx, conn, namespace)
		case "qdrant":
			return cleanupQdrant(ctx, conn, namespace)
		}
	case provider.StreamConn:
		switch base {
		case "kafka":
			return cleanupKafka(ctx, conn, namespace)
		case "nats":
			return cleanupNats(ctx, conn, namespace)
		}
	}
	return fmt.Errorf("seed: no cleanup for service type %q (descriptor %T)", svcType, desc)
}
