package bundle

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// What a group recipe does with a live variable.
//
// The recipe composes unattended — a reconcile, with nobody to classify a
// variable the way the export and launch flows ask the agent to — and what it
// writes lands in a repository the whole group reads, landed by Core without
// a person when the proposal only adds files. So the composer decides
// every variable itself, and it fails closed: a value is written as it is
// only when nothing says secret and the value has a config shape.
//
//   - A value made only of `${name}` references is wiring, kept as written
//     whatever its name or flag says: a reference carries no secret of its
//     own, and replacing `STORE_PUBLISHABLE_KEY: ${medusa_CHANNEL_PUBLISHABLE_KEY}`
//     with a random string would cut the storefront off its backend.
//   - A value the platform flags sensitive or reads back masked (`REDACTED`,
//     a read without the right to see it) is generated.
//   - A name public by design — PUBLIC or PUBLISHABLE a word of it, and no
//     SECRET, PASSWORD or PRIVATE beside it — is written as it is: a browser
//     bundle ships its value anyway. A private key or a vendor's secret key
//     under it is still generated.
//   - Anything else is generated when its name says credential
//     (recipeCredentialName), when its value is shaped like a secret (a
//     private key, a JWT, a URL carrying a password), or when its value has
//     none of the narrow shapes a person writes a setting in
//     (recipeConfigShape) — an opaque value is regenerated, never published.
//
// The platform's flag cannot be the only signal. It is the person's, not the
// variable's: the 2026-08 migration read every older service secret back as
// sensitive:false, a project variable's flag never persisted, and ZCP_API_KEY
// — a bearer token — reads false (spec-zerops-env-lifecycle.md §7). So the
// flag, the name and the shape can each only add secrets, and a value none of
// them marks is still written only in a config shape.
//
// A secret becomes `<@generateRandomString(<N>)>`, N the live value's length
// and at least 16, so every environment the tier creates gets its own value
// of the shape its reader expects; an empty secret stays empty. A generated
// value gets a line above it saying it was set by hand in the Mate and has to
// be set again — all but a secret an app makes for itself (recipeAppSecret),
// which a fresh environment simply generates anew.

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

// recipeCredentialWords are the words a credential goes by, wherever they
// stand in a name. A name is the only signal for what no shape tells from a
// hostname or a word — a one-case run of letters, a passphrase, a PIN — so
// the list is wide: a seed, a PIN, a phrase. AUTH is one only as the last
// word (`MP_UI_AUTH`): before another it names a mechanism
// (`AUTH_PROVIDER`). CODE is none: a ZIP_CODE is config.
var recipeCredentialWords = map[string]bool{
	"PASS": true, "PWD": true, "PW": true, "PIN": true, "PINS": true,
	"SALT": true, "SALTS": true, "PEPPER": true, "PEPPERS": true, "CREDS": true,
}

// recipeCredentialEndings end a word that names a credential, alone or run
// into the words before it: `PASSWORD`, `DBPASSWORD`, `SECRETKEY`,
// `MASTERKEY`, `AUTHTOKEN`, `jwtSecret`'s `SECRET`.
var recipeCredentialEndings = []string{
	"PASSWORD", "PASSWORDS", "PASSWD", "PASSPHRASE", "PASSPHRASES", "PASSCODE", "PASSCODES",
	"SECRET", "SECRETS", "TOKEN", "TOKENS", "KEY", "KEYS", "CREDENTIAL", "CREDENTIALS",
	"MNEMONIC", "MNEMONICS", "PHRASE", "PHRASES", "SEED", "SEEDS", "WEBHOOK", "WEBHOOKS",
}

// recipeAddressWords are the property words a webhook's address goes by:
// the address of a webhook is its credential, so they leave it one.
var recipeAddressWords = map[string]bool{"URL": true, "URI": true, "ENDPOINT": true}

// recipePropertyWords, after a credential word, make the name one about the
// credential rather than the credential itself: `TOKEN_TTL`,
// `CACHE_KEY_PREFIX`, `JWT_SECRET_EXPIRES_IN`, `DB_PASSWORD_FILE`. Any other
// word after it — `DB_PASSWORD_PROD` — leaves it a credential. A missing word
// here costs setting a value again; its value is still judged by its shape.
var recipePropertyWords = map[string]bool{
	"TTL": true, "EXPIRY": true, "EXPIRE": true, "EXPIRES": true, "EXPIRATION": true,
	"LIFETIME": true, "AGE": true, "TIMEOUT": true, "INTERVAL": true, "ROTATION": true,
	"LENGTH": true, "LEN": true, "SIZE": true, "BITS": true, "MIN": true, "MAX": true,
	"PREFIX": true, "SUFFIX": true, "HEADER": true, "NAME": true, "FIELD": true, "PARAM": true,
	"TYPE": true, "ALGORITHM": true, "ALG": true, "ISSUER": true, "AUDIENCE": true,
	"URL": true, "URI": true, "ENDPOINT": true, "HOST": true, "PORT": true, "REGION": true,
	"PATH": true, "FILE": true, "DIR": true, "ID": true,
	"ENABLED": true, "DISABLED": true, "REQUIRED": true, "MODE": true, "PROVIDER": true,
}

