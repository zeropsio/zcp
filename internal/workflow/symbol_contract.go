package workflow

import (
	"sort"
	"strings"
)

// Contract kind slugs — the short identifiers used in EnvVarsByKind and
// hostname-role entries. Distinct from internal/workflow managed_types.go
// "kindDatabase / kindCache / ..." constants which are long-form human-
// readable labels used for comment generation; the contract uses short
// slugs for byte-compact interpolation into every downstream brief.
const (
	contractKindDB      = "db"
	contractKindCache   = "cache"
	contractKindQueue   = "queue"
	contractKindStorage = "storage"
	contractKindSearch  = "search"
	contractKindMail    = "mail"
)

// Service type base-name constants that do not yet exist in managed_types.go.
// Kept local to the contract-derivation surface so the existing taxonomy
// file stays the single source of truth for the types it already names.
const (
	svcClickHouse      = "clickhouse"
	svcValkey          = "valkey"
	svcKeydb           = "keydb"
	svcNATS            = "nats"
	svcKafka           = "kafka"
	svcRabbitMQ        = "rabbitmq"
	svcObjectStorage   = "object-storage"
	svcSharedStorage   = "shared-storage"
	svcElasticsearch   = "elasticsearch"
	svcQdrant          = "qdrant"
	svcTypesense       = "typesense"
	svcMailpit         = "mailpit"
	contractRoleApp    = "app"
	contractRoleWorker = "worker"
)

// SymbolContract carries the cross-codebase naming contract derived from the
// recipe plan. It is computed once per recipe at research-complete and
// interpolated byte-identically into every scaffold / feature / writer
// dispatch so parallel sub-agents share env var names, NATS subjects and
// queues, HTTP route paths, DTO names, and hostname conventions.
//
// Closes v22 NATS URL-creds, v22 S3 endpoint, v30 worker SIGTERM, v31
// apidev enableShutdownHooks, v34 DB_PASS/DB_PASSWORD cross-scaffold
// coordination classes per principle P3.
type SymbolContract struct {
	// EnvVarsByKind is keyed by service kind ("db", "cache", "queue",
	// "storage", "search", "mail"); each value is the map of platform-
	// provided env var roles -> names as the container sees them. Derived
	// from the plan's managed-service targets.
	EnvVarsByKind map[string]map[string]string `json:"envVarsByKind,omitempty"`

	// HTTPRoutes is the declared-once cross-codebase route table. Derived
	// list is empty by default; populated by scaffold sub-agents against
	// the contract's HTTPRoutes keys.
	HTTPRoutes map[string]string `json:"httpRoutes,omitempty"`

	// NATSSubjects + NATSQueues are populated when plan targets include a
	// NATS-kind service. Producer publishes to subject, consumer subscribes
	// with queue; the competing-consumer invariant requires explicit queue
	// naming per v22 recurrence class.
	NATSSubjects map[string]string `json:"natsSubjects,omitempty"`
	NATSQueues   map[string]string `json:"natsQueues,omitempty"`

	// Hostnames lists each runtime target's {dev, stage} hostname pair.
	// Workers that share a codebase with a host target list only the
	// shared host's entries (no workerdev / workerstage).
	Hostnames []HostnameEntry `json:"hostnames,omitempty"`

	// DTOs names the cross-codebase DTO / interface symbols scaffold
	// sub-agents must produce identically. Seeded empty; the scaffold
	// brief prompts agents to fill based on HTTPRoutes payload shapes.
	DTOs []string `json:"dtos,omitempty"`

	// FixRecurrenceRules is the seeded list of scaffold-phase MUST-DO
	// items that close past recurrence classes. Each rule has a positive
	// form + a PreAttestCmd the scaffold sub-agent runs against its mount
	// via SSH before returning. P1: the check is author-runnable.
	FixRecurrenceRules []FixRule `json:"fixRecurrenceRules,omitempty"`
}

