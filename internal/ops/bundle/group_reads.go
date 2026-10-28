package bundle

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// What a tier's runtimes read of the project's services.
//
// A runtime reads a service through a variable: one of the setup it builds —
// the setup a tier names for it, with those of every setup it extends — or
// one of its own the tier writes as written in its vault. A variable reads
// a service by a `${host_key}` reference (the platform's interpolator spells
// `my-db` as `my_db`), or by the service as the host of an address on the
// project network — right after `://` or `@`, or opening a `host:port`
// value, as the hostname alone or `host.zerops`, and followed by what ends a
// hostname (`:`, `/`, the end). A `-` or a `.` after it makes it another
// name: a subdomain URL (`box-${zeropsSubdomainHost}-8080.prg1.zerops.app`)
// reaches the service from outside, a domain is not the project's. A bare
// word stays a word — `SMTP_HOST: mailpit` and `SANDBOX_KIND: box` read
// alike — so it reads nothing.
//
// A tier has to carry what its runtimes read, or an environment made from it
// starts an app that cannot reach it. So a tier carries the Zerops Box a
// runtime it carries reads, by either kind of variable (GroupBox) — a
// runtime it does not carry, such as one serving the Mate itself, takes its
// Box nowhere — and a read by a setup the tier builds of any other service
// the Mate's project runs that the tier does not carry refuses the whole
// recipe (groupTierClosure): a tier on the group repository stays there. A
// runtime's own variables never refuse it — a value the composer carries
// knowing it points nowhere until a person sets it, such as one naming the
// Mate's dev tooling, stays a warning. Only a hostname the project runs is a
// service, so a project variable (`${zeropsSubdomainHost}`) or a group
// environment's own runtime under its own name never is.

// serviceReader finds the services among a set of hostnames a setup's
// variables read, its patterns built once per recipe.
type serviceReader struct {
	// refHosts are the hostnames as a reference spells them, longest first:
	// `${api_v2_port}` is api-v2's, not api's.
	refHosts []string
	// byRef maps a reference spelling back to the hostname.
	byRef map[string]string
	// addresses is each hostname's address pattern.
	addresses map[string]*regexp.Regexp
}

// serviceRead is one variable of a setup that reads a service.
type serviceRead struct {
	// variable is the variable's block and key: `run.envVariables.BOX_API_URL`.
	variable string
	host     string
}

func newServiceReader(hosts []string) *serviceReader {
	sr := &serviceReader{byRef: map[string]string{}, addresses: map[string]*regexp.Regexp{}}
	for _, h := range hosts {
		if h = strings.TrimSpace(h); h == "" || sr.addresses[h] != nil {
			continue
		}
		ref := canonicalRefHost(h)
		sr.refHosts = append(sr.refHosts, ref)
		sr.byRef[ref] = h
		q := regexp.QuoteMeta(h)
		sr.addresses[h] = regexp.MustCompile(`(?:(?:://|@)` + q + `(?:\.zerops)?|(?:^|[\s,])` + q + `(?:\.zerops)?:[0-9]+)(?:[^A-Za-z0-9_.\-]|$)`)
	}
	sort.Slice(sr.refHosts, func(i, j int) bool {
		if len(sr.refHosts[i]) != len(sr.refHosts[j]) {
			return len(sr.refHosts[i]) > len(sr.refHosts[j])
		}
		return sr.refHosts[i] < sr.refHosts[j]
	})
	return sr
}

// reads is what setup of a zerops.yaml body reads among the reader's
// hostnames, in its build.envVariables and its run.envVariables — each with
// those of the setups it extends — by variable, then hostname. A body that
// does not parse, or a setup it does not declare, reads nothing.
func (sr *serviceReader) reads(body, setup string) []serviceRead {
	if len(sr.addresses) == 0 || strings.TrimSpace(body) == "" || strings.TrimSpace(setup) == "" {
		return nil
	}
	var doc struct {
		Zerops []buildReadSetup `yaml:"zerops"`
	}
	if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
		return nil
	}
	bySetup := make(map[string]buildReadSetup, len(doc.Zerops))
	for _, s := range doc.Zerops {
		bySetup[s.Setup] = s
	}
	if _, declared := bySetup[setup]; !declared {
		return nil
	}
	blocks := []struct {
		name string
		vars map[string]any
	}{
		{"build.envVariables", inheritedVars(bySetup, setup, func(x buildReadSetup) map[string]any { return x.Build.EnvVariables }, map[string]bool{})},
		{"run.envVariables", inheritedVars(bySetup, setup, func(x buildReadSetup) map[string]any { return x.Run.EnvVariables }, map[string]bool{})},
	}
	var out []serviceRead
	for _, block := range blocks {
		for _, key := range slices.Sorted(maps.Keys(block.vars)) {
			value, ok := block.vars[key].(string)
			if !ok {
				continue
			}
			for _, host := range sr.hostsIn(value) {
				out = append(out, serviceRead{variable: block.name + "." + key, host: host})
			}
		}
	}
	return out
}

