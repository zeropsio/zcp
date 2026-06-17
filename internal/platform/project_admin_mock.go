package platform

import (
	"context"
	"sync"
)

// MockProjectAdminClient is the mock implementation of ProjectAdminClient
// used by unit + integration tests. Mirrors the configuration shape of
// platform.Mock but kept separate because the surfaces are disjoint.
//
// Configuration is via With* setters (option-pattern); state is captured
// for assertions via Captured* fields. Concurrent-safe.
type MockProjectAdminClient struct {
	mu sync.Mutex

	// configured returns
	importResult       *ImportResult
	importErr          error
	listServicesResult []ServiceStack
	listServicesErr    error
	serviceEnvKeys     map[string][]EnvKey // keyed by serviceID
	serviceEnvErr      error
	projectEnvKeys     map[string][]EnvKey // keyed by projectID
	projectEnvErr      error
	processResult      *Process
	processErr         error
	deleteResult       *Process
	deleteErr          error

	// state capture for assertions
	CapturedImportYAML    string
	CapturedDeleteProject string
	CapturedGetProcessID  string
	Closed                bool

	// clientUserID returned by ClientUserID(); tests configure via WithClientUserID.
	clientUserID string

	// GrantSelfRole capture + error injection
	CapturedGrantSelfRoleProject string
	CapturedGrantSelfRoleCode    string
	grantSelfRoleErr             error

	// F7 bring-up management capture/config.
	DeletedServiceIDs []string
	DeleteServiceErr  error
	LifecycleCalls    []string
	LogAccess         *LogAccess
	LogAccessErr      error

	// GetServiceStackIntegrationStatus configurable results + capture
	integrationStatuses               map[string]IntegrationStatus // keyed by serviceStackID
	integrationStatusErr              error
	CapturedIntegrationStatusServices []string

	// ListIntegrationTokens configurable result + error injection.
	integrationTokens    []IntegrationTokenInfo
	integrationTokensErr error
}

// WithClientUserID sets the ClientUserID() return value for tests.
func (m *MockProjectAdminClient) WithClientUserID(id string) *MockProjectAdminClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clientUserID = id
	return m
}

// ClientUserID implements ProjectAdminClient.
func (m *MockProjectAdminClient) ClientUserID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.clientUserID
}

// GrantSelfRole implements ProjectAdminClient. Mock captures the call
// args; configurable error via WithGrantSelfRoleError.
func (m *MockProjectAdminClient) GrantSelfRole(_ context.Context, projectID, roleCode string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Closed {
		return ErrClientClosed
	}
	m.CapturedGrantSelfRoleProject = projectID
	m.CapturedGrantSelfRoleCode = roleCode
	return m.grantSelfRoleErr
}

// WithGrantSelfRoleError configures the error returned by GrantSelfRole.
func (m *MockProjectAdminClient) WithGrantSelfRoleError(err error) *MockProjectAdminClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.grantSelfRoleErr = err
	return m
}

// NewMockProjectAdminClient creates a fresh mock.
func NewMockProjectAdminClient() *MockProjectAdminClient {
	return &MockProjectAdminClient{
		serviceEnvKeys:      make(map[string][]EnvKey),
		projectEnvKeys:      make(map[string][]EnvKey),
		integrationStatuses: make(map[string]IntegrationStatus),
	}
}

// WithIntegrationStatus configures GetServiceStackIntegrationStatus result
// for a given serviceStackID. Tests that want to assert per-service
// behavior populate the map per service.
func (m *MockProjectAdminClient) WithIntegrationStatus(serviceStackID string, status IntegrationStatus) *MockProjectAdminClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.integrationStatuses[serviceStackID] = status
	return m
}

// WithIntegrationStatusError configures the error returned by
// GetServiceStackIntegrationStatus.
func (m *MockProjectAdminClient) WithIntegrationStatusError(err error) *MockProjectAdminClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.integrationStatusErr = err
	return m
}

