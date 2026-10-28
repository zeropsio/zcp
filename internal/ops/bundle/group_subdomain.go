package bundle

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

// A value of the Mate's project may name the public address of one of its
// own services written out — `https://flipstage-3901-5195.prg1.zerops.app`,
// 3901 the first label of the project's subdomain host. Copied into a tier it
// names the Mate's service from every project the tier makes, and renamed
// with its part (`flip-3901-5195`) a host that exists nowhere: flip.studio's
// Stage and Small Production came up with their cloud agent's server so
// (group change #2, 2026-10-28). Every project a tier makes — the AI Agent
// tier's too — gets a subdomain label of its own, and the platform gives each
// of its services its address there as `zeropsSubdomain`: the whole
// `https://` address with no slash after it, one line for each HTTP port the
// service serves. So every tier writes such an address as a reference to the
// address the service it names has in the environment the tier makes:
//
//   - `${<host>_zeropsSubdomain}` for another service, <host> the tier's name
//     for it (a group environment's part renamed: flipstage → flip);
//   - `${zeropsSubdomain}` for the address of the service the value is of;
//   - what follows the address — a path, a query — kept.
//
// A reference stands for the address only where the tier's service builds a
// setup declaring exactly one HTTP port, the one the address names (80 when
// it names none), and the value writes the address `https://` with no port
// after it. An address the reference cannot be shown to express is kept as
// written and said, never guessed: a setup declaring several HTTP ports,
// another one or none; a service the recipe reads no setup of (a utility, a
// managed service, a Box, a dev pool); a bare host or another scheme; a
// reference no priority orders before its reader (orderSubdomainReads); and
// any address in a project variable, which every service reads, so no
// priority creates the service it names before all of its readers. An
// address of a service the tier does not carry is kept as it is: an address
// written out reads nothing (spec-workflows.md §8 PS-3), what a tier's
// runtimes read is the closure check's (PS-3a), and a reference the rewrite
// writes names only what the tier carries, so it adds no read.
//
// The address is as public as the subdomain it names, so it never makes a
// value look secret: the secret rule reads its label — the platform's name
// for the project, which may be a number (3901) and as often is not (1df2) —
// as a number (tierSecret), where before a label with a letter in it made
// the whole value opaque and generated. Everything else the rule weighs
// stands: a credential's name, a password in the URL. A value the platform
// flags sensitive or reads back masked is a secret as before; a secret is
// generated (groupVaultFields), never rewritten and never said.

// mateAddress is a public address of a service of the Mate's own project
// written out in a value.
type mateAddress struct {
	// start and end bound the address in the value, `https://` included.
	start, end int
	// host is the service's hostname, lowercase.
	host string
	// port is the port the address names, 80 when it names none.
	port int
	// address is the host as written: `flipstage-3901-5195.prg1.zerops.app`.
	address string
	// https reports the address written `https://` with no port after it,
	// the form zeropsSubdomain holds.
	https bool
}

// mateAddresses is every public address in value of a service among hosts
// (lowercase) of the Mate's own project, written out —
// `<hostname>-<label>[-<port>].<region>.zerops.app` (zerops.dev on devel),
// label the project's (mateSubdomainLabel) — in the order they stand. An
// address through the subdomain variables (`${zeropsSubdomainHost}`) names
// whichever project reads it, and a hostname holds no dash, so the label
// after the first one is the project's; a name going on past the address
// (`….zerops.app.example.com`) is a domain of its own.
func mateAddresses(value, label string, hosts map[string]bool) []mateAddress {
	if label == "" {
		return nil
	}
	var out []mateAddress
	for _, at := range zeropsSubdomainAddress.FindAllStringIndex(value, -1) {
		start, end := at[0], at[1]
		if end < len(value) && (value[end] == '.' || value[end] == '-') || start > 0 && value[start-1] == '_' {
			continue
		}
		address := value[start:end]
		first, _, _ := strings.Cut(strings.ToLower(address), ".")
		parts := strings.Split(first, "-")
		if len(parts) < 2 || len(parts) > 3 || !hosts[parts[0]] || parts[1] != label {
			continue
		}
		port := 80
		if len(parts) == 3 {
			if !recipePort.MatchString(parts[2]) {
				continue
			}
			port, _ = strconv.Atoi(parts[2])
		}
		found := mateAddress{start: start, end: end, host: parts[0], port: port, address: address}
		if strings.HasSuffix(strings.ToLower(value[:start]), "https://") {
			found.start -= len("https://")
			found.https = end == len(value) || value[end] != ':'
		}
		out = append(out, found)
	}
	return out
}

