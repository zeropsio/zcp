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
	// SetAutoscaling returns *Process which MAY be nil (API: ResponseProcessNil).
	// When process == nil -> treat as sync (scaling applied immediately).
	// When process != nil -> treat as async (track via process ID).
	SetAutoscaling(ctx context.Context, serviceID string, params AutoscalingParams) (*Process, error)

	// Environment variables
	GetServiceEnv(ctx context.Context, serviceID string) ([]EnvVar, error)
	SetServiceEnvFile(ctx context.Context, serviceID string, content string) (*Process, error)
	DeleteUserData(ctx context.Context, userDataID string) (*Process, error)
	GetProjectEnv(ctx context.Context, projectID string) ([]EnvVar, error)
	CreateProjectEnv(ctx context.Context, projectID string, key, content string, sensitive bool) (*Process, error)
	DeleteProjectEnv(ctx context.Context, envID string) (*Process, error)

	// Import
	ImportServices(ctx context.Context, projectID string, yaml string) (*ImportResult, error)

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
}

// LogFetcher fetches logs from the log backend (step 2).
// Separate interface because it's an HTTP call to a different service.
type LogFetcher interface {
	FetchLogs(ctx context.Context, logAccess *LogAccess, params LogFetchParams) ([]LogEntry, error)
}
