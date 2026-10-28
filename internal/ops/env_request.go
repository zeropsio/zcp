package ops

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zeropsio/zcp/internal/platform"
)

// EnvRequestScope names the vault a requested value goes to, and the vault a
// set wrote to (EnvSetResult).
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
	// Label is what the value is, in people's words ("Stripe secret key"),
	// and Why the one line of what it is for ("for checkout"): Mate asks
	// "Milo needs your Stripe secret key, for checkout".
	Label           string          `json:"label"`
	Why             string          `json:"why"`
	Scope           EnvRequestScope `json:"scope"`
	ServiceHostname string          `json:"serviceHostname,omitempty"`
	// Sensitive is the flag the value is asked with (the caller's, else the
	// default by name) — or, when AlreadySet, the flag the stored row carries.
	Sensitive bool `json:"sensitive"`
	// AlreadySet is true when the key is already in that vault: nothing is
	// asked, the agent references it by name.
	AlreadySet bool `json:"alreadySet,omitempty"`
	// Replace is true when the person is asked to replace the value the
	// vault holds: Why then ends with what is wrong with it.
	Replace bool `json:"replace,omitempty"`
	// HeldID and HeldChanged are the vault's row under the key when asked
	// — its id and when it last changed, never its value — so a later
	// request tells a save since from an ask still open. Not on the wire.
	HeldID      string    `json:"-"`
	HeldChanged time.Time `json:"-"`
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

// keyAcronyms are the words a key's name holds that people write in capitals.
var keyAcronyms = map[string]bool{
	"API": true, "AWS": true, "CI": true, "DB": true, "DSN": true, "ID": true, "IP": true,
	"JWT": true, "S3": true, "SMTP": true, "SSH": true, "SSO": true, "URL": true,
}

// LabelOfKey names a value by its key, in words a person reads, for a
// request that gave no label: STRIPE_SECRET_KEY is "Stripe secret key".
func LabelOfKey(key string) string {
	parts := strings.FieldsFunc(key, func(r rune) bool { return r == '_' })
	for i, part := range parts {
		upper := strings.ToUpper(part)
		switch {
		case keyAcronyms[upper]:
			parts[i] = upper
		case i == 0:
			parts[i] = upper[:1] + strings.ToLower(part[1:])
		default:
			parts[i] = strings.ToLower(part)
		}
	}
	return strings.Join(parts, " ")
}

// requestLabelMax bounds the label to a name; requestWhyMax the why to a
// short line Mate shows whole after it.
const (
	requestLabelMax = 60
	requestWhyMax   = 160
)

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

// ValidateRequestWords refuses a label or a why the person could not read in
// one line: none, more than one line, or longer than a name or a short line.
// A second line is where a value pasted by mistake would hide.
func ValidateRequestWords(label, why string) error {
	if err := validateRequestLine("label", label, requestLabelMax,
		"label: what the value is, in people's words (Stripe secret key)."); err != nil {
		return err
	}
	return validateRequestLine("why", why, requestWhyMax,
		"why: what it is for, one short line (for checkout). Never a value.")
}

func validateRequestLine(field, text string, limit int, hint string) error {
	trimmed := strings.TrimSpace(text)
	switch {
	case trimmed == "":
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			"A request needs its "+field, hint)
	case strings.ContainsAny(trimmed, "\r\n"):
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			"The "+field+" is one line", hint)
	case utf8.RuneCountInString(trimmed) > limit:
		return platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("The %s is longer than %d characters", field, limit), hint)
	}
	return nil
}

// EnvRequestInput is what a request names: the vault (Project for Shared, else
// Hostname's own), the key, what it is in people's words (Label), the line of
// what it is for (Why) and the flag it is asked with (Sensitive; nil picks it
// by name).
type EnvRequestInput struct {
	Hostname  string
	Project   bool
	Key       string
	Label     string
	Why       string
	Sensitive *bool
	// Replace asks even though the vault holds the key: the person replaces
	// its value. ReplaceWhy is the line they read on what is wrong with it.
	Replace    bool
	ReplaceWhy string
}

// EnvRequest checks a request for a vault value and writes nothing: it
// validates the key, resolves the scope (project wins over a hostname, as in
// set/delete) and reports whether the key is already in that vault, by
// presence only — the value is never read into the result.
func EnvRequest(ctx context.Context, client platform.Client, projectID string, in EnvRequestInput) (*EnvRequestResult, error) {
	key, label, why := in.Key, in.Label, in.Why
	if err := validateRequestKey(key); err != nil {
		return nil, err
	}
	if strings.TrimSpace(label) == "" {
		label = LabelOfKey(key)
	}
	if err := ValidateRequestWords(label, why); err != nil {
		return nil, err
	}
	if in.Hostname == "" && !in.Project {
		return nil, platform.NewPlatformError(platform.ErrInvalidUsage,
			"Provide serviceHostname or set project=true",
			"project=true asks for a Shared value; serviceHostname for one service's own.")
	}

	result := &EnvRequestResult{Key: key, Label: strings.TrimSpace(label), Why: strings.TrimSpace(why), Sensitive: resolveSensitive(key, "", in.Sensitive)}
	if in.Project {
		result.Scope = EnvRequestScopeShared
		envs, err := client.GetProjectEnv(ctx, projectID)
		if err != nil {
			return nil, fmt.Errorf("read the Shared vault: %w", err)
		}
		markAlreadySet(result, envs)
		return result, nil
	}

	svc, err := resolveService(ctx, client, projectID, in.Hostname)
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
