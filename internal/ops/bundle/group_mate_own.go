package bundle

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/topology"
)

// What serves a Mate itself stays on the AI Agent tier its project is made
// from (docs/spec-mate.md §2.8a.5): beside its dev tooling (devOnlyUtility),
// a runtime — a utility, or a stage of a pair — wired to the address of a
// service of the Mate's own project no group environment runs (a dev half,
// the Mate's own infrastructure, a utility already so wired; never an
// address of the app's own parts), and a volume only such a runtime can be
// using. A stage and a production are projects of their own, so a runtime
// wired to the Mate's project serves nobody there — flip.studio's Stage and
// Small Production once came up with the owner's cloud agent, its server the
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
// Mate's own project among hosts (mateOwnHosts), written out —
// `<hostname>-<label>[-<port>].<region>.zerops.app` (zerops.dev on devel),
// label the project's (mateSubdomainLabel) — "" when it names none. An
// address through the subdomain variables (`${zeropsSubdomainHost}`) names
// whichever project reads it, and a hostname holds no dash, so the label
// after the first one is the project's.
func mateAddressIn(value, label string, hosts map[string]bool) string {
	if label == "" {
		return ""
	}
	for _, address := range zeropsSubdomainAddress.FindAllString(value, -1) {
		first, _, _ := strings.Cut(strings.ToLower(address), ".")
		parts := strings.Split(first, "-")
		switch {
		case len(parts) < 2 || len(parts) > 3 || !hosts[parts[0]] || parts[1] != label:
		case len(parts) == 3 && !recipePort.MatchString(parts[2]):
		default:
			return address
		}
	}
	return ""
}

// wiredToMate names the variable among envs, the own variables of a runtime
// — a utility, or a stage of a pair — that names the public address of a
// service of the Mate's own project no group environment runs (hosts,
// mateOwnHosts), and that address; "" when none does. An address of
// anything a group environment runs — a stage, a managed service, a utility
// those environments carry or the Mate's dev tooling — is the app's own
// wiring, and wires nothing.
func wiredToMate(envs []ProjectEnvVar, label string, hosts map[string]bool) (key, address string) {
	for _, env := range envs {
		if address := mateAddressIn(env.Value, label, hosts); address != "" {
			return env.Key, address
		}
	}
	return "", ""
}

// mateOwnHosts is the services of the Mate's own project no group
// environment runs, whose address wires a runtime naming it to the Mate
// (wiredToMate), lowercased: each pair's dev half — a dev service or a dev
// pool — beside its stages, and what the recipe carries on no tier, the
// Mate's own infrastructure (zcp, core): a hostname the project runs that is
// no pair's half, no managed service, no utility and no Box. A pair with no
// stage runs its part on its dev half, so that dev half is none of them; a
// utility wired to the Mate joins them (wiredUtilities).
func mateOwnHosts(inputs GroupRecipeInputs) map[string]bool {
	key := func(host string) string { return strings.ToLower(strings.TrimSpace(host)) }
	hosts := map[string]bool{}
	for _, host := range inputs.ProjectHostnames {
		if host = key(host); host != "" {
			hosts[host] = true
		}
	}
	for _, r := range inputs.Runtimes {
		delete(hosts, key(r.DevHostname))
		for _, stage := range r.Stages {
			delete(hosts, key(stage.Hostname))
		}
	}
	for _, m := range inputs.ManagedServices {
		delete(hosts, key(m.Hostname))
	}
	for _, u := range inputs.Utilities {
		delete(hosts, key(u.Hostname))
	}
	for _, b := range inputs.Boxes {
		delete(hosts, key(b.Hostname))
	}
	for _, r := range inputs.Runtimes {
		if len(r.Stages) > 0 {
			hosts[key(r.DevHostname)] = true
		}
	}
	return hosts
}

// mateWire is why a runtime serves only the Mate: the person chose it
// (chosen), or its variable key names address (wiredToMate).
type mateWire struct {
	key, address string
	chosen       bool
}

