package platform

import (
	"context"
	"fmt"
	"sort"
)

// ValidateZeropsYaml records the call (capturing inputs for test assertions)
// and returns the error configured via WithError("ValidateZeropsYaml", err).
// Nil = success. Deploy-flow tests wire a *PlatformError here to simulate
// either a structured validation failure (APIMeta populated) or a transport
// failure (NETWORK_ERROR code).
func (m *Mock) ValidateZeropsYaml(_ context.Context, in ValidateZeropsYamlInput) error {
	m.trackCall("ValidateZeropsYaml")
	m.mu.Lock()
	m.CapturedValidateZeropsYaml = append(m.CapturedValidateZeropsYaml, in)
	m.mu.Unlock()
	return m.getError("ValidateZeropsYaml")
}

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
	m.trackCall("ListServices")
	if err := m.getError("ListServices"); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.services, nil
}

func (m *Mock) ListServicesDirect(_ context.Context, _ string) ([]ServiceStack, error) {
	m.trackCall("ListServicesDirect")
	if err := m.getError("ListServicesDirect"); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.servicesDirect != nil {
		return m.servicesDirect, nil
	}
	return m.services, nil
}

func (m *Mock) GetProjectProcessesDirect(_ context.Context, _ string) ([]Process, error) {
	m.trackCall("GetProjectProcessesDirect")
	if err := m.getError("GetProjectProcessesDirect"); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.projectProcesses, nil
}

