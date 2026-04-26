package platform

import (
	"context"
	"slices"
	"sync"
)

// Compile-time interface checks.
var _ Client = (*Mock)(nil)
var _ LogFetcher = (*MockLogFetcher)(nil)

// Mock is a configurable mock for the Platform Client interface.
type Mock struct {
	mu sync.RWMutex

	userInfo           *UserInfo
	projects           []Project
	project            *Project
	services           []ServiceStack
	service            *ServiceStack
	processes          map[string]*Process
	processScenarios   map[string]*processScenarioState // optional per-process state machine driven by GetProcess calls
	envVars            map[string][]EnvVar              // serviceID -> env vars
	projectEnv         []EnvVar
	logAccess          *LogAccess
	importResult       *ImportResult
	processEvents      []ProcessEvent
	appVersionEvents   []AppVersionEvent
	stackTypes         []ServiceStackType
	autoscalingProcess *Process // non-nil → SetAutoscaling returns this process
	exportYAML         string   // project export YAML
	serviceExportYAML  string   // service export YAML

	// CapturedImportYAML stores the YAML content passed to ImportServices.
	CapturedImportYAML string

	// CapturedValidateZeropsYaml stores inputs passed to ValidateZeropsYaml
	// so deploy-flow tests can assert call ordering / field propagation.
	CapturedValidateZeropsYaml []ValidateZeropsYamlInput

	// CallCounts tracks how many times each method was called.
	CallCounts map[string]int

	// Error overrides: method name -> error
	errors map[string]error
}

// NewMock creates a new configurable mock.
func NewMock() *Mock {
	return &Mock{
		processes:        make(map[string]*Process),
		processScenarios: make(map[string]*processScenarioState),
		envVars:          make(map[string][]EnvVar),
		CallCounts:       make(map[string]int),
		errors:           make(map[string]error),
	}
}

// trackCall increments the call count for a method.
func (m *Mock) trackCall(method string) {
	m.mu.Lock()
	m.CallCounts[method]++
	m.mu.Unlock()
}

// WithUserInfo sets the user info returned by GetUserInfo.
func (m *Mock) WithUserInfo(info *UserInfo) *Mock {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.userInfo = info
	return m
}

// WithProjects sets the projects returned by ListProjects.
func (m *Mock) WithProjects(projects []Project) *Mock {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.projects = projects
	return m
}

// WithProject sets the project returned by GetProject.
func (m *Mock) WithProject(project *Project) *Mock {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.project = project
	return m
}

// WithServices sets the services returned by ListServices.
func (m *Mock) WithServices(services []ServiceStack) *Mock {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.services = services
	return m
}

// WithService sets the service returned by GetService.
func (m *Mock) WithService(service *ServiceStack) *Mock {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.service = service
	return m
}

// WithProcess adds a process to the mock.
func (m *Mock) WithProcess(process *Process) *Mock {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.processes[process.ID] = process
	return m
}

// WithServiceEnv sets env vars for a service.
func (m *Mock) WithServiceEnv(serviceID string, vars []EnvVar) *Mock {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.envVars[serviceID] = vars
	return m
}

// WithProjectEnv sets project-level env vars.
func (m *Mock) WithProjectEnv(vars []EnvVar) *Mock {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.projectEnv = vars
	return m
}

// WithLogAccess sets the log access returned by GetProjectLog.
func (m *Mock) WithLogAccess(access *LogAccess) *Mock {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logAccess = access
	return m
}

// WithImportResult sets the result returned by ImportServices.
func (m *Mock) WithImportResult(result *ImportResult) *Mock {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.importResult = result
	return m
}

// WithProcessEvents sets the process events returned by SearchProcesses.
func (m *Mock) WithProcessEvents(events []ProcessEvent) *Mock {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.processEvents = events
	return m
}

// WithAppVersionEvents sets the app version events returned by SearchAppVersions.
func (m *Mock) WithAppVersionEvents(events []AppVersionEvent) *Mock {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.appVersionEvents = events
	return m
}

// WithAutoscalingProcess sets the process returned by SetAutoscaling (nil = sync, no process).
func (m *Mock) WithAutoscalingProcess(proc *Process) *Mock {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.autoscalingProcess = proc
	return m
}

// WithServiceStackTypes sets the service stack types returned by ListServiceStackTypes.
func (m *Mock) WithServiceStackTypes(types []ServiceStackType) *Mock {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stackTypes = types
	return m
}