// GetServiceStackIntegrationStatus implements ProjectAdminClient. Returns
// the configured per-service status; unconfigured serviceIDs default to
// IntegrationNotConfigured so tests focus on the configured cases.
func (m *MockProjectAdminClient) GetServiceStackIntegrationStatus(_ context.Context, serviceStackID string) (IntegrationStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Closed {
		return IntegrationStatus{}, ErrClientClosed
	}
	m.CapturedIntegrationStatusServices = append(m.CapturedIntegrationStatusServices, serviceStackID)
	if m.integrationStatusErr != nil {
		return IntegrationStatus{}, m.integrationStatusErr
	}
	if status, ok := m.integrationStatuses[serviceStackID]; ok {
		return status, nil
	}
	return IntegrationStatus{State: IntegrationNotConfigured}, nil
}

// WithIntegrationTokens configures the ListIntegrationTokens result.
func (m *MockProjectAdminClient) WithIntegrationTokens(tokens []IntegrationTokenInfo) *MockProjectAdminClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.integrationTokens = tokens
	return m
}

// WithIntegrationTokensError configures the ListIntegrationTokens error.
func (m *MockProjectAdminClient) WithIntegrationTokensError(err error) *MockProjectAdminClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.integrationTokensErr = err
	return m
}

// ListIntegrationTokens implements ProjectAdminClient.
func (m *MockProjectAdminClient) ListIntegrationTokens(_ context.Context) ([]IntegrationTokenInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Closed {
		return nil, ErrClientClosed
	}
	if m.integrationTokensErr != nil {
		return nil, m.integrationTokensErr
	}
	return m.integrationTokens, nil
}

// WithImportResult configures the result returned by CreateAndImportProject.
func (m *MockProjectAdminClient) WithImportResult(r *ImportResult) *MockProjectAdminClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.importResult = r
	return m
}

// WithImportError configures the error returned by CreateAndImportProject.
func (m *MockProjectAdminClient) WithImportError(err error) *MockProjectAdminClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.importErr = err
	return m
}

// WithServices configures ListServices result.
func (m *MockProjectAdminClient) WithServices(services []ServiceStack) *MockProjectAdminClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listServicesResult = services
	return m
}

// WithServiceEnvKeys configures GetServiceEnvKeys result for a serviceID.
func (m *MockProjectAdminClient) WithServiceEnvKeys(serviceID string, keys []EnvKey) *MockProjectAdminClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.serviceEnvKeys[serviceID] = keys
	return m
}

// WithProjectEnvKeys configures GetProjectEnvKeys result for a projectID.
func (m *MockProjectAdminClient) WithProjectEnvKeys(projectID string, keys []EnvKey) *MockProjectAdminClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.projectEnvKeys[projectID] = keys
	return m
}

// WithProcess configures GetProcess result.
func (m *MockProjectAdminClient) WithProcess(p *Process) *MockProjectAdminClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.processResult = p
	return m
}

// WithDeleteResult configures DeleteProject result.
func (m *MockProjectAdminClient) WithDeleteResult(p *Process) *MockProjectAdminClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteResult = p
	return m
}

// WithDeleteError configures DeleteProject error.
func (m *MockProjectAdminClient) WithDeleteError(err error) *MockProjectAdminClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteErr = err
	return m
}

// CreateAndImportProject implements ProjectAdminClient.
func (m *MockProjectAdminClient) CreateAndImportProject(_ context.Context, yaml string) (*ImportResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Closed {
		return nil, ErrClientClosed
	}
	m.CapturedImportYAML = yaml
	if m.importErr != nil {
		return nil, m.importErr
	}
	return m.importResult, nil
}

// ListServices implements ProjectAdminClient.
func (m *MockProjectAdminClient) ListServices(_ context.Context, _ string) ([]ServiceStack, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Closed {
		return nil, ErrClientClosed
	}
	if m.listServicesErr != nil {
		return nil, m.listServicesErr
	}
	return m.listServicesResult, nil
}

// GetServiceEnvKeys implements ProjectAdminClient.
func (m *MockProjectAdminClient) GetServiceEnvKeys(_ context.Context, serviceID string) ([]EnvKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Closed {
		return nil, ErrClientClosed
	}
	if m.serviceEnvErr != nil {
		return nil, m.serviceEnvErr
	}
	return m.serviceEnvKeys[serviceID], nil
}

// GetProjectEnvKeys implements ProjectAdminClient.
func (m *MockProjectAdminClient) GetProjectEnvKeys(_ context.Context, projectID string) ([]EnvKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Closed {
		return nil, ErrClientClosed
	}
	if m.projectEnvErr != nil {
		return nil, m.projectEnvErr
	}
	return m.projectEnvKeys[projectID], nil
}

