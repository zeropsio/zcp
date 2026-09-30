package bundle

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// The shapes a group recipe writes a live value in as it is.
//
// recipeSecret fails closed: a value nothing marks secret is written as it is
// only in one of the narrow shapes a person writes a setting in, and is
// generated otherwise. A shape missing here costs a person setting one value
// again — the generated value says so — while a shape too wide publishes a
// secret to the whole group, so every shape is built from words a person
// writes and nothing a generator makes. A word is letters, at most 24, whose
// case changes at most three times (`production`, `PostgreSQL`, never a
// generator's random mix), and it may end in two digits (`v1`, `local0`); a
// `${name}` reference stands wherever a word may.
//
//   - Nothing, or wiring: only references, joined by punctuation or spaces.
//   - A number of up to ten digits, a duration or a size (`30s`, `512M`,
//     `1Gi`), a percentage, a version (`v3.0.1`).
//   - A word or a phrase of words joined by spaces or `_ . - /`, up to 64
//     characters — `true`, `production`, `Europe/Prague`, `us-east-1`, `wp_` —
//     where a number of one or two digits may follow a word.
//   - An email address.
//   - A URL, or a host with its port, that carries nothing but its place: no
//     query, no fragment, no user but references (`${db_user}:${db_password}@`),
//     a host of DNS labels, and a path of word segments — so a webhook's
//     `/services/T024BE7LD/…` is no config.
//   - An absolute path of word segments.
//   - A URL may carry a query whose every key names no credential and whose
//     every value is a word, a number or a reference (`?sslmode=require`);
//     `?password=`, `?key=`, `?token=`, `?sig=` make it no config.
//   - A glob: words and a standalone `*` (`*`, `medusa:*`, `*.example.com`).
//   - A cron line (`0 3 * * *`, `*/15 9-17 * * MON-FRI`, `@daily`).
//   - A mailbox: a phrase and an email address (`Acme <noreply@acme.example>`).
//   - A container image with its tag (`ghcr.io/acme/worker:1.2.3`); a bare
//     name's tag opens with a version (`node:22-alpine`), so `admin:hunter`
//     is none.
//   - A comma-separated list of the above.
//   - Command-line flags (`--max-old-space-size=4096`) no flag or value of
//     which names a credential.
//
// A reference's default (`${PORT:-587}`) is what the value reads whenever the
// variable is unset, so a value is config only when it is config both ways.

// maxPhrase is the longest phrase written as it is.
const maxPhrase = 64

// recipeIdentifier is a reference's name.
var recipeIdentifier = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// recipeReference is one `${name}` reference.
var recipeReference = regexp.MustCompile(`\$\{[A-Za-z0-9_]+\}`)

// recipeWordShape is a word's letters and the two digits it may end in;
// isWord adds the case rule.
var recipeWordShape = regexp.MustCompile(`^[A-Za-z]{1,24}[0-9]{0,2}$`)

// recipePlainWord is a word in one case, or capitalized: `production`,
// `UTF`, `Prague`. Beside `_` or `-` — where base64url puts its separators —
// a word is plain, so a generator's short mixed-case runs are no phrase.
var recipePlainWord = regexp.MustCompile(`^([a-z]{1,24}|[A-Z]{1,24}|[A-Z][a-z]{1,23})[0-9]{0,2}$`)

// recipeShortNumber is a number that may follow a word: `us-east-1`.
var recipeShortNumber = regexp.MustCompile(`^[0-9]{1,2}$`)

// recipeLabelNumber is a number a DNS label carries: an address's octet, a
// port in a subdomain (`api-${zeropsSubdomainHost}-3000`).
var recipeLabelNumber = regexp.MustCompile(`^[0-9]{1,5}$`)

// recipeTopLevel is the last label of a bare host: letters.
var recipeTopLevel = regexp.MustCompile(`^[A-Za-z]{2,24}$`)

// recipeIPv4 is an IPv4 address.
var recipeIPv4 = regexp.MustCompile(`^([0-9]{1,3}\.){3}[0-9]{1,3}$`)

// recipePort is a port.
var recipePort = regexp.MustCompile(`^[0-9]{1,5}$`)