// warning says that the runtime host stays on the AI Agent tier, and why.
func (w mateWire) warning(host string) string {
	why := fmt.Sprintf("its %s names %s, an address of this Mate's own project, so it serves this Mate", w.key, w.address)
	if w.chosen {
		why = "the person said it serves only this Mate"
	}
	return fmt.Sprintf(
		"runtime %q stays on the AI Agent tier only: %s, which a stage or a production is not — a value there naming it points nowhere until a person sets it",
		host, why)
}

// wiredUtilities is the utilities that serve only the Mate, by hostname,
// each of a hostname, type and public build (groupUtilities leaves the rest
// out): one the person chose so (choices true), and one they said nothing
// of whose own variables name the address of a service among hosts
// (wiredToMate) — never one they chose to carry (choices false). Each joins
// hosts, so a stage naming one is wired too. A utility is read against
// hosts as they were before any joined, so the order the utilities come in
// decides nothing.
func wiredUtilities(utilities []GroupUtility, hosts map[string]bool, label string, choices map[string]bool) map[string]mateWire {
	wires := map[string]mateWire{}
	for _, u := range utilities {
		if strings.TrimSpace(u.Hostname) == "" || strings.TrimSpace(u.ServiceType) == "" || strings.TrimSpace(u.BuildFromGit) == "" {
			continue
		}
		if chosen, said := choices[u.Hostname]; said {
			if chosen {
				wires[u.Hostname] = mateWire{chosen: true}
			}
			continue
		}
		if key, address := wiredToMate(u.ServiceEnvs, label, hosts); key != "" {
			wires[u.Hostname] = mateWire{key: key, address: address}
		}
	}
	for host := range wires {
		hosts[strings.ToLower(host)] = true
	}
	return wires
}

// markMateWiredStages marks each part whose stage serves only the Mate: one
// the person chose so (choices true), and one they said nothing of that is
// wired to the Mate (wiredToMate against hosts, label the Mate's subdomain
// label) — never one they chose to carry (choices false). No group
// environment runs it, so nameGroupParts gives it no name and only the AI
// Agent tier carries its stage. It returns those stages and those the
// address rule alone decided (ruled), each sorted, and says each. A pair's
// dev half is never read so: a dev service naming another's dev address is
// the Mate's own dev loop, and a pair with no stage runs its part on it.
func markMateWiredStages(runtimes []GroupRuntime, parts map[string][]groupPart, hosts map[string]bool, label string, choices map[string]bool) (wired, ruled, warnings []string) {
	for _, r := range runtimes {
		list := parts[r.DevHostname]
		for i := range list {
			part := &list[i]
			if part.stage == "" {
				continue
			}
			var wire mateWire
			if chosen, said := choices[part.stage]; said {
				if !chosen {
					continue
				}
				wire.chosen = true
			} else if wire.key, wire.address = wiredToMate(part.envs, label, hosts); wire.key == "" {
				continue
			} else {
				ruled = append(ruled, part.stage)
			}
			part.mateWired = true
			wired = append(wired, part.stage)
			warnings = append(warnings, wire.warning(part.stage))
		}
	}
	sort.Strings(wired)
	sort.Strings(ruled)
	return wired, ruled, warnings
}

// mateRuleLine is the one line that says which runtimes zcp's address rule
// alone kept on the AI Agent tier (ruled) — the person said nothing of them
// — and that the choice is the person's, with the call that records it; ""
// when the rule decided none.
func mateRuleLine(ruled []string) string {
	if len(ruled) == 0 {
		return ""
	}
	hosts := slices.Sorted(slices.Values(ruled))
	quoted := make([]string, len(hosts))
	for i, host := range hosts {
		quoted[i] = strconv.Quote(host)
	}
	which := "it"
	if len(hosts) > 1 {
		which = "each"
	}
	return fmt.Sprintf(
		"zcp's address rule alone decided what serves only this Mate here (%s): Stage and Small Production leave %s out, with a volume only it uses. That is the person's call, not zcp's: ask them, and record their answer — zerops_workflow action=\"group-recipe\" mateOnly={%q: false} carries one in every tier, mateOnly={%q: true} keeps it on the AI Agent tier whatever the rule reads.",
		strings.Join(quoted, ", "), which, hosts[0], hosts[0])
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
