package ops

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/preprocess"
)

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
// semantics — existing keys are replaced, new ones are created.
//
// Service-level: a single PUT replaces the entire env file (idempotent by
// API design). Project-level: the platform exposes CREATE+DELETE only, so
// zcp does delete-then-create for keys that already exist, eliminating
// projectEnvDuplicateKey errors from the caller's perspective.
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

	content := buildEnvFileContent(pairs)
	proc, err := client.SetServiceEnvFile(ctx, svc.ID, content)
	if err != nil {
		return nil, err
	}

	stored := make([]StoredEnv, len(pairs))
	for i, p := range pairs {
		stored[i] = StoredEnv{Key: p.Key, Value: p.Value}
	}
	return &EnvSetResult{Process: proc, Stored: stored}, nil
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
			return nil, setErr
		}
		lastProc = proc
		stored = append(stored, StoredEnv{Key: p.Key, Value: p.Value, Replaced: replaced})
	}
	return &EnvSetResult{Process: lastProc, Stored: stored}, nil
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
			return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
				fmt.Sprintf("Environment variable '%s' not found", key), "")
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

func buildEnvFileContent(pairs []envPair) string {
	var b strings.Builder
	for _, p := range pairs {
		b.WriteString(p.Key)
		b.WriteByte('=')
		b.WriteString(p.Value)
		b.WriteByte('\n')
	}
	return b.String()
}
