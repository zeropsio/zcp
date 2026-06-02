package knowledge

import (
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

// compactBase renders a base with its versions in brace notation:
// {base:"nodejs", versions:["18","20","22"]} -> "nodejs@{18,20,22}";
// a versionless base -> just the base; a single version -> "base@v".
func compactBase(bv baseVersions) string {
	switch len(bv.versions) {
	case 0:
		return bv.base
	case 1:
		return bv.base + "@" + bv.versions[0]
	default:
		return bv.base + "@{" + strings.Join(bv.versions, ",") + "}"
	}
}
