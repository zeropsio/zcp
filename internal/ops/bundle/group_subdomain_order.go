package bundle

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// A tier creates the service a runtime reads through ${<host>_zeropsSubdomain}
// before that runtime.
//
// The platform gives a service its zeropsSubdomain from its first build, and
// a reference to it resolves only in a container started after that service
// was deployed: a reader started with it or before it reads the literal
// ${<host>_zeropsSubdomain} and keeps it until it restarts. The order a
// tier's services are written in decides nothing; their priorities do —
// the platform creates a priority at a time, each wave deployed before the
// next starts — so a target at priority 9 resolved in a reader at 1 on the
// first boot (live, Mate2 lab, 2026-10-28). A service's own
// ${zeropsSubdomain} resolves on its first boot, and a reference in a
// project variable resolves in each service under the same rule.
//
// So each tier lifts, on top of the ranks the build reads give
// (rankGroupApps), every runtime another runtime of the tier reads that way
// above that reader: a reference in the reader's own variables as the tier
// writes them — one the rewrite writes for an address (tierAddresses) or one
// a person wrote — or in the build and run variables of the setup the tier
// builds for it. A runtime lifted lifts what its build reads above it in
// turn. A managed service or a Box is created first anyway.
//
// Two runtimes naming each other, directly or through what their builds
// read, cannot both come first. The reads a person wrote are ordered first
// and kept whatever comes of them; then the rewrite's, each ordered unless
// the runtime it names has to be up before its reader already — then the
// address is kept as written and said, rather than written as a reference
// certain to boot literal. A chain deeper than the priorities below the
// managed services is said the same way. A project variable is read by
// every service, so no priority puts the service it names before all of
// them: an address there is kept as written (tierAddresses), and a person's
// reference there is said.

// tierRank is a runtime of a tier the order ranks: its hostname in the tier,
// the app it is of (groupApp.key), and the priority the build reads give it.
type tierRank struct {
	host, app string
	base      int
}

// subdomainRead is a read by a runtime of a tier (reader) of another of the
// tier's services' public address (target), through a
// ${<host>_zeropsSubdomain} reference: where the reader reads it
// (variable: `its FLIP_SERVER`, `setup "prod"'s run.envVariables.API_URL`),
// and whether the tier's rewrite writes it for an address written out
// (rewritten), which it keeps as written instead when no priority orders it.
type subdomainRead struct {
	reader, target, variable string
	rewritten                bool
}

// unorderedRead is a read no priority orders, and why.
type unorderedRead struct {
	read subdomainRead
	why  string
}

