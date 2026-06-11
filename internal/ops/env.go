package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/preprocess"
)

// apiCodeUserDataDuplicateKey is the Zerops API error for a service env-file
// write on a key already owned by yaml run.envVariables (the yaml var owns the
// key; spec §2). Surfaced raw it's cryptic — EnvSet translates it.
const apiCodeUserDataDuplicateKey = "userDataDuplicateKey"

// credentialValueKeys are env-var names whose VALUE is a ZCP-managed secret
// that must be masked client-side when a response would otherwise echo it
// (zerops_discover includeEnvValues=true). The platform does NOT mask these:
// a PROJECT env's sensitive flag does not persist (it reads back USER/
// non-sensitive — see EnvSetSensitiveProject's LIMITATION note), so a
// read-only token reads GIT_TOKEN verbatim and any value dump would leak it.
// Keys-only listing (includeValues=false) is unaffected.
var credentialValueKeys = map[string]bool{
	GitTokenEnvKey: true,
	"ZCP_API_KEY":  true,
}

// RedactCredentialValue masks the value of a ZCP-managed credential key,
// returning (maskedValue, true) when key is a credential and (value, false)
// otherwise. Single owner so every value-echo site masks identically.
func RedactCredentialValue(key, value string) (string, bool) {
	if credentialValueKeys[key] {
		return "<redacted: ZCP-managed credential>", true
	}
	return value, false
}

// GitTokenEnvKey is the single owner of the git-push credential env-var name.
// git-push-setup writes it (EnvSetSensitiveProject) onto project env as a
// secret; the deploy git-push credential helper and the auth probe read
// it as $GIT_TOKEN inside the push-source container shell; env_generate
// denylists it from generated .env files; build-integration's gh-auth tell
// derives the read-back command from it. Every tell/check that names the key
// must reference this constant so they cannot drift apart.
const GitTokenEnvKey = "GIT_TOKEN"

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
	// never touched. A key owned by yaml run.envVariables collides at create
	// with userDataDuplicateKey, translated below to an actionable
	// "edit zerops.yaml + redeploy" message.
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
				return nil, delErr
			}
			replaced = true
		}
		proc, setErr := client.CreateServiceEnvVar(ctx, svc.ID, p.Key, p.Value)
		if setErr != nil {
			var pe *platform.PlatformError
			if errors.As(setErr, &pe) && pe.APICode == apiCodeUserDataDuplicateKey && !replaced {
				return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
					fmt.Sprintf("env key %q is owned by %s's zerops.yaml run.envVariables — a yaml-baked key cannot be overridden at service scope. Edit zerops.yaml and redeploy to change its value.", p.Key, hostname),
					"Either change the value in zerops.yaml and redeploy, or remove the key from run.envVariables to make it settable at service scope.")
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

// EnvSetSensitiveProject writes one project-level env var with sensitive=true
// at the platform layer. Used by handlers that write user secrets (today:
// GIT_TOKEN via git-push-setup verifier) where the value must NEVER appear
// in any response, state file, or audit log. Upsert semantics mirror
// EnvSet: existing key is delete-then-created.
//
// Returns the platform process only (no Stored echo). Callers that need
// confirmation that the value landed should poll the process, not read
// the value back — by design we don't expose it.
//
// LIMITATION (live-verified 2026-05-28; spec §7): a PROJECT env's
// sensitive=true flag does NOT persist — the platform reads it back as
// sensitive=false, type=USER. So this var is NOT server-masked: a read-only
// project token reads its value verbatim (a true service-level SECRET would
// return REDACTED). ZCP's own no-echo protection still holds, and GIT_TOKEN
// is denylisted from generate-dotenv (env_generate.go), so the residual
// exposure is read-only-token readability only. A true secret surface is
// service-level (envSecrets); relocating GIT_TOKEN there is deferred (it
// touches git-push deploy wiring) — documented here per the decision to
// document-not-relocate.
//
// The supplied value is run through the same preprocessor expansion as
// EnvSet so a recipe-style <@expr> would resolve identically; today's
// only caller (git-push-setup) passes literal PATs, but the path stays
// consistent.
func EnvSetSensitiveProject(ctx context.Context, client platform.Client, projectID, key, value string) (*platform.Process, error) {
	if key == "" {
		return nil, platform.NewPlatformError(platform.ErrInvalidUsage,
			"EnvSetSensitiveProject: key required", "")
	}
	if value == "" {
		return nil, platform.NewPlatformError(platform.ErrInvalidUsage,
			"EnvSetSensitiveProject: value required", "")
	}

	pairs := []envPair{{Key: key, Value: value}}
	if err := expandPairs(ctx, pairs); err != nil {
		return nil, err
	}
	if err := rejectEncodingPrefixedSecrets(pairs, []string{key + "=" + value}); err != nil {
		return nil, err
	}

	existing, err := client.GetProjectEnv(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for _, e := range existing {
		if e.Key == key {
			if _, delErr := client.DeleteProjectEnv(ctx, e.ID); delErr != nil {
				return nil, delErr
			}
			break
		}
	}
	proc, err := client.CreateProjectEnv(ctx, projectID, pairs[0].Key, pairs[0].Value, true /* sensitive */)
	if err != nil {
		return nil, err
	}
	return proc, nil
}

// EnvSetSecretService writes one secret env at SERVICE scope on the given
// service ID — the F5 home of GIT_TOKEN (per push-source service, one
// token per repo). Mirror of EnvSetSensitiveProject: preprocessor
// expansion + encoding-prefix guard + per-key upsert (delete existing,
// recreate). The platform assigns Type=SECRET to POSTed service userData
// (live-verified; FetchServiceSecretEnvs relies on it), which masks on
// read for low-privilege tokens — unlike the project-level sensitive
// flag, which does NOT persist (the old project-singleton GIT_TOKEN was
// effectively unmasked). Value never echoes back.
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
			return nil, delErr
		}
	}
	return client.CreateServiceEnvVar(ctx, serviceID, pairs[0].Key, pairs[0].Value)
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
			// Not in the slim service env. A yaml-baked run.envVariables key
			// lives on the app version, not the service env store, so it's
			// absent here and cannot be deleted at service scope — mirror
			// EnvSet's yaml-owned guidance instead of a bare "not found".
			return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
				fmt.Sprintf("env var %q not found in %s's service-scope env — if it is a yaml-baked zerops.yaml run.envVariables key it can't be deleted at service scope", key, hostname),
				"Remove it from zerops.yaml run.envVariables and redeploy; otherwise check the key name.")
		}
		proc, delErr := client.DeleteUserData(ctx, envID)
		if delErr != nil {
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
