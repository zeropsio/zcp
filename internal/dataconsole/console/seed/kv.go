package seed

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

const (
	kvSeedTimeout    = 15 * time.Second
	kvCleanupTimeout = 15 * time.Second
	kvScanCount      = 200
)

// seedValkey writes the sample keyspace: a couple of plain strings (one
// with a TTL, mirroring session-token shapes), two hashes, a list, a set,
// and a sorted set — one representative per KV collection kind the console
// renders. Every key name is namespace-derived via KVName so an empty
// Namespace reproduces dcseed's original, unprefixed key names exactly.
func seedValkey(ctx context.Context, conn provider.KVConn, opts Options) error {
	cli := redis.NewClient(&redis.Options{Addr: net.JoinHostPort(conn.Host, conn.Port), Password: conn.Password})
	defer cli.Close()
	cx, cancel := context.WithTimeout(ctx, kvSeedTimeout)
	defer cancel()
	if err := cli.Ping(cx).Err(); err != nil {
		return fmt.Errorf("seed valkey: ping: %w", err)
	}

	n := func(base string) string { return KVName(opts.Namespace, base) }

	if err := cli.Set(cx, n("greeting"), "hello from seed", 0).Err(); err != nil {
		return fmt.Errorf("seed valkey: set %s: %w", n("greeting"), err)
	}
	if err := cli.Set(cx, n("session:abc123"), "user-1", time.Hour).Err(); err != nil {
		return fmt.Errorf("seed valkey: set %s: %w", n("session:abc123"), err)
	}
	if err := cli.Set(cx, n("session:def456"), "user-2", 30*time.Minute).Err(); err != nil {
		return fmt.Errorf("seed valkey: set %s: %w", n("session:def456"), err)
	}
	if err := cli.HSet(cx, n("user:1"), "name", "Alice", "email", "alice@example.io", "role", "admin").Err(); err != nil {
		return fmt.Errorf("seed valkey: hset %s: %w", n("user:1"), err)
	}
	if err := cli.HSet(cx, n("user:2"), "name", "Bob", "email", "bob@example.io", "role", "member").Err(); err != nil {
		return fmt.Errorf("seed valkey: hset %s: %w", n("user:2"), err)
	}
	if err := cli.RPush(cx, n("queue:jobs"), "job-1", "job-2", "job-3").Err(); err != nil {
		return fmt.Errorf("seed valkey: rpush %s: %w", n("queue:jobs"), err)
	}
	if err := cli.SAdd(cx, n("tags"), "go", "zerops", "data", "console").Err(); err != nil {
		return fmt.Errorf("seed valkey: sadd %s: %w", n("tags"), err)
	}
	if err := cli.ZAdd(cx, n("leaderboard"),
		redis.Z{Score: 100, Member: "alice"}, redis.Z{Score: 250, Member: "bob"}, redis.Z{Score: 175, Member: "carol"},
	).Err(); err != nil {
		return fmt.Errorf("seed valkey: zadd %s: %w", n("leaderboard"), err)
	}
	return nil
}

// cleanupValkey SCANs the keyspace for everything under "<namespace>:*"
// (the exact shape KVName produces) and deletes it in one batch. SCAN MATCH
// already restricts the walk to the namespace's own keys; KVOwns is
// applied again as a defense-in-depth filter so a MATCH/glob mismatch can
// never turn into an unintended deletion.
func cleanupValkey(ctx context.Context, conn provider.KVConn, namespace string) error {
	cli := redis.NewClient(&redis.Options{Addr: net.JoinHostPort(conn.Host, conn.Port), Password: conn.Password})
	defer cli.Close()
	cx, cancel := context.WithTimeout(ctx, kvCleanupTimeout)
	defer cancel()

	match := namespace + ":*"
	var cursor uint64
	var keys []string
	for {
		batch, next, err := cli.Scan(cx, cursor, match, kvScanCount).Result()
		if err != nil {
			return fmt.Errorf("seed cleanup valkey: scan: %w", err)
		}
		for _, k := range batch {
			if KVOwns(namespace, k) {
				keys = append(keys, k)
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	if len(keys) == 0 {
		return nil
	}
	if err := cli.Del(cx, keys...).Err(); err != nil {
		return fmt.Errorf("seed cleanup valkey: del: %w", err)
	}
	return nil
}
