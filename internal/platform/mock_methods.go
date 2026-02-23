package platform

import (
	"context"
	"fmt"
)

func (m *Mock) GetUserInfo(_ context.Context) (*UserInfo, error) {
	if err := m.getError("GetUserInfo"); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.userInfo == nil {
		return nil, fmt.Errorf("mock: no user info configured")
	}
	return m.userInfo, nil
}

func (m *Mock) ListProjects(_ context.Context, _ string) ([]Project, error) {
	if err := m.getError("ListProjects"); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.projects, nil
}

func (m *Mock) GetProject(_ context.Context, _ string) (*Project, error) {
	if err := m.getError("GetProject"); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.project == nil {
		return nil, fmt.Errorf("mock: no project configured")
	}
	return m.project, nil
}

func (m *Mock) ListServices(_ context.Context, _ string) ([]ServiceStack, error) {
	if err := m.getError("ListServices"); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.services, nil
}

func (m *Mock) GetService(_ context.Context, serviceID string) (*ServiceStack, error) {
	if err := m.getError("GetService"); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.service != nil {
		return m.service, nil
	}
	for i := range m.services {
		if m.services[i].ID == serviceID {
			return &m.services[i], nil
		}
	}
	return nil, fmt.Errorf("mock: service %s not found", serviceID)
}

func (m *Mock) StartService(_ context.Context, serviceID string) (*Process, error) {
	if err := m.getError("StartService"); err != nil {
		return nil, err
	}
	return &Process{
		ID:            "proc-start-" + serviceID,
		ActionName:    "start",
		Status:        "PENDING",
		ServiceStacks: []ServiceStackRef{{ID: serviceID}},
	}, nil
}

func (m *Mock) StopService(_ context.Context, serviceID string) (*Process, error) {
	if err := m.getError("StopService"); err != nil {
		return nil, err
	}
	return &Process{
		ID:            "proc-stop-" + serviceID,
		ActionName:    "stop",
		Status:        "PENDING",
		ServiceStacks: []ServiceStackRef{{ID: serviceID}},
	}, nil
}

func (m *Mock) RestartService(_ context.Context, serviceID string) (*Process, error) {
	if err := m.getError("RestartService"); err != nil {
		return nil, err
	}
	return &Process{
		ID:            "proc-restart-" + serviceID,
		ActionName:    "restart",
		Status:        "PENDING",
		ServiceStacks: []ServiceStackRef{{ID: serviceID}},
	}, nil
}

func (m *Mock) ReloadService(_ context.Context, serviceID string) (*Process, error) {
	if err := m.getError("ReloadService"); err != nil {
		return nil, err
	}
	return &Process{
		ID:            "proc-reload-" + serviceID,
		ActionName:    "reload",
		Status:        "PENDING",
		ServiceStacks: []ServiceStackRef{{ID: serviceID}},
	}, nil
}

func (m *Mock) SetAutoscaling(_ context.Context, _ string, _ AutoscalingParams) (*Process, error) {
	if err := m.getError("SetAutoscaling"); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.autoscalingProcess != nil {
		return m.autoscalingProcess, nil
	}
	return nil, nil //nolint:nilnil // intentional: nil process means sync (no async process)
}

func (m *Mock) GetServiceEnv(_ context.Context, serviceID string) ([]EnvVar, error) {
	if err := m.getError("GetServiceEnv"); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.envVars[serviceID], nil
}

func (m *Mock) SetServiceEnvFile(_ context.Context, serviceID string, _ string) (*Process, error) {
	if err := m.getError("SetServiceEnvFile"); err != nil {
		return nil, err
	}
	return &Process{
		ID:            "proc-envset-" + serviceID,
		ActionName:    "envSet",
		Status:        "PENDING",
		ServiceStacks: []ServiceStackRef{{ID: serviceID}},
	}, nil
}

func (m *Mock) DeleteUserData(_ context.Context, userDataID string) (*Process, error) {
	if err := m.getError("DeleteUserData"); err != nil {
		return nil, err
	}
	return &Process{
		ID:         "proc-envdel-" + userDataID,
		ActionName: "envDelete",
		Status:     "PENDING",
	}, nil
}

