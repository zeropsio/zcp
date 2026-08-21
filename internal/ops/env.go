package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/preprocess"
	"github.com/zeropsio/zcp/internal/topology"
)

// apiCodeUserDataDuplicateKey is the Zerops API error for a service userData
// write on a key already owned by yaml run.envVariables (the yaml var owns the
// key; spec §1). Surfaced raw it's cryptic — EnvSet translates it.
const apiCodeUserDataDuplicateKey = "userDataDuplicateKey"

// apiCodeUserDataDeleteForbidden is the Zerops API error for a DELETE on a
// yaml-baked run.envVariables key's read-only mirror on the slim service
// env (spec-zerops-env-lifecycle.md §1, [LIVE 08-21]: since 2026-08 those
// keys are ALSO present on GET service-stack/{id}/env, not just the
// app-version surface). Surfaced raw it's cryptic — EnvSet/EnvDelete
// translate it to the same "edit zerops.yaml + redeploy" guidance as the
// create-side userDataDuplicateKey.
const apiCodeUserDataDeleteForbidden = "userDataDeleteForbidden"

// yamlOwnedKeyError is the actionable guidance returned when a service-scope
// env write collides with a key owned by zerops.yaml run.envVariables —
// either CreateServiceEnvVar hit userDataDuplicateKey (the key already
// exists as the yaml-baked read-only mirror) or the pre-create
// DeleteUserData hit userDataDeleteForbidden (the existing record IS that
// read-only mirror). Both signals mean the same thing to the caller: the
// value lives in zerops.yaml, not in the service env store.
func yamlOwnedKeyError(key, hostname string) error {
	return platform.NewPlatformError(platform.ErrInvalidParameter,
		fmt.Sprintf("env key %q is owned by %s's zerops.yaml run.envVariables — a yaml-baked key cannot be overridden at service scope. Edit zerops.yaml and redeploy to change its value.", key, hostname),
		"Either change the value in zerops.yaml and redeploy, or remove the key from run.envVariables to make it settable at service scope.")
}

// credentialValueKeys are ZCP-owned credential env-var names whose VALUE must
// be masked client-side whenever a response would echo it (zerops_discover
// includeEnvValues=true). The platform does NOT mask these at project scope:
// a PROJECT env's sensitive flag does not persist (spec-zerops-env-lifecycle.md
// §7), so a read-only token reads GIT_TOKEN verbatim and any value dump would
// leak it. Masked regardless of the owning service type. Keys-only listing
// (includeValues=false) is unaffected.
var credentialValueKeys = map[string]bool{
	GitTokenEnvKey:    true,
	"ZCP_API_KEY":     true,
	LaunchTokenEnvKey: true,
}

// managedCredentialFieldKeys are env-var KEYS whose VALUE is a generated
// secret on a MANAGED service (database / cache / search / object-storage /
// messaging). They are masked client-side at every presentation surface when
// the OWNING service is a managed service — the agent wires them by
// ${host_var} reference and never needs the literal, so echoing the value
// only risks pasting it into a command/commit. Keyed on the curated field set
// + topology.IsManagedService, never on the platform Sensitive flag (which is
// not authoritative — see credentialValueKeys). Generic enough names
// (password, connectionString) that the managed-type gate is what keeps a
// user runtime var of the same name from being masked.
var managedCredentialFieldKeys = map[string]bool{
	"password":                 true,
	"superUserPassword":        true,
	"zeropsPassword":           true,
	"secretAccessKey":          true,
	"connectionString":         true,
	"connectionTlsString":      true,
	"connectionStringReplicas": true,
	"grpcConnectionString":     true,
	"masterKey":                true,
}

