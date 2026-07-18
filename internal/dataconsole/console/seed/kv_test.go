package seed

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// kvFixtureBases mirrors dcseed's seedValkey fixture set — every base name
// it writes under the kv family (before namespacing).
var kvFixtureBases = []string{
	"greeting", "session:abc123", "session:def456",
	"user:1", "user:2", "queue:jobs", "tags", "leaderboard",
}

// TestKV_Seed_Cleanup_RealEngine is the real-engine offline test: miniredis
// is an in-memory Valkey/Redis implementation (already vendored for the kv
// provider's own contract suite, provider/kv/kv_redis_test.go), so this
// case seeds a namespace into a REAL engine, asserts the artifacts exist
// with the expected shapes, tears down via Cleanup, and asserts they are
// gone — all offline, no network, no build tag.
func TestKV_Seed_Cleanup_RealEngine(t *testing.T) {
	t.Parallel()
	mr := miniredis.RunT(t)
	desc := provider.KVConn{Host: mr.Host(), Port: mr.Port()}
	ctx := context.Background()
	const namespace = "dcconf_test"

	if err := Service(ctx, "valkey", desc, Options{Namespace: namespace}); err != nil {
		t.Fatalf("Service (seed): %v", err)
	}

	for _, base := range kvFixtureBases {
		key := KVName(namespace, base)
		if !mr.Exists(key) {
			t.Errorf("expected namespaced key %q to exist after seed", key)
		}
	}
	if got, err := mr.Get(KVName(namespace, "greeting")); err != nil || got != "hello from seed" {
		t.Errorf("greeting value = %q, err=%v, want %q, nil", got, err, "hello from seed")
	}
	hkeys, err := mr.HKeys(KVName(namespace, "user:1"))
	if err != nil || len(hkeys) == 0 {
		t.Errorf("HKeys(%q) = %v, %v — want a populated hash", KVName(namespace, "user:1"), hkeys, err)
	}
	if ttl := mr.TTL(KVName(namespace, "session:abc123")); ttl <= 0 {
		t.Errorf("TTL(%q) = %v, want > 0 (seedValkey sets an hour TTL)", KVName(namespace, "session:abc123"), ttl)
	}

	// Seeding a namespace must never touch the unnamespaced static dataset.
	if mr.Exists("greeting") {
		t.Error("unnamespaced \"greeting\" must not exist — a namespaced Seed must never touch the static dataset")
	}

	if err := Cleanup(ctx, "valkey", desc, namespace); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	for _, base := range kvFixtureBases {
		key := KVName(namespace, base)
		if mr.Exists(key) {
			t.Errorf("expected namespaced key %q to be gone after Cleanup", key)
		}
	}

	// Cleanup must be idempotent — safe to call again when nothing remains.
	if err := Cleanup(ctx, "valkey", desc, namespace); err != nil {
		t.Errorf("Cleanup on an already-clean namespace returned an error: %v", err)
	}
}

// TestKV_Seed_StaticNamespace_ReproducesFixtures pins byte-identical static
// behavior: Options{} (empty Namespace) must write exactly the unprefixed
// keys dcseed has always written.
func TestKV_Seed_StaticNamespace_ReproducesFixtures(t *testing.T) {
	t.Parallel()
	mr := miniredis.RunT(t)
	desc := provider.KVConn{Host: mr.Host(), Port: mr.Port()}
	ctx := context.Background()

	if err := Service(ctx, "valkey", desc, Options{}); err != nil {
		t.Fatalf("Service (seed, static): %v", err)
	}
	for _, base := range kvFixtureBases {
		if !mr.Exists(base) {
			t.Errorf("expected static key %q to exist", base)
		}
	}
}

// TestCleanup_RefusesEmptyNamespace pins the safety invariant: Cleanup must
// never target the static dataset, so an empty namespace is a hard error,
// never a silent no-op or (worse) a mass sweep of unprefixed keys.
func TestCleanup_RefusesEmptyNamespace(t *testing.T) {
	t.Parallel()
	mr := miniredis.RunT(t)
	desc := provider.KVConn{Host: mr.Host(), Port: mr.Port()}
	mr.Set("greeting", "hello from seed") //nolint:errcheck // miniredis Set never fails

	if err := Cleanup(context.Background(), "valkey", desc, ""); err == nil {
		t.Fatal("Cleanup with empty namespace: want error, got nil")
	}
	if !mr.Exists("greeting") {
		t.Fatal("Cleanup with empty namespace must never touch the static dataset, even when it errors")
	}
}

// TestCleanup_IdempotentOnNamespaceThatNeverSeeded pins the "safe to run
// when nothing exists" contract independent of any prior Seed call — the
// recovery-sweep case (S10a/S10b): a conformance run sweeping its namespace
// before seeding must not fail just because nothing was ever created.
func TestCleanup_IdempotentOnNamespaceThatNeverSeeded(t *testing.T) {
	t.Parallel()
	mr := miniredis.RunT(t)
	desc := provider.KVConn{Host: mr.Host(), Port: mr.Port()}

	if err := Cleanup(context.Background(), "valkey", desc, "dcconf_never_seeded"); err != nil {
		t.Fatalf("Cleanup on a namespace that never seeded: %v", err)
	}
}

// TestKV_Seed_ConvergesPreexistingWrongType pins the live-shakeout fix
// (2026-07-16, VPN run against zcp-eval-clean): a fixture key that already
// exists on the engine with a DIFFERENT Redis type — e.g. left over from an
// older/legacy seeder layout — must converge to this run's canonical typed
// value, never fail the typed write with WRONGTYPE. The live failure was
// exactly "hset user:1: WRONGTYPE Operation against a key holding the wrong
// kind of value" because user:1 pre-existed as a String where seedValkey
// wants a Hash.
func TestKV_Seed_ConvergesPreexistingWrongType(t *testing.T) {
	t.Parallel()
	mr := miniredis.RunT(t)
	desc := provider.KVConn{Host: mr.Host(), Port: mr.Port()}
	ctx := context.Background()

	// user:1 is normally a Hash; pre-create it as a String — the exact
	// WRONGTYPE shape the live shakeout hit.
	if err := mr.Set("user:1", "legacy-string-value"); err != nil {
		t.Fatalf("pre-seed user:1 as string: %v", err)
	}
	// queue:jobs is normally a List; pre-create it as a String too, so the
	// fix is pinned across more than one collection type (Hash and List).
	if err := mr.Set("queue:jobs", "legacy-string-value"); err != nil {
		t.Fatalf("pre-seed queue:jobs as string: %v", err)
	}

	if err := Service(ctx, "valkey", desc, Options{}); err != nil {
		t.Fatalf("Service (seed, static): %v", err)
	}

	if got := mr.Type("user:1"); got != "hash" {
		t.Errorf("Type(user:1) = %q, want %q after convergence", got, "hash")
	}
	hkeys, err := mr.HKeys("user:1")
	if err != nil || len(hkeys) == 0 {
		t.Errorf("HKeys(user:1) = %v, %v — want a populated hash (name/email/role) after convergence", hkeys, err)
	}

	if got := mr.Type("queue:jobs"); got != "list" {
		t.Errorf("Type(queue:jobs) = %q, want %q after convergence", got, "list")
	}
	list, err := mr.List("queue:jobs")
	if err != nil || len(list) == 0 {
		t.Errorf("List(queue:jobs) = %v, %v — want a populated list after convergence", list, err)
	}
}