// recipeScheme is a URL's scheme.
var recipeScheme = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]{0,31}$`)

// recipeScalarShapes are a number, a duration or a size, a Go duration, a
// percentage and a version. A number keeps to ten digits: a longer run is an
// ID or a key more often than a setting.
var recipeScalarShapes = []*regexp.Regexp{
	regexp.MustCompile(`^[-+]?[0-9]{1,10}(\.[0-9]{1,6})?$`),
	regexp.MustCompile(`^[0-9]{1,10}(\.[0-9]{1,6})?([nuµm]?s|m|h|d|w|y|[kKmMgGtTpP]i?[bB]?|[bB])$`),
	regexp.MustCompile(`^([0-9]{1,6}(\.[0-9]{1,6})?([nuµm]?s|m|h))+$`),
	regexp.MustCompile(`^[0-9]{1,3}(\.[0-9]{1,6})?%$`),
	regexp.MustCompile(`^v?[0-9]{1,6}(\.[0-9]{1,6}){1,3}(-[A-Za-z]{1,12}(\.?[0-9]{1,8})?)?$`),
}

// recipeQueryKey is a URL query's key.
var recipeQueryKey = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,63}$`)

// recipeQueryNumber is a URL query's number: a count, a switch, a moment.
var recipeQueryNumber = regexp.MustCompile(`^[0-9]{1,10}$`)

// recipeCronField is one field of a cron line: `*`, a number, a range, a
// day's or a month's name, with a step, in a comma list.
var recipeCronField = regexp.MustCompile(`^(?i:(\*|[0-9]{1,2}(-[0-9]{1,2})?|(mon|tue|wed|thu|fri|sat|sun|jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)(-(mon|tue|wed|thu|fri|sat|sun|jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec))?)(/[0-9]{1,2})?)(,(\*|[0-9]{1,2}(-[0-9]{1,2})?)(/[0-9]{1,2})?)*$`)

// recipeCronNickname is a cron line's nickname.
var recipeCronNickname = regexp.MustCompile(`^@(yearly|annually|monthly|weekly|daily|midnight|hourly|reboot)$`)

// recipeMailbox is a mailbox: a name and an address in angle brackets.
var recipeMailbox = regexp.MustCompile(`^"?([^"<>]+?)"? <([^<>\s]+)>$`)

// recipeFlagName is a command-line flag, built from words so that no token a
// generator opens with a dash reads as one: `--max-old-space-size`, `-v`,
// `-Xmx512m`, `-XX:+UseG1GC`, and a system property `-Dfile.encoding` —
// always dotted, which base64url never is.
var recipeFlagName = regexp.MustCompile(`^(` +
	`--[a-z]{1,24}[0-9]{0,2}(-[a-z]{1,24}[0-9]{0,2})*` +
	`|-[a-z]{1,24}[0-9]{0,2}` +
	`|-X[a-z]{2,3}[0-9]{1,6}[kKmMgGtT]?` +
	`|-XX:[+-]?[A-Za-z][A-Za-z0-9]{0,63}` +
	`|-D[A-Za-z][A-Za-z0-9_-]*(\.[A-Za-z][A-Za-z0-9_-]*)+` +
	`)$`)

// recipeFlagCredential is a word no flag and no flag's value may carry.
var recipeFlagCredential = regexp.MustCompile(`(?i)password|secret|token|key|auth|pass`)

// isStrictWiring reports a value made only of `${name}` references, joined by
// punctuation or spaces: nothing in it is a literal a secret could be. A
// reference with a default is not wiring — its default is a literal.
func isStrictWiring(value string) bool {
	if !recipeReference.MatchString(value) {
		return false
	}
	rest := recipeReference.ReplaceAllString(value, "")
	return !strings.ContainsFunc(rest, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) })
}

// recipeConfigShape reports a value in a shape a person writes a setting in —
// as the platform reads it, and with every reference's default in its place.
// A setting is one line: a line break splits a list or a flag into what
// no shape vouches for.
func recipeConfigShape(value string) bool {
	value = strings.TrimSpace(value)
	if strings.ContainsAny(value, "\n\r") {
		return false
	}
	referenced, defaulted, ok := resolveDefaults(value)
	return ok && isConfigValue(referenced) && isConfigValue(defaulted)
}

// resolveDefaults reads a value both ways a reference with a default
// (`${name:-default}`) can resolve: as `${name}`, and as its default. ok is
// false when `${` opens anything but a reference.
func resolveDefaults(value string) (referenced, defaulted string, ok bool) {
	var ref, def strings.Builder
	for {
		start := strings.Index(value, "${")
		if start < 0 {
			ref.WriteString(value)
			def.WriteString(value)
			return ref.String(), def.String(), true
		}
		length := strings.IndexByte(value[start:], '}')
		if length < 0 {
			return "", "", false
		}
		name, fallback, hasDefault := strings.Cut(value[start+2:start+length], ":-")
		if !recipeIdentifier.MatchString(name) {
			return "", "", false
		}
		ref.WriteString(value[:start] + "${" + name + "}")
		def.WriteString(value[:start])
		if hasDefault {
			def.WriteString(fallback)
		} else {
			def.WriteString("${" + name + "}")
		}
		value = value[start+length+1:]
	}
}