// GetProcess implements ProjectAdminClient.
func (m *MockProjectAdminClient) GetProcess(_ context.Context, processID string) (*Process, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Closed {
		return nil, ErrClientClosed
	}
	m.CapturedGetProcessID = processID
	if m.processErr != nil {
		return nil, m.processErr
	}
	return m.processResult, nil
}

// DeleteProject implements ProjectAdminClient.
func (m *MockProjectAdminClient) DeleteProject(_ context.Context, projectID string) (*Process, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Closed {
		return nil, ErrClientClosed
	}
	m.CapturedDeleteProject = projectID
	if m.deleteErr != nil {
		return nil, m.deleteErr
	}
	return m.deleteResult, nil
}

// Close implements ProjectAdminClient. After Close, all method calls return
// ErrClientClosed — matches the real client's contract.
func (m *MockProjectAdminClient) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Closed = true
}

// --- F7 bring-up management mock surface -----------------------------------

// DeleteService implements ProjectAdminClient. Records the call; returns
// the configured error or a PENDING delete process.
func (m *MockProjectAdminClient) DeleteService(_ context.Context, serviceID string) (*Process, error) {
	m.mu.Lock()
	m.DeletedServiceIDs = append(m.DeletedServiceIDs, serviceID)
	m.mu.Unlock()
	if m.DeleteServiceErr != nil {
		return nil, m.DeleteServiceErr
	}
	return &Process{ID: "proc-del-" + serviceID, ActionName: "stackDelete", Status: "PENDING"}, nil
}

// RestartService implements ProjectAdminClient.
func (m *MockProjectAdminClient) RestartService(_ context.Context, serviceID string) (*Process, error) {
	m.mu.Lock()
	m.LifecycleCalls = append(m.LifecycleCalls, "restart:"+serviceID)
	m.mu.Unlock()
	return &Process{ID: "proc-restart-" + serviceID, ActionName: "restart", Status: "PENDING"}, nil
}

// SetServiceScaling implements ProjectAdminClient.
func (m *MockProjectAdminClient) SetServiceScaling(_ context.Context, serviceID string, _ AutoscalingParams) (*Process, error) {
	m.mu.Lock()
	m.LifecycleCalls = append(m.LifecycleCalls, "scale:"+serviceID)
	m.mu.Unlock()
	return &Process{ID: "proc-scale-" + serviceID, ActionName: "scale", Status: "PENDING"}, nil
}

// StopService implements ProjectAdminClient.
func (m *MockProjectAdminClient) StopService(_ context.Context, serviceID string) (*Process, error) {
	m.mu.Lock()
	m.LifecycleCalls = append(m.LifecycleCalls, "stop:"+serviceID)
	m.mu.Unlock()
	return &Process{ID: "proc-stop-" + serviceID, ActionName: "stop", Status: "PENDING"}, nil
}

// StartService implements ProjectAdminClient.
func (m *MockProjectAdminClient) StartService(_ context.Context, serviceID string) (*Process, error) {
	m.mu.Lock()
	m.LifecycleCalls = append(m.LifecycleCalls, "start:"+serviceID)
	m.mu.Unlock()
	return &Process{ID: "proc-start-" + serviceID, ActionName: "start", Status: "PENDING"}, nil
}

// EnableSubdomainAccess implements ProjectAdminClient (F4c).
func (m *MockProjectAdminClient) EnableSubdomainAccess(_ context.Context, serviceID string) (*Process, error) {
	m.mu.Lock()
	m.LifecycleCalls = append(m.LifecycleCalls, "enable-subdomain:"+serviceID)
	m.mu.Unlock()
	return &Process{ID: "proc-enable-subdomain-" + serviceID, ActionName: "enableSubdomainAccess", Status: "PENDING"}, nil
}

// GetProjectLogAccess implements ProjectAdminClient.
func (m *MockProjectAdminClient) GetProjectLogAccess(_ context.Context, _ string) (*LogAccess, error) {
	if m.LogAccessErr != nil {
		return nil, m.LogAccessErr
	}
	if m.LogAccess != nil {
		return m.LogAccess, nil
	}
	return &LogAccess{URL: "https://logs.example"}, nil
}
