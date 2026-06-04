package workflow

import (
	"fmt"

	"github.com/zeropsio/zcp/internal/topology"
	"gopkg.in/yaml.v3"
)

// RecipeRuntimeRoleKind is the role a recipe runtime service plays, derived
// from its `zeropsSetup`. Orthogonal to envelope Mode — it says which half of
// the recipe shape a service is (build-side dev, serve-side stage, or a
// non-HTTP background worker), so the derived plan + the YAML rewrite key off
// one shape instead of re-matching plan slots back onto the YAML (R3).
type RecipeRuntimeRoleKind string

const (
	RecipeRuntimeRoleDev    RecipeRuntimeRoleKind = "dev"    // zeropsSetup: dev
	RecipeRuntimeRoleStage  RecipeRuntimeRoleKind = "stage"  // zeropsSetup: prod (and any non-dev/non-worker)
	RecipeRuntimeRoleWorker RecipeRuntimeRoleKind = "worker" // zeropsSetup: worker — background, no HTTP
)

// RecipeImportShape is the parsed, authoritative shape of a recipe's
// project-import YAML — the single owner the recipe route derives its plan AND
// its YAML rewrite from (R3). Replaces the lossy (mode, count) reduction with
// the full runtime + managed-dep enumeration, so workers and cross-type pairs
// are first-class instead of collapsing to an unrecognized shape.
// (Named *Import* to avoid the test-only RecipeShape fixture-enum.)
type RecipeImportShape struct {
	Runtimes    []RecipeRuntimeShape    `json:"runtimes,omitempty"`
	ManagedDeps []RecipeManagedDepShape `json:"managedDeps,omitempty"`
}

// RecipeRuntimeShape is one runtime service in the recipe (a service that
// declares `zeropsSetup`). Index is the position in the original services
// sequence so the rewrite can key by original identity.
type RecipeRuntimeShape struct {
	Hostname     string
	Type         string
	RoleKind     RecipeRuntimeRoleKind
	ZeropsSetup  string
	BuildFromGit string
	IsWorker     bool
	ServesHTTP   bool
	Index        int
}

// RecipeManagedDepShape is one managed dependency in the recipe (a service
// with no `zeropsSetup`). Resolution is empty (CREATE-implied) until a plan
// override sets EXISTS.
type RecipeManagedDepShape struct {
	Hostname   string
	Type       string
	Mode       string
	Resolution string
	Index      int
}

// roleKindFromSetup maps a zeropsSetup value to a role kind. dev→Dev,
// worker→Worker, everything else (prod, staging, …)→Stage — mirroring the
// rewrite's historical non-dev→stage fold (recipe_override.go).
func roleKindFromSetup(zeropsSetup string) RecipeRuntimeRoleKind {
	switch zeropsSetup {
	case RecipeSetupDev:
		return RecipeRuntimeRoleDev
	case RecipeSetupWorker:
		return RecipeRuntimeRoleWorker
	default:
		return RecipeRuntimeRoleStage
	}
}

// ParseRecipeImportShape parses a recipe's project-import YAML into the
// RecipeImportShape owner. Runtime services are those declaring `zeropsSetup`;
// everything else (no zeropsSetup) is a managed dependency. Returns an error
// only when the YAML is malformed.
func ParseRecipeImportShape(importYAML string) (RecipeImportShape, error) {
	var doc recipeImportDoc
	if err := yaml.Unmarshal([]byte(importYAML), &doc); err != nil {
		return RecipeImportShape{}, err
	}
	var shape RecipeImportShape
	for i, svc := range doc.Services {
		if svc.ZeropsSetup != "" {
			role := roleKindFromSetup(svc.ZeropsSetup)
			shape.Runtimes = append(shape.Runtimes, RecipeRuntimeShape{
				Hostname:     svc.Hostname,
				Type:         svc.Type,
				RoleKind:     role,
				ZeropsSetup:  svc.ZeropsSetup,
				BuildFromGit: svc.BuildFromGit,
				IsWorker:     role == RecipeRuntimeRoleWorker,
				ServesHTTP:   role != RecipeRuntimeRoleWorker,
				Index:        i,
			})
			continue
		}
		shape.ManagedDeps = append(shape.ManagedDeps, RecipeManagedDepShape{
			Hostname: svc.Hostname,
			Type:     svc.Type,
			Mode:     svc.Mode,
			Index:    i,
		})
	}
	return shape, nil
}

// recipeImportDoc is the single unmarshal target for a recipe import YAML.
type recipeImportDoc struct {
	Services []struct {
		Hostname     string `yaml:"hostname"`
		Type         string `yaml:"type"`
		ZeropsSetup  string `yaml:"zeropsSetup"`
		BuildFromGit string `yaml:"buildFromGit"`
		Mode         string `yaml:"mode"`
	} `yaml:"services"`
}