// setupHTTPPorts is the HTTP ports — run.ports items with httpSupport on,
// sorted — that setup of a zerops.yaml body declares: its own, else those of
// the one setup it extends. None when neither declares run.ports, the setup
// extends several, or the body does not parse.
func setupHTTPPorts(body, setup string) []int {
	entries := zeropsYAMLSetupEntries(body)
	for seen := map[string]bool{}; setup != "" && !seen[setup]; {
		seen[setup] = true
		entry, ok := entries[setup]
		if !ok {
			return nil
		}
		if run, ok := entry["run"].(map[string]any); ok {
			if list, ok := run["ports"].([]any); ok {
				return httpPorts(list)
			}
		}
		bases := yamlExtendsNames(entry["extends"])
		if len(bases) != 1 {
			return nil
		}
		setup = bases[0]
	}
	return nil
}

// httpPorts is the ports of a run.ports list whose httpSupport is on,
// sorted, each once.
func httpPorts(list []any) []int {
	var ports []int
	for _, item := range list {
		p, ok := item.(map[string]any)
		if !ok || p["httpSupport"] != true {
			continue
		}
		var port int
		switch v := p["port"].(type) {
		case int:
			port = v
		case string:
			port, _ = strconv.Atoi(strings.TrimSpace(v))
		}
		if port > 0 && !slices.Contains(ports, port) {
			ports = append(ports, port)
		}
	}
	slices.Sort(ports)
	return ports
}

// mateAddressReader tells the public addresses of the Mate's own project's
// services written out in a value (mateAddresses): label is the project's
// subdomain label, hosts its services, lowercase. The zero value tells none.
type mateAddressReader struct {
	label string
	hosts map[string]bool
}

// in is every address of the Mate's own project in value.
func (m mateAddressReader) in(value string) []mateAddress {
	return mateAddresses(value, m.label, m.hosts)
}

// public is value as the secret rule judges it: the label of each address of
// the Mate's own project read as the number it may well be.
func (m mateAddressReader) public(value string) string {
	found := m.in(value)
	if len(found) == 0 {
		return value
	}
	var b strings.Builder
	at := 0
	for _, a := range found {
		// The label follows the hostname and its dash.
		labelAt := a.end - len(a.address) + len(a.host) + len("-")
		b.WriteString(value[at:labelAt])
		b.WriteString("0")
		at = labelAt + len(m.label)
	}
	b.WriteString(value[at:])
	return b.String()
}

// tierSecret is recipeSecret of a variable a tier carries: an address of the
// Mate's own project written out is public, so its label never makes the
// value look secret (mateAddressReader.public) — but a value the platform
// flags sensitive or reads back masked is judged as it is.
func tierSecret(env ProjectEnvVar, mate mateAddressReader) (secret, setAgain bool) {
	if !env.Sensitive && env.Value != platform.MaskedEnvValue {
		env.Value = mate.public(env.Value)
	}
	return recipeSecret(env)
}

// valueWriter writes the variables a tier carries: which of them are secrets
// the tier generates or leaves out, what each other value is written as, and
// it hears of each value left out for a person to set (unsetValue).
type valueWriter interface {
	secret(env ProjectEnvVar) (secret, setAgain bool)
	write(key, value string) string
	leaveOut(key string)
}

// vaultWriter is the valueWriter of one owner's variables in a tier
// (tierAddresses.writer).
type vaultWriter struct {
	tier       *tierAddresses
	owner, own string
}