// isConfigValue reports a whole value in a config shape, its references
// already without defaults.
func isConfigValue(value string) bool {
	return value == "" || isStrictWiring(value) || isConfigItem(value) || isConfigList(value) || isFlagList(value)
}

// isConfigItem reports one setting: a scalar, a phrase, an email address, an
// address, an absolute path, a glob, a cron line, a mailbox or an image.
func isConfigItem(value string) bool {
	return matchesAny(recipeScalarShapes, value) || isPhrase(value) || isEmail(value) || isAddress(value) ||
		isAbsolutePath(value) || isGlob(value) || isCron(value) || isMailbox(value) || isImageRef(value)
}

// isConfigList reports settings separated by commas: CORS origins, hosts,
// words.
func isConfigList(value string) bool {
	if !strings.Contains(value, ",") {
		return false
	}
	for item := range strings.SplitSeq(value, ",") {
		if item = strings.TrimSpace(item); item == "" || !isConfigItem(item) {
			return false
		}
	}
	return true
}

// isFlagList reports the flags a runtime is started with —
// `--max-old-space-size=4096`, `-Xmx512m -Dspring.profiles.active=prod` —
// each flag's value, after `=` or after the flag, a setting of its own. No
// credential goes by a flag: none names one (recipeCredentialName on what
// it names, `-Dspring.datasource.pwd`), and no credential word stands
// anywhere in a field.
func isFlagList(value string) bool {
	flags := 0
	for field := range strings.FieldsSeq(value) {
		if recipeFlagCredential.MatchString(field) {
			return false
		}
		if !strings.HasPrefix(field, "-") || matchesAny(recipeScalarShapes, field) {
			// A flag's own value, standing after it: `-r dotenv/config`.
			if !isConfigItem(field) {
				return false
			}
			continue
		}
		name, setting, hasSetting := strings.Cut(field, "=")
		if !recipeFlagName.MatchString(name) || recipeCredentialName(flagNameBody(name)) {
			return false
		}
		if hasSetting && !isConfigItem(setting) && !isConfigList(setting) {
			return false
		}
		flags++
	}
	return flags > 0
}

// flagNameBody is what a flag names: `db-pw` of `--db-pw`,
// `spring.datasource.pwd` of `-Dspring.datasource.pwd`, `UseG1GC` of
// `-XX:+UseG1GC`.
func flagNameBody(name string) string {
	if rest, ok := strings.CutPrefix(name, "-XX:"); ok {
		return strings.TrimLeft(rest, "+-")
	}
	if rest, ok := strings.CutPrefix(name, "-D"); ok && strings.Contains(rest, ".") {
		return rest
	}
	return strings.TrimLeft(name, "-")
}

// isPhrase reports a word, or words and references joined by spaces or
// `_ . - /` — plain words where `_` or `-` joins them. A number of one or two
// digits may follow a word, never open the phrase.
func isPhrase(value string) bool {
	if len(value) > maxPhrase {
		return false
	}
	plain := strings.ContainsAny(value, "_-")
	words := 0
	for _, token := range recipeTokens(value, " _./-") {
		switch {
		case token == "":
		case isReference(token) || isWordIn(token, plain):
			words++
		case words > 0 && recipeShortNumber.MatchString(token):
		default:
			return false
		}
	}
	return words > 0
}

// isEmail reports an email address: words joined by `. _ - +` before the
// `@`, a bare host after it.
func isEmail(value string) bool {
	local, domain, ok := strings.Cut(value, "@")
	if !ok || local == "" {
		return false
	}
	for _, token := range recipeTokens(local, "._-+") {
		if token != "" && !isReference(token) && !isWordIn(token, true) && !recipeShortNumber.MatchString(token) {
			return false
		}
	}
	return isHost(domain, true)
}

