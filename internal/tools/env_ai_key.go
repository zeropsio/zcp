package tools

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// EnvOption adds what only some containers have to zerops_env.
type EnvOption func(*envOptions)

type envOptions struct {
	// aiKey reaches this Mate's HQ; nil outside a Mate,
	// where zerops_env has no ai-key action.
	aiKey *orgAIKey
}

type orgAIKey struct {
	http hq.Doer
}

// WithOrgAIKey gives zerops_env its ai-key action: the organization's AI key,
// asked of this Mate's HQ under its project key, put in the project's Shared
// vault (docs/spec-mate.md §5.8). Mate-gated by the server like every Mate
// effect.
func WithOrgAIKey(httpClient hq.Doer) EnvOption {
	return func(o *envOptions) { o.aiKey = &orgAIKey{http: httpClient} }
}

// aiKeyVariableName is the variable HQ names for a provider's key, from which
// its base URL's is made: NEBIUS_API_KEY → NEBIUS_BASE_URL.
var aiKeyVariableName = regexp.MustCompile(`^[A-Z][A-Z0-9_]*_API_KEY$`)

// HQ answers ai-key within these bounds, once: a person waits on the call.
const (
	aiKeyConnectWithin = 5 * time.Second
	aiKeyAnswerWithin  = 10 * time.Second
)

// aiKeyResult is the answer to action=ai-key: the provider, the vault and the
// names it set with their sensitivity, and the readers it restarted — never
// the key.
type aiKeyResult struct {
	Provider  string          `json:"provider"`
	Vault     string          `json:"vault"`
	Variables []aiKeyVariable `json:"variables"`
	envChangeResult
}

type aiKeyVariable struct {
	Key       string `json:"key"`
	Sensitive bool   `json:"isSensitive"`
}

// setOrgAIKey asks HQ for the organization's AI key as this Mate and sets it
// in the Shared vault: the key sensitive under the variable HQ names
// (NEBIUS_API_KEY), its base URL plain beside it (NEBIUS_BASE_URL). The key
// passes from HQ's answer to the vault write and nowhere else: not the
// answer, an error or a log.
func setOrgAIKey(ctx context.Context, client platform.Client, projectID, selfHostname string, aiKey *orgAIKey, input EnvInput, onProgress ops.ProgressCallback) *mcp.CallToolResult {
	nothingSet := func(code, message, suggestion string) *mcp.CallToolResult {
		return convertError(platform.NewPlatformError(code, message+"; nothing was set.", suggestion))
	}
	hqc, err := hq.Open(aiKey.http, hqIdentity())
	switch {
	case err != nil:
		return nothingSet(platform.ErrPrerequisiteMissing,
			fmt.Sprintf("This Mate cannot address its HQ (%v), so it cannot ask for the organization's AI key", err), "")
	case hqc.ProjectID() != projectID:
		return nothingSet(platform.ErrPrerequisiteMissing,
			"This Mate's key belongs to another project", "")
	}

	key, err := hqc.Once().Bounded(aiKeyConnectWithin, aiKeyAnswerWithin).AIKey(ctx)
	var refused *hq.RefusedError
	switch {
	case errors.Is(err, hq.ErrNoAIKey):
		return nothingSet(platform.ErrPrerequisiteMissing,
			"The organization has no AI key yet",
			"An org Owner or Admin sets it in Mate under Settings › Zerops › HQ. Tell the person that in one sentence; once it is set, call zerops_env action=\"ai-key\" again.")
	case hq.IsUnavailable(err):
		return nothingSet(platform.ErrNetworkError,
			"HQ is not answering now, so the organization's AI key could not be fetched",
			"Try again in a minute; continue with what does not need the key meanwhile.")
	case errors.As(err, &refused):
		why := strings.TrimSpace(fmt.Sprintf("%d %s %s", refused.Status, refused.Code, refused.Reason))
		return nothingSet(platform.ErrPermissionDenied, "HQ refused the organization's AI key ("+why+")", "")
	case err != nil:
		return nothingSet(platform.ErrAPIError, err.Error(), "")
	}

	// The value is stored as it is: a `<@…>` in it would be expanded.
	if !aiKeyVariableName.MatchString(key.Env) || strings.Contains(key.Value, "<@") || strings.ContainsAny(key.Value, "\r\n") {
		return nothingSet(platform.ErrAPIError, "HQ's answer is not a key zcp can store as it is", "")
	}
	baseKey := strings.TrimSuffix(key.Env, "_API_KEY") + "_BASE_URL"

	set, err := ops.EnvSetEach(ctx, client, projectID, "", true,
		[]string{key.Env + "=" + key.Value, baseKey + "=" + key.BaseURL},
		map[string]bool{key.Env: true, baseKey: false})
	if err != nil {
		return convertError(err)
	}
	resp := aiKeyResult{Provider: key.Provider, Vault: "Shared"}
	keys := make([]string, 0, len(set.Stored))
	for _, stored := range set.Stored {
		resp.Variables = append(resp.Variables, aiKeyVariable{Key: stored.Key, Sensitive: stored.Sensitive})
		keys = append(keys, stored.Key)
	}
	if set.Process != nil {
		resp.Process, resp.TimedOut = pollManageProcess(ctx, client, set.Process, onProgress)
	}
	shared := EnvInput{Action: input.Action, Project: true, SkipRestart: input.SkipRestart}
	resp.ShadowWarnings, resp.ShadowUnverified = detectSetShadows(ctx, client, projectID, shared, selfHostname, set.Stored)
	applyAutoRestart(ctx, client, projectID, shared, selfHostname, keys, false, &resp.envChangeResult, onProgress)
	resp.NextActions = joinSentences(
		fmt.Sprintf("Set %s (sensitive) and %s in the Shared vault. A service calls the model once its run.envVariables reads them under other names — `OPENAI_API_KEY: ${%s}` and `OPENAI_BASE_URL: ${%s}` — and it deploys.", key.Env, baseKey, key.Env, baseKey),
		resp.NextActions)
	return jsonResult(resp)
}