func (w vaultWriter) secret(env ProjectEnvVar) (secret, setAgain bool) {
	return tierSecret(env, w.tier.mate)
}

func (w vaultWriter) write(key, value string) string {
	return w.tier.write(w.owner, w.own, key, value)
}

// leaveOut records a value the tier leaves out — once, on the write that
// renders the tier, never on the dry run that orders it.
func (w vaultWriter) leaveOut(key string) {
	if !w.tier.collecting {
		w.tier.unset = append(w.tier.unset, unsetValue{owner: w.owner, key: key})
	}
}

// ownSubdomainReference is the reference to a service's own public address.
const ownSubdomainReference = "${zeropsSubdomain}"

// addressBuild is the setup a tier's runtime builds, in its pair's
// zerops.yaml.
type addressBuild struct{ body, setup string }

// keptAddress is an address of the Mate's own project a tier keeps as
// written for a person to set (tierAddresses): the variable's owner and key,
// the address, why the tier keeps it, and the tier.
type keptAddress struct {
	owner, key, address, why, tier string
}

// tierAddresses writes the Mate's own addresses in one tier's values.
type tierAddresses struct {
	// mate tells an address of the Mate's own project.
	mate  mateAddressReader
	title string
	// promote names a group environment's own runtimes in the rest of a
	// value (pairPromoter), nil on the AI Agent tier; renames is its pair
	// halves' renames.
	promote func(string) string
	renames map[string]string
	// builds is the setup each runtime of the tier builds, and carried every
	// service the tier carries, by their hostnames in the tier; refHosts
	// maps a carried hostname as a reference spells it to the hostname.
	builds   map[string]addressBuild
	carried  map[string]bool
	refHosts map[string]string
	// collecting has write collect the public-address reads of the values it
	// writes (reads, projectReads) and say nothing of what it keeps: the dry
	// run the tier orders its priorities from (orderSubdomainReads).
	collecting   bool
	reads        []subdomainRead
	projectReads []projectRead
	// unordered is why no priority orders a reference the rewrite would
	// write, by its reader and the service it names: the address is kept as
	// written instead.
	unordered map[[2]string]string
	// kept is what the tier kept as written for a person to set.
	kept []keptAddress
	// unset is what the tier left out for a person to set, a value set by
	// hand no random value serves (groupVaultFields): its header names it.
	unset []unsetValue
}

// projectRead is a project variable's ${<host>_zeropsSubdomain} reference,
// which every service reads: the variable and the service it names.
type projectRead struct{ key, target string }

// carry records the services the tier carries, by their hostnames in it.
func (t *tierAddresses) carry(hosts []string) {
	t.carried, t.refHosts = map[string]bool{}, map[string]string{}
	for _, host := range hosts {
		t.carried[host] = true
		t.refHosts[canonicalRefHost(host)] = host
	}
}

// writer writes the variables of owner — `runtime "flip"`, `dev pool
// "webpool"`, "" for the project's — whose own address is the one of the
// tier's service own ("" for none).
func (t *tierAddresses) writer(owner, own string) valueWriter {
	return vaultWriter{tier: t, owner: owner, own: own}
}

// write writes one value: each address of the Mate's own project as the
// tier's reference to it, or as written; the rest through promote.
func (t *tierAddresses) write(owner, own, key, value string) string {
	var b strings.Builder
	at := 0
	for _, a := range t.mate.in(value) {
		b.WriteString(t.rest(own, key, value[at:a.start]))
		written := value[a.start:a.end]
		if host := t.tierHost(a.host); t.carried[host] {
			ref, why := t.reference(own, host, a)
			if why == "" && host != own {
				if t.collecting {
					t.reads = append(t.reads, subdomainRead{reader: own, target: host, variable: "its " + key, rewritten: true})
				} else if unordered, ok := t.unordered[[2]string{own, host}]; ok {
					why = unordered
				}
			}
			switch {
			case why == "":
				written = ref
			case !t.collecting:
				t.kept = append(t.kept, keptAddress{owner: owner, key: key, address: a.address, why: why, tier: t.title})
			}
		}
		b.WriteString(written)
		at = a.end
	}
	b.WriteString(t.rest(own, key, value[at:]))
	return b.String()
}

