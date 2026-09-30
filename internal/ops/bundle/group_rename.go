package bundle

import (
	"regexp"
	"sort"
	"strings"
)

// A group environment has one runtime per pair, under its promoted name
// (`medusadev`/`medusastage` → `medusa`), so a value that names a pair's dev
// or stage half names a service that is not there. The Mate's project is
// where a pair's environment-specific wiring lives — a storefront reaching
// its backend through `MEDUSA_INTERNAL_URL: http://medusastage:9000`, a
// `${medusastage_CHANNEL_PUBLISHABLE_KEY}` — and copied as it is into a stage
// or a production it points nowhere. So the group environments rewrite what
// a value says about a pair's halves, and nothing else:
//
//   - a `${half_key}` reference names the promoted runtime's `${runtime_key}`;
//   - a half's hostname right after `://` or `@`, and followed by what ends a
//     hostname in a URL (`:`, `/`, `-` of a subdomain URL, the end) — never a
//     `.`, which would make it a custom domain — names the promoted runtime.
//
// A bare word, another service's reference and a longer hostname stay as
// written. The AI Agent tier re-creates the Mate's pairs and keeps every
// value as it is.

// promotePairHostnames rewrites a value for a group environment. renames maps
// each pair half's hostname to its pair's promoted one.
func promotePairHostnames(value string, renames map[string]string) string {
	return newPairPromoter(renames).promote(value)
}

// pairPromoter rewrites values for a group environment, its patterns built
// once per recipe.
type pairPromoter struct {
	// halves is every renamed hostname, the longest first: `${api_dev_port}`
	// is api-dev's, not api's.
	halves   []string
	renames  map[string]string
	patterns []*regexp.Regexp
}

func newPairPromoter(renames map[string]string) *pairPromoter {
	p := &pairPromoter{renames: renames}
	for half := range renames {
		p.halves = append(p.halves, half)
	}
	sort.Slice(p.halves, func(i, j int) bool {
		if len(p.halves[i]) != len(p.halves[j]) {
			return len(p.halves[i]) > len(p.halves[j])
		}
		return p.halves[i] < p.halves[j]
	})
	for _, half := range p.halves {
		p.patterns = append(p.patterns, regexp.MustCompile(`(://|@)`+regexp.QuoteMeta(half)+`([^A-Za-z0-9_.]|$)`))
	}
	return p
}

// promote rewrites one value.
func (p *pairPromoter) promote(value string) string {
	if len(p.halves) == 0 || value == "" {
		return value
	}
	value = promoteReferences(value, p.halves, p.renames)
	for i, half := range p.halves {
		value = p.patterns[i].ReplaceAllString(value, "${1}"+p.renames[half]+"${2}")
	}
	return value
}

// promoteReferences rewrites every `${half_key}` to `${runtime_key}`.
func promoteReferences(value string, halves []string, renames map[string]string) string {
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
		name := value[start+2 : start+end]
		for _, half := range halves {
			prefix := canonicalRefHost(half) + "_"
			if strings.HasPrefix(name, prefix) && len(name) > len(prefix) {
				name = canonicalRefHost(renames[half]) + "_" + strings.TrimPrefix(name, prefix)
				break
			}
		}
		b.WriteString(value[:start] + "${" + name + "}")
		value = value[start+end+1:]
	}
	b.WriteString(value)
	return b.String()
}

// pairRenames maps every pair half's hostname that a group environment
// renames to the pair's promoted hostname.
func pairRenames(runtimes []GroupRuntime) map[string]string {
	renames := map[string]string{}
	for _, r := range runtimes {
		promoted := GroupPromotedHostname(r.DevHostname)
		for _, half := range []string{r.DevHostname, r.StageHostname} {
			if half != "" && half != promoted {
				renames[half] = promoted
			}
		}
	}
	return renames
}
