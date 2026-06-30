package platform

import (
	"time"

	"github.com/zeropsio/zerops-go/types/enum"
)

// DefaultAPITimeout is the global timeout for each API call.
const DefaultAPITimeout = 30 * time.Second

// UserInfo contains user details from auth/info endpoint.
//
// Field-naming note: ID is historically populated with the
// ClientUserList[0].ClientId (the org/client ID), NOT the user's own ID.
// Existing code paths (ListProjects' clientID filter) depend on this
// convention. ClientUserID is the separate clientUserList[0].id value —
// the link between user and client, used by project.userRoles[].id when
// the launch-production workflow grants ADMIN to the launching user on
// a freshly-created prod project (P-LP-5 substrate, A.10 spike finding).
type UserInfo struct {
	ID           string `json:"id"`
	ClientUserID string `json:"clientUserId,omitempty"`
	FullName     string `json:"fullName"`
	Email        string `json:"email"`
}

// Project represents a Zerops project.
type Project struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Status        string `json:"status"`
	SubdomainHost string `json:"subdomainHost,omitempty"` // e.g. "1df2.prg1.zerops.app"
	// Mode is the project core tier the platform reports: LIGHT | SERIOUS
	// (| LEGACY). The launch read-back verifies the emitted
	// project.corePackage was honored (the platform has silently dropped
	// import-yaml fields before — userRoles, spike A.10).
	Mode string `json:"mode,omitempty"`
	// LocationID is the region code of the primary instance location
	// (e.g. "eu-central") — read-back counterpart of project.location.
	LocationID string `json:"locationId,omitempty"`
}

// ServiceStack represents a Zerops service.
type ServiceStack struct {
	ID                   string          `json:"id"`
	Name                 string          `json:"name"` // hostname
	ProjectID            string          `json:"projectId"`
	ServiceStackTypeInfo ServiceTypeInfo `json:"serviceStackTypeInfo"`
	Status               string          `json:"status"`
	Mode                 string          `json:"mode"` // HA, NON_HA (deprecated; the deployment variant in the type is authoritative)
	// Profile is the live scaling tier (autoscalingProfileId) for
	// profile-bearing managed services (PostgreSQL/Valkey) — e.g.
	// "oltp-hobby", "oltp-staging", "staging". Read only from the FULL
	// GetService DTO; the lighter list (EsServiceStack) does not carry it,
	// so a service surfaced only via ListServices has an empty Profile.
	Profile            string                  `json:"autoscalingProfileId,omitempty"`
	SubdomainAccess    bool                    `json:"subdomainAccess,omitempty"`
	Ports              []Port                  `json:"ports,omitempty"`
	CustomAutoscaling  *CustomAutoscaling      `json:"customAutoscaling,omitempty"`
	CurrentAutoscaling *CustomAutoscaling      `json:"currentAutoscaling,omitempty"`
	Created            string                  `json:"created"`
	LastUpdate         string                  `json:"lastUpdate,omitempty"`
	ActiveAppVersion   *ActiveAppVersionDigest `json:"activeAppVersion,omitempty"`
}

// ActiveAppVersionDigest projects the platform's ActiveAppVersion onto a
// minimal shape ZCP cares about: the version ID (used by setup-name
// cascade step 4 to fetch the source archive via GetAppVersionAppCode)
// and the GithubIntegration.ZeropsYamlSetup field (cascade step 3 — the
// per-latest-deploy setup-block name when the deploy came via the GH
// integration). Empty fields mean the service has no active app version
// OR the version wasn't deployed via integration.
//
// Plan: plans/setup-name-local-canonical-2026-05-27.md §SDK surface.
type ActiveAppVersionDigest struct {
	ID                         string `json:"id,omitempty"`
	GithubIntegrationSetup     string `json:"githubIntegrationSetup,omitempty"`
	PublicGitSourceExplicitSet *bool  `json:"publicGitSourceExplicitSetup,omitempty"`
}

// ServiceTypeInfo contains service type details.
type ServiceTypeInfo struct {
	ServiceStackTypeID           string `json:"serviceStackTypeId,omitempty"` // opaque ID required by ValidateZeropsYaml
	ServiceStackTypeVersionName  string `json:"serviceStackTypeVersionName"`  // e.g. "nodejs@22"
	ServiceStackTypeCategoryName string `json:"serviceStackTypeCategoryName"` // e.g. "USER", "CORE", "BUILD"
}