// RuntimeCount returns the number of runtime services (including workers),
// matching the historical InferRecipeShape count.
func (s RecipeImportShape) RuntimeCount() int { return len(s.Runtimes) }

// Mode derives the bootstrap mode from the dev/stage runtimes, IGNORING worker
// extras (a queue worker doesn't change whether the app is standard/simple/dev).
// Byte-identical to the historical InferRecipeShape switch over non-worker
// setups, so appdev+appstage+workerstage is "standard" rather than the old
// lossy "" — letting worker recipes stop collapsing to unrecognized.
//
//	standard — one dev + one prod(stage)
//	simple   — single prod(stage)
//	dev      — single dev
//	""       — managed-only, unknown pattern
func (s RecipeImportShape) Mode() topology.Mode {
	var setups []string
	for _, r := range s.Runtimes {
		if r.IsWorker {
			continue
		}
		setups = append(setups, r.ZeropsSetup)
	}
	switch len(setups) {
	case 1:
		if setups[0] == RecipeSetupProd {
			return topology.PlanModeSimple
		}
		if setups[0] == RecipeSetupDev {
			return topology.PlanModeDev
		}
		return ""
	case 2:
		hasDev := setups[0] == RecipeSetupDev || setups[1] == RecipeSetupDev
		hasProd := setups[0] == RecipeSetupProd || setups[1] == RecipeSetupProd
		if hasDev && hasProd {
			return topology.PlanModeStandard
		}
		return ""
	default:
		return ""
	}
}

// InferRecipeShape inspects a recipe's project-import YAML and returns the
// bootstrap mode it implies plus the runtime service count. BC shim over
// ParseRecipeShape + the derived Mode()/RuntimeCount() accessors (R3).
//
// Modes:
//   - "standard" — two runtimes, one `zeropsSetup: dev` + one `zeropsSetup: prod`
//   - "simple"   — single runtime with `zeropsSetup: prod`
//   - "dev"      — single runtime with `zeropsSetup: dev`
//   - ""         — managed-only, unknown pattern, or invalid YAML
func InferRecipeShape(importYAML string) (mode topology.Mode, runtimeCount int) {
	shape, err := ParseRecipeImportShape(importYAML)
	if err != nil {
		return "", 0
	}
	return shape.Mode(), shape.RuntimeCount()
}

// RecipeShapeOverrides carries the choices the recipe import YAML can't hold:
// runtime hostname renames (keyed by the recipe's ORIGINAL hostname) and
// managed-dep resolution flips (CREATE→EXISTS for a collision). Empty = derive
// the recipe's default shape verbatim. (Dev-only narrowing is a separate
// opt-in phase and adds its own field then.)
type RecipeShapeOverrides struct {
	RuntimeHostnameByOriginal map[string]string
	ManagedResolutionByHost   map[string]string
}

