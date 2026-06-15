package recipe

import (
	"strings"
	"testing"
)

// TestGateCacheConsumerWiresPassword pins the positive cache-auth gate:
// a codebase that consumes the Valkey/redis-family cache service MUST wire
// the password (${..._password}) OR the full connection string
// (${..._connectionString}). Valkey on Zerops ENFORCES auth (requirepass on
// the `default` ACL user; a bare unauthenticated PING returns "NOAUTH
// Authentication required"), so omitting both ships a NOAUTH-broken
// connection. Replaces the inverted run-48 "ioredis-auth-against-unauth-
// valkey" self-inflicted-reversible gate. Live-verified 2026-06-14 against
// valkey:single@7.2.
func TestGateCacheConsumerWiresPassword(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		hostname     string
		yamlFragment string
		wantRefusal  bool
		wantMatch    string
	}{
		{
			name:     "consumes cache, omits password + connectionString (REFUSE)",
			hostname: "worker",
			yamlFragment: "zerops:\n  - setup: worker\n    run:\n      envVariables:\n" +
				"        CACHE_HOST: ${cache_hostname}\n        CACHE_PORT: ${cache_port}\n",
			wantRefusal: true,
			wantMatch:   "NOAUTH",
		},
		{
			name:     "consumes cache, wires ${cache_password} (ACCEPT)",
			hostname: "worker",
			yamlFragment: "zerops:\n  - setup: worker\n    run:\n      envVariables:\n" +
				"        CACHE_HOST: ${cache_hostname}\n        CACHE_PORT: ${cache_port}\n        CACHE_PASSWORD: ${cache_password}\n",
			wantRefusal: false,
		},
		{
			name:         "consumes cache via full connectionString (ACCEPT)",
			hostname:     "api",
			yamlFragment: "zerops:\n  - setup: api\n    run:\n      envVariables:\n        REDIS_URL: ${cache_connectionString}\n",
			wantRefusal:  false,
		},
		{
			name:         "does not consume cache (ACCEPT — no false positive)",
			hostname:     "api",
			yamlFragment: "zerops:\n  - setup: api\n    run:\n      envVariables:\n        DB_URL: ${db_connectionString}\n",
			wantRefusal:  false,
		},
		{
			name:     "valkey_password variant alias (ACCEPT)",
			hostname: "api",
			yamlFragment: "zerops:\n  - setup: api\n    run:\n      envVariables:\n" +
				"        REDIS_HOST: ${valkey_hostname}\n        REDIS_PASSWORD: ${valkey_password}\n",
			wantRefusal: false,
		},
		{
			name:     "password only in yaml comment is NOT wired (REFUSE)",
			hostname: "worker",
			yamlFragment: "zerops:\n  - setup: worker\n    run:\n      envVariables:\n" +
				"        # set CACHE_PASSWORD: ${cache_password} if you need auth\n" +
				"        CACHE_HOST: ${cache_hostname}\n        CACHE_PORT: ${cache_port}\n",
			wantRefusal: true,
			wantMatch:   "NOAUTH",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plan := &Plan{
				Codebases: []Codebase{{Hostname: tc.hostname, SourceRoot: tc.hostname}},
				Fragments: map[string]string{},
			}
			if tc.yamlFragment != "" {
				plan.Fragments["codebase/"+tc.hostname+"/zerops-yaml"] = tc.yamlFragment
			}
			vs := gateCacheConsumerWiresPassword(GateContext{Plan: plan})
			got := len(vs) > 0
			if got != tc.wantRefusal {
				t.Fatalf("refusal = %v, want %v (violations: %+v)", got, tc.wantRefusal, vs)
			}
			if tc.wantRefusal && tc.wantMatch != "" {
				found := false
				for _, v := range vs {
					if strings.Contains(v.Message, tc.wantMatch) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("no violation message contains %q; got %+v", tc.wantMatch, vs)
				}
			}
		})
	}
}
