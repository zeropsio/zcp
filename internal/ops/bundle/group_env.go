package bundle

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// What a group recipe does with a live variable.
//
// The recipe composes unattended — a reconcile, with nobody to classify a
// variable the way the export and launch flows ask the agent to — and what it
// writes lands in a repository the whole group reads, merged by the broker
// without a person when the proposal only adds files. So the composer decides
// every variable itself, and a secret's value never enters the file:
//
//   - A value made only of `${...}` references is wiring, kept as written
//     whatever its name or flag says: a reference carries no secret of its
//     own, and replacing `STORE_PUBLISHABLE_KEY: ${medusa_CHANNEL_PUBLISHABLE_KEY}`
//     with a random string would cut the storefront off its backend.
//   - Anything else is a secret when the platform flags it sensitive, when
//     it reads back masked (`REDACTED`, a read without the right to see it),
//     when its name says credential (recipeCredentialName) or when its value
//     is shaped like one (a URL carrying a password, a private key, a JWT, a
//     token in a third party's own format).
//   - Everything else is config, kept as written.
//
// The platform's flag cannot be the only signal. It is the person's, not the
// variable's: the 2026-08 migration read every older service secret back as
// sensitive:false, a project variable's flag never persisted, and ZCP_API_KEY
// — a bearer token — reads false (spec-zerops-env-lifecycle.md §7). So the
// flag can only add secrets, and so can the name and the shape; a secret all
// three miss is kept as written, which is why the flag stays the person's
// lever: mark it sensitive and the next proposal generates it.
//
// A secret becomes `<@generateRandomString(<N>)>`, N the live value's length
// and at least 16, so every environment the tier creates gets its own value
// of the shape its reader expects; an empty secret stays empty. A credential
// a third party issued — a `*_API_KEY`, a `*_TOKEN`, a webhook or client
// secret, a token in a vendor's own format — is useless regenerated, so it
// gets a line above it saying it was set by hand in the Mate and has to be
// set again.

// minGeneratedSecret is the shortest secret the recipe generates.
const minGeneratedSecret = 16

// maxGeneratedSecret is the preprocessor's own limit for generateRandomString.
const maxGeneratedSecret = 1024

// maskedSecretLength stands in for a masked value's unknown length: the
// platform recipes' own default.
const maskedSecretLength = 32

// maskedValue is what the platform returns for a sensitive value the reader
// may not see (spec-zerops-env-lifecycle.md §7).
const maskedValue = "REDACTED"

// recipeCredentialName is a key that names a credential: the last word a
// secret's (`JWT_SECRET`, `SUPERADMIN_PASSWORD`, `STRIPE_API_KEY`, `APP_KEY`,
// `HASH_SALT`), or SECRET opening the name (`SECRET_KEY_BASE`).
var recipeCredentialName = regexp.MustCompile(
	`(?i)((^|_)(KEY|APIKEY|TOKEN|SECRETS?|PASS|PASSWORD|PASSWD|PWD|SALT|PEPPER|CREDENTIALS?|AUTH)$)|(^SECRETS?_)`)

// recipePublicName is a key naming something public by design — a
// publishable key, a variable a browser bundle bakes. Its name alone does not
// make it a secret.
var recipePublicName = regexp.MustCompile(`(?i)(^|_)(PUBLIC|PUBLISHABLE)(_|$)`)

// recipeExternalName is a key naming a credential a third party issued.
var recipeExternalName = regexp.MustCompile(`(?i)(_API_?KEY|_TOKEN|_WEBHOOK_SECRET|_CLIENT_SECRET)$`)

// recipeSecretShapes are values shaped like a secret whatever their name.
var recipeSecretShapes = []*regexp.Regexp{
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
	regexp.MustCompile(`^eyJ[0-9A-Za-z_-]+\.eyJ[0-9A-Za-z_-]+\.[0-9A-Za-z_-]+$`),
}

// recipeExternalShapes are tokens in a third party's own format: Stripe,
// GitHub, GitLab, Slack, AWS, OpenAI/Anthropic, SendGrid.
var recipeExternalShapes = []*regexp.Regexp{
	regexp.MustCompile(`^(sk|rk)_(live|test)_[0-9A-Za-z]{8,}$`),
	regexp.MustCompile(`^whsec_[0-9A-Za-z]{8,}$`),
	regexp.MustCompile(`^(ghp|gho|ghu|ghs|ghr)_[0-9A-Za-z]{20,}$`),
	regexp.MustCompile(`^github_pat_[0-9A-Za-z_]{20,}$`),
	regexp.MustCompile(`^glpat-[0-9A-Za-z_-]{16,}$`),
	regexp.MustCompile(`^xox[abposr]-[0-9A-Za-z-]{10,}$`),
	regexp.MustCompile(`^AKIA[0-9A-Z]{16}$`),
	regexp.MustCompile(`^sk-[0-9A-Za-z_-]{20,}$`),
	regexp.MustCompile(`^SG\.[0-9A-Za-z_-]{16,}\.[0-9A-Za-z_-]{16,}$`),
}

