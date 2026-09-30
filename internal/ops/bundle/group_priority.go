package bundle

import (
	"fmt"
	"sort"
	"strings"
)

// A tier's priorities are its dependency order.
//
// The platform creates a project's services a priority group at a time,
// highest first, and each group waits until the one before it is created and
// DEPLOYED (measured 2026-09-29 on two Mates: the data at 10, then the backend
// pair at 6, then the storefront pair at 5, as three serial waves), and the
// group's broker deploys an environment's runtimes in the same order. So a
// priority says what has to be up before what: a runtime another runtime
// references comes up before it.
//
// The managed services keep 10 and go first: nothing of theirs is built, and
// every runtime may reference them. A runtime's rank is the length of the
// longest chain of runtimes that reference it, one reference at a time:
// nothing references a storefront, so it sits in the lowest rank (priority
// 1); the backend it references sits one above (2); an auth service the
// backend references, one above that (3). A runtime nothing references and
// that references no runtime sits in the lowest rank with the storefront —
// nothing waits for it.
//
// A reference is what a runtime's BUILD reads (BuildReadValues): a
// `${hostname_key}` in any setup's build.envVariables of the pair's
// zerops.yaml — a storefront's build bakes the backend's publishable key and
// pre-renders from its API — directly, through a runtime variable the build
// lifts (`${RUNTIME_X}`), or through a project variable. What a runtime reads
// only once it runs orders nothing: it starts in the same wave as what it
// reads. Any hostname a pair goes by names the pair: its dev half, its stage
// half, and the promoted name a group environment gives it.
// Runtimes that reference each other share a rank, and the composer says so;
// a chain deeper than the ranks below the managed services stops at the last
// one, said too. The ranks are computed from sorted inputs, so the same
// project always ranks the same way.

// managedPriority is what every managed service is created at: first.
const managedPriority = 10

// lowestRuntimePriority is the rank nothing waits for.
const lowestRuntimePriority = 1

// groupApp is one runtime the priorities rank: a pair, or a utility.
type groupApp struct {
	// key names it: a pair's dev hostname, a utility's hostname.
	key string
	// hostnames are every name it goes by on any tier.
	hostnames []string
	// sources are the values its build reads (BuildReadValues): their
	// references say what it needs up first.
	sources []string
}

// groupPriorities ranks every app by the runtimes that reference it, and says
// where the ranking had to compromise: a reference cycle, a chain too deep.
func groupPriorities(apps []groupApp, projectEnvs []ProjectEnvVar) (map[string]int, []string) {
	sorted := append([]groupApp(nil), apps...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].key < sorted[j].key })

	owner := map[string]string{} // a hostname → the app it names
	var hosts []string
	for _, a := range sorted {
		for _, h := range a.hostnames {
			if h = strings.TrimSpace(h); h != "" {
				owner[h] = a.key
				hosts = append(hosts, h)
			}
		}
	}
	project := make(map[string]string, len(projectEnvs))
	for _, env := range projectEnvs {
		project[env.Key] = env.Value
	}

	// references[a][b]: a's build reads b, so b comes up first.
	references := make(map[string]map[string]bool, len(sorted))
	for _, a := range sorted {
		targets := map[string]bool{}
		for _, h := range ReferencedHosts(a.sources, hosts, project) {
			if target := owner[h]; target != a.key {
				targets[target] = true
			}
		}
		references[a.key] = targets
	}

	components := referenceComponents(sorted, references)
	var warnings []string
	componentOf := map[string]int{}
	for i, members := range components {
		for _, key := range members {
			componentOf[key] = i
		}
		if len(members) > 1 {
			warnings = append(warnings, fmt.Sprintf(
				"runtimes %s reference each other, so they share a priority and the platform creates them in one wave",
				quoteAnd(members)))
		}
	}
	// referrers[c]: the components holding a runtime that references one in c.
	referrers := make([]map[int]bool, len(components))
	for i := range referrers {
		referrers[i] = map[int]bool{}
	}
	for from, targets := range references {
		for to := range targets {
			if cf, ct := componentOf[from], componentOf[to]; cf != ct {
				referrers[ct][cf] = true
			}
		}
	}
	levels := make([]int, len(components))
	done := make([]bool, len(components))
	var level func(c int) int
	level = func(c int) int {
		if done[c] {
			return levels[c]
		}
		best := -1
		for r := range referrers[c] {
			best = max(best, level(r))
		}
		levels[c], done[c] = best+1, true
		return levels[c]
	}

	priorities := make(map[string]int, len(sorted))
	var capped []string
	for _, a := range sorted {
		p := lowestRuntimePriority + level(componentOf[a.key])
		if p >= managedPriority {
			p = managedPriority - 1
			capped = append(capped, a.key)
		}
		priorities[a.key] = p
	}
	if len(capped) > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"runtimes %s sit deeper in a chain of references than the ranks below the managed services, so they share priority %d",
			quoteAnd(capped), managedPriority-1))
	}
	return priorities, warnings
}

// canonicalRefHost is a hostname as a reference spells it: the platform's
// env interpolator reads `my-db` as `my_db`.
func canonicalRefHost(hostname string) string {
	return strings.ReplaceAll(hostname, "-", "_")
}

// referenceComponents groups the apps into the strongly connected components
// of the reference graph (Tarjan), each component's members sorted, the
// components in a deterministic order.
func referenceComponents(apps []groupApp, references map[string]map[string]bool) [][]string {
	index := map[string]int{}
	low := map[string]int{}
	onStack := map[string]bool{}
	var stack []string
	var components [][]string
	next := 0

	var connect func(v string)
	connect = func(v string) {
		index[v], low[v] = next, next
		next++
		stack = append(stack, v)
		onStack[v] = true
		targets := make([]string, 0, len(references[v]))
		for w := range references[v] {
			targets = append(targets, w)
		}
		sort.Strings(targets)
		for _, w := range targets {
			if _, seen := index[w]; !seen {
				connect(w)
				low[v] = min(low[v], low[w])
			} else if onStack[w] {
				low[v] = min(low[v], index[w])
			}
		}
		if low[v] == index[v] {
			var members []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				members = append(members, w)
				if w == v {
					break
				}
			}
			sort.Strings(members)
			components = append(components, members)
		}
	}
	for _, a := range apps {
		if _, seen := index[a.key]; !seen {
			connect(a.key)
		}
	}
	return components
}

// quoteAnd reads keys as a sentence does: "a", "a" and "b", "a", "b" and "c".
func quoteAnd(keys []string) string {
	quoted := make([]string, 0, len(keys))
	for _, k := range keys {
		quoted = append(quoted, fmt.Sprintf("%q", k))
	}
	if len(quoted) == 1 {
		return quoted[0]
	}
	return strings.Join(quoted[:len(quoted)-1], ", ") + " and " + quoted[len(quoted)-1]
}