// HostnameEntry pairs a runtime role with its dev + stage hostnames.
type HostnameEntry struct {
	// Role is "api" | "app" | "worker" | "db" | service kind. Derived from
	// RecipeTarget.Role for runtimes, and from a lowercase service-kind
	// slug for managed services (e.g. "db" for postgresql).
	Role string `json:"role"`
	// Dev is the dev-env hostname (e.g. "apidev").
	Dev string `json:"dev"`
	// Stage is the stage-env hostname (e.g. "apistage"). Empty for
	// managed services that do not carry a per-env suffix.
	Stage string `json:"stage,omitempty"`
}

// FixRule is one positive-form invariant with an author-runnable pre-attest.
type FixRule struct {
	// ID is the stable identifier used by briefs and tests.
	ID string `json:"id"`
	// PositiveForm is the ≤1-sentence positive statement of the invariant.
	PositiveForm string `json:"positiveForm"`
	// PreAttestCmd is a shell command the scaffold sub-agent runs via SSH
	// before returning. Non-zero exit = fix before return. Interpolation
	// token `{host}` is replaced at brief-composition time with the
	// scaffold sub-agent's mount hostname.
	PreAttestCmd string `json:"preAttestCmd"`
	// AppliesTo lists hostname roles this rule applies to. "any" matches
	// every role.
	AppliesTo []string `json:"appliesTo"`
}

// BuildSymbolContract derives the contract from a RecipePlan. The derivation
// is idempotent: same input → same output. Designed to be called at the
// research-complete boundary; the result is stored on plan.SymbolContract
// (top-level per Q1 resolution) and interpolated into every downstream
// dispatch brief.
//
// Empty or nil plan yields an empty contract (non-nil). Unknown targets are
// skipped silently — the contract reflects only the services the plan
// actually declared.
func BuildSymbolContract(plan *RecipePlan) SymbolContract {
	contract := SymbolContract{
		EnvVarsByKind:      map[string]map[string]string{},
		HTTPRoutes:         map[string]string{},
		NATSSubjects:       map[string]string{},
		NATSQueues:         map[string]string{},
		Hostnames:          []HostnameEntry{},
		DTOs:               []string{},
		FixRecurrenceRules: SeededFixRecurrenceRules(),
	}
	if plan == nil {
		return contract
	}

	for _, t := range plan.Targets {
		switch {
		case IsRuntimeType(t.Type):
			contract.Hostnames = appendRuntimeHostname(contract.Hostnames, t)
		case IsManagedService(t.Type) || IsUtilityType(t.Type):
			if kind := contractKindForType(t.Type); kind != "" {
				if _, exists := contract.EnvVarsByKind[kind]; !exists {
					contract.EnvVarsByKind[kind] = envVarsForKind(kind, t.Hostname)
				}
				contract.Hostnames = append(contract.Hostnames, HostnameEntry{
					Role: kind,
					Dev:  t.Hostname,
				})
			}
			if kind := contractKindForType(t.Type); kind == contractKindQueue {
				// NATS producers publish to subjects; consumers subscribe with
				// queue group (competing-consumer). Scaffold sub-agents fill
				// the subject / queue names keyed by feature; we seed one
				// default pair for every plan with a NATS service so the
				// brief has a starting point that survives the v22 class.
				contract.NATSSubjects["job_dispatch"] = "jobs.dispatch"
				contract.NATSQueues["workers"] = "workers"
			}
		}
	}

	sort.Slice(contract.Hostnames, func(i, j int) bool {
		if contract.Hostnames[i].Role != contract.Hostnames[j].Role {
			return contract.Hostnames[i].Role < contract.Hostnames[j].Role
		}
		return contract.Hostnames[i].Dev < contract.Hostnames[j].Dev
	})

	return contract
}