// systemCategories are internal service categories hidden from user-facing outputs.
var systemCategories = map[string]bool{
	"CORE":             true,
	"BUILD":            true,
	"INTERNAL":         true,
	"PREPARE_RUNTIME":  true,
	"HTTP_L7_BALANCER": true,
}

// IsSystem returns true if the service belongs to a system/internal category.
func (s *ServiceStack) IsSystem() bool {
	return systemCategories[s.ServiceStackTypeInfo.ServiceStackTypeCategoryName]
}

// IsLive reports whether the service is a live, running process — either
// RUNNING (a started runtime/managed service) or ACTIVE (a deployed runtime
// serving an appVersion). Single owner for the "is this service live right
// now" question; a RUNNING service keeps its boot env until restarted, so
// env changes still need an auto-restart against it.
func (s *ServiceStack) IsLive() bool {
	return s.Status == ServiceStatusRunning || s.Status == ServiceStatusActive
}

// Port represents a service port.
type Port struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	Public   bool   `json:"public"`
	// HTTPSupport mirrors the SDK ServicePort.HttpRouting flag — POST-ENABLE L7
	// routing state, NOT zerops.yaml intent. It flips true only after a
	// successful subdomain enable propagates, so it is empty on a freshly
	// deployed (not-yet-enabled) service. Use Scheme to identify an HTTP port
	// reliably; HTTPSupport is a secondary hint only.
	HTTPSupport bool `json:"httpSupport"`
	// Scheme is the port's protocol scheme (http, https, tcp, postgresql, …),
	// mapped from the SDK ServicePort.Scheme enum. Set at deploy time from the
	// port declaration, so it identifies the HTTP-serving port independent of
	// subdomain-enable timing — the reliable signal for HTTP-port selection.
	Scheme string `json:"scheme,omitempty"`
}

// CustomAutoscaling contains scaling configuration.
type CustomAutoscaling struct {
	HorizontalMinCount int32   `json:"horizontalMinCount"`
	HorizontalMaxCount int32   `json:"horizontalMaxCount"`
	CPUMode            string  `json:"cpuMode"` // SHARED, DEDICATED
	StartCPUCoreCount  int32   `json:"startCpuCoreCount"`
	MinCPU             int32   `json:"minCpu"`
	MaxCPU             int32   `json:"maxCpu"`
	MinRAM             float64 `json:"minRam"`
	MaxRAM             float64 `json:"maxRam"`
	MinDisk            float64 `json:"minDisk"`
	MaxDisk            float64 `json:"maxDisk"`
	MinFreeCPUCores    float64 `json:"minFreeCpuCores"`
	MinFreeCPUPercent  float64 `json:"minFreeCpuPercent"`
	MinFreeRAMGB       float64 `json:"minFreeRamGB"` //nolint:tagliatelle // matches Zerops import.yaml naming
	MinFreeRAMPercent  float64 `json:"minFreeRamPercent"`
	SwapEnabled        bool    `json:"swapEnabled"`
}

// AutoscalingParams maps MCP tool params to API request.
type AutoscalingParams struct {
	ServiceMode             string // Current HA/NON_HA mode — must be set to avoid API "mode update forbidden"
	HorizontalMinCount      *int32
	HorizontalMaxCount      *int32
	VerticalCPUMode         *string
	VerticalStartCPU        *int32
	VerticalMinCPU          *int32
	VerticalMaxCPU          *int32
	VerticalMinRAM          *float64
	VerticalMaxRAM          *float64
	VerticalMinDisk         *float64
	VerticalMaxDisk         *float64
	VerticalSwapEnabled     *bool
	VerticalMinFreeRAMGB    *float64
	VerticalMinFreeRAMPct   *float64
	VerticalMinFreeCPUCores *float64
	VerticalMinFreeCPUPct   *float64
}