// RedactCredentialValue masks the value of a credential env var, returning
// (maskedValue, true) when the value must not be echoed and (value, false)
// otherwise. Single owner so every value-echo / presentation site masks
// identically (get/discover renderers, set-echo, layered-shadow message,
// generate-dotenv preview diff).
//
// Two classes mask:
//   - ZCP-owned credential keys (GIT_TOKEN, ZCP_API_KEY, ZCP_LAUNCH_TOKEN) —
//     regardless of serviceType.
//   - Managed-service credential fields (connectionString, password, …) —
//     only when serviceType is a managed service.
//
// serviceType is the owning service's type version (e.g. "postgresql@18").
// Pass "" for project-scope or non-service echo sites: only the ZCP-owned
// class can mask there. Presentation-only — internal value paths (ref
// resolution, shadow detection, the generate-dotenv .env file render) pass
// the raw value untouched.
func RedactCredentialValue(key, value, serviceType string) (string, bool) {
	if credentialValueKeys[key] {
		return "<redacted: ZCP-managed credential>", true
	}
	if managedCredentialFieldKeys[key] && topology.IsManagedService(serviceType) {
		return "<redacted: managed-service credential>", true
	}
	return value, false
}

// GitTokenEnvKey is the single owner of the git-push credential env-var name.
// git-push-setup writes it (EnvSetSecretService) as a SERVICE-scope secret on
// the push source (the F5 relocation off the legacy project-scope key); the
// deploy git-push credential helper and the auth probe read it as $GIT_TOKEN
// inside the push-source container shell; env_generate denylists it from
// generated .env files; build-integration's gh-auth tell derives the
// read-back command from it. Every tell/check that names the key must
// reference this constant so they cannot drift apart.
const GitTokenEnvKey = "GIT_TOKEN"

// LaunchTokenEnvKey is the single owner of the staged launch-token env-var
// name (single-token launch lifecycle). The launch-production mutation
// stages the user's integration token (EnvSetSecretService) as a
// SERVICE-scope secret on the source push service BEFORE the irreversible
// project create; every later launch-window operation (prod-ops, pipeline
// resume, reset, confirm-production) reads the token from that staged
// secret instead of re-asking for the value, and the GitHub Actions
// repo-secret conveyance reads it over ssh ($ZCP_LAUNCH_TOKEN) so the
// value never re-enters the conversation. confirm-production DELETES the
// env — closing the launch window physically. env_generate denylists it
// from generated .env files; topology's classify-infrastructure allowlist
// keeps it out of export/launch bundles. Every tell/check that names the
// key must reference this constant so they cannot drift apart. The name
// deliberately carries the ZCP_ prefix: the platform REJECTS custom envs
// with a ZEROPS_ prefix ("Custom env variables with 'ZEROPS_' prefix are
// forbidden", live-verified 2026-06-11); the GitHub repo secret keeps its
// own independent name (ZEROPS_TOKEN_PROD).
const LaunchTokenEnvKey = "ZCP_LAUNCH_TOKEN"

// EnvSetResult contains the result of an env set operation.
type EnvSetResult struct {
	Process *platform.Process `json:"process,omitempty"`
	// Stored is the list of {key, value} pairs that were actually written.
	// Values reflect post-expansion state (preprocessor already applied),
	// letting the caller verify what the platform actually stores — e.g.
	// catching an unintended base64: prefix or a miscounted byte length
	// BEFORE the app runtime trips on it.
	Stored      []StoredEnv `json:"stored,omitempty"`
	NextActions string      `json:"nextActions,omitempty"`
}

// StoredEnv describes one env var as it now lives in the platform.
type StoredEnv struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	// Replaced is true when the key was upserted (existing entry deleted,
	// new entry created). False when the key is newly added.
	Replaced bool `json:"replaced,omitempty"`
}

// EnvDeleteResult contains the result of an env delete operation.
type EnvDeleteResult struct {
	Process     *platform.Process `json:"process,omitempty"`
	NextActions string            `json:"nextActions,omitempty"`
}

