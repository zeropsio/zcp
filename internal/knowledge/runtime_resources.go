package knowledge

// RuntimeResources defines per-runtime RAM recommendations for dev and stage services.
// Dev services run compilation on the container (SSH workflow) — minRam must absorb
// the compilation spike before autoscaling reacts (~10-20s).
// Stage/prod services run pre-built artifacts — minRam only needs to cover startup peak.
//
// These values are the SINGLE SOURCE OF TRUTH for:
//   - Runtime guide "Resource Requirements" sections
//   - Scaling guide general table
//   - Workflow import.yaml generation (dev minRam per runtime)
//   - runtime_resources_test.go cross-validation
type RuntimeResources struct {
	DevMinRAM   float64 // GB — compilation/install spike on dev container
	StageMinRAM float64 // GB — application startup peak
	DevReason   string  // why dev needs this much
	StageReason string  // why stage needs this much
}

// runtimeResourceMap defines RAM recommendations keyed by runtime guide slug.
// Only runtimes where agents compile on the container are included.
// Static, nginx, alpine, ubuntu, docker are omitted — no compilation spike.
var runtimeResourceMap = map[string]RuntimeResources{
	"go": {
		DevMinRAM: 1.0, StageMinRAM: 0.25,
		DevReason:   "go build peak ~0.8 GB",
		StageReason: "compiled binary, minimal footprint",
	},
	"java": {
		DevMinRAM: 1.5, StageMinRAM: 1.0,
		DevReason:   "Maven/Gradle + javac peak ~1.2 GB",
		StageReason: "JVM heap requires baseline allocation",
	},
	"nodejs": {
		DevMinRAM: 1.0, StageMinRAM: 0.5,
		DevReason:   "npm/pnpm install peak ~0.7 GB",
		StageReason: "V8 heap for SSR/API workloads",
	},
	"php": {
		DevMinRAM: 0.5, StageMinRAM: 0.25,
		DevReason:   "composer install peak ~0.3 GB",
		StageReason: "PHP-FPM workers are lightweight",
	},
	"python": {
		DevMinRAM: 0.5, StageMinRAM: 0.25,
		DevReason:   "pip install moderate peak",
		StageReason: "WSGI/ASGI workers are lightweight",
	},
	"rust": {
		DevMinRAM: 2.0, StageMinRAM: 0.25,
		DevReason:   "cargo build peak ~1.5 GB (link phase)",
		StageReason: "compiled binary, minimal footprint",
	},
	"elixir": {
		DevMinRAM: 1.0, StageMinRAM: 0.25,
		DevReason:   "mix compile + release build peak ~0.8 GB",
		StageReason: "BEAM VM lightweight for most apps",
	},
	"gleam": {
		DevMinRAM: 1.0, StageMinRAM: 0.25,
		DevReason:   "gleam build + erlang-shipment peak ~0.7 GB",
		StageReason: "BEAM VM lightweight for most apps",
	},
	"bun": {
		DevMinRAM: 0.5, StageMinRAM: 0.25,
		DevReason:   "bun install fast, lower peak than npm",
		StageReason: "Bun runtime lightweight",
	},
	"deno": {
		DevMinRAM: 0.5, StageMinRAM: 0.25,
		DevReason:   "deno cache moderate peak",
		StageReason: "Deno runtime lightweight",
	},
	"ruby": {
		DevMinRAM: 1.0, StageMinRAM: 0.5,
		DevReason:   "bundle install + asset compilation peak ~0.8 GB",
		StageReason: "Puma workers need baseline allocation",
	},
	"dotnet": {
		DevMinRAM: 1.0, StageMinRAM: 0.5,
		DevReason:   "dotnet build peak ~0.8 GB",
		StageReason: "Kestrel + CLR needs baseline allocation",
	},
}

// GetRuntimeResources returns RAM recommendations for a runtime slug.
// Returns zero-value struct if runtime has no specific recommendations.
func GetRuntimeResources(slug string) RuntimeResources {
	return runtimeResourceMap[slug]
}

// RuntimeResourceSlugs returns all runtime slugs that have resource recommendations, sorted.
func RuntimeResourceSlugs() []string {
	slugs := make([]string, 0, len(runtimeResourceMap))
	for slug := range runtimeResourceMap {
		slugs = append(slugs, slug)
	}
	// Sort is imported in sections.go (same package), no need to re-import.
	return slugs
}