// WithExportYAML sets the YAML returned by GetProjectExport.
func (m *Mock) WithExportYAML(yaml string) *Mock {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.exportYAML = yaml
	return m
}

// WithServiceExportYAML sets the YAML returned by GetServiceStackExport.
func (m *Mock) WithServiceExportYAML(yaml string) *Mock {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.serviceExportYAML = yaml
	return m
}

// WithError sets an error for a specific method.
func (m *Mock) WithError(method string, err error) *Mock {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errors[method] = err
	return m
}

func (m *Mock) getError(method string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.errors[method]
}

// MockLogFetcher is a configurable mock for LogFetcher.
type MockLogFetcher struct {
	entries []LogEntry
	err     error
}

// NewMockLogFetcher creates a new MockLogFetcher.
func NewMockLogFetcher() *MockLogFetcher {
	return &MockLogFetcher{}
}

// WithEntries sets the log entries to return.
func (f *MockLogFetcher) WithEntries(entries []LogEntry) *MockLogFetcher {
	f.entries = entries
	return f
}

// WithError sets the error to return.
func (f *MockLogFetcher) WithError(err error) *MockLogFetcher {
	f.err = err
	return f
}

// FetchLogs applies the same filters that ZeropsLogFetcher applies in
// production so consumer unit tests prove real behaviour:
//   - Server-side simulated: Severity (syslog numeric compare against entry),
//     Facility (maps "application"→"local0", "webserver"→"local1"),
//     Tags (OR over exact matches on entry.Tag), ContainerID (exact match).
//   - Client-side (shared with real fetcher via filterEntries): Since, Search,
//     Limit tail-trim.
//
// Matches the ordering documented in filterEntries — server-side first, then
// client-side, then limit.
func (f *MockLogFetcher) FetchLogs(_ context.Context, _ *LogAccess, params LogFetchParams) ([]LogEntry, error) {
	if f.err != nil {
		return nil, f.err
	}

	// Copy so mutations (tail-trim aliasing via entries[:0]) don't leak into
	// the mock's canned entries on a subsequent call.
	entries := make([]LogEntry, len(f.entries))
	copy(entries, f.entries)

	// Severity: keep entries whose numeric severity is <= the requested min.
	if params.Severity != "" && params.Severity != "all" {
		minNum := severityNumeric(params.Severity)
		filtered := entries[:0]
		for _, e := range entries {
			if severityNumeric(e.Severity) <= minNum {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}

	// Facility: map label to expected entry.Facility value. Unknown facility
	// values (want == "") are treated as "no filter", matching the real
	// fetcher which omits the query param for unknown facility.
	if want := facilityEntryLabel(params.Facility); want != "" {
		filtered := entries[:0]
		for _, e := range entries {
			if e.Facility == want {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}

	// Tags: OR over exact matches.
	if len(params.Tags) > 0 {
		filtered := entries[:0]
		for _, e := range entries {
			if slices.Contains(params.Tags, e.Tag) {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}

	// ContainerID: exact match.
	if params.ContainerID != "" {
		filtered := entries[:0]
		for _, e := range entries {
			if e.ContainerID == params.ContainerID {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}

	return filterEntries(entries, params, clampLimit(params.Limit)), nil
}

// severityNumeric converts a severity label (the on-wire LogEntry.Severity
// form — "Warning", "Error", etc., case-insensitive) OR the request form
// ("warning", "error") to syslog numeric 0..7. Unknown → 7 (debug) so entries
// with missing severity pass every filter except debug.
func severityNumeric(label string) int {
	if label == "" {
		return 7
	}
	n := mapSeverityToNumeric(label)
	switch n {
	case "0":
		return 0
	case "1":
		return 1
	case "2":
		return 2
	case "3":
		return 3
	case "4":
		return 4
	case "5":
		return 5
	case "6":
		return 6
	default:
		return 7
	}
}

// facilityEntryLabel maps a LogFetchParams.Facility ("application" / "webserver")
// to the syslog label that appears in LogEntry.Facility on the wire (e.g. the
// backend emits "local0" for facility=16). Empty return means "unknown facility
// in the request" — caller omits the filter. Matches probed backend behaviour.
func facilityEntryLabel(facility string) string {
	switch facility {
	case "application":
		return "local0"
	case "webserver":
		return "local1"
	default:
		return ""
	}
}