// EnvSet sets environment variables for a service or project with upsert
// semantics — existing keys are replaced, new ones are created. BOTH scopes
// upsert ONE key at a time (delete-then-create on collision) and never touch
// vars the caller didn't name.
//
// Service-level: per-var via DeleteUserData + CreateServiceEnvVar. The bulk
// env-file PUT is deliberately NOT used — it replaces the entire file and
// silently drops every other user-set var (proven live). Project-level: the
// platform exposes CREATE+DELETE only, so the same delete-then-create runs,
// eliminating projectEnvDuplicateKey errors from the caller's perspective.
//
// Values are run through zParser preprocessor expansion before being stored,
// so an agent can write the same <@...> expression a recipe deliverable
// uses and get byte-for-byte identical output. The Stored slice on the
// result lets the caller verify the final values that landed on the
// platform (catches base64:<@...> antipatterns and similar mistakes).
func EnvSet(
	ctx context.Context,
	client platform.Client,
	projectID string,
	hostname string,
	isProject bool,
	variables []string,
) (*EnvSetResult, error) {
	if hostname == "" && !isProject {
		return nil, platform.NewPlatformError(platform.ErrInvalidUsage,
			"Provide serviceHostname or set project=true", "")
	}

	pairs, err := parseEnvPairs(variables)
	if err != nil {
		return nil, err
	}

	// Expand Zerops preprocessor expressions (e.g. <@generateRandomString(<32>)>)
	// through zParser — the same library the platform uses at recipe import
	// time. Gives recipe-creation workflows a single source of truth for
	// shared-secret values: the workspace setup and the published deliverable
	// run the exact same expression, so a bug caught at workspace time
	// can't reappear at deploy time. Keys are batched into one parse so
	// setVar/getVar correlate across variables in a single call.
	if err := expandPairs(ctx, pairs); err != nil {
		return nil, err
	}

	// Reject values where preprocessor output is wrapped in a framework
	// encoding prefix (`base64:{expanded}`). The platform stores this literal,
	// the framework then decodes the suffix, and a 32-char expansion becomes
	// ~24 bytes — the recurring APP_KEY footgun. Caught at zcp instead of
	// at app boot.
	if err := rejectEncodingPrefixedSecrets(pairs, variables); err != nil {
		return nil, err
	}

	if isProject {
		return setProjectEnvs(ctx, client, projectID, pairs)
	}

	svc, err := resolveService(ctx, client, projectID, hostname)
	if err != nil {
		return nil, err
	}

	// Per-var upsert — NOT a whole-file replace. The service env-file PUT
	// replaces every userData record, silently dropping vars the caller didn't
	// pass (proven live: set A then B → A gone). So upsert one key at a time:
	// delete-then-create on collision, create otherwise — exactly like
	// setProjectEnvs. Other vars are never read or re-sent, so their values
	// (incl. secrets that read back REDACTED on low-privilege tokens) are
	// never touched. A key owned by yaml run.envVariables now collides on
	// EITHER side of the upsert: create sees userDataDuplicateKey (the key
	// already exists) when the key wasn't in `existing` for some reason, and
	// — since 2026-08 the yaml-baked mirror IS in `existing` — the pre-create
	// delete below sees userDataDeleteForbidden instead. Both translate to
	// the same actionable "edit zerops.yaml + redeploy" message via
	// yamlOwnedKeyError.
	existing, err := client.GetServiceEnv(ctx, svc.ID)
	if err != nil {
		return nil, err
	}

	var lastProc *platform.Process
	stored := make([]StoredEnv, 0, len(pairs))
	for _, p := range pairs {
		replaced := false
		if id := findEnvIDByKey(existing, p.Key); id != "" {
			if _, delErr := client.DeleteUserData(ctx, id); delErr != nil {
				var pe *platform.PlatformError
				if errors.As(delErr, &pe) && pe.APICode == apiCodeUserDataDeleteForbidden {
					return nil, yamlOwnedKeyError(p.Key, hostname)
				}
				return nil, delErr
			}
			replaced = true
		}
		proc, setErr := client.CreateServiceEnvVar(ctx, svc.ID, p.Key, p.Value, true)
		if setErr != nil {
			var pe *platform.PlatformError
			if errors.As(setErr, &pe) && pe.APICode == apiCodeUserDataDuplicateKey && !replaced {
				return nil, yamlOwnedKeyError(p.Key, hostname)
			}
			if replaced {
				// The old value was already deleted (upsert is delete-then-create);
				// disclose that so the agent knows the key is now absent, not stale.
				return nil, fmt.Errorf("env key %q: write failed after the previous value was already removed — re-run zerops_env set to restore it: %w", p.Key, setErr)
			}
			return nil, setErr
		}
		lastProc = proc
		stored = append(stored, StoredEnv{Key: p.Key, Value: p.Value, Replaced: replaced})
	}
	return &EnvSetResult{Process: lastProc, Stored: stored}, nil
}