// Process represents an async operation tracked by Zerops.
type Process struct {
	ID            string            `json:"id"`
	ActionName    string            `json:"actionName"`
	Status        string            `json:"status"` // see ProcessStatus* constants
	ServiceStacks []ServiceStackRef `json:"serviceStacks,omitempty"`
	Created       string            `json:"created"`
	Started       *string           `json:"started,omitempty"`
	Finished      *string           `json:"finished,omitempty"`
	FailReason    *string           `json:"failReason,omitempty"`
}

// Process / build / service-stack status values returned by the Zerops API.
// These are wire-format strings — keep them in sync with the API rather
// than redefining them per-package.
const (
	ProcessStatusPending = "PENDING"
	ProcessStatusRunning = "RUNNING"
	// ProcessStatusRollbacking / ProcessStatusCanceling are the two in-flight
	// (non-terminal) process states beyond PENDING/RUNNING — a process that is
	// rolling back or being canceled is still running, not done. Mirror the SDK
	// ProcessStatusEnum (rollbacking/canceling). The terminal trio is
	// FINISHED/FAILED/CANCELED.
	ProcessStatusRollbacking = "ROLLBACKING"
	ProcessStatusCanceling   = "CANCELING"
	ProcessStatusFinished    = "FINISHED"
	ProcessStatusFailed      = "FAILED"
	ProcessStatusCanceled    = "CANCELED"

	BuildStatusBuilding             = "BUILDING"
	BuildStatusDeploying            = "DEPLOYING"
	BuildStatusBuildFailed          = "BUILD_FAILED"
	BuildStatusDeployFailed         = "DEPLOY_FAILED"
	BuildStatusPreparingRuntimeFail = "PREPARING_RUNTIME_FAILED"
	BuildStatusDeployed             = "DEPLOYED"

	ServiceStatusNew           = "NEW"
	ServiceStatusActive        = "ACTIVE"
	ServiceStatusReadyToDeploy = "READY_TO_DEPLOY"
	ServiceStatusRunning       = "RUNNING"
	ServiceStatusFailed        = "FAILED"
	ServiceStatusStopped       = "STOPPED"
)

// KnownStatusStrings is the single owner of "is this a real platform status
// string". It unions ZCP's curated service/build/process constants — which
// reflect the LIVE platform and include states the SDK enums omit
// (READY_TO_DEPLOY, RUNNING, DEPLOYED) — with the SDK status enums, which add
// transitional states the curated set doesn't enumerate (CREATING, STARTING,
// …). Atom-content lint validates status tokens against this so a phantom like
// NOT_YET_DEPLOYED (which exists in neither) cannot ship in agent-facing prose.
func KnownStatusStrings() map[string]bool {
	set := make(map[string]bool)
	for _, s := range []string{
		ProcessStatusPending, ProcessStatusRunning, ProcessStatusFinished, ProcessStatusFailed, ProcessStatusCanceled,
		BuildStatusBuilding, BuildStatusBuildFailed, BuildStatusDeployFailed, BuildStatusPreparingRuntimeFail, BuildStatusDeployed,
		ServiceStatusNew, ServiceStatusActive, ServiceStatusReadyToDeploy, ServiceStatusRunning, ServiceStatusFailed, ServiceStatusStopped,
	} {
		set[s] = true
	}
	for _, group := range [][]string{
		enum.ServiceStatusEnumAllStrings(),
		enum.AppVersionStatusEnumAllStrings(),
		enum.ProcessStatusEnumAllStrings(),
		enum.ContainerStatusEnumAllStrings(),
	} {
		for _, s := range group {
			set[s] = true
		}
	}
	return set
}

// ServiceStackRef is a lightweight service reference in a process.
type ServiceStackRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ProjectEnvType is the server-authoritative enum on project-level
// env entries (SDK EnvTypeEnum). USER values come from user authorship
// (ZCP_API_KEY, JWT_SECRET, ...); SYSTEM values are platform-injected
// (zeropsSubdomainHost, staticCdnUrl, envIsolation, ...).
//
// Closed enum — Phase 2 envclass treats SYSTEM as universal drop and
// classifies USER per LLM. New values would surface as drift via
// TestProjectEnvType_ClosedEnum.
type ProjectEnvType string

const (
	ProjectEnvUser   ProjectEnvType = "USER"
	ProjectEnvSystem ProjectEnvType = "SYSTEM"
)