// SeededFixRecurrenceRules returns the 12 v20–v34 recurrence classes as a
// positive-form rule list. Exported so tests and the brief-composition
// helpers can assert / iterate the seed.
func SeededFixRecurrenceRules() []FixRule {
	return []FixRule{
		{
			ID:           "nats-separate-creds",
			PositiveForm: "pass user and pass as separate ConnectionOptions fields; servers is ${queue_hostname}:${queue_port} only",
			PreAttestCmd: `ssh {host} "grep -rnE 'nats://[^ \t]*:[^ \t]*@' /var/www 2>/dev/null; test $? -eq 1"`,
			AppliesTo:    []string{"api", "worker"},
		},
		{
			ID:           "s3-uses-api-url",
			PositiveForm: "S3 client endpoint is process.env.storage_apiUrl (https://), not storage_apiHost (http redirect)",
			PreAttestCmd: `ssh {host} "grep -rn 'storage_apiHost' /var/www/src 2>/dev/null; test $? -eq 1"`,
			AppliesTo:    []string{"api"},
		},
		{
			ID:           "s3-force-path-style",
			PositiveForm: "S3 client forcePathStyle: true",
			PreAttestCmd: `ssh {host} "grep -rn 'forcePathStyle' /var/www/src 2>/dev/null | grep -q true"`,
			AppliesTo:    []string{"api"},
		},
		{
			ID:           "routable-bind",
			PositiveForm: "HTTP servers bind 0.0.0.0, not localhost or 127.0.0.1",
			PreAttestCmd: `ssh {host} "grep -rnE 'listen\\(.*(localhost|127\\.0\\.0\\.1)' /var/www/src 2>/dev/null; test $? -eq 1"`,
			AppliesTo:    []string{"api", "app"},
		},
		{
			ID:           "trust-proxy",
			PositiveForm: "Express/Fastify set trust proxy 1 (or framework equivalent) for L7 balancer IP forwarding",
			PreAttestCmd: `ssh {host} "grep -rnE 'trust[ _]proxy' /var/www/src 2>/dev/null | grep -q ."`,
			AppliesTo:    []string{"api"},
		},
		{
			ID:           "graceful-shutdown",
			PositiveForm: "worker + api register SIGTERM drain-then-exit; Nest apps call app.enableShutdownHooks()",
			PreAttestCmd: `ssh {host} "grep -rnE 'SIGTERM|enableShutdownHooks' /var/www/src 2>/dev/null | grep -q ."`,
			AppliesTo:    []string{"api", "worker"},
		},
		{
			ID:           "queue-group",
			PositiveForm: "NATS subscribers declare queue: '<contract.NATSQueues[role]>' (competing-consumer)",
			PreAttestCmd: `ssh {host} "grep -rnE 'subscribe.*queue' /var/www/src 2>/dev/null | grep -q ."`,
			AppliesTo:    []string{"worker"},
		},
		{
			ID:           "env-self-shadow",
			PositiveForm: "zerops.yaml run.envVariables contains no KEY: ${KEY} self-shadow lines",
			PreAttestCmd: `ssh {host} "grep -nE '^[[:space:]]+([A-Z_]+):[[:space:]]+\\$\\{\\1\\}[[:space:]]*$' /var/www/zerops.yaml 2>/dev/null; test $? -eq 1"`,
			AppliesTo:    []string{"any"},
		},
		{
			ID:           "gitignore-baseline",
			PositiveForm: ".gitignore contains node_modules, dist, .env, .DS_Store, plus framework-specific cache dirs",
			PreAttestCmd: `ssh {host} "grep -q node_modules /var/www/.gitignore && grep -q dist /var/www/.gitignore && grep -q '\\.env' /var/www/.gitignore"`,
			AppliesTo:    []string{"any"},
		},
		{
			ID:           "env-example-preserved",
			PositiveForm: "framework-scaffolder's .env.example is kept if the scaffolder produced one",
			PreAttestCmd: `ssh {host} "test ! -f /var/www/.env.example || test -s /var/www/.env.example"`,
			AppliesTo:    []string{"any"},
		},
		{
			ID:           "no-scaffold-test-artifacts",
			PositiveForm: "no preship.sh / .assert.sh / self-test shell scripts committed to the codebase",
			PreAttestCmd: `ssh {host} "find /var/www -maxdepth 2 -type f \\( -name 'preship.sh' -o -name '*.assert.sh' \\) | head -n1 | grep -q . ; test $? -eq 1"`,
			AppliesTo:    []string{"any"},
		},
		{
			ID:           "skip-git",
			PositiveForm: "framework scaffolders invoked with --skip-git, OR .git removed after scaffolder returns (ssh {host} rm -rf /var/www/.git)",
			PreAttestCmd: `ssh {host} "test ! -d /var/www/.git || ls /var/www/.git/HEAD 2>/dev/null | grep -q . ; test $? -eq 0 -o $? -eq 1"`,
			AppliesTo:    []string{"any"},
		},
	}
}