// recipeCredentialName reports a name that says credential: a credential
// word (recipeCredentialWords, recipeCredentialEndings) with no property word
// after it — but for a webhook's address, which is the credential.
func recipeCredentialName(key string) bool {
	words := recipeNameWords(key)
	for i, word := range words {
		credential := recipeCredentialWords[word] ||
			(word == "AUTH" && i == len(words)-1) ||
			hasAnySuffix(word, recipeCredentialEndings)
		webhook := hasAnySuffix(word, []string{"WEBHOOK", "WEBHOOKS"})
		property := func(w string) bool { return recipePropertyWords[w] && (!webhook || !recipeAddressWords[w]) }
		if credential && !slices.ContainsFunc(words[i+1:], property) {
			return true
		}
	}
	return false
}

// recipeBoolean is a switch's value: it hides nothing, whatever the name.
var recipeBoolean = regexp.MustCompile(`^(?i:true|false|yes|no|on|off|0|1)$`)

// recipePublicName reports a name public by design — PUBLIC or PUBLISHABLE a
// word of it: `NEXT_PUBLIC_*`, `*_PUBLISHABLE_KEY` — that says nothing
// secret beside it: in `S3_PUBLIC_BUCKET_SECRET_KEY` the value is the
// secret.
func recipePublicName(key string) bool {
	words := recipeNameWords(key)
	if slices.ContainsFunc(words, recipeSecretWord) {
		return false
	}
	return slices.ContainsFunc(words, func(w string) bool { return w == "PUBLIC" || w == "PUBLISHABLE" })
}

// recipeSecretWords end a name's word that says its value is secret
// whatever else the name says.
var recipeSecretWords = []string{"SECRET", "SECRETS", "PASSWORD", "PASSWORDS", "PASSWD", "PASSPHRASE", "PASSPHRASES", "PRIVATEKEY"}

// recipeSecretWord reports a word that says secret: PRIVATE, and SECRET and
// PASSWORD in any of their forms.
func recipeSecretWord(word string) bool {
	switch word {
	case "PRIVATE", "PASS", "PWD", "PW":
		return true
	}
	return hasAnySuffix(word, recipeSecretWords)
}

// recipeNameWords splits a variable's name into its words, upper-cased: at
// `_ - .`, and where a lower-case letter or a digit meets an upper-case one
// (`jwtSecret` → JWT, SECRET).
func recipeNameWords(key string) []string {
	var words []string
	var word strings.Builder
	prev := rune(0)
	flush := func() {
		if word.Len() > 0 {
			words = append(words, word.String())
			word.Reset()
		}
	}
	for _, r := range key {
		switch {
		case r == '_' || r == '-' || r == '.':
			flush()
			prev = 0
			continue
		case unicode.IsUpper(r) && (unicode.IsLower(prev) || unicode.IsDigit(prev)):
			flush()
		}
		word.WriteRune(unicode.ToUpper(r))
		prev = r
	}
	flush()
	return words
}

// recipeAppSecretEndings end the name of a secret an app makes for itself.
var recipeAppSecretEndings = []string{"SECRET", "SALT", "PEPPER", "PASSWORD", "PASS", "PASSPHRASE", "KEY_BASE"}

// recipeAppSecret reports a secret an app makes for itself — a name ending in
// SECRET, SALT, PEPPER, PASSWORD, PASS, PASSPHRASE or KEY_BASE, and APP_KEY
// and APP_KEYS — which a fresh environment simply generates anew. A webhook's
// or a client's secret, or a value in a vendor's format, is the vendor's
// whatever its name ends in.
func recipeAppSecret(key, value string) bool {
	upper := strings.ToUpper(key)
	if recipeThirdPartySecretName.MatchString(upper) || matchesAny(recipeExternalShapes, value) {
		return false
	}
	return upper == "APP_KEY" || upper == "APP_KEYS" || hasAnySuffix(upper, recipeAppSecretEndings)
}

// recipeThirdPartySecretName is a secret a vendor issues under a name that
// ends in SECRET: Stripe's webhook secret, an OAuth client's.
var recipeThirdPartySecretName = regexp.MustCompile(`(WEBHOOK|CLIENT)_?SECRETS?$`)

