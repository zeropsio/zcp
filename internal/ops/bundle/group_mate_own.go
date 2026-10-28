package bundle

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/topology"
)

// What serves a Mate itself stays on the AI Agent tier its project is made
// from (docs/spec-mate.md §2.8a.5): beside its dev tooling (devOnlyUtility),
// a runtime wired to an address of the Mate's own project — a utility, or a
// stage of a pair — and a volume only such a runtime can be using. A stage
// and a production are projects of their own, so a runtime wired to the
// Mate's project serves nobody there — flip.studio's Stage and Small
// Production once came up with the owner's cloud agent, its server the
// Mate's dev address and its token a string zcp generated, beside the volume
// it kept its home on (2026-10-28); the agent was a stage of the flipdev
// pair, built from flip's own repository by a setup of its own that mounts
// that volume. Both are read from what the project shows, never from a list
// of names.

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

// wiredToMate names the variable among envs, the own variables of the
// runtime self — a utility, or a stage of a pair — that names a public
// address of another service of the Mate's own project, and that address;
// "" when none does.
func wiredToMate(self string, envs []ProjectEnvVar, label string) (key, address string) {
	for _, env := range envs {
		if address := mateAddressIn(env.Value, label, self); address != "" {
			return env.Key, address
		}
	}
	return "", ""
}

// mateWiredWarning says that the runtime host stays on the AI Agent tier:
// its variable key names address, an address of the Mate's own project.
func mateWiredWarning(host, key, address string) string {
	return fmt.Sprintf(
		"runtime %q stays on the AI Agent tier only: its %s names %s, an address of this Mate's own project, so it serves this Mate, which a stage or a production is not — a value there naming it points nowhere until a person sets it",
		host, key, address)
}

// markMateWiredStages marks each part whose stage is wired to the Mate
// (wiredToMate, label the Mate's subdomain label): no group environment
// runs it, so nameGroupParts gives it no name and only the AI Agent tier
// carries its stage. It returns those stages, sorted, and says each. A
// pair's dev half is never read so: a dev service naming another's dev
// address is the Mate's own dev loop, and a pair with no stage runs its part
// on it.
func markMateWiredStages(runtimes []GroupRuntime, parts map[string][]groupPart, label string) (wired, warnings []string) {
	for _, r := range runtimes {
		list := parts[r.DevHostname]
		for i := range list {
			part := &list[i]
			if part.stage == "" {
				continue
			}
			key, address := wiredToMate(part.stage, part.envs, label)
			if key == "" {
				continue
			}
			part.mateWired = true
			wired = append(wired, part.stage)
			warnings = append(warnings, mateWiredWarning(part.stage, key, address))
		}
	}
	sort.Strings(wired)
	return wired, warnings
}

// markMateOwnVolumes adds to mateOnly the volumes — a local or a shared
// storage, used only by a runtime that mounts it — that nothing a group
// environment runs mounts or names, while a runtime wired to the Mate
// (wired, sorted) runs beside them: that runtime's. What a group environment
// runs is what the group tiers carry: the setup each part builds there
// (setupText) and the part's variables — never a pair's dev half, nor a
// stage wired to the Mate, which only the AI Agent tier carries, while a
// part left out for want of a name or a type still counts, which keeps a
// volume rather than drop it on a guess — the project's variables, and the
// utilities those tiers carry. Nothing is marked while a part's
// zerops.yaml was not read, since what it mounts is unknown, and a pair
// whose parts are not resolved counts whole: its zerops.yaml and every
// variable of it. A database, a cache or an object storage is never marked:
// a runtime reaches one through variables the platform may hand it without
// a reference, so nothing naming it is no proof nothing uses it. It answers
// the warnings.
func markMateOwnVolumes(managed []ManagedServiceEntry, runtimes []GroupRuntime, parts map[string][]groupPart, utilities []GroupUtility, projectEnvs []ProjectEnvVar, wired []string, mateOnly map[string]bool) []string {
	if len(wired) == 0 || slices.ContainsFunc(runtimes, func(r GroupRuntime) bool { return strings.TrimSpace(r.ZeropsYAMLBody) == "" }) {
		return nil
	}
	var carried []string
	values := func(envs []ProjectEnvVar) {
		for _, env := range envs {
			carried = append(carried, env.Key, env.Value)
		}
	}
	for _, r := range runtimes {
		list, resolved := parts[r.DevHostname]
		if !resolved {
			carried = append(carried, r.ZeropsYAMLBody)
			values(r.ServiceEnvs)
			for _, stage := range r.Stages {
				values(stage.ServiceEnvs)
			}
			continue
		}
		for _, part := range list {
			if part.mateWired {
				continue
			}
			carried = append(carried, setupText(r.ZeropsYAMLBody, part.environmentSetup))
			values(part.envs)
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

// setupText is setup of a zerops.yaml body as text, with every setup it
// extends: what an environment building it runs, mounts among it. The whole
// body when it does not parse or does not declare one of them, since what
// the setup runs is unknown then.
func setupText(body, setup string) string {
	entries := zeropsYAMLSetupEntries(body)
	var b strings.Builder
	for queue, seen := []string{setup}, map[string]bool{}; len(queue) > 0; queue = queue[1:] {
		name := queue[0]
		if seen[name] {
			continue
		}
		seen[name] = true
		entry, declared := entries[name]
		if !declared {
			return body
		}
		text, err := yaml.Marshal(entry)
		if err != nil {
			return body
		}
		b.Write(text)
		queue = append(queue, yamlExtendsNames(entry["extends"])...)
	}
	return b.String()
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
