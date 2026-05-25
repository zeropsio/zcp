package recipe

// CitationMap binds fact topics to zerops_knowledge guide ids, per
// docs/spec-content-surfaces.md §"Citation map". Content touching any
// topic in this map MUST cite the guide — writing new mental models
// for topics the platform already documents is how folk-doctrine ships
// (run 7's workerdev gotcha #1 class).
//
// Workstream C uses this map to auto-attach guide-ids on facts. Workstream
// D validates that every KB fragment whose topic has a mapping carries
// the guide reference in its body.
var CitationMap = map[string]string{
	// Cross-service env vars, project-vs-service scope, aliasing.
	"cross-service-env": "env-var-model",
	"self-shadow":       "env-var-model",
	"env-var-model":     "env-var-model",
	// execOnce + init commands.
	"init-commands": "init-commands",
	"execOnce":      "init-commands",
	"appVersionId":  "init-commands",
	"migrations":    "init-commands",
	// Rolling deploys / SIGTERM / minContainers.
	"rolling-deploys": "rolling-deploys",
	"minContainers":   "rolling-deploys",
	"SIGTERM":         "rolling-deploys",
	// Object storage (MinIO).
	"object-storage": "object-storage",
	"forcePathStyle": "object-storage",
	"MinIO":          "object-storage",
	// L7 balancer / httpSupport / trust proxy / 0.0.0.0.
	"http-support": "http-support",
	"httpSupport":  "http-support",
	"trust-proxy":  "http-support",
	"l7-balancer":  "http-support",
	// Deploy files / tilde / static base.
	"deploy-files": "deploy-files",
	"tilde-suffix": "deploy-files",
	// Readiness / health checks.
	"readiness-check": "readiness-health-checks",
	"health-check":    "readiness-health-checks",

	// Run-16 §7.2 — per-managed-service idiom topics. These guide ids
	// resolve via zerops_knowledge runtime=<type>; the agent calls the
	// knowledge tool when filling per-service fact shells (§7.2 fact-
	// shell pattern). Engine never duplicates atom prose into code.
	"managed-services-postgresql":     "managed-services-postgresql",
	"managed-services-mariadb":        "managed-services-mariadb",
	"managed-services-keydb":          "managed-services-keydb",
	"managed-services-valkey":         "managed-services-valkey",
	"managed-services-redis":          "managed-services-redis",
	"managed-services-nats":           "managed-services-nats",
	"managed-services-rabbitmq":       "managed-services-rabbitmq",
	"managed-services-kafka":          "managed-services-kafka",
	"managed-services-meilisearch":    "managed-services-meilisearch",
	"managed-services-elasticsearch":  "managed-services-elasticsearch",
	"managed-services-typesense":      "managed-services-typesense",
	"managed-services-qdrant":         "managed-services-qdrant",
	"managed-services-clickhouse":     "managed-services-clickhouse",
	"managed-services-object-storage": "object-storage",
	"managed-services-shared-storage": "managed-services-shared-storage",
}

// GuideForTopic returns the zerops_knowledge guide id for a topic, or
// an empty string when the topic is not in the citation map.
func GuideForTopic(topic string) string {
	return CitationMap[topic]
}

// CitationGuideURL — Run-44 G1. Maps a guide id (CitationMap value) to
// its canonical `docs.zerops.io` URL. The `kb-citation-missing`
// validator accepts EITHER the slug-stem guide id OR the canonical URL
// as proof-of-citation. Refinement-2's brief (citation-map block in
// briefs_refinement2.go) renders form-(b) / form-(c) citations using
// the same URLs; surfacing the URL acceptance here closes the run-43
// substrate-internal contradiction where the audit's suggested fix
// (canonical URL) tripped the validator's slug-stem-only check.
//
// Keys are guide ids (values from CitationMap). Values are the canonical
// URL WITHOUT a `https://` prefix — the validator's anchored-match
// pattern handles scheme-prefix matching at scan time.
//
// Drift between this map and the citationURL* constants in
// briefs_refinement2.go is pinned by
// TestCitationGuideURL_MatchesBriefConstants.
var CitationGuideURL = map[string]string{
	"rolling-deploys":         citationURLRollingDeploys,
	"init-commands":           citationURLInitCommands,
	"object-storage":          citationURLObjectStorage,
	"env-var-model":           citationURLEnvVarModel,
	"http-support":            citationURLHTTPSupport,
	"deploy-files":            citationURLDeployFiles,
	"readiness-health-checks": citationURLReadinessChecks,
	// Managed-service rows in CitationMap point at object-storage's
	// existing guide URL for `managed-services-object-storage`. The
	// distinct managed-service guide ids that don't share an existing
	// URL get their citation URLs from briefs_refinement2.go too.
	"managed-services-postgresql":     "docs.zerops.io/services/postgresql",
	"managed-services-mariadb":        "docs.zerops.io/services/mariadb",
	"managed-services-keydb":          "docs.zerops.io/services/keydb",
	"managed-services-valkey":         "docs.zerops.io/services/valkey",
	"managed-services-redis":          "docs.zerops.io/services/redis",
	"managed-services-nats":           citationURLManagedNATS,
	"managed-services-rabbitmq":       "docs.zerops.io/services/rabbitmq",
	"managed-services-kafka":          "docs.zerops.io/services/kafka",
	"managed-services-meilisearch":    citationURLManagedMeilisearch,
	"managed-services-elasticsearch":  "docs.zerops.io/services/elasticsearch",
	"managed-services-typesense":      "docs.zerops.io/services/typesense",
	"managed-services-qdrant":         "docs.zerops.io/services/qdrant",
	"managed-services-clickhouse":     "docs.zerops.io/services/clickhouse",
	"managed-services-shared-storage": "docs.zerops.io/services/shared-storage",
}