// ServiceEnvType is the server-authoritative enum on service-stack
// env entries (SDK UserDataTypeEnum). Five values cover the
// platform-managed categories — Phase 2 envclass drops every service
// env regardless of Type (target's own managed services regenerate
// equivalents on import).
type ServiceEnvType string

const (
	ServiceEnvReadOnly ServiceEnvType = "READ_ONLY"
	ServiceEnvEditable ServiceEnvType = "EDITABLE"
	ServiceEnvSecret   ServiceEnvType = "SECRET"
	ServiceEnvInternal ServiceEnvType = "INTERNAL"
	ServiceEnvEnv      ServiceEnvType = "ENV"
)

// ProjectEnvVar is a project-level env entry. Mirrors the SDK's
// ProjectEnv DTO with Type/Sensitive/Editable propagated from server.
// Returned by Client.GetProjectEnv.
type ProjectEnvVar struct {
	ID        string         `json:"id"`
	Key       string         `json:"key"`
	Content   string         `json:"content"`
	Type      ProjectEnvType `json:"type,omitempty"`
	Sensitive bool           `json:"sensitive,omitempty"`
	Editable  bool           `json:"editable,omitempty"`
}

// ServiceEnvVar is a service-stack env entry. Mirrors the SDK's
// ServiceStackEnv DTO. Note: NO Editable field — the SDK doesn't
// expose Editable on service-stack-env scope (verified live, see
// plans/research/env-types-investigation-2026-05-14.md). Returned
// by Client.GetServiceEnv.
type ServiceEnvVar struct {
	ID        string         `json:"id"`
	Key       string         `json:"key"`
	Content   string         `json:"content"`
	Type      ServiceEnvType `json:"type,omitempty"`
	Sensitive bool           `json:"sensitive,omitempty"`
}

// EnvAccessor is the minimal read-side interface shared by
// ProjectEnvVar and ServiceEnvVar. Consumers that operate uniformly
// over either scope (envVarsToMaps, findEnvIDByKey, etc.) accept any
// type implementing this interface — keeps helpers single-source while
// preserving the compile-time scope split at API boundaries.
type EnvAccessor interface {
	GetID() string
	GetKey() string
	GetContent() string
}

// GetID, GetKey, GetContent — ProjectEnvVar implements EnvAccessor.
func (p ProjectEnvVar) GetID() string      { return p.ID }
func (p ProjectEnvVar) GetKey() string     { return p.Key }
func (p ProjectEnvVar) GetContent() string { return p.Content }

// GetID, GetKey, GetContent — ServiceEnvVar implements EnvAccessor.
func (s ServiceEnvVar) GetID() string      { return s.ID }
func (s ServiceEnvVar) GetKey() string     { return s.Key }
func (s ServiceEnvVar) GetContent() string { return s.Content }

// ImportResult represents the result of an import operation.
type ImportResult struct {
	ProjectID     string                 `json:"projectId"`
	ProjectName   string                 `json:"projectName"`
	ServiceStacks []ImportedServiceStack `json:"serviceStacks"`
}

// ImportedServiceStack represents one imported service.
type ImportedServiceStack struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Processes []Process `json:"processes,omitempty"`
	Error     *APIError `json:"error,omitempty"`
}

// APIError represents an error from the Zerops API.
//
// Meta mirrors the server's `error.meta[]` array — same shape as the
// top-level APIMeta plumbed through PlatformError — so per-service failures
// on the import endpoint also carry field-level detail to the LLM instead
// of collapsing to "Invalid parameter provided." See APIMetaItem in errors.go.
type APIError struct {
	Code    string        `json:"code"`
	Message string        `json:"message"`
	Meta    []APIMetaItem `json:"meta,omitempty"`
}

func (e *APIError) Error() string {
	return e.Message
}

// LogAccess carries the signed log-backend URL. Auth rides inside the URL
// itself (the backend ignores an Authorization header), so no separate token
// is kept.
type LogAccess struct {
	URL string `json:"url"`
}