// hostsIn is the hostnames one value reads, sorted.
func (sr *serviceReader) hostsIn(value string) []string {
	found := map[string]bool{}
	for _, name := range parseDollarBraceRefs(value) {
		if ref := longestHostPrefix(name, sr.refHosts); ref != "" {
			found[sr.byRef[ref]] = true
		}
	}
	for host, address := range sr.addresses {
		if address.MatchString(value) {
			found[host] = true
		}
	}
	return slices.Sorted(maps.Keys(found))
}

// tierReader is an entry of a tier and what it reads with: the setup of its
// pair's zerops.yaml the tier builds for it — a runtime's, or the one a
// part's dev pool (pool) has its leases deploy; none for a utility, whose
// zerops.yaml the recipe never reads — and the values of its own variables
// the tier writes as written (writtenValues).
type tierReader struct {
	hostname, setup, body string
	pool                  bool
	vault                 []string
}

// writtenValues is the values of envs a tier writes as written, each through
// promote (nil keeps it as it is): a secret the tier generates
// (recipeSecret) is never written, so it names nothing.
func writtenValues(envs []ProjectEnvVar, promote func(string) string) []string {
	values := make([]string, 0, len(envs))
	for _, env := range envs {
		if secret, _ := recipeSecret(env); secret {
			continue
		}
		value := env.Value
		if promote != nil {
			value = promote(value)
		}
		values = append(values, value)
	}
	return values
}

// groupTierClosure is each read by a reader of a tier — by the setup the tier
// builds for it — of a service the project runs (project) that the tier does
// not carry (carried), said as `tier "<title>": runtime "<host>" builds setup
// "<setup>", whose <variable> names "<service>"`, in the tier's order, each
// once. A hostname no service of the project has is no service: a project
// variable, or a group environment's own runtime under its own name.
func groupTierClosure(sr *serviceReader, readers []tierReader, carried, project map[string]bool, title string) []string {
	var out []string
	for _, reader := range readers {
		builds := fmt.Sprintf("runtime %q builds", reader.hostname)
		if reader.pool {
			builds = fmt.Sprintf("dev pool %q deploys", reader.hostname)
		}
		for _, read := range sr.reads(reader.body, reader.setup) {
			if !project[read.host] || carried[read.host] {
				continue
			}
			line := fmt.Sprintf("tier %q: %s setup %q, whose %s names %q", title, builds, reader.setup, read.variable, read.host)
			if !slices.Contains(out, line) {
				out = append(out, line)
			}
		}
	}
	return out
}

// tierHostnames is the hostnames a tier's services carry.
func tierHostnames(services []yamlItem) map[string]bool {
	hosts := make(map[string]bool, len(services))
	for _, item := range services {
		for _, field := range item.fields {
			if host, ok := field.value.(string); ok && field.key == "hostname" {
				hosts[host] = true
			}
		}
	}
	return hosts
}

// tierReads is what the readers of a tier read among sr's hostnames — through
// the setup the tier builds for them or the variables it writes for them —
// by hostname: the readers that read it, in the tier's order.
func tierReads(sr *serviceReader, readers []tierReader) map[string][]string {
	out := map[string][]string{}
	add := func(host, reader string) {
		if !slices.Contains(out[host], reader) {
			out[host] = append(out[host], reader)
		}
	}
	for _, reader := range readers {
		for _, read := range sr.reads(reader.body, reader.setup) {
			add(read.host, reader.hostname)
		}
		for _, value := range reader.vault {
			for _, host := range sr.hostsIn(value) {
				add(host, reader.hostname)
			}
		}
	}
	return out
}