// setProjectEnvs upserts project-level env vars. The platform API only
// exposes CREATE + DELETE, so existing keys are delete-then-created; new
// keys are created directly. Returns the last process plus the full list
// of stored pairs so the caller can verify what was written.
func setProjectEnvs(ctx context.Context, client platform.Client, projectID string, pairs []envPair) (*EnvSetResult, error) {
	existing, err := client.GetProjectEnv(ctx, projectID)
	if err != nil {
		return nil, err
	}
	existingByKey := make(map[string]string, len(existing))
	for _, e := range existing {
		existingByKey[e.Key] = e.ID
	}

	var lastProc *platform.Process
	stored := make([]StoredEnv, 0, len(pairs))

	for _, p := range pairs {
		replaced := false
		if envID, ok := existingByKey[p.Key]; ok {
			if _, delErr := client.DeleteProjectEnv(ctx, envID); delErr != nil {
				return nil, delErr
			}
			replaced = true
		}
		proc, setErr := client.CreateProjectEnv(ctx, projectID, p.Key, p.Value, false)
		if setErr != nil {
			if replaced {
				return nil, fmt.Errorf("project env key %q: write failed after the previous value was already removed — re-run zerops_env set to restore it: %w", p.Key, setErr)
			}
			return nil, setErr
		}
		lastProc = proc
		stored = append(stored, StoredEnv{Key: p.Key, Value: p.Value, Replaced: replaced})
	}
	return &EnvSetResult{Process: lastProc, Stored: stored}, nil
}