// isAddress reports a URL, or a host with its port, that carries nothing but
// its place: no fragment, a user made only of references, a host (isHost), a
// port that is a number or a reference, a path of word segments, and a query
// of settings (isPlainQuery).
func isAddress(value string) bool {
	rest, scheme := value, false
	if name, after, ok := strings.Cut(value, "://"); ok {
		if !recipeScheme.MatchString(name) {
			return false
		}
		rest, scheme = after, true
	}
	if strings.Contains(rest, "#") {
		return false
	}
	rest, query, hasQuery := strings.Cut(rest, "?")
	if hasQuery && !isPlainQuery(query) {
		return false
	}
	authority, path, hasPath := strings.Cut(rest, "/")
	if at := strings.LastIndexByte(authority, '@'); at >= 0 {
		if !scheme || !isStrictWiring(authority[:at]) {
			return false
		}
		authority = authority[at+1:]
	}
	host, port, hasPort := strings.Cut(authority, ":")
	if hasPort && !isReference(port) && !recipePort.MatchString(port) {
		return false
	}
	if !isHost(host, !scheme && !hasPort) {
		return false
	}
	return !hasPath || isPathSegments(path)
}

// isPlainQuery reports a URL query of settings: every key a word naming no
// credential — no password, key, token, secret or signature
// (recipeCredentialName, SIG) — and every value empty, a word, a number or a
// reference.
func isPlainQuery(query string) bool {
	if query == "" {
		return false
	}
	for pair := range strings.SplitSeq(query, "&") {
		key, value, _ := strings.Cut(pair, "=")
		if !recipeQueryKey.MatchString(key) || recipeCredentialName(key) || slices.ContainsFunc(recipeNameWords(key), func(w string) bool {
			return w == "SIG" || w == "SIGNATURE"
		}) {
			return false
		}
		if value != "" && !isStrictWiring(value) && !recipeQueryNumber.MatchString(value) && !isQueryWord(value) {
			return false
		}
	}
	return true
}

// isQueryWord reports a query's value made of words: `require`,
// `verify-full`, `admin`.
func isQueryWord(value string) bool {
	plain := strings.ContainsAny(value, "_-")
	words := 0
	for _, token := range recipeTokens(value, "._-") {
		switch {
		case token == "":
		case isWordIn(token, plain):
			words++
		case !recipeShortNumber.MatchString(token):
			return false
		}
	}
	return words > 0
}

// isGlob reports a pattern of words and a standalone `*` joined by
// `: . / - _`: `*`, `medusa:*`, `*.example.com`, `https://*.example.com`. A
// star inside a token (`hunter*`) is no glob.
func isGlob(value string) bool {
	if len(value) > maxPhrase || !strings.Contains(value, "*") {
		return false
	}
	plain := strings.ContainsAny(value, "_-")
	for _, token := range recipeTokens(value, ":./-_") {
		if token != "" && token != "*" && !isReference(token) && !isWordIn(token, plain) && !recipeShortNumber.MatchString(token) {
			return false
		}
	}
	return true
}

// isCron reports a cron line: five or six fields, or a nickname.
func isCron(value string) bool {
	if recipeCronNickname.MatchString(value) {
		return true
	}
	fields := strings.Split(value, " ")
	if len(fields) != 5 && len(fields) != 6 {
		return false
	}
	for _, field := range fields {
		if !recipeCronField.MatchString(field) {
			return false
		}
	}
	return true
}

// isMailbox reports a name and an email address: `Acme <noreply@acme.example>`.
func isMailbox(value string) bool {
	m := recipeMailbox.FindStringSubmatch(value)
	return m != nil && isPhrase(m[1]) && isEmail(m[2])
}

// isImageRef reports a container image with its tag: a repository of plain
// words, behind a registry host or not — `ghcr.io/acme/worker:1.2.3`,
// `acme/worker:latest` — or an official image whose tag opens with a
// version, `node:22-alpine`, so a bare `admin:hunter` is none. A digest is a
// hash, and no image here.
func isImageRef(value string) bool {
	slash := strings.LastIndexByte(value, '/')
	colon := strings.LastIndexByte(value, ':')
	if colon <= slash || strings.ContainsAny(value, "@ ") {
		return false
	}
	name, tag := value[:colon], value[colon+1:]
	if len(tag) > 128 || !isImagePart(tag) {
		return false
	}
	parts := strings.Split(name, "/")
	if len(parts) == 1 {
		return isImagePart(parts[0]) && tag[0] >= '0' && tag[0] <= '9'
	}
	if host, port, hasPort := strings.Cut(parts[0], ":"); hasPort || strings.Contains(host, ".") || host == "localhost" {
		if (hasPort && !recipePort.MatchString(port)) || !isHost(host, false) {
			return false
		}
		parts = parts[1:]
	}
	return len(parts) > 0 && !slices.ContainsFunc(parts, func(part string) bool { return !isImagePart(part) })
}

