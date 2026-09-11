package farm

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// farmServiceHostname / osServiceHostname name the two services a fresh
// session's ZCP_FARM_* configuration resolves from (docs/spec-eval-farm.md
// §3.1 FM-17): the farm host's own "farm" service carries the ZCP_FARM_*
// values — some of them literal "${os_<key>}" references, unresolved
// because they were read via the API rather than substituted by the
// platform into a running container's own environment — and the
// referenced values live on the "os" object-storage service, both in the
// same project (ZCP_FARM_PROJECT_ID).
const (
	farmServiceHostname = "farm"
	osServiceHostname   = "os"
)

// farmKeyPrefix is the only prefix EnvResolver ever resolves from the farm
// service's env. ZCP_FARM_ACCOUNT_TOKEN and ZCP_FARM_PROJECT_ID share the
// prefix but are never looked up this way — Lookup never reaches the farm
// service for either since the base lookup (the environment) already
// answers them: the token is the credential that reads the farm service,
// and the project id is what names which project to read it from.
const farmKeyPrefix = "ZCP_FARM_"

// osRefPrefix/osRefSuffix delimit the one reference shape a ZCP_FARM_*
// value may carry: "${os_<key>}", naming a key on the os service's own
// env. Any other reference is refused, naming the ZCP_FARM_* key — never
// the value itself, which stays out of every error string.
const (
	osRefPrefix = "${os_"
	osRefSuffix = "}"
)

// EnvResolver resolves ZCP_FARM_* keys missing from a base lookup by
// reading the farm service's env in one Zerops project, following a
// "${os_<key>}" value to the os service's own env (docs/spec-eval-farm.md
// §3.1 FM-17). It is built once per `zcp eval farm` invocation
// (cmd/zcp/eval_farm.go) and fetches each service's env at most once — the
// first missing key pays for the fetch(es), every later key reads the
// cached map.
//
// A resolution failure (the farm/os service lookup, the env fetch, or an
// unsupported reference) is recorded and returned by Err; once set, Lookup
// answers "" for every further key rather than pressing on with a
// half-resolved config.
type EnvResolver struct {
	ctx       context.Context //nolint:containedctx // Lookup's signature is fixed at func(string) string (the farm.ConfigFromLookup overlay contract) — no room for a ctx parameter, so it is fixed at construction, same precedent as observer.SinkBundle
	client    platform.Client
	projectID string
	base      func(string) string

	farmFetched bool
	farmEnv     map[string]string
	osFetched   bool
	osEnv       map[string]string
	err         error
}

// NewEnvResolver builds a resolver over client (an account client able to
// read projectID — the ZCP_FARM_PROJECT_ID project), with base as the
// environment-first fallback. client is only ever touched once Lookup sees
// a ZCP_FARM_* key base doesn't already answer AND projectID is non-empty
// — a nil client with an empty projectID (docs/spec-eval-farm.md §3.1:
// "with neither, today's error messages stay") is safe.
func NewEnvResolver(ctx context.Context, client platform.Client, projectID string, base func(string) string) *EnvResolver {
	return &EnvResolver{ctx: ctx, client: client, projectID: projectID, base: base}
}

// Lookup answers key from base first; only for a ZCP_FARM_* key base
// doesn't answer, and only once projectID is non-empty, it resolves the
// key from the farm service's env. See the EnvResolver doc comment for the
// once-failed-stays-failed rule.
func (r *EnvResolver) Lookup(key string) string {
	if v := r.base(key); v != "" {
		return v
	}
	if r.err != nil || r.projectID == "" || !strings.HasPrefix(key, farmKeyPrefix) {
		return ""
	}
	if err := r.ensureFarmFetched(); err != nil {
		r.err = err
		return ""
	}
	raw, ok := r.farmEnv[key]
	if !ok {
		return ""
	}
	resolved, err := r.resolveValue(key, raw)
	if err != nil {
		r.err = err
		return ""
	}
	return resolved
}

// Err returns the first resolution failure Lookup recorded, or nil. Its
// message never carries an env var's value (CLAUDE.md: never print/log a
// secret) — only key and service names.
func (r *EnvResolver) Err() error {
	return r.err
}

// resolveValue returns raw verbatim unless it carries a "${" reference, in
// which case it must be exactly "${os_<key>}" — anything else is refused,
// naming key (never raw).
func (r *EnvResolver) resolveValue(key, raw string) (string, error) {
	if !strings.Contains(raw, "${") {
		return raw, nil
	}
	if !strings.HasPrefix(raw, osRefPrefix) || !strings.HasSuffix(raw, osRefSuffix) {
		return "", fmt.Errorf("farm: resolve %s: value is a reference this resolver does not support (only ${os_*} resolves)", key)
	}
	osKey := strings.TrimSuffix(strings.TrimPrefix(raw, osRefPrefix), osRefSuffix)
	if err := r.ensureOSFetched(); err != nil {
		return "", err
	}
	val, ok := r.osEnv[osKey]
	if !ok {
		return "", fmt.Errorf("farm: resolve %s: os service has no env var %q", key, osKey)
	}
	return val, nil
}

func (r *EnvResolver) ensureFarmFetched() error {
	if r.farmFetched {
		return nil
	}
	r.farmFetched = true
	env, err := r.fetchServiceEnv(farmServiceHostname)
	if err != nil {
		return err
	}
	r.farmEnv = make(map[string]string, len(env))
	for k, v := range env {
		if strings.HasPrefix(k, farmKeyPrefix) {
			r.farmEnv[k] = v
		}
	}
	return nil
}

func (r *EnvResolver) ensureOSFetched() error {
	if r.osFetched {
		return nil
	}
	r.osFetched = true
	env, err := r.fetchServiceEnv(osServiceHostname)
	if err != nil {
		return err
	}
	r.osEnv = env
	return nil
}

// fetchServiceEnv looks hostname up in r.projectID and reads its full env
// as a key->content map (ops.LookupService / ops.FetchServiceEnv, per
// docs/spec-eval-farm.md §3.1 FM-17). Its error never carries a value —
// ops's own "not found" / platform errors name services and keys only.
func (r *EnvResolver) fetchServiceEnv(hostname string) (map[string]string, error) {
	if r.client == nil {
		return nil, fmt.Errorf("farm: resolve env: no account client available (construct one from ZCP_FARM_ACCOUNT_TOKEN)")
	}
	svc, err := ops.LookupService(r.ctx, r.client, r.projectID, hostname)
	if err != nil {
		return nil, fmt.Errorf("farm: resolve env: %w", err)
	}
	vars, err := ops.FetchServiceEnv(r.ctx, r.client, svc.ID)
	if err != nil {
		return nil, fmt.Errorf("farm: resolve env: %w", err)
	}
	out := make(map[string]string, len(vars))
	for _, v := range vars {
		out[v.Key] = v.Content
	}
	return out, nil
}
