package knowledge

import (
	"sort"
	"strconv"
	"strings"

	"github.com/zeropsio/zcp/internal/schema"
	"github.com/zeropsio/zcp/internal/topology"
)

// catalogView is the briefing-ready projection of the live schema — it replaces
// the deleted stack-types API as the source for "what can this instance run?".
// Import service types are bucketed by topology category (runtime / managed /
// shared-storage / object-storage), OS- and mode-variant duplicates collapsed to
// their canonical bare base (so `alpine/nodejs@22` + `ubuntu/nodejs@22` →
// `nodejs` with version `22`, and `postgresql:single@18` + `postgresql:ha@18` →
// `postgresql` with version `18`). buildBaseNames carries the base names usable
// as a `build.base` (so the briefing can mark them and surface build-only bases
// like `php`, whose runtime is `php-nginx`/`php-apache`).
type catalogView struct {
	runtime        []baseVersions
	managed        []baseVersions
	sharedStorage  bool
	objectStorage  bool
	buildOnly      []baseVersions // build bases with no matching runtime base
	buildBaseNames map[string]bool
	versionsByBase map[string][]string // base -> full "base@version" names (runtime + managed)
}

// baseVersions is one base name and its available versions, in schema order.
type baseVersions struct {
	base     string
	versions []string // bare version tags ("18","8.4","latest"); empty for versionless types
}

// groupAcc accumulates bases and their versions preserving first-seen order.
type groupAcc struct {
	order    []string
	versions map[string][]string
	baseSeen map[string]bool
	verSeen  map[string]bool // "base@version"
}

func newGroupAcc() *groupAcc {
	return &groupAcc{versions: map[string][]string{}, baseSeen: map[string]bool{}, verSeen: map[string]bool{}}
}

func (g *groupAcc) add(base, version string) {
	if !g.baseSeen[base] {
		g.baseSeen[base] = true
		g.order = append(g.order, base)
	}
	if version == "" {
		return
	}
	key := base + "@" + version
	if g.verSeen[key] {
		return
	}
	g.verSeen[key] = true
	g.versions[base] = append(g.versions[base], version)
}

func (g *groupAcc) materialize() []baseVersions {
	out := make([]baseVersions, 0, len(g.order))
	for _, b := range g.order {
		out = append(out, baseVersions{base: b, versions: g.versions[b]})
	}
	return out
}

// baseAndVersion reduces a service type / base value to its canonical bare base
// and version tag (OS prefix + mode suffix stripped).
func baseAndVersion(t string) (base, version string) {
	base, version, _ = strings.Cut(topology.CanonicalBareForm(strings.ToLower(t)), "@")
	return base, version
}

func buildCatalogView(schemas *schema.Schemas) *catalogView {
	if schemas == nil || schemas.ImportYml == nil {
		return nil
	}
	cv := &catalogView{buildBaseNames: map[string]bool{}, versionsByBase: map[string][]string{}}

	runtime := newGroupAcc()
	managed := newGroupAcc()
	for _, t := range schemas.ImportYml.ServiceTypes {
		switch {
		case topology.IsObjectStorageType(t):
			cv.objectStorage = true
		case topology.IsSharedStorageType(t):
			cv.sharedStorage = true
		case topology.IsManagedService(t):
			base, ver := baseAndVersion(t)
			managed.add(base, ver)
		default:
			base, ver := baseAndVersion(t)
			runtime.add(base, ver)
		}
	}
	cv.runtime = runtime.materialize()
	cv.managed = managed.materialize()

	// Build-base names + a build-only accumulator for bases that have no runtime
	// service type (e.g. `php` — the runtime is `php-nginx`/`php-apache`).
	build := newGroupAcc()
	if schemas.ZeropsYml != nil {
		for _, bb := range schemas.ZeropsYml.BuildBases {
			base, ver := baseAndVersion(bb)
			cv.buildBaseNames[base] = true
			build.add(base, ver)
		}
	}
	runtimeBases := make(map[string]bool, len(cv.runtime))
	for _, bv := range cv.runtime {
		runtimeBases[bv.base] = true
	}
	for _, bv := range build.materialize() {
		if !runtimeBases[bv.base] {
			cv.buildOnly = append(cv.buildOnly, bv)
		}
	}

	// Index full version names per base for the version-check lookup.
	for _, group := range [][]baseVersions{cv.runtime, cv.managed} {
		for _, bv := range group {
			for _, v := range bv.versions {
				cv.versionsByBase[bv.base] = append(cv.versionsByBase[bv.base], bv.base+"@"+v)
			}
		}
	}
	return cv
}