func (m *Mock) GetProjectEnv(_ context.Context, _ string) ([]EnvVar, error) {
	if err := m.getError("GetProjectEnv"); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.projectEnv, nil
}

func (m *Mock) CreateProjectEnv(_ context.Context, _ string, _, _ string, _ bool) (*Process, error) {
	if err := m.getError("CreateProjectEnv"); err != nil {
		return nil, err
	}
	return &Process{
		ID:         "proc-projenvset",
		ActionName: "envSet",
		Status:     "PENDING",
	}, nil
}

func (m *Mock) DeleteProjectEnv(_ context.Context, envID string) (*Process, error) {
	if err := m.getError("DeleteProjectEnv"); err != nil {
		return nil, err
	}
	return &Process{
		ID:         "proc-projenvdel-" + envID,
		ActionName: "envDelete",
		Status:     "PENDING",
	}, nil
}

func (m *Mock) ImportServices(_ context.Context, _ string, _ string) (*ImportResult, error) {
	if err := m.getError("ImportServices"); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.importResult == nil {
		return nil, fmt.Errorf("mock: no import result configured")
	}
	return m.importResult, nil
}

func (m *Mock) DeleteService(_ context.Context, serviceID string) (*Process, error) {
	if err := m.getError("DeleteService"); err != nil {
		return nil, err
	}
	return &Process{
		ID:            "proc-delete-" + serviceID,
		ActionName:    "delete",
		Status:        "PENDING",
		ServiceStacks: []ServiceStackRef{{ID: serviceID}},
	}, nil
}

func (m *Mock) GetProcess(_ context.Context, processID string) (*Process, error) {
	if err := m.getError("GetProcess"); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.processes[processID]
	if !ok {
		return nil, fmt.Errorf("mock: process %s not found", processID)
	}
	return p, nil
}

func (m *Mock) CancelProcess(_ context.Context, processID string) (*Process, error) {
	if err := m.getError("CancelProcess"); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.processes[processID]
	if !ok {
		return nil, fmt.Errorf("mock: process %s not found", processID)
	}
	p.Status = statusCancelled
	return p, nil
}

func (m *Mock) EnableSubdomainAccess(_ context.Context, serviceID string) (*Process, error) {
	if err := m.getError("EnableSubdomainAccess"); err != nil {
		return nil, err
	}
	return &Process{
		ID:            "proc-subdomain-enable-" + serviceID,
		ActionName:    "enableSubdomain",
		Status:        "PENDING",
		ServiceStacks: []ServiceStackRef{{ID: serviceID}},
	}, nil
}

func (m *Mock) DisableSubdomainAccess(_ context.Context, serviceID string) (*Process, error) {
	if err := m.getError("DisableSubdomainAccess"); err != nil {
		return nil, err
	}
	return &Process{
		ID:            "proc-subdomain-disable-" + serviceID,
		ActionName:    "disableSubdomain",
		Status:        "PENDING",
		ServiceStacks: []ServiceStackRef{{ID: serviceID}},
	}, nil
}

func (m *Mock) GetProjectLog(_ context.Context, _ string) (*LogAccess, error) {
	if err := m.getError("GetProjectLog"); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.logAccess == nil {
		return nil, fmt.Errorf("mock: no log access configured")
	}
	return m.logAccess, nil
}

func (m *Mock) SearchProcesses(_ context.Context, _ string, _ int) ([]ProcessEvent, error) {
	if err := m.getError("SearchProcesses"); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.processEvents, nil
}

func (m *Mock) SearchAppVersions(_ context.Context, _ string, _ int) ([]AppVersionEvent, error) {
	if err := m.getError("SearchAppVersions"); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.appVersionEvents, nil
}

func (m *Mock) ListServiceStackTypes(_ context.Context) ([]ServiceStackType, error) {
	if err := m.getError("ListServiceStackTypes"); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.stackTypes, nil
}