// contractKindForType maps a Zerops service type to the contract kind slug
// used for EnvVarsByKind and hostname role. Empty string for types that
// don't carry env-var contracts (runtime types handled separately).
func contractKindForType(serviceType string) string {
	base, _, _ := strings.Cut(strings.ToLower(serviceType), "@")
	switch base {
	case svcPostgreSQL, svcMariaDB, svcClickHouse:
		return contractKindDB
	case svcValkey, svcKeydb:
		return contractKindCache
	case svcNATS, svcKafka, svcRabbitMQ:
		return contractKindQueue
	case svcObjectStorage, svcSharedStorage:
		return contractKindStorage
	case svcElasticsearch, svcMeilisearch, svcQdrant, svcTypesense:
		return contractKindSearch
	case svcMailpit:
		return contractKindMail
	}
	return ""
}

// envVarsForKind returns the platform-provided env var names for a service
// kind. The hostname is the service's hostname from the plan (e.g. "db"),
// which becomes the prefix for runtime-facing env vars the container sees.
func envVarsForKind(kind, hostname string) map[string]string {
	h := strings.ToUpper(hostname)
	switch kind {
	case contractKindDB:
		return map[string]string{
			"host": h + "_HOST",
			"port": h + "_PORT",
			"user": h + "_USER",
			"pass": h + "_PASSWORD",
			"name": h + "_DBNAME",
		}
	case contractKindCache:
		return map[string]string{
			"host": h + "_HOST",
			"port": h + "_PORT",
		}
	case contractKindQueue:
		return map[string]string{
			"host": h + "_HOST",
			"port": h + "_PORT",
			"user": h + "_USER",
			"pass": h + "_PASSWORD",
		}
	case contractKindStorage:
		return map[string]string{
			"apiUrl":    h + "_apiUrl",
			"accessKey": h + "_accessKeyId",
			"secretKey": h + "_secretAccessKey",
			"bucket":    h + "_bucketName",
		}
	case contractKindSearch:
		return map[string]string{
			"host":   h + "_HOST",
			"port":   h + "_PORT",
			"apiKey": h + "_apiKey",
		}
	case contractKindMail:
		return map[string]string{
			"host": h + "_HOST",
			"port": h + "_PORT",
		}
	}
	return map[string]string{}
}

// appendRuntimeHostname adds a runtime target's dev/stage pair to the list.
// Workers that share a codebase with another runtime target do not get their
// own dev/stage entries (no workerdev / workerstage); their host target
// already carries the pair.
func appendRuntimeHostname(list []HostnameEntry, t RecipeTarget) []HostnameEntry {
	if t.IsWorker && t.SharesCodebaseWith != "" {
		return list
	}
	role := t.Role
	if role == "" {
		if t.IsWorker {
			role = contractRoleWorker
		} else {
			role = contractRoleApp
		}
	}
	dev, stage := deriveHostnamePair(t.Hostname)
	return append(list, HostnameEntry{Role: role, Dev: dev, Stage: stage})
}

// deriveHostnamePair derives the {dev, stage} hostname pair from a runtime
// target's configured hostname. Convention: the target's hostname is either
// already suffixed "dev" (then stage is hostname-with-"stage" swapped in)
// or a bare name (then dev = name+"dev", stage = name+"stage").
func deriveHostnamePair(hostname string) (dev, stage string) {
	h := strings.ToLower(hostname)
	if root, ok := strings.CutSuffix(h, "dev"); ok {
		return h, root + "stage"
	}
	return h + "dev", h + "stage"
}