// GetServiceStackIntegrationStatus returns the seeded IntegrationStatus
// or IntegrationStatus{State: IntegrationNotConfigured} when unseeded —
// mirrors the real wrapper's HTTP-400-as-state mapping. Seed via
// WithIntegrationStatus.
func (m *Mock) GetServiceStackIntegrationStatus(_ context.Context, serviceID string) (IntegrationStatus, error) {
	if err := m.getError("GetServiceStackIntegrationStatus"); err != nil {
		return IntegrationStatus{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if status, ok := m.integrationStatus[serviceID]; ok {
		return status, nil
	}
	return IntegrationStatus{State: IntegrationNotConfigured}, nil
}

// GetAppVersionAppCode returns the seeded download URL for appVersionID
// or empty string when unseeded (cascade treats empty URL as miss).
// Seed via WithAppVersionAppCode.
func (m *Mock) GetAppVersionAppCode(_ context.Context, appVersionID string) (string, error) {
	if err := m.getError("GetAppVersionAppCode"); err != nil {
		return "", err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.appVersionURLs[appVersionID], nil
}

// GetAppVersionUserData returns the seeded yaml-baked run.envVariables for an
// app-version ID, run through the SAME classifier as the real client
// (classifyAppVersionUserData) so a test cannot model a shape the real API
// can't produce: a seeded Sensitive is ignored (the app-version DTO carries
// no Sensitive field — always false, spec §1), and SYSTEM-typed / ZEROPS_YAML
// records are filtered out. A bare seed (no Type) is a run.envVariables var
// by convention → normalized to USER before classifying (the real API always
// sets Type, so this masks no real behavior). The lifecycle gate
// (managed/never-deployed → no app version) is enforced by
// ops.AppVersionEnvVars BEFORE this is reached, not here.
func (m *Mock) GetAppVersionUserData(_ context.Context, appVersionID string) ([]ServiceEnvVar, error) {
	m.trackCall("GetAppVersionUserData")
	if err := m.getError("GetAppVersionUserData"); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	seeded := m.appVersionUserData[appVersionID]
	out := make([]ServiceEnvVar, 0, len(seeded))
	for _, v := range seeded {
		typeStr := string(v.Type)
		if typeStr == "" {
			typeStr = "USER"
		}
		if classifyAppVersionUserData(v.Key, typeStr) != kindRunEnvVariable {
			continue
		}
		// Return the normalized Type too (bare seed → USER), so the returned
		// DTO matches what the real client produces — the real API always
		// sets Type. Sensitive always false (see doc above).
		v.Type = ServiceEnvType(typeStr)
		v.Sensitive = false
		out = append(out, v)
	}
	return out, nil
}

func (m *Mock) GetService(_ context.Context, serviceID string) (*ServiceStack, error) {
	m.trackCall("GetService")
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
	// Return the same typed error the real client maps a 404 to (C4), so
	// production code that does errors.As(*PlatformError) + code switches is
	// exercised against a shape the platform can actually produce.
	return nil, NewPlatformError(ErrServiceNotFound, fmt.Sprintf("mock: service %s not found", serviceID), "Check service ID")
}

func (m *Mock) ActiveServiceTypeVersions(_ context.Context) ([]string, error) {
	m.trackCall("ActiveServiceTypeVersions")
	if err := m.getError("ActiveServiceTypeVersions"); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	var out []string
	for _, v := range m.activeServiceTypes {
		if v.Status == "ACTIVE" && v.Name != "" {
			out = append(out, v.Name)
		}
	}
	return out, nil
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

func (m *Mock) ConnectSharedStorage(_ context.Context, serviceID, _ string) (*Process, error) {
	if err := m.getError("ConnectSharedStorage"); err != nil {
		return nil, err
	}
	return &Process{
		ID:            "proc-connect-storage-" + serviceID,
		ActionName:    "stack.sharedStorageConnect",
		Status:        "PENDING",
		ServiceStacks: []ServiceStackRef{{ID: serviceID}},
	}, nil
}

func (m *Mock) DisconnectSharedStorage(_ context.Context, serviceID, _ string) (*Process, error) {
	if err := m.getError("DisconnectSharedStorage"); err != nil {
		return nil, err
	}
	return &Process{
		ID:            "proc-disconnect-storage-" + serviceID,
		ActionName:    "stack.sharedStorageDisconnect",
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

func (m *Mock) GetServiceEnv(_ context.Context, serviceID string) ([]ServiceEnvVar, error) {
	if err := m.getError("GetServiceEnv"); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.envVars[serviceID], nil
}

func (m *Mock) CreateServiceEnvVar(_ context.Context, serviceID, key, content string, sensitive bool) (*Process, error) {
	m.trackCall("CreateServiceEnvVar")
	if err := m.getError("CreateServiceEnvVar"); err != nil {
		return nil, err
	}
	// Faithful single-var create: append ONE userData record, leaving every
	// other var untouched (mirrors the real platform's POST user-data — NOT a
	// whole-file replace). A non-faithful mock here previously hid the
	// service-env data-loss bug from the integration tests. Type is USER —
	// the platform's 2026-08 model (spec-zerops-env-lifecycle.md §1); Sensitive
	// mirrors exactly what the caller sent, never inferred.
	m.mu.Lock()
	m.envVars[serviceID] = append(m.envVars[serviceID], ServiceEnvVar{
		ID:        fmt.Sprintf("udata-%s-%s", serviceID, key),
		Key:       key,
		Content:   content,
		Type:      ServiceEnvUser,
		Sensitive: sensitive,
	})
	m.mu.Unlock()
	return &Process{
		ID:            "proc-envset-" + serviceID,
		ActionName:    "envSet",
		Status:        "PENDING",
		ServiceStacks: []ServiceStackRef{{ID: serviceID}},
	}, nil
}

func (m *Mock) DeleteUserData(_ context.Context, userDataID string) (*Process, error) {
	m.trackCall("DeleteUserData")
	if err := m.getError("DeleteUserData"); err != nil {
		return nil, err
	}
	// Faithful single-record delete: remove the userData entry with this ID
	// (IDs are unique across the project), leaving siblings intact.
	m.mu.Lock()
	for svcID, vars := range m.envVars {
		kept := make([]ServiceEnvVar, 0, len(vars))
		for _, v := range vars {
			if v.ID != userDataID {
				kept = append(kept, v)
			}
		}
		m.envVars[svcID] = kept
	}
	m.mu.Unlock()
	return &Process{
		ID:         "proc-envdel-" + userDataID,
		ActionName: "envDelete",
		Status:     "PENDING",
	}, nil
}

func (m *Mock) GetProjectEnv(_ context.Context, _ string) ([]ProjectEnvVar, error) {
	if err := m.getError("GetProjectEnv"); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.projectEnv, nil
}

func (m *Mock) CreateProjectEnv(_ context.Context, projectID string, key, content string, sensitive bool) (*Process, error) {
	m.mu.Lock()
	m.CapturedProjectEnvCreations = append(m.CapturedProjectEnvCreations, CapturedProjectEnvCreate{
		ProjectID: projectID,
		Key:       key,
		Content:   content,
		Sensitive: sensitive,
	})
	m.mu.Unlock()
	m.trackCall("CreateProjectEnv")
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
	m.trackCall("DeleteProjectEnv")
	if err := m.getError("DeleteProjectEnv"); err != nil {
		return nil, err
	}
	return &Process{
		ID:         "proc-projenvdel-" + envID,
		ActionName: "envDelete",
		Status:     "PENDING",
	}, nil
}

func (m *Mock) GetProjectExport(_ context.Context, _ string) (string, error) {
	if err := m.getError("GetProjectExport"); err != nil {
		return "", err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.exportYAML == "" {
		return "project:\n  name: mock-export\nservices: []\n", nil
	}
	return m.exportYAML, nil
}

func (m *Mock) GetServiceStackExport(_ context.Context, _ string) (string, error) {
	if err := m.getError("GetServiceStackExport"); err != nil {
		return "", err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.serviceExportYAML == "" {
		return "services: []\n", nil
	}
	return m.serviceExportYAML, nil
}

func (m *Mock) ImportServices(_ context.Context, projectID string, yamlContent string) (*ImportResult, error) {
	if err := m.getError("ImportServices"); err != nil {
		return nil, err
	}
	m.trackCall("ImportServices")
	m.mu.Lock()
	m.CapturedImportYAML = yamlContent
	m.CapturedImportProjectID = projectID
	m.mu.Unlock()

	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.importResult == nil {
		return nil, fmt.Errorf("mock: no import result configured")
	}
	return m.importResult, nil
}

func (m *Mock) DeleteService(_ context.Context, serviceID string) (*Process, error) {
	m.trackCall("DeleteService")
	if err := m.getError("DeleteService"); err != nil {
		return nil, err
	}
	if m.deleteRemovesService {
		m.mu.Lock()
		filtered := m.services[:0]
		for _, s := range m.services {
			if s.ID != serviceID {
				filtered = append(filtered, s)
			}
		}
		m.services = filtered
		m.mu.Unlock()
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
	// Write lock — even pure-read scenarios advance callCount, and
	// every return path must copy the Process struct out from under
	// the lock to avoid caller mutation aliasing the live mock state.
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.processes[processID]
	if !ok {
		return nil, NewPlatformError(ErrProcessNotFound, fmt.Sprintf("mock: process %s not found", processID), "Check process ID")
	}
	out := *p
	if state, ok := m.processScenarios[processID]; ok {
		state.callCount++
		out.Status = scenarioStatusAt(state.scenario, state.callCount)
	}
	return &out, nil
}

func (m *Mock) CancelProcess(_ context.Context, processID string) (*Process, error) {
	if err := m.getError("CancelProcess"); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.processes[processID]
	if !ok {
		return nil, NewPlatformError(ErrProcessNotFound, fmt.Sprintf("mock: process %s not found", processID), "Check process ID")
	}
	p.Status = statusCancelled
	return p, nil
}

func (m *Mock) EnableSubdomainAccess(_ context.Context, serviceID string) (*Process, error) {
	m.trackCall("EnableSubdomainAccess")
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
	m.trackCall("DisableSubdomainAccess")
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

func (m *Mock) SearchProcesses(_ context.Context, projectID string, limit int) ([]ProcessEvent, error) {
	if err := m.getError("SearchProcesses"); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	// Mirror the real client (C4): scope to the project, order newest-first by
	// Created, then apply the limit — so consumers that depend on "latest"
	// semantics (LatestFailedAppVersionContext, the events timeline) are tested
	// against realistic ordering instead of fixture insertion order.
	out := make([]ProcessEvent, 0, len(m.processEvents))
	for _, e := range m.processEvents {
		if projectID == "" || e.ProjectID == "" || e.ProjectID == projectID {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Created > out[j].Created })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ListOwnTokenDelegations returns the seeded delegations (WithTokenDelegations),
// or empty when unseeded / after a successful mint (F4).
func (m *Mock) ListOwnTokenDelegations(_ context.Context) ([]TokenDelegation, error) {
	m.trackCall("ListOwnTokenDelegations")
	if err := m.getError("ListOwnTokenDelegations"); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]TokenDelegation, len(m.tokenDelegations))
	copy(out, m.tokenDelegations)
	return out, nil
}

// MintDelegatedLaunchToken encodes F4's one-shot semantics: with no
// delegation seeded it returns ErrDelegationUnavailable (the default,
// pre-delegation/consumed-platform shape); with one seeded it consumes it
// (clears tokenDelegations, so a subsequent ListOwnTokenDelegations /
// MintDelegatedLaunchToken sees none) and returns the WithMintedToken value
// verbatim, or a generated placeholder if unseeded.
func (m *Mock) MintDelegatedLaunchToken(_ context.Context, _ string) (MintedToken, error) {
	m.trackCall("MintDelegatedLaunchToken")
	if err := m.getError("MintDelegatedLaunchToken"); err != nil {
		return MintedToken{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.tokenDelegations) == 0 {
		return MintedToken{}, NewPlatformError(ErrDelegationUnavailable, "mock: no unused delegation for this token", "Fall back to the manual launchKey path")
	}
	m.tokenDelegations = nil // F4: a successful mint consumes the one-time delegation
	if m.mintedToken != nil {
		return *m.mintedToken, nil
	}
	return MintedToken{Token: "mock-minted-token", TokenID: "mock-minted-token-id"}, nil
}

func (m *Mock) SearchAppVersions(_ context.Context, projectID string, limit int) ([]AppVersionEvent, error) {
	if err := m.getError("SearchAppVersions"); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]AppVersionEvent, 0, len(m.appVersionEvents))
	for _, e := range m.appVersionEvents {
		if projectID == "" || e.ProjectID == "" || e.ProjectID == projectID {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Created > out[j].Created })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
