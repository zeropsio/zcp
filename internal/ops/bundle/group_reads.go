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
// A runtime reads a service through the variables of the setup it builds —
// the setup a tier names for it, with those of every setup it extends: a
// `${host_key}` reference (the platform's interpolator spells `my-db` as
// `my_db`), or the service as the host of an address on the project network
// — right after `://` or `@`, or opening a `host:port` value, as the hostname
// alone or `host.zerops`, and followed by what ends a hostname (`:`, `/`,
// the end). A `-` or a `.` after it makes it another name: a subdomain URL
// (`box-${zeropsSubdomainHost}-8080.prg1.zerops.app`) reaches the service
// from outside, a domain is not the project's. A bare word stays a word —
// `SMTP_HOST: mailpit` and `SANDBOX_KIND: box` read alike — so it reads
// nothing.
//
// A tier carries the Zerops Box a runtime it carries reads (GroupBox).

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

// tierReader is an entry of a tier that builds a setup of its pair's
// zerops.yaml: a runtime, or a part's dev pool, whose leases deploy the
// setup its image is.
type tierReader struct {
	hostname, setup, body string
}

// tierReads is what the readers of a tier read among sr's hostnames, by
// hostname: the readers that read it, in the tier's order.
func tierReads(sr *serviceReader, readers []tierReader) map[string][]string {
	out := map[string][]string{}
	for _, reader := range readers {
		for _, read := range sr.reads(reader.body, reader.setup) {
			if !slices.Contains(out[read.host], reader.hostname) {
				out[read.host] = append(out[read.host], reader.hostname)
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
	comment := fmt.Sprintf("A Zerops Box, which the setups of %s read. A Box can be created only in a project in region prg1, and it comes up with no base: finish its setup with `zbox setup` (zcp's zerops_import does it itself).",
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