// recipePrivateKeyShape is a private key, PEM or PGP.
var recipePrivateKeyShape = regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY`)

// recipeSecretShapes are values shaped like a secret whatever their name: a
// private key, a JWT.
var recipeSecretShapes = []*regexp.Regexp{
	recipePrivateKeyShape,
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

// recipeSecret decides one live variable: whether its value stays out of the
// group repo, and whether the generated value asks a person to set it again.
func recipeSecret(env ProjectEnvVar) (secret, setAgain bool) {
	value := strings.TrimSpace(env.Value)
	if isStrictWiring(value) {
		return false, false
	}
	if value == "" {
		return env.Sensitive || (recipeCredentialName(env.Key) && !recipePublicName(env.Key)), false
	}
	flagged := env.Sensitive || value == maskedValue
	// A public name is public only for a value that is no key: a private key
	// or a vendor's secret under NEXT_PUBLIC_ is a secret in the wrong place.
	public := recipePublicName(env.Key) && !recipePrivateKeyShape.MatchString(value) && !matchesAny(recipeExternalShapes, value)
	if !flagged && (public || !recipeLooksSecret(env.Key, value)) {
		return false, false
	}
	return true, !recipeAppSecret(env.Key, value)
}

// recipeLooksSecret reports a value its name or its shape keeps out of the
// repo: a credential's name — unless the value is a switch — a secret's or a
// vendor's shape, or no config shape at all.
func recipeLooksSecret(key, value string) bool {
	return (recipeCredentialName(key) && !recipeBoolean.MatchString(value)) ||
		matchesAny(recipeSecretShapes, value) ||
		matchesAny(recipeExternalShapes, value) ||
		urlCarriesPassword(value) ||
		!recipeConfigShape(value)
}

// generatedSecret is the value a secret is written as: a generator as long as
// the live value (16 to 1024 characters), 32 for a masked one whose length is
// unknown, empty for an empty one — keeping the shape its reader parses:
//
//   - `base64:…`, Laravel's APP_KEY, becomes 32 generated characters, the raw
//     key Laravel takes for AES-256 and what the platform's Laravel recipes
//     write; the same length behind the prefix would decode to a key of the
//     wrong size.
//   - `user:password`, a basic-auth pair like mailpit's MP_UI_AUTH, keeps a
//     user that is a plain word and has only the password generated: the name
//     is no secret, and a value without the colon is no pair at all.
func generatedSecret(value string) string {
	if value == "" {
		return ""
	}
	if value == maskedValue {
		return generator(maskedSecretLength)
	}
	if strings.HasPrefix(value, "base64:") {
		return generator(laravelKeyLength)
	}
	if user, password, ok := basicAuthPair(value); ok {
		return user + ":" + generator(utf8.RuneCountInString(password))
	}
	return generator(utf8.RuneCountInString(value))
}

// laravelKeyLength is the raw key Laravel takes for AES-256-CBC.
const laravelKeyLength = 32

// generator is the preprocessor directive for a random string of n
// characters, kept between 16 and the preprocessor's 1024.
func generator(n int) string {
	return fmt.Sprintf("<@generateRandomString(<%d>)>", min(max(n, minGeneratedSecret), maxGeneratedSecret))
}

// basicAuthPair splits a `user:password` value whose user is a plain word
// (`admin`, `mailpit`): anything else before the colon — an access key's id,
// a token, a UUID — is as secret as what follows it, and the whole value is
// generated. A URL, a value with spaces or a second colon is no pair.
func basicAuthPair(value string) (user, password string, ok bool) {
	if strings.Contains(value, "://") || strings.ContainsFunc(value, unicode.IsSpace) {
		return "", "", false
	}
	user, password, found := strings.Cut(value, ":")
	if !found || password == "" || strings.Contains(password, ":") || !recipePlainWord.MatchString(user) {
		return "", "", false
	}
	return user, password, true
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

// hasAnySuffix reports whether s ends in any of suffixes.
func hasAnySuffix(s string, suffixes []string) bool {
	return slices.ContainsFunc(suffixes, func(suffix string) bool { return strings.HasSuffix(s, suffix) })
}

func matchesAny(patterns []*regexp.Regexp, value string) bool {
	for _, p := range patterns {
		if p.MatchString(value) {
			return true
		}
	}
	return false
}

// groupVaultFields writes live variables for a tier as a `vault:` block,
// sorted by key: config as written — through promote, which names a group
// environment's own runtimes (nil keeps it as it is) — secrets generated,
// each but an app's own with a line saying it has to be set again. Each
// value is sensitive per newVaultValue, a generated secret counting as one.
// The project's vault and each service's own carry the same shape.
func groupVaultFields(envs []ProjectEnvVar, source string, promote func(string) string) []yamlField {
	sorted := append([]ProjectEnvVar(nil), envs...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Key < sorted[j].Key })
	fields := make([]yamlField, 0, len(sorted))
	for _, env := range sorted {
		secret, setAgain := recipeSecret(env)
		if !secret {
			value := env.Value
			if promote != nil {
				value = promote(value)
			}
			fields = append(fields, yamlField{key: env.Key, value: newVaultValue(env.Key, value, false)})
			continue
		}
		field := yamlField{key: env.Key, value: newVaultValue(env.Key, generatedSecret(env.Value), true)}
		if setAgain {
			field.comment = fmt.Sprintf("Set by hand in %s; set it again here.", source)
		}
		fields = append(fields, field)
	}
	return fields
}