// DeriveRecipePlan builds the bootstrap plan directly from the recipe import
// shape (the owner) plus explicit user overrides — the agent authors nothing
// in the happy path, mirroring the adopt route (InferServicePairing). EVERY
// runtime in the recipe becomes a declared target that earns a ServiceMeta;
// this is what stops a recipe runtime from being provisioned-but-untracked (R3).
//
// Runtimes are grouped by buildFromGit repo — one repo is one app codebase, and
// its dev/stage halves are one deployable. Per group:
//   - dev + stage (non-worker) → ONE standard target; StageType is set when the
//     stage half is a different type (cross-type pair). dev-only → dev mode;
//     stage-only → simple mode.
//   - each zeropsSetup:worker → a standalone simple target (its own ServiceMeta;
//     ServesHTTP=false is stamped at provision from the shape).
//
// Multi-repo recipes (zerops-showcase: a bun app repo + a python worker repo)
// therefore yield one target per repo, so the second pair is no longer dropped.
//
// Managed deps → CREATE deps (or EXISTS via override) attached to the PRIMARY
// app target only (the first repo's app target). Unlike adopt's shared
// EXISTS-on-both-halves (adopt.go), recipe managed deps are CREATE and must be
// created exactly once; every other runtime reaches them via ${host_*} env
// refs, not its own plan dep.
//
// Returns an error when the shape has no derivable runtime (managed-only).
func DeriveRecipePlan(shape RecipeImportShape, overrides RecipeShapeOverrides) ([]BootstrapTarget, error) {
	hostOf := func(original string) string {
		if overrides.RuntimeHostnameByOriginal != nil {
			if h, ok := overrides.RuntimeHostnameByOriginal[original]; ok && h != "" {
				return h
			}
		}
		return original
	}

	// repoGroup collects the runtimes that share one buildFromGit repo, in
	// first-seen order, splitting them into the dev/stage app pair + workers.
	type repoGroup struct {
		dev     *RecipeRuntimeShape
		stage   *RecipeRuntimeShape
		workers []RecipeRuntimeShape
	}
	var order []string
	groups := map[string]*repoGroup{}
	for i := range shape.Runtimes {
		r := &shape.Runtimes[i]
		key := r.BuildFromGit
		if key == "" {
			key = r.Hostname // no repo (rare) → its own group, never merged
		}
		g := groups[key]
		if g == nil {
			g = &repoGroup{}
			groups[key] = g
			order = append(order, key)
		}
		switch {
		case r.IsWorker:
			g.workers = append(g.workers, *r)
		case r.RoleKind == RecipeRuntimeRoleDev && g.dev == nil:
			g.dev = r
		case r.RoleKind == RecipeRuntimeRoleStage && g.stage == nil:
			g.stage = r
		default:
			// Extra dev/stage beyond the first pair in one repo (not present in
			// any current recipe). Treat as a standalone runtime rather than
			// silently drop it — completeness is the invariant R3 protects.
			g.workers = append(g.workers, *r)
		}
	}

	deps := deriveManagedDeps(shape, overrides)
	depsAssigned := false

	var targets []BootstrapTarget
	for _, key := range order {
		g := groups[key]
		switch {
		case g.dev != nil && g.stage != nil:
			rt := RuntimeTarget{
				DevHostname:   hostOf(g.dev.Hostname),
				ExplicitStage: hostOf(g.stage.Hostname),
				Type:          g.dev.Type,
				BootstrapMode: topology.PlanModeStandard,
			}
			if !topology.TypesAreEquivalent(g.dev.Type, g.stage.Type) {
				rt.StageType = g.stage.Type
			}
			t := BootstrapTarget{Runtime: rt}
			if !depsAssigned {
				t.Dependencies = deps
				depsAssigned = true
			}
			targets = append(targets, t)
		case g.dev != nil:
			t := BootstrapTarget{Runtime: RuntimeTarget{DevHostname: hostOf(g.dev.Hostname), Type: g.dev.Type, BootstrapMode: topology.PlanModeDev}}
			if !depsAssigned {
				t.Dependencies = deps
				depsAssigned = true
			}
			targets = append(targets, t)
		case g.stage != nil:
			t := BootstrapTarget{Runtime: RuntimeTarget{DevHostname: hostOf(g.stage.Hostname), Type: g.stage.Type, BootstrapMode: topology.PlanModeSimple}}
			if !depsAssigned {
				t.Dependencies = deps
				depsAssigned = true
			}
			targets = append(targets, t)
		}
		for _, w := range g.workers {
			targets = append(targets, BootstrapTarget{
				Runtime: RuntimeTarget{DevHostname: hostOf(w.Hostname), Type: w.Type, BootstrapMode: topology.PlanModeSimple},
			})
		}
	}

	if len(targets) == 0 {
		return nil, fmt.Errorf("recipe import shape has no runtime services to derive a plan from")
	}

	return targets, nil
}

// deriveManagedDeps maps the recipe shape's managed deps to plan Dependencies,
// defaulting to CREATE (the recipe provisions them) unless an override flips a
// hostname to EXISTS (the service already exists — collision recovery).
func deriveManagedDeps(shape RecipeImportShape, overrides RecipeShapeOverrides) []Dependency {
	if len(shape.ManagedDeps) == 0 {
		return nil
	}
	deps := make([]Dependency, 0, len(shape.ManagedDeps))
	for _, m := range shape.ManagedDeps {
		res := ResolutionCreate
		if overrides.ManagedResolutionByHost != nil {
			if r, ok := overrides.ManagedResolutionByHost[m.Hostname]; ok && r != "" {
				res = r
			}
		}
		deps = append(deps, Dependency{Hostname: m.Hostname, Type: m.Type, Mode: m.Mode, Resolution: res})
	}
	return deps
}

// ValidateBootstrapRecipeMode rejects plans whose bootstrap mode deviates from
// the recipe the route matched. Recipes ship with a fixed shape (standard,
// simple, or dev) baked into their import YAML; deviation strips the agent of
// the recipe-specific rules it needs (e.g. startWithoutCode on simple).
//
// A nil match or empty Mode (unrecognised shape) disables the check — recipe
// atoms may ship without a structured shape, and we shouldn't block on that.
func ValidateBootstrapRecipeMode(match *RecipeMatch, targets []BootstrapTarget) error {
	if match == nil || match.Mode == "" {
		return nil
	}
	for _, t := range targets {
		if t.Runtime.EffectiveMode() != match.Mode {
			return fmt.Errorf("recipe %q is %s mode but target %q uses %s — deviating from the recipe strips mode-specific rules from provisioning; either follow the recipe or restart bootstrap with a different intent",
				match.Slug, match.Mode, t.Runtime.DevHostname, t.Runtime.EffectiveMode())
		}
	}
	return nil
}
