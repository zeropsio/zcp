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
	GetService(ctx context.Context, serviceID string) (*ServiceStack, error)

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
	// distinct concepts (different SDK enums: EnvTypeEnum vs
	// UserDataTypeEnum); the wrappers mirror that split so envclass
	// (Layer 3, Phase 2) sees the structured taxonomy at compile time.
	GetServiceEnv(ctx context.Context, serviceID string) ([]ServiceEnvVar, error)
	// CreateServiceEnvVar creates ONE service userData record (single key).
	// Per-var by design: the bulk env-file PUT replaces the whole file and
	// silently drops every other user-set var, so EnvSet upserts one key at a
	// time (delete-then-create on collision, mirroring the project path).
	// Errors with userDataDuplicateKey when the key is owned by yaml run.envVariables.
	CreateServiceEnvVar(ctx context.Context, serviceID, key, content string) (*Process, error)
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

	// Service stack types (public, no auth required for search)
	ListServiceStackTypes(ctx context.Context) ([]ServiceStackType, error)

	// External-repository integration status — public read. Same response
	// shape ProjectAdminClient.GetServiceStackIntegrationStatus exposes
	// for the launch-window. Used by setup-name cascade (P1) step 2 to
	// read GithubIntegration.ZeropsYamlSetup without requiring an admin
	// session. Plan: plans/setup-name-local-canonical-2026-05-27.md.
	GetServiceStackIntegrationStatus(ctx context.Context, serviceID string) (IntegrationStatus, error)

	// AppVersion source archive — signed download URL for the source
	// bundle uploaded at deploy time. Only platform-side way to recover
	// the deployed zerops.yaml content (appVersion DTOs do not carry
	// yaml). Used by setup-name cascade (P1) step 4 for orphan services
	// with at least one prior deploy.
	GetAppVersionAppCode(ctx context.Context, appVersionID string) (string, error)
}

// LogFetcher fetches logs from the log backend (step 2).
// Separate interface because it's an HTTP call to a different service.
type LogFetcher interface {
	FetchLogs(ctx context.Context, logAccess *LogAccess, params LogFetchParams) ([]LogEntry, error)
}