// recipeSecret decides one live variable: whether its value must stay out of
// the group repo, and whether a third party issued it.
func recipeSecret(env ProjectEnvVar) (secret, external bool) {
	value := strings.TrimSpace(env.Value)
	if value != "" && isPureWiring(value) {
		return false, false
	}
	external = recipeExternalName.MatchString(env.Key) || matchesAny(recipeExternalShapes, value)
	secret = env.Sensitive ||
		value == maskedValue ||
		external ||
		(recipeCredentialName.MatchString(env.Key) && !recipePublicName.MatchString(env.Key)) ||
		matchesAny(recipeSecretShapes, value) ||
		urlCarriesPassword(value)
	return secret, secret && external
}

// generatedSecret is the value a secret is written as: a generator as long as
// the live value (16 to 1024 characters), 32 for a masked one whose length is
// unknown, empty for an empty one.
func generatedSecret(value string) string {
	if value == "" {
		return ""
	}
	n := maskedSecretLength
	if value != maskedValue {
		n = min(max(utf8.RuneCountInString(value), minGeneratedSecret), maxGeneratedSecret)
	}
	return fmt.Sprintf("<@generateRandomString(<%d>)>", n)
}

// isPureWiring reports whether a value is only `${...}` references, joined by
// nothing that could be a secret of its own.
func isPureWiring(value string) bool {
	if !strings.Contains(value, "${") {
		return false
	}
	rest := stripReferences(value)
	return !strings.ContainsFunc(rest, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) })
}

// stripReferences removes every complete `${...}` from a value.
func stripReferences(value string) string {
	var b strings.Builder
	for {
		start := strings.Index(value, "${")
		if start < 0 {
			break
		}
		end := strings.Index(value[start:], "}")
		if end < 0 {
			break
		}
		b.WriteString(value[:start])
		value = value[start+end+1:]
	}
	b.WriteString(value)
	return b.String()
}

// urlCarriesPassword reports a URL whose userinfo holds a literal password —
// `postgresql://user:pass@host`, not `postgresql://${db_user}:${db_password}@…`.
func urlCarriesPassword(value string) bool {
	_, rest, ok := strings.Cut(value, "://")
	if !ok {
		return false
	}
	authority := rest
	if end := strings.IndexAny(authority, "/?#"); end >= 0 {
		authority = authority[:end]
	}
	at := strings.LastIndex(authority, "@")
	if at < 0 {
		return false
	}
	_, password, hasPassword := strings.Cut(authority[:at], ":")
	return hasPassword && strings.TrimSpace(stripReferences(password)) != ""
}

func matchesAny(patterns []*regexp.Regexp, value string) bool {
	for _, p := range patterns {
		if p.MatchString(value) {
			return true
		}
	}
	return false
}

// groupEnvFields writes live variables for a tier, sorted by key: config as
// written, secrets generated, a third party's credential with a line saying
// it has to be set again. It returns the config and the secrets apart — a
// project keeps them under envVariables and envSecrets, a service carries
// both under envSecrets, the only channel an import has for its variables.
func groupEnvFields(envs []ProjectEnvVar, source string) (config, secrets []yamlField) {
	sorted := append([]ProjectEnvVar(nil), envs...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Key < sorted[j].Key })
	for _, env := range sorted {
		secret, external := recipeSecret(env)
		if !secret {
			config = append(config, yamlField{key: env.Key, value: env.Value})
			continue
		}
		field := yamlField{key: env.Key, value: generatedSecret(env.Value)}
		if external && env.Value != "" {
			field.comment = fmt.Sprintf("Set by hand in %s; set it again here.", source)
		}
		secrets = append(secrets, field)
	}
	return config, secrets
}

// serviceSecretFields is a service's envSecrets: its config and its secrets
// together, sorted by key.
func serviceSecretFields(envs []ProjectEnvVar, source string) []yamlField {
	config, secrets := groupEnvFields(envs, source)
	fields := make([]yamlField, 0, len(config)+len(secrets))
	fields = append(fields, config...)
	fields = append(fields, secrets...)
	sort.SliceStable(fields, func(i, j int) bool { return fields[i].key < fields[j].key })
	return fields
}