// orderSubdomainReads is the priority of each runtime of a tier (ranks) once
// every read is ordered that can be — the service it names above its reader
// — and the reads that cannot be. ranking is the build reads' (rankGroupApps).
func orderSubdomainReads(ranks []tierRank, reads []subdomainRead, ranking groupRanking) (map[string]int, []unorderedRead) {
	app, base := map[string]string{}, map[string]int{}
	for _, r := range ranks {
		app[r.host], base[r.host] = r.app, r.base
	}
	hosts := slices.Sorted(maps.Keys(app))
	// up[x][y]: y has to be up before x — x's app's build reads y's, in
	// another reference cycle, or x reads y's public address. tied[x][y]: the
	// builds of x's and y's apps read each other, which no priority orders.
	up, tied := map[string]map[string]bool{}, map[string]map[string]bool{}
	link := func(edges map[string]map[string]bool, x, y string) {
		if edges[x] == nil {
			edges[x] = map[string]bool{}
		}
		edges[x][y] = true
	}
	for _, x := range hosts {
		for _, y := range hosts {
			if app[x] == app[y] || !ranking.reads(app[x], app[y]) {
				continue
			}
			if ranking.component[app[x]] == ranking.component[app[y]] {
				link(tied, x, y)
			} else {
				link(up, x, y)
			}
		}
	}
	// reaches reports whether from has to be up after to already.
	reaches := func(from, to string) bool {
		seen := map[string]bool{from: true}
		queue := []string{from}
		for len(queue) > 0 {
			x := queue[0]
			queue = queue[1:]
			for _, edges := range []map[string]map[string]bool{up, tied} {
				for y := range edges[x] {
					if y == to {
						return true
					}
					if !seen[y] {
						seen[y] = true
						queue = append(queue, y)
					}
				}
			}
		}
		return false
	}

	sorted := append([]subdomainRead(nil), reads...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.rewritten != b.rewritten {
			return !a.rewritten
		}
		if a.reader != b.reader {
			return a.reader < b.reader
		}
		if a.target != b.target {
			return a.target < b.target
		}
		return a.variable < b.variable
	})
	var unordered []unorderedRead
	var ordered []subdomainRead
	for _, r := range sorted {
		if _, ranked := app[r.target]; !ranked || r.reader == r.target {
			// A managed service or a Box is created first anyway.
			continue
		}
		if reaches(r.target, r.reader) {
			unordered = append(unordered, unorderedRead{r, fmt.Sprintf(
				"%s resolves only in a container started after %q is deployed, and %q has to be up before %q already, so no priority orders them",
				subdomainReference(r.target), r.target, r.reader, r.target)})
			continue
		}
		link(up, r.reader, r.target)
		ordered = append(ordered, r)
	}

	// A runtime sits above every runtime that has to be up after it, and
	// never below the rank its build reads give it.
	after := map[string][]string{}
	for x, targets := range up {
		for y := range targets {
			after[y] = append(after[y], x)
		}
	}
	priorities := make(map[string]int, len(hosts))
	var rank func(host string) int
	rank = func(host string) int {
		if p, done := priorities[host]; done {
			return p
		}
		p := base[host]
		for _, x := range after[host] {
			p = max(p, rank(x)+1)
		}
		priorities[host] = p
		return p
	}
	for _, host := range hosts {
		rank(host)
	}
	for host, p := range priorities {
		priorities[host] = min(p, managedPriority-1)
	}
	for _, r := range ordered {
		if priorities[r.target] <= priorities[r.reader] {
			unordered = append(unordered, unorderedRead{r, fmt.Sprintf(
				"%s resolves only in a container started after %q is deployed, and more services have to be up before it than there are priorities below the managed services, so no priority orders them",
				subdomainReference(r.target), r.target)})
		}
	}
	return priorities, unordered
}

// orderTierReads orders the public-address reads of the tier titled title
// (orderSubdomainReads): those addresses collected from the values it
// writes, beside those of the setup each of its runtimes (ranks) builds. It
// returns each runtime's priority and says each read the tier keeps that no
// priority orders; a reference the rewrite would write that none orders is
// kept as written instead (tierAddresses.unordered).
func orderTierReads(ranking groupRanking, ranks []tierRank, addresses *tierAddresses, title string) (map[string]int, []orderNote) {
	reads := append([]subdomainRead(nil), addresses.reads...)
	for _, rank := range ranks {
		build, ok := addresses.builds[rank.host]
		if !ok {
			continue
		}
		for _, ref := range setupSubdomainRefs(build.body, build.setup) {
			if target := addresses.refHosts[ref[1]]; target != "" {
				reads = append(reads, subdomainRead{reader: rank.host, target: target, variable: fmt.Sprintf("setup %q's %s", build.setup, ref[0])})
			}
		}
	}
	priorities, unordered := orderSubdomainReads(ranks, reads, ranking)
	var notes []orderNote
	for _, u := range unordered {
		if u.read.rewritten {
			addresses.unordered[[2]string{u.read.reader, u.read.target}] = u.why
			continue
		}
		notes = append(notes, orderNote{
			subject: fmt.Sprintf("runtime %q reads %s in %s", u.read.reader, subdomainReference(u.read.target), u.read.variable),
			why:     u.why, tier: title,
		})
	}
	for _, read := range addresses.projectReads {
		notes = append(notes, orderNote{
			subject: fmt.Sprintf("project variable %s reads %s", read.key, subdomainReference(read.target)),
			why:     fmt.Sprintf("every service reads a project variable, and the reference resolves only in one started after %q is deployed", read.target),
			tier:    title,
		})
	}
	return priorities, notes
}