// LogFetchParams contains parameters for fetching logs from the backend.
//
// Server-side filters (honoured by the Zerops log backend):
//   - ServiceID   → serviceStackId
//   - Severity    → minimumSeverity (syslog 0..7)
//   - Facility    → facility (16 = application, 17 = webserver)
//   - Tags        → tags= (CSV; exact match per element)
//   - ContainerID → containerId
//   - Limit       → limit (clamped to [1, 1000] by the fetcher)
//
// Client-side filters (backend silently ignores these query names;
// logfetcher applies them post-fetch):
//   - Since  — parsed time.Time comparison (never lex)
//   - Search — case-sensitive substring match on Message
type LogFetchParams struct {
	ServiceID   string
	Severity    string // "" | "emergency" | "alert" | "critical" | "error" | "warning" | "notice" | "info" | "debug" | "all"
	Facility    string // "" | "application" | "webserver"
	Tags        []string
	ContainerID string
	Since       time.Time
	Limit       int
	Search      string
}

// LogEntry represents a single log entry.
//
// Tag and Facility are populated from the syslog envelope so consumers can
// scope filters at those dimensions without re-querying the backend. Tag in
// particular is the primary build-identity dimension: the Zerops builder emits
// entries with Tag = "zbuilder@<appVersionId>". FetchBuildWarnings relies on
// this to scope results to a single build.
type LogEntry struct {
	ID          string `json:"id,omitempty"`
	Timestamp   string `json:"timestamp"`
	Severity    string `json:"severity"`
	Facility    string `json:"facility,omitempty"` // e.g. "local0" (application), "daemon"
	Tag         string `json:"tag,omitempty"`      // syslog tag, e.g. "zbuilder@abc"
	Message     string `json:"message"`
	Container   string `json:"container,omitempty"`   // container hostname
	ContainerID string `json:"containerId,omitempty"` // container UUID
}

// ProcessEvent represents a process from the search API (activity timeline).
type ProcessEvent struct {
	ID              string            `json:"id"`
	ProjectID       string            `json:"projectId"`
	ServiceStacks   []ServiceStackRef `json:"serviceStacks,omitempty"`
	ActionName      string            `json:"actionName"`
	Status          string            `json:"status"`
	Created         string            `json:"created"`
	Started         *string           `json:"started,omitempty"`
	Finished        *string           `json:"finished,omitempty"`
	FailReason      *string           `json:"failReason,omitempty"`
	CreatedByUser   *UserRef          `json:"createdByUser,omitempty"`
	CreatedBySystem bool              `json:"createdBySystem"`
}

// AppVersionEvent represents a build/deploy event from the search API.
type AppVersionEvent struct {
	ID             string     `json:"id"`
	ProjectID      string     `json:"projectId"`
	ServiceStackID string     `json:"serviceStackId"`
	Source         string     `json:"source"`
	Status         string     `json:"status"`
	Sequence       int        `json:"sequence"`
	Build          *BuildInfo `json:"build,omitempty"`
	Created        string     `json:"created"`
	LastUpdate     string     `json:"lastUpdate"`
}

// BuildInfo contains build pipeline timing and target-container metadata.
//
// Timestamps are RFC3339Nano strings (upstream SDK emits with nanoseconds).
// ContainerCreationStart is the authoritative anchor for "this deploy's
// runtime container lifetime starts here" — FetchRuntimeLogs uses it as a
// Since filter so stale logs from prior container generations are excluded.
type BuildInfo struct {
	ServiceStackID            *string `json:"serviceStackId,omitempty"`            // build service-stack (persists across builds)
	ServiceStackName          *string `json:"serviceStackName,omitempty"`          // human-readable build service-stack name
	ServiceStackTypeVersionID *string `json:"serviceStackTypeVersionId,omitempty"` // e.g. "nodejs@22"
	PipelineStart             *string `json:"pipelineStart,omitempty"`
	PipelineFinish            *string `json:"pipelineFinish,omitempty"`
	PipelineFailed            *string `json:"pipelineFailed,omitempty"`
	ContainerCreationStart    *string `json:"containerCreationStart,omitempty"` // runtime container creation moment
	StartDate                 *string `json:"startDate,omitempty"`              // build-artifact upload start
	EndDate                   *string `json:"endDate,omitempty"`                // build-artifact upload end
	CacheSnapshotID           *string `json:"cacheSnapshotId,omitempty"`        // build-cache snapshot reused, if any
}

// UserRef is a lightweight user reference.
type UserRef struct {
	FullName string `json:"fullName"`
	Email    string `json:"email"`
}