// compactBase renders a base with its CONCRETE recommended versions, newest
// first and marked "(latest)":
//
//	{nodejs, [18,20,22,24,latest]} -> "nodejs@24 (latest) · 22 · 20 · 18"
//	{go, [1,1.22,latest]}          -> "go@1.22"
//	{static, []}                   -> "static"
//
// The agent is shown the active concrete versions it can pick (so it knows what
// it uses + what's available) — but never a version-family alias (`go@1`) or a
// rolling tag (`latest`/`canary`), because those resolve to a concrete version
// at import and then mismatch the provision check (F1). The full version list
// (incl. families, for the "X not found, available: …" lookup) stays in
// cv.versionsByBase — this is presentation only.
func compactBase(bv baseVersions) string {
	leaves := concreteLeaves(bv.versions)
	mark := true
	if len(leaves) == 0 {
		// No concrete versions active (some runtimes — e.g. rust — ship only
		// rolling channels like stable/nightly). Those ARE the only importable
		// choice, so show them rather than a bare, un-authorable base name.
		leaves = rollingVersions(bv.versions)
		mark = false // "(latest)" is meaningless on a rolling-only base
	}
	switch len(leaves) {
	case 0:
		return bv.base
	case 1:
		return bv.base + "@" + leaves[0]
	default:
		parts := make([]string, 0, len(leaves))
		first := bv.base + "@" + leaves[0]
		if mark {
			first += " (latest)"
		}
		parts = append(parts, first)
		parts = append(parts, leaves[1:]...)
		return strings.Join(parts, " · ")
	}
}

// rollingVersions returns the rolling tags present, "stable" first (the sane
// default), then the rest in input order. Used only when a base has no concrete
// active version to recommend.
func rollingVersions(versions []string) []string {
	var stable, rest []string
	for _, v := range versions {
		if !isRollingTag(v) {
			continue
		}
		if strings.EqualFold(v, "stable") {
			stable = append(stable, v)
		} else {
			rest = append(rest, v)
		}
	}
	return append(stable, rest...)
}

// concreteLeaves reduces a base's version list to its concrete leaf versions:
// drops rolling tags (latest/canary/nightly/stable) and any version that is a
// strict dot-component prefix of another (a family alias like "1" or "1.3" when
// "1.3.9" is present), then sorts newest-first.
func concreteLeaves(versions []string) []string {
	concrete := make([]string, 0, len(versions))
	for _, v := range versions {
		if !isRollingTag(v) {
			concrete = append(concrete, v)
		}
	}
	leaves := make([]string, 0, len(concrete))
	for i, v := range concrete {
		isPrefix := false
		for j, w := range concrete {
			if i != j && isDotPrefix(v, w) {
				isPrefix = true
				break
			}
		}
		if !isPrefix {
			leaves = append(leaves, v)
		}
	}
	sort.Slice(leaves, func(i, j int) bool { return versionLess(leaves[j], leaves[i]) })
	return leaves
}

func isRollingTag(v string) bool {
	switch strings.ToLower(v) {
	case "latest", "canary", "nightly", "stable", "edge", "dev":
		return true
	default:
		return false
	}
}

// isDotPrefix reports whether a is a STRICT dot-component prefix of b
// ("1" ⊂ "1.3.9", "1.3" ⊂ "1.3.9"; "22" is NOT a prefix of "24").
func isDotPrefix(a, b string) bool {
	ap, bp := strings.Split(a, "."), strings.Split(b, ".")
	if len(ap) >= len(bp) {
		return false
	}
	for i := range ap {
		if ap[i] != bp[i] {
			return false
		}
	}
	return true
}

// versionLess compares two dot-segmented versions numerically per segment,
// falling back to string compare for non-numeric segments.
func versionLess(a, b string) bool {
	ap, bp := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(ap) && i < len(bp); i++ {
		ai, aerr := strconv.Atoi(ap[i])
		bi, berr := strconv.Atoi(bp[i])
		switch {
		case aerr == nil && berr == nil:
			if ai != bi {
				return ai < bi
			}
		case ap[i] != bp[i]:
			return ap[i] < bp[i]
		}
	}
	return len(ap) < len(bp)
}
