package recipe

import (
	"fmt"
	"path/filepath"
	"regexp"
)

// Valkey/redis-family on Zerops ENFORCES auth. requirepass is set on the
// `default` ACL user, so an unauthenticated connection — even a bare PING —
// returns "NOAUTH Authentication required". The managed service injects a
// `password` secret and a full `connectionString`
// (redis://default:${password}@<host>.zerops:6379). A recipe codebase that
// consumes the cache MUST therefore wire the password: either
// ${<host>_password} as a discrete option, OR the full
// ${<host>_connectionString} (which already carries default:${password}).
// Omitting both ships a connection that fails at runtime with NOAUTH.
//
// This gate REPLACES the run-48 "ioredis-auth-against-unauth-valkey"
// self-inflicted-reversible pattern, which had the platform fact exactly
// inverted: it treated the password-less wiring as the recipe's correct
// shipped state and refused KB content warning about it. There is no
// self-inflicted-reversible case here — auth is mandatory, so a missing
// password is a real deploy-breaking defect, not a porter-reversible trap.
// Live-verified 2026-06-14 against valkey:single@7.2 (cross-service
// connect returned NOAUTH without the injected password).
var (
	// cacheServiceRefRE — the codebase consumes the cache service: any
	// ${cache_*}/${valkey_*}/${redis_*} cross-service env reference.
	cacheServiceRefRE = regexp.MustCompile(`\$\{(?:cache|valkey|redis)\w*_\w+\}`)
	// cachePasswordRefRE — the password secret is wired as a discrete
	// option (${cache_password} / ${valkey_password} / ${redis_password}).
	cachePasswordRefRE = regexp.MustCompile(`\$\{(?:cache|valkey|redis)\w*_password\}`)
	// cacheConnStringRefRE — the full authed connection string is wired
	// (it embeds default:${password}, so it satisfies the auth need on
	// its own).
	cacheConnStringRefRE = regexp.MustCompile(`\$\{(?:cache|valkey|redis)\w*_connectionString\}`)
)

// gateCacheConsumerWiresPassword refuses codebase-content complete-phase
// when a codebase consumes the cache (Valkey/redis-family) service but
// wires NEITHER the password NOR the full connection string. Severity:
// blocking — the omission ships a connection that fails at runtime with
// NOAUTH. Comment-stripped so a password mentioned only in a yaml comment
// does not count as wired. Pinned by TestGateCacheConsumerWiresPassword.
func gateCacheConsumerWiresPassword(ctx GateContext) []Violation {
	if ctx.Plan == nil {
		return nil
	}
	var out []Violation
	for _, cb := range ctx.Plan.Codebases {
		if cb.Hostname == "" {
			continue
		}
		yaml := stripAllYAMLComments(codebaseYAMLBody(ctx.Plan, cb.Hostname))
		if !cacheServiceRefRE.MatchString(yaml) {
			continue // codebase doesn't consume the cache service
		}
		if cachePasswordRefRE.MatchString(yaml) || cacheConnStringRefRE.MatchString(yaml) {
			continue // password OR full connection string wired — auth satisfied
		}
		out = append(out, Violation{
			Code:     "cache-consumer-missing-password",
			Path:     filepath.Join(cb.SourceRoot, "zerops.yaml"),
			Severity: SeverityBlocking,
			Message: fmt.Sprintf(
				"codebase/%s consumes the cache (Valkey/redis-family) service but wires neither ${..._password} nor ${..._connectionString} in its zerops.yaml. Valkey on Zerops REQUIRES auth (requirepass on the `default` ACL user); an unauthenticated connection fails at runtime with `NOAUTH Authentication required`. Wire ${<cache-host>_password} as a discrete password option, or pass the full ${<cache-host>_connectionString} (redis://default:${password}@<host>.zerops:6379). There is no separate ${<cache-host>_user} — the user is always `default`.",
				cb.Hostname,
			),
		})
	}
	return out
}