// rest writes text of a value that holds no address of the Mate's own
// project, through promote, and collects the public-address references it
// holds while the tier collects them.
func (t *tierAddresses) rest(own, key, text string) string {
	if t.promote != nil {
		text = t.promote(text)
	}
	if !t.collecting {
		return text
	}
	for _, name := range parseDollarBraceRefs(text) {
		spelled, ok := subdomainRefHost(name)
		target := t.refHosts[spelled]
		switch {
		case !ok || target == "" || target == own:
		case own == "":
			t.projectReads = append(t.projectReads, projectRead{key: key, target: target})
		default:
			t.reads = append(t.reads, subdomainRead{reader: own, target: target, variable: "its " + key})
		}
	}
	return text
}

// tierHost is the tier's name for host, a service of the Mate's project.
func (t *tierAddresses) tierHost(host string) string {
	if renamed, ok := t.renames[host]; ok {
		return renamed
	}
	return host
}

// reference is the reference that stands for a in a value of the tier's
// service own ("" for a project variable), host the tier's name for a's
// service; why says why none does.
func (t *tierAddresses) reference(own, host string, a mateAddress) (ref, why string) {
	ref = subdomainReference(host)
	if own != "" && own == host {
		ref = ownSubdomainReference
	}
	if own == "" {
		return "", fmt.Sprintf("every service reads a project variable, and %s resolves only in one started after %q is deployed, so no priority orders it", ref, host)
	}
	if !a.https {
		return "", ref + " holds the address as https:// with no port after it, which this value does not write"
	}
	build, read := t.builds[host]
	if !read {
		return "", fmt.Sprintf("the recipe reads no setup of %q, so which HTTP ports it serves is unknown, and %s holds an address for each", host, ref)
	}
	ports := setupHTTPPorts(build.body, build.setup)
	switch {
	case len(ports) == 0:
		return "", fmt.Sprintf("%q builds setup %q, which declares no HTTP port", host, build.setup)
	case len(ports) > 1:
		return "", fmt.Sprintf("%q builds setup %q, which declares HTTP ports %s, and %s holds an address for each", host, build.setup, joinPorts(ports), ref)
	case ports[0] != a.port:
		return "", fmt.Sprintf("%q builds setup %q, whose HTTP port is %d, not %d", host, build.setup, ports[0], a.port)
	}
	return ref, ""
}

// joinPorts reads ports as `3000`, `3000 and 5195`, or `80, 3000 and 5195`.
func joinPorts(ports []int) string {
	words := make([]string, len(ports))
	for i, port := range ports {
		words[i] = strconv.Itoa(port)
	}
	if len(words) < 2 {
		return strings.Join(words, "")
	}
	return strings.Join(words[:len(words)-1], ", ") + " and " + words[len(words)-1]
}

// keptAddressWarnings says each address a tier kept as written for a person
// to set, once for every tier that kept it for the same reason, in the order
// the tiers kept them.
func keptAddressWarnings(kept []keptAddress) []string {
	order, tiers := saidInTiers(kept, func(k keptAddress) (keptAddress, string) {
		tier := k.tier
		k.tier = ""
		return k, tier
	})
	warnings := make([]string, 0, len(order))
	for _, k := range order {
		variable := "project variable " + k.key
		if k.owner != "" {
			variable = k.key + " of " + k.owner
		}
		in, several := tierPhrase(tiers[k])
		keep, them := in+" keeps", "it"
		if several {
			keep, them = in+" keep", "them"
		}
		warnings = append(warnings, fmt.Sprintf(
			"%s names %s, an address of this Mate's own project, and %s it as written: %s — set it by hand in an environment made from %s",
			variable, k.address, keep, k.why, them))
	}
	return warnings
}
