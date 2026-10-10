package ops

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/zeropsio/zcp/internal/platform"
)

// EnvRequestScope names the vault a requested value goes to.
type EnvRequestScope string

const (
	// EnvRequestScopeShared is the project's Shared vault (project env).
	EnvRequestScopeShared EnvRequestScope = "shared"
	// EnvRequestScopeService is one service's own vault (its user data).
	EnvRequestScopeService EnvRequestScope = "service"
)

// EnvRequestResult is the answer to a request for a value only the person
// has. It never carries a value: the person types it into Mate, which writes
// it to the vault directly, so it never crosses the conversation.
type EnvRequestResult struct {
	Key string `json:"key"`
	// Reason is the one sentence the person reads: what the value is for and
	// where to find it. Mate's card shows it beside the field.
	Reason          string          `json:"reason"`
	Scope           EnvRequestScope `json:"scope"`
	ServiceHostname string          `json:"serviceHostname,omitempty"`
	// Sensitive is the flag the value is asked with (the caller's, else the
	// default by name) — or, when AlreadySet, the flag the stored row carries.
	Sensitive bool `json:"sensitive"`
	// AlreadySet is true when the key is already in that vault: nothing is
	// asked, the agent references it by name.
	AlreadySet bool `json:"alreadySet,omitempty"`
}

// envKeyPattern is a key the platform takes (alphanumerics and `_`) that a
// shell also reads as a variable name (no leading digit).
var envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// platformKeyPrefix is the prefix the platform refuses on a custom env.
const platformKeyPrefix = "ZEROPS_"

// ValidateEnvKey refuses a key the vault cannot store under that name.
func ValidateEnvKey(key string) error {
	if !envKeyPattern.MatchString(key) {
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("%q is not an env key", key),
			"An env key is letters, digits and `_`, not starting with a digit — the name only, never KEY=value.")
	}
	if strings.HasPrefix(strings.ToUpper(key), platformKeyPrefix) {
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("%q uses the %s prefix, which the platform keeps for itself", key, platformKeyPrefix),
			"Pick a name without the "+platformKeyPrefix+" prefix.")
	}
	return nil
}

// The names Mate's vault never lists: the keys Mate and zcp keep for
// themselves (zcp's admin token, Mate's git and setup keys). A value asked for
// under one would land where the person can neither see nor remove it. This
// list must agree with isMateOwned in mate's
// packages/client-runtime/src/data/projections/vault.ts (case-sensitive
// prefixes, one exact name).
var (
	vaultHiddenPrefixes = []string{"ZCP_", "MATE_", "GITEA_"}
	vaultHiddenNames    = []string{"GIT_TOKEN"}
)

// isVaultHidden reports whether Mate's vault keeps key out of every list.
func isVaultHidden(key string) bool {
	for _, prefix := range vaultHiddenPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return slices.Contains(vaultHiddenNames, key)
}

// validateRequestKey refuses a key the person could not be asked for: one the
// vault cannot store, or one Mate's vault never lists.
func validateRequestKey(key string) error {
	if err := ValidateEnvKey(key); err != nil {
		return err
	}
	if isVaultHidden(key) {
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("%q is a name Mate and zcp keep for themselves: the vault never lists it, so the person could neither see nor remove the value", key),
			"Ask for it under another name, without the ZCP_, MATE_ or GITEA_ prefix and not GIT_TOKEN, and reference that name in zerops.yaml.")
	}
	return nil
}

// requestReasonMax bounds the purpose to a sentence a card shows whole.
const requestReasonMax = 240

// ValidateRequestReason refuses a purpose the person could not read in one
// line: none, more than one line, or longer than a sentence. A second line is
// where a value pasted by mistake would hide.
func ValidateRequestReason(reason string) error {
	trimmed := strings.TrimSpace(reason)
	switch {
	case trimmed == "":
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			"A request needs its reason",
			"reason: one sentence for the person — what the value is for and where to find it.")
	case strings.ContainsAny(trimmed, "\r\n"):
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			"The reason is one line",
			"reason: one sentence, no line breaks — and never a value.")
	case utf8.RuneCountInString(trimmed) > requestReasonMax:
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("The reason is longer than %d characters", requestReasonMax),
			"reason: one short sentence the card shows whole.")
	}
	return nil
}

// EnvRequest checks a request for a vault value and writes nothing: it
// validates the key, resolves the scope (project wins over a hostname, as in
// set/delete) and reports whether the key is already in that vault, by
// presence only — the value is never read into the result.
func EnvRequest(
	ctx context.Context,
	client platform.Client,
	projectID string,
	hostname string,
	isProject bool,
	key string,
	reason string,
	sensitive *bool,
) (*EnvRequestResult, error) {
	if err := validateRequestKey(key); err != nil {
		return nil, err
	}
	if err := ValidateRequestReason(reason); err != nil {
		return nil, err
	}
	if hostname == "" && !isProject {
		return nil, platform.NewPlatformError(platform.ErrInvalidUsage,
			"Provide serviceHostname or set project=true",
			"project=true asks for a Shared value; serviceHostname for one service's own.")
	}

	result := &EnvRequestResult{Key: key, Reason: strings.TrimSpace(reason), Sensitive: resolveSensitive(key, "", sensitive)}
	if isProject {
		result.Scope = EnvRequestScopeShared
		envs, err := client.GetProjectEnv(ctx, projectID)
		if err != nil {
			return nil, fmt.Errorf("read the Shared vault: %w", err)
		}
		markAlreadySet(result, envs)
		return result, nil
	}

	svc, err := resolveService(ctx, client, projectID, hostname)
	if err != nil {
		return nil, err
	}
	result.Scope = EnvRequestScopeService
	result.ServiceHostname = svc.Name
	envs, err := client.GetServiceEnv(ctx, svc.ID)
	if err != nil {
		return nil, fmt.Errorf("read %s's vault: %w", svc.Name, err)
	}
	markAlreadySet(result, envs)
	return result, nil
}

// markAlreadySet flags the result when the key is in envs, taking the stored
// row's sensitive flag.
func markAlreadySet[T platform.EnvAccessor](result *EnvRequestResult, envs []T) {
	for _, e := range envs {
		if e.GetKey() == result.Key {
			result.AlreadySet = true
			result.Sensitive = e.IsSensitive()
			return
		}
	}
}