// subdomainReference is the reference to host's public address.
func subdomainReference(host string) string {
	return "${" + canonicalRefHost(host) + "_zeropsSubdomain}"
}

// subdomainRefHost is the hostname a ${<host>_zeropsSubdomain} reference
// names, as the reference spells it; ok is false for any other reference.
func subdomainRefHost(name string) (host string, ok bool) {
	host, ok = strings.CutSuffix(name, "_zeropsSubdomain")
	return host, ok && host != ""
}

// setupSubdomainRefs is each ${<host>_zeropsSubdomain} reference in the
// build and run variables of setup of a zerops.yaml body, with those of the
// setups it extends: the variable (`run.envVariables.API_URL`) and the
// hostname as the reference spells it, by variable. A body that does not
// parse, or a setup it does not declare, has none.
func setupSubdomainRefs(body, setup string) [][2]string {
	var doc struct {
		Zerops []buildReadSetup `yaml:"zerops"`
	}
	if strings.TrimSpace(body) == "" || yaml.Unmarshal([]byte(body), &doc) != nil {
		return nil
	}
	bySetup := make(map[string]buildReadSetup, len(doc.Zerops))
	for _, s := range doc.Zerops {
		bySetup[s.Setup] = s
	}
	if _, declared := bySetup[setup]; !declared {
		return nil
	}
	var out [][2]string
	for _, block := range []struct {
		name string
		vars map[string]any
	}{
		{"build.envVariables", inheritedVars(bySetup, setup, func(x buildReadSetup) map[string]any { return x.Build.EnvVariables }, map[string]bool{})},
		{"run.envVariables", inheritedVars(bySetup, setup, func(x buildReadSetup) map[string]any { return x.Run.EnvVariables }, map[string]bool{})},
	} {
		for _, key := range slices.Sorted(maps.Keys(block.vars)) {
			value, ok := block.vars[key].(string)
			if !ok {
				continue
			}
			for _, name := range parseDollarBraceRefs(value) {
				if host, ok := subdomainRefHost(name); ok {
					out = append(out, [2]string{block.name + "." + key, host})
				}
			}
		}
	}
	return out
}

// orderNote is a public-address read a tier cannot order and keeps, for a
// person to know of: what reads it, the read, why, and the tier.
type orderNote struct {
	subject, why, tier string
}

// orderNoteWarnings says each read a tier keeps unordered, once for every
// tier that keeps it, in the order the tiers kept them.
func orderNoteWarnings(notes []orderNote) []string {
	order, tiers := saidInTiers(notes, func(n orderNote) (orderNote, string) {
		tier := n.tier
		n.tier = ""
		return n, tier
	})
	warnings := make([]string, 0, len(order))
	for _, n := range order {
		in, _ := tierPhrase(tiers[n])
		warnings = append(warnings, fmt.Sprintf(
			"%s, which no priority orders in %s: %s — a service created with or before the one it names starts with the literal text and keeps it until it restarts",
			n.subject, in, n.why))
	}
	return warnings
}

// saidInTiers groups what tiers said: each item once, with every tier that
// said it, in the order first said. split is an item without its tier, and
// the tier.
func saidInTiers[K comparable](items []K, split func(K) (K, string)) ([]K, map[K][]string) {
	var order []K
	tiers := map[K][]string{}
	for _, item := range items {
		key, tier := split(item)
		if _, seen := tiers[key]; !seen {
			order = append(order, key)
		}
		if !slices.Contains(tiers[key], tier) {
			tiers[key] = append(tiers[key], tier)
		}
	}
	return order, tiers
}

// tierPhrase reads tier titles as `the AI Agent tier` or `the Stage and
// Small Production tiers`; several reports the second.
func tierPhrase(titles []string) (phrase string, several bool) {
	if len(titles) == 1 {
		return "the " + titles[0] + " tier", false
	}
	return "the " + strings.Join(titles[:len(titles)-1], ", ") + " and " + titles[len(titles)-1] + " tiers", true
}
