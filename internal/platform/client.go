package platform

import "context"

// Client is the interface for Zerops API operations.
// Mocked in tests, real implementation wraps zerops-go SDK.
type Client interface {
	// Auth
	GetUserInfo(ctx context.Context) (*UserInfo, error)

	// Project discovery
	ListProjects(ctx context.Context, clientID string) ([]Project, error)
	GetProject(ctx context.Context, projectID string) (*Project, error)

	// Service discovery
	ListServices(ctx context.Context, projectID string) ([]ServiceStack, error)
	// ListServicesDirect reads the project's service stacks via the DIRECT
	// (non-Elasticsearch) GET /project/{id}/service-stack — authoritative +
	// lag-free right after an import, where ListServices (ES search) trails by
	// seconds. Used by ops.Discover so a just-imported service is visible
	// immediately.
	ListServicesDirect(ctx context.Context, projectID string) ([]ServiceStack, error)
	GetService(ctx context.Context, serviceID string) (*ServiceStack, error)
	// ActiveServiceTypeVersions reads the platform's current provisioning
	// availability. It is a runtime overlay on the public import schema, not a
	// schema-drift or artifact-sync dependency.
	ActiveServiceTypeVersions(ctx context.Context) ([]string, error)

	// Service management (async -- return process)
	StartService(ctx context.Context, serviceID string) (*Process, error)
	StopService(ctx context.Context, serviceID string) (*Process, error)
	RestartService(ctx context.Context, serviceID string) (*Process, error)
	ReloadService(ctx context.Context, serviceID string) (*Process, error)
	// Shared storage connection (async -- return process)
	ConnectSharedStorage(ctx context.Context, serviceID, storageID string) (*Process, error)
	DisconnectSharedStorage(ctx context.Context, serviceID, storageID string) (*Process, error)

	// SetAutoscaling returns *Process which MAY be nil (API: ResponseProcessNil).
	// When process == nil -> treat as sync (scaling applied immediately).
	// When process != nil -> treat as async (track via process ID).
	SetAutoscaling(ctx context.Context, serviceID string, params AutoscalingParams) (*Process, error)

	// Environment variables. Project + service envs are server-side
	// distinct entities sharing one type enum since the platform's 2026-08
	// model (USER|SYSTEM — spec-zerops-env-lifecycle.md §1); the wrappers
	// keep the scope split at compile time regardless.
	GetServiceEnv(ctx context.Context, serviceID string) ([]ServiceEnvVar, error)
	// CreateServiceEnvVar creates ONE service userData record (single key).
	// Per-var by design: the bulk env-file PUT replaces the whole file and
	// silently drops every other user-set var, so EnvSet upserts one key at a
	// time (delete-then-create on collision, mirroring the project path).
	// sensitive is REQUIRED by the platform on every write (masked for
	// read-only roles; spec §7) — hand-rolled on the
	// SDK's authorized transport because the pinned SDK's generated body
	// lacks the field (see TestSDKUserDataBody_StillLacksSensitive).
	// Errors with userDataDuplicateKey when the key is owned by yaml
	// run.envVariables (incl. the read-only mirror on this same slim env).
	CreateServiceEnvVar(ctx context.Context, serviceID, key, content string, sensitive bool) (*Process, error)
	DeleteUserData(ctx context.Context, userDataID string) (*Process, error)
	GetProjectEnv(ctx context.Context, projectID string) ([]ProjectEnvVar, error)
	CreateProjectEnv(ctx context.Context, projectID string, key, content string, sensitive bool) (*Process, error)
	DeleteProjectEnv(ctx context.Context, envID string) (*Process, error)

	// Export (returns re-importable YAML)
	GetProjectExport(ctx context.Context, projectID string) (string, error)
	GetServiceStackExport(ctx context.Context, serviceID string) (string, error)

	// Import
	ImportServices(ctx context.Context, projectID string, yaml string) (*ImportResult, error)

	// Pre-deploy zerops.yaml validation. Nil error = valid. Non-nil error
	// = deploy should abort — structured validation failures land as
	// *PlatformError{Code: ErrInvalidZeropsYml, APIMeta: <fields>};
	// transport/auth failures land as the corresponding network/auth code.
	// No "proceed on failure" fallback — if the validator can't confirm
	// valid, downstream deploy steps would fail against the same API
	// anyway, so fail fast with the clearer error.
	ValidateZeropsYaml(ctx context.Context, in ValidateZeropsYamlInput) error

	// Delete
	DeleteService(ctx context.Context, serviceID string) (*Process, error)

	// Process
	GetProcess(ctx context.Context, processID string) (*Process, error)
	CancelProcess(ctx context.Context, processID string) (*Process, error)

	// Subdomain
	EnableSubdomainAccess(ctx context.Context, serviceID string) (*Process, error)
	DisableSubdomainAccess(ctx context.Context, serviceID string) (*Process, error)

	// Logs (2-step: get access URL, then fetch from log backend)
	GetProjectLog(ctx context.Context, projectID string) (*LogAccess, error)

	// Activity
	SearchProcesses(ctx context.Context, projectID string, limit int) ([]ProcessEvent, error)
	SearchAppVersions(ctx context.Context, projectID string, limit int) ([]AppVersionEvent, error)
	// GetProjectProcessesDirect reads ALL of a project's processes via the
	// DIRECT (non-Elasticsearch) GET /project/{id}/process — lag-free, each
	// process carrying its embedded appVersion phase. Used by ops.ProjectActivity
	// for live in-flight detection (the caller filters to live + sorts).
	GetProjectProcessesDirect(ctx context.Context, projectID string) ([]Process, error)

	// External-repository integration status — public read. Same response
	// shape ProjectAdminClient.GetServiceStackIntegrationStatus exposes
	// for the launch-window. Used by setup-name cascade (P1) step 2 to
	// read GithubIntegration.ZeropsYamlSetup without requiring an admin
	// session. Plan: plans/setup-name-local-canonical-2026-05-27.md.
	GetServiceStackIntegrationStatus(ctx context.Context, serviceID string) (IntegrationStatus, error)

	// AppVersion source archive — signed download URL for the source
	// bundle uploaded at deploy time. Used by setup-name cascade (P1)
	// step 4 for orphan services with at least one prior deploy.
	GetAppVersionAppCode(ctx context.Context, appVersionID string) (string, error)

	// GetAppVersionUserData returns the app version's yaml-baked
	// run.envVariables (as templates), Sensitive always false (the DTO
	// carries no Sensitive field). The raw userDataList is a superset
	// (run.envVariables Type USER + SYSTEM intrinsics + the ZEROPS_YAML
	// blob); the mapper classifies it and returns ONLY genuine
	// run.envVariables (SYSTEM intrinsics + ZEROPS_YAML are filtered out at
	// the boundary — classifyAppVersionUserData). This is the GUI
	// "Environment variables from master" source; since 2026-08 these
	// yaml-baked vars are ALSO mirrored read-only on the slim
	// GetServiceEnv, but Type alone can't tell that mirror apart from a
	// user-set var there (both USER) — this endpoint stays the
	// unambiguous source. Read for env-ref validation, shadow detection,
	// and discover env review on LIVE runtime services (a service
	// must have an active app version — managed deps and never-deployed services
	// have none). Spec: docs/spec-zerops-env-lifecycle.md §1/§6.
	GetAppVersionUserData(ctx context.Context, appVersionID string) ([]ServiceEnvVar, error)

	// ListOwnTokenDelegations returns the delegations attached to the token
	// this client authenticates with. Fresh read every call — the platform
	// is the sole source of delegation truth (D-1); ZCP never persists or
	// infers availability locally. See zerops_delegation.go / P-LP-15.
	ListOwnTokenDelegations(ctx context.Context) ([]TokenDelegation, error)

	// MintDelegatedLaunchToken consumes the one-time delegation to mint a
	// NO_ACCESS + canCreateProjects integration token named name. The
	// returned Token value is shown by the platform exactly once — the
	// caller owns the P-LP-14 staging discipline (never persisted here).
	MintDelegatedLaunchToken(ctx context.Context, name string) (MintedToken, error)
}

// LogFetcher fetches logs from the log backend (step 2).
// Separate interface because it's an HTTP call to a different service.
type LogFetcher interface {
	FetchLogs(ctx context.Context, logAccess *LogAccess, params LogFetchParams) ([]LogEntry, error)
}