// EnvSetSecretService writes one service-scope env var on the given
// service ID — the F5 home of GIT_TOKEN (per push-source service, one
// token per repo) and the launch-production staged ZCP_LAUNCH_TOKEN.
// Preprocessor expansion + encoding-prefix guard + per-key upsert (delete
// existing, recreate). Written with sensitive:true — the platform's
// 2026-08 model requires the flag on every service userData write, and
// masks it for read-only roles / encrypts it at rest (spec-zerops-env-lifecycle.md
// §7). Value never echoes back.
func EnvSetSecretService(ctx context.Context, client platform.Client, serviceID, key, value string) (*platform.Process, error) {
	if serviceID == "" {
		return nil, platform.NewPlatformError(platform.ErrInvalidUsage,
			"EnvSetSecretService: serviceID required", "")
	}
	if key == "" {
		return nil, platform.NewPlatformError(platform.ErrInvalidUsage,
			"EnvSetSecretService: key required", "")
	}
	if value == "" {
		return nil, platform.NewPlatformError(platform.ErrInvalidUsage,
			"EnvSetSecretService: value required", "")
	}

	pairs := []envPair{{Key: key, Value: value}}
	if err := expandPairs(ctx, pairs); err != nil {
		return nil, err
	}
	if err := rejectEncodingPrefixedSecrets(pairs, []string{key + "=" + value}); err != nil {
		return nil, err
	}

	existing, err := client.GetServiceEnv(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	if id := findEnvIDByKey(existing, pairs[0].Key); id != "" {
		if _, delErr := client.DeleteUserData(ctx, id); delErr != nil {
			var pe *platform.PlatformError
			if errors.As(delErr, &pe) && pe.APICode == apiCodeUserDataDeleteForbidden {
				// hostname is unknown at this call site (callers pass a
				// resolved serviceID, not a hostname) — name the service ID
				// instead; the guidance reads the same either way.
				return nil, yamlOwnedKeyError(pairs[0].Key, serviceID)
			}
			return nil, delErr
		}
	}
	return client.CreateServiceEnvVar(ctx, serviceID, pairs[0].Key, pairs[0].Value, true)
}

// EnvDeleteProjectKeyIfPresent deletes one project-scope env key when it
// exists; missing key is a silent no-op. Owner of the F5 lazy migration
// off the legacy project-singleton GIT_TOKEN.
func EnvDeleteProjectKeyIfPresent(ctx context.Context, client platform.Client, projectID, key string) error {
	envs, err := client.GetProjectEnv(ctx, projectID)
	if err != nil {
		return err
	}
	for _, e := range envs {
		if e.Key == key {
			_, delErr := client.DeleteProjectEnv(ctx, e.ID)
			return delErr
		}
	}
	return nil
}

// EnvHasServiceKey reports whether the service has an env var with the given
// key, by PRESENCE only — it never reads, returns, or logs the value. Credential
// safety: the adopt git-push reconcile uses it to detect that a GIT_TOKEN secret
// EXISTS on a service (a signal that git-push was configured outside ZCP) without
// the value ever entering the reconcile / response / meta / audit surfaces.
func EnvHasServiceKey(ctx context.Context, client platform.Client, serviceID, key string) (bool, error) {
	envs, err := client.GetServiceEnv(ctx, serviceID)
	if err != nil {
		return false, fmt.Errorf("get service env: %w", err)
	}
	return findEnvIDByKey(envs, key) != "", nil
}

// EnvDeleteServiceKeyIfPresent deletes one service-scope env key when it
// exists; missing key is a silent no-op (returns deleted=false). Sister
// of EnvDeleteProjectKeyIfPresent for the SERVICE scope — owner of the
// confirm-production staged-secret delete, where the absent-key case is
// a benign idempotent re-call, not an error.
func EnvDeleteServiceKeyIfPresent(ctx context.Context, client platform.Client, serviceID, key string) (bool, error) {
	envs, err := client.GetServiceEnv(ctx, serviceID)
	if err != nil {
		return false, err
	}
	id := findEnvIDByKey(envs, key)
	if id == "" {
		return false, nil
	}
	if _, err := client.DeleteUserData(ctx, id); err != nil {
		return false, err
	}
	return true, nil
}

// EnvDelete deletes environment variables from a service or project.
// Service-level: each variable is deleted individually; only the last process
// is returned. Project-level: same behavior. On error, returns immediately —
// earlier variables may already be deleted.
func EnvDelete(
	ctx context.Context,
	client platform.Client,
	projectID string,
	hostname string,
	isProject bool,
	variables []string,
) (*EnvDeleteResult, error) {
	if hostname == "" && !isProject {
		return nil, platform.NewPlatformError(platform.ErrInvalidUsage,
			"Provide serviceHostname or set project=true", "")
	}

	if isProject {
		envs, err := client.GetProjectEnv(ctx, projectID)
		if err != nil {
			return nil, err
		}
		var lastProc *platform.Process
		for _, key := range variables {
			envID := findEnvIDByKey(envs, key)
			if envID == "" {
				return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
					fmt.Sprintf("Environment variable '%s' not found", key), "")
			}
			proc, delErr := client.DeleteProjectEnv(ctx, envID)
			if delErr != nil {
				return nil, delErr
			}
			lastProc = proc
		}
		return &EnvDeleteResult{Process: lastProc}, nil
	}

	svc, err := resolveService(ctx, client, projectID, hostname)
	if err != nil {
		return nil, err
	}

	envs, err := client.GetServiceEnv(ctx, svc.ID)
	if err != nil {
		return nil, err
	}

	var lastProc *platform.Process
	for _, key := range variables {
		envID := findEnvIDByKey(envs, key)
		if envID == "" {
			// Genuinely absent: since 2026-08 a yaml-baked run.envVariables key
			// is ALSO mirrored (read-only) on this slim service env, so a
			// PRESENT yaml-baked key is now caught by the userDataDeleteForbidden
			// branch below instead of landing here — envID=="" means no record
			// with this key exists at all. Still hint at the yaml-baked
			// possibility (a stale local zerops.yaml reference, a typo, etc.).
			return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
				fmt.Sprintf("env var %q not found in %s's service-scope env — if it is a yaml-baked zerops.yaml run.envVariables key it can't be deleted at service scope", key, hostname),
				"Remove it from zerops.yaml run.envVariables and redeploy; otherwise check the key name.")
		}
		proc, delErr := client.DeleteUserData(ctx, envID)
		if delErr != nil {
			var pe *platform.PlatformError
			if errors.As(delErr, &pe) && pe.APICode == apiCodeUserDataDeleteForbidden {
				return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
					fmt.Sprintf("env var %q on %s is a yaml-baked zerops.yaml run.envVariables key — it cannot be deleted at service scope", key, hostname),
					"Remove it from zerops.yaml run.envVariables and redeploy")
			}
			return nil, delErr
		}
		lastProc = proc
	}

	return &EnvDeleteResult{Process: lastProc}, nil
}

