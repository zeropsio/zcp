package bundle

import (
	"fmt"
	"slices"
	"strings"

	"github.com/zeropsio/zcp/internal/topology"
)

// What serves a Mate itself stays on the AI Agent tier its project is made
// from (docs/spec-mate.md §2.8a.5): beside its dev tooling (devOnlyUtility),
// a runtime of no pair wired to an address of the Mate's own project, and a
// volume only such a runtime can be using. A stage and a production are
// projects of their own, so a runtime wired to the Mate's project serves
// nobody there — flip.studio's Stage and Small Production once came up with
// the owner's cloud agent, its server the Mate's dev address and its token a
// string zcp generated, beside the volume it kept its home on (2026-10-28).
// Both are read from what the project shows, never from a list of names.

// mateSubdomainLabel is the label every public address of a service of the
// Mate's own project carries after the service's hostname: the first label
// of the project's subdomain host (`1df2.prg1.zerops.app` → `1df2`).
func mateSubdomainLabel(subdomainHost string) string {
	label, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(subdomainHost)), ".")
	return label
}

// mateAddressIn is the first public address in value of a service of the
// Mate's own project other than self, written out —
// `<hostname>-<label>[-<port>].<region>.zerops.app` (zerops.dev on devel),
// label the project's (mateSubdomainLabel) — "" when it names none. An
// address through the subdomain variables (`${zeropsSubdomainHost}`) names
// whichever project reads it, and a hostname holds no dash, so the label
// after the first one is the project's.
func mateAddressIn(value, label, self string) string {
	if label == "" {
		return ""
	}
	for _, address := range zeropsSubdomainAddress.FindAllString(value, -1) {
		first, _, _ := strings.Cut(strings.ToLower(address), ".")
		parts := strings.Split(first, "-")
		switch {
		case len(parts) < 2 || len(parts) > 3 || parts[0] == "" || parts[1] != label || parts[0] == self:
		case len(parts) == 3 && !recipePort.MatchString(parts[2]):
		default:
			return address
		}
	}
	return ""
}

// wiredToMate names the variable of u that names a public address of another
// service of the Mate's own project, and that address; "" when none does.
func wiredToMate(u GroupUtility, label string) (key, address string) {
	for _, env := range u.ServiceEnvs {
		if address := mateAddressIn(env.Value, label, u.Hostname); address != "" {
			return env.Key, address
		}
	}
	return "", ""
}

// markMateOwnVolumes adds to mateOnly the volumes — a local or a shared
// storage, used only by a runtime that mounts it — that nothing a group
// environment runs mounts or names, while a runtime wired to the Mate
// (wired, sorted) runs beside them: that runtime's. Nothing is marked while
// a part's zerops.yaml was not read, since what it mounts is unknown. A
// database, a cache or an object storage is never marked: a runtime reaches
// one through variables the platform may hand it without a reference, so
// nothing naming it is no proof nothing uses it. It answers the warnings.
func markMateOwnVolumes(managed []ManagedServiceEntry, runtimes []GroupRuntime, utilities []GroupUtility, projectEnvs []ProjectEnvVar, wired []string, mateOnly map[string]bool) []string {
	if len(wired) == 0 || slices.ContainsFunc(runtimes, func(r GroupRuntime) bool { return strings.TrimSpace(r.ZeropsYAMLBody) == "" }) {
		return nil
	}
	// What a group environment runs, as text: every part's zerops.yaml and
	// variables, the project's, and the utilities it carries.
	var carried []string
	values := func(envs []ProjectEnvVar) {
		for _, env := range envs {
			carried = append(carried, env.Key, env.Value)
		}
	}
	for _, r := range runtimes {
		carried = append(carried, r.ZeropsYAMLBody)
		values(r.ServiceEnvs)
		for _, stage := range r.Stages {
			values(stage.ServiceEnvs)
		}
	}
	values(projectEnvs)
	for _, u := range utilities {
		if !mateOnly[u.Hostname] {
			values(u.ServiceEnvs)
		}
	}
	var warnings []string
	for _, m := range managed {
		if !topology.IsLocalStorageType(m.Type) && !topology.IsSharedStorageType(m.Type) {
			continue
		}
		if slices.ContainsFunc(carried, func(text string) bool { return namesHostname(text, m.Hostname) }) {
			continue
		}
		mateOnly[m.Hostname] = true
		warnings = append(warnings, fmt.Sprintf(
			"volume %q stays on the AI Agent tier only: nothing a stage or a production runs mounts or names it, and it runs beside %q, which serves this Mate alone",
			m.Hostname, wired[0]))
	}
	return warnings
}

// namesHostname reports whether text names hostname as a word of its own —
// a reference's prefix (`${agenthome_hostname}`), a mount, a host — in any
// case: a hostname is letters and digits, so anything else bounds it. A
// word that only happens to be the hostname counts too, which keeps a volume
// in the group tiers rather than leave one out on a guess.
func namesHostname(text, hostname string) bool {
	text, hostname = strings.ToLower(text), strings.ToLower(hostname)
	if hostname == "" {
		return false
	}
	for from := 0; ; {
		at := strings.Index(text[from:], hostname)
		if at < 0 {
			return false
		}
		start, end := from+at, from+at+len(hostname)
		if (start == 0 || !isHostnameChar(text[start-1])) && (end == len(text) || !isHostnameChar(text[end])) {
			return true
		}
		from = start + 1
	}
}

// isHostnameChar reports a byte a hostname may hold.
func isHostnameChar(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= '0' && b <= '9'
}
