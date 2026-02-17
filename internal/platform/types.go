package platform

import "time"

// DefaultAPITimeout is the global timeout for each API call.
const DefaultAPITimeout = 30 * time.Second

// UserInfo contains user details from auth/info endpoint.
type UserInfo struct {
	ID       string `json:"id"`
	FullName string `json:"fullName"`
	Email    string `json:"email"`
}

// Project represents a Zerops project.
type Project struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// ServiceStack represents a Zerops service.
type ServiceStack struct {
	ID                   string             `json:"id"`
	Name                 string             `json:"name"` // hostname
	ProjectID            string             `json:"projectId"`
	ServiceStackTypeInfo ServiceTypeInfo    `json:"serviceStackTypeInfo"`
	Status               string             `json:"status"`
	Mode                 string             `json:"mode"` // HA, NON_HA
	Ports                []Port             `json:"ports,omitempty"`
	CustomAutoscaling    *CustomAutoscaling `json:"customAutoscaling,omitempty"`
	Created              string             `json:"created"`
	LastUpdate           string             `json:"lastUpdate,omitempty"`
}

// ServiceTypeInfo contains service type details.
type ServiceTypeInfo struct {
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

// Port represents a service port.
type Port struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	Public   bool   `json:"public"`
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
}

// AutoscalingParams maps MCP tool params to API request.
type AutoscalingParams struct {
	ServiceMode         string // Current HA/NON_HA mode — must be set to avoid API "mode update forbidden"
	HorizontalMinCount  *int32
	HorizontalMaxCount  *int32
	VerticalCPUMode     *string
	VerticalStartCPU    *int32
	VerticalMinCPU      *int32
	VerticalMaxCPU      *int32
	VerticalMinRAM      *float64
	VerticalMaxRAM      *float64
	VerticalMinDisk     *float64
	VerticalMaxDisk     *float64
	VerticalSwapEnabled *bool
}

// Process represents an async operation tracked by Zerops.
type Process struct {
	ID            string            `json:"id"`
	ActionName    string            `json:"actionName"`
	Status        string            `json:"status"` // PENDING, RUNNING, FINISHED, FAILED, CANCELED
	ServiceStacks []ServiceStackRef `json:"serviceStacks,omitempty"`
	Created       string            `json:"created"`
	Started       *string           `json:"started,omitempty"`
	Finished      *string           `json:"finished,omitempty"`
	FailReason    *string           `json:"failReason,omitempty"`
}

// ServiceStackRef is a lightweight service reference in a process.
type ServiceStackRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// EnvVar represents an environment variable.
type EnvVar struct {
	ID      string `json:"id"`
	Key     string `json:"key"`
	Content string `json:"content"`
}

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
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *APIError) Error() string {
	return e.Message
}

// LogAccess contains temporary credentials for log backend access.
type LogAccess struct {
	AccessToken string `json:"accessToken"`
	Expiration  string `json:"expiration"`
	URL         string `json:"url"`
	URLPlain    string `json:"urlPlain"`
}

// LogFetchParams contains parameters for fetching logs from the backend.
type LogFetchParams struct {
	ServiceID string
	Severity  string // error, warning, info, debug, all
	Since     time.Time
	Limit     int
	Search    string
}

// LogEntry represents a single log entry.
type LogEntry struct {
	ID        string `json:"id,omitempty"`
	Timestamp string `json:"timestamp"`
	Severity  string `json:"severity"`
	Message   string `json:"message"`
	Container string `json:"container,omitempty"`
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

// BuildInfo contains build pipeline timing.
type BuildInfo struct {
	PipelineStart  *string `json:"pipelineStart,omitempty"`
	PipelineFinish *string `json:"pipelineFinish,omitempty"`
	PipelineFailed *string `json:"pipelineFailed,omitempty"`
}

// UserRef is a lightweight user reference.
type UserRef struct {
	FullName string `json:"fullName"`
	Email    string `json:"email"`
}

// ServiceStackType represents an available service stack type (e.g. "Node.js", "PostgreSQL").
type ServiceStackType struct {
	Name     string                    `json:"name"`
	Category string                    `json:"category"`
	Versions []ServiceStackTypeVersion `json:"versions"`
}

// ServiceStackTypeVersion represents a specific version of a service stack type.
type ServiceStackTypeVersion struct {
	Name    string `json:"name"`
	IsBuild bool   `json:"isBuild"`
	Status  string `json:"status"`
}