// groupBoxes is the Boxes every tier chooses from, sorted and each once; a
// Box the reader did not describe whole is left out and said.
func groupBoxes(in []GroupBox) ([]GroupBox, []string) {
	var boxes []GroupBox
	var warnings []string
	seen := map[string]bool{}
	for _, b := range in {
		if strings.TrimSpace(b.Hostname) == "" || strings.TrimSpace(b.ServiceType) == "" {
			warnings = append(warnings, fmt.Sprintf("Zerops Box %q is left out of the recipe: its hostname or type is unknown", b.Hostname))
			continue
		}
		if seen[b.Hostname] {
			continue
		}
		seen[b.Hostname] = true
		boxes = append(boxes, b)
	}
	sort.SliceStable(boxes, func(i, j int) bool { return boxes[i].Hostname < boxes[j].Hostname })
	return boxes, warnings
}

// boxHostnames is the Boxes' hostnames.
func boxHostnames(boxes []GroupBox) []string {
	hosts := make([]string, 0, len(boxes))
	for _, b := range boxes {
		hosts = append(hosts, b.Hostname)
	}
	return hosts
}

// boxesAnyPartReads is the Boxes some tier may carry, sorted: those a setup
// any tier builds for a runtime reads — the dev half's, and each part's
// stage and group-environment setup — or a runtime's or a utility's own
// variables do. A group environment's part never takes such a Box's
// hostname.
func boxesAnyPartReads(sr *serviceReader, boxes []GroupBox, runtimes []GroupRuntime, parts map[string][]groupPart, utilities []GroupUtility) []string {
	readers := make([]tierReader, 0, 3*len(runtimes)+len(utilities))
	for _, r := range runtimes {
		readers = append(readers, tierReader{hostname: r.DevHostname, setup: r.SetupName, body: r.ZeropsYAMLBody, vault: writtenValues(r.ServiceEnvs, nil)})
		for _, part := range parts[r.DevHostname] {
			readers = append(readers,
				tierReader{hostname: r.DevHostname, setup: part.setup, body: r.ZeropsYAMLBody, vault: writtenValues(part.envs, nil)},
				tierReader{hostname: r.DevHostname, setup: part.environmentSetup, body: r.ZeropsYAMLBody})
		}
	}
	for _, u := range utilities {
		readers = append(readers, tierReader{hostname: u.Hostname, vault: writtenValues(u.ServiceEnvs, nil)})
	}
	read := tierReads(sr, readers)
	var hosts []string
	for _, b := range boxes {
		if len(read[b.Hostname]) > 0 {
			hosts = append(hosts, b.Hostname)
		}
	}
	return hosts
}

// groupBoxEntry writes a Box as a Box import takes it: its hostname and
// type, created with the managed services — before every runtime that reads
// it — and its public access as it runs, which its sandboxes' port URLs
// need. No mode, no HA variant and no scale: it is one VM. The comment names
// the runtimes that read it and what a new Box needs.
func groupBoxEntry(b GroupBox, readers []string) (map[string]any, string) {
	entry := map[string]any{
		"hostname": b.Hostname,
		"type":     b.ServiceType,
		"priority": managedPriority,
	}
	if b.SubdomainEnabled {
		entry["enableSubdomainAccess"] = true
	}
	comment := fmt.Sprintf("A Zerops Box, read by %s. A Box can be created only in a project in region prg1, and it comes up with no base: finish its setup with `zbox setup` (zcp's zerops_import does it itself).",
		quoteAnd(readers))
	return entry, strings.Join(wrapWords(comment, commentWidth-4), "\n")
}

// groupBoxWarnings says which Box no tier carries: nothing the recipe
// carries reads it, so it is no part of the app — an agent's own Box.
func groupBoxWarnings(boxes []GroupBox, carried map[string]bool) []string {
	var warnings []string
	for _, b := range boxes {
		if !carried[b.Hostname] {
			warnings = append(warnings, fmt.Sprintf(
				"Zerops Box %q (%s) is not carried in the recipe: no runtime the recipe carries reads it, so it is no part of the app — a setup that names it in a variable (a ${%s_…} reference, or http://%s:8080) brings it into every tier that carries that runtime",
				b.Hostname, b.ServiceType, canonicalRefHost(b.Hostname), b.Hostname))
		}
	}
	return warnings
}