// encodingPrefixes names framework conventions where a prefix tells the
// framework to DECODE the suffix. Wrapping preprocessor output in one of
// these turns an N-char string into ceil(3N/4) bytes — breaking fixed-
// length ciphers like aes-256-cbc and causing boot-time failures. The
// list stays intentionally short: these are prefixes where the framework
// mutates the trailing bytes, not prefixes that are just stored verbatim.
var encodingPrefixes = []string{"base64:", "hex:"}

// rejectEncodingPrefixedSecrets refuses values shaped like
// `base64:<preprocessor-output>`. The original caller input (`variables`)
// is inspected for the `<@` token — that's the signal the prefix was
// slapped on top of a preprocessor expression, rather than being part of
// a literal value the caller actually base64-encoded themselves.
func rejectEncodingPrefixedSecrets(pairs []envPair, originalVariables []string) error {
	if len(pairs) != len(originalVariables) {
		// Can't line up pairs with originals — fall back to skipping the
		// check rather than emitting a spurious error.
		return nil
	}
	for i, p := range pairs {
		lower := strings.ToLower(p.Value)
		for _, prefix := range encodingPrefixes {
			if !strings.HasPrefix(lower, prefix) {
				continue
			}
			// Only reject when the ORIGINAL input wrapped a preprocessor
			// expression. A caller passing a pre-encoded literal (e.g.
			// `base64:{their-own-real-base64}`) is fine and passes through.
			if !strings.Contains(originalVariables[i], "<@") {
				continue
			}
			return platform.NewPlatformError(platform.ErrInvalidParameter,
				fmt.Sprintf("value for %q starts with %q wrapping a preprocessor expression — the framework will decode the suffix, turning %d-char output into ~%d bytes and breaking any fixed-length cipher",
					p.Key, strings.TrimSuffix(prefix, ":"),
					len(p.Value)-len(prefix), (len(p.Value)-len(prefix))*3/4),
				"Pass the <@...> expression without the "+prefix+" prefix. Frameworks like Laravel accept the raw 32-char output directly (Encrypter::supported() checks the byte length, which equals the char length for the preprocessor's single-byte ASCII alphabet).")
		}
	}
	return nil
}

// expandPairs runs each pair's value through the zParser-backed preprocess
// wrapper in one batch, so setVar/getVar correlations work across variables
// in the same call. Pairs with no preprocessor syntax pass through untouched.
// Batching means one shared variable store and one parse — cheaper, and
// matches how the platform's own preprocessor handles multi-key imports.
func expandPairs(ctx context.Context, pairs []envPair) error {
	keys := make([]string, len(pairs))
	inputs := make(map[string]string, len(pairs))
	for i, p := range pairs {
		// Use the index as the batch key — pair keys may repeat (shouldn't,
		// but we don't want to silently drop duplicates at this layer).
		batchKey := fmt.Sprintf("%d", i)
		keys[i] = batchKey
		inputs[batchKey] = p.Value
	}
	expanded, err := preprocess.Batch(ctx, keys, inputs)
	if err != nil {
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("preprocessor expansion failed: %v", err),
			"Check your <@...> syntax, or omit it for literal values")
	}
	for i := range pairs {
		pairs[i].Value = expanded[keys[i]]
	}
	return nil
}