// isImagePart reports one part of an image — a repository name or a tag —
// made of plain words and numbers joined by `. _ -`.
func isImagePart(part string) bool {
	for _, token := range recipeTokens(part, "._-") {
		if !recipePlainWord.MatchString(token) && !recipeQueryNumber.MatchString(token) {
			return false
		}
	}
	return true
}

// isHost reports a host: DNS labels of words, numbers of up to five digits
// and references joined by `-` — `medusa-${zeropsSubdomainHost}-9000.prg1.zerops.app`.
// A bare one, with no scheme and no port to say it is a host, must be a
// reference alone, an IPv4 address, or two labels or more ending in letters
// (`redis.internal`): a single word is a phrase's to judge.
func isHost(host string, bare bool) bool {
	if host == "" {
		return false
	}
	labels := strings.Split(host, ".")
	for _, label := range labels {
		if !isHostLabel(label) {
			return false
		}
	}
	switch {
	case !bare:
		return true
	case len(labels) == 1:
		return isReference(host)
	case recipeIPv4.MatchString(host):
		return true
	default:
		last := labels[len(labels)-1]
		return isReference(last) || recipeTopLevel.MatchString(last)
	}
}

// isHostLabel reports one DNS label of plain words, numbers and references
// joined by `-`.
func isHostLabel(label string) bool {
	parts := 0
	for _, token := range recipeTokens(label, "-") {
		switch {
		case token == "":
		case isReference(token) || isWordIn(token, true) || recipeLabelNumber.MatchString(token):
			parts++
		default:
			return false
		}
	}
	return parts > 0
}

// isAbsolutePath reports an absolute filesystem path of word segments.
func isAbsolutePath(value string) bool {
	rest, ok := strings.CutPrefix(value, "/")
	return ok && isPathSegments(rest)
}

// isPathSegments reports path segments made of words, numbers of one or two
// digits and references joined by `. _ -` — plain words where `_` or `-`
// joins them: `v1`, `store/search`, `index.html`, `.well-known`,
// `python3.11`.
func isPathSegments(path string) bool {
	for segment := range strings.SplitSeq(path, "/") {
		plain := strings.ContainsAny(segment, "_-")
		for _, token := range recipeTokens(segment, "._-") {
			if token != "" && !isReference(token) && !isWordIn(token, plain) && !recipeShortNumber.MatchString(token) {
				return false
			}
		}
	}
	return true
}

// isWord reports a word a person wrote: letters whose case changes at most
// three times — `production`, `PostgreSQL`, `XMLHttpRequest`, where a
// generator mixes cases at random — that may end in two digits.
func isWord(token string) bool {
	if !recipeWordShape.MatchString(token) {
		return false
	}
	changes := 0
	for i := 1; i < len(token); i++ {
		prev, cur := rune(token[i-1]), rune(token[i])
		if unicode.IsLetter(prev) && unicode.IsLetter(cur) && unicode.IsUpper(prev) != unicode.IsUpper(cur) {
			changes++
		}
	}
	return changes <= 3
}

// isWordIn reports a word — a plain one when plain is set.
func isWordIn(token string, plain bool) bool {
	if plain {
		return recipePlainWord.MatchString(token)
	}
	return isWord(token)
}

// isReference reports a token that is one `${name}` reference.
func isReference(token string) bool {
	name, ok := strings.CutPrefix(token, "${")
	if !ok {
		return false
	}
	name, ok = strings.CutSuffix(name, "}")
	return ok && recipeIdentifier.MatchString(name)
}

// recipeTokens splits a value at every separator byte in seps, each `${name}`
// reference a token of its own wherever it stands; separators at either end
// or side by side leave empty tokens.
func recipeTokens(value, seps string) []string {
	var tokens []string
	var current strings.Builder
	for i := 0; i < len(value); {
		if strings.HasPrefix(value[i:], "${") {
			if length := strings.IndexByte(value[i:], '}'); length > 0 && isReference(value[i:i+length+1]) {
				if current.Len() > 0 {
					tokens = append(tokens, current.String())
					current.Reset()
				}
				tokens = append(tokens, value[i:i+length+1])
				i += length + 1
				continue
			}
		}
		if strings.IndexByte(seps, value[i]) >= 0 {
			tokens = append(tokens, current.String())
			current.Reset()
			i++
			continue
		}
		current.WriteByte(value[i])
		i++
	}
	return append(tokens, current.String())
}
