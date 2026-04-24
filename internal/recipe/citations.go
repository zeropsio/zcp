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
	// Cross-service env vars, self-shadow, aliasing.
	"cross-service-env": "env-var-model",
	"self-shadow":       "env-var-model",
	"env-var-model":     "env-var-model",
	"envIsolation":      "env-var-model",
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
}

// GuideForTopic returns the zerops_knowledge guide id for a topic, or
// an empty string when the topic is not in the citation map.
func GuideForTopic(topic string) string {
	return CitationMap[topic]
}
