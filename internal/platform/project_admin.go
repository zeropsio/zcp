package platform

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/zeropsio/zerops-go/dto/input/body"
	"github.com/zeropsio/zerops-go/dto/input/path"
	"github.com/zeropsio/zerops-go/dto/output"
	zgotypes "github.com/zeropsio/zerops-go/types"
	"github.com/zeropsio/zerops-go/types/enum"
	"github.com/zeropsio/zerops-go/types/uuid"
)

// ProjectAdminClient is the cross-project mutation surface used exclusively
// by the launch-production workflow handler. Constructed per workflow
// invocation from a user-supplied launch-window key, used during a single
// workflow run, and discarded.
//
// Discipline (P-LP-2): NewProjectAdminClient is callable only from
// internal/tools/workflow_launch_production.go. Pinned by
// internal/topology/architecture_test.go::TestProjectAdminClientRestrictedImport.
//
// Key handling (P-LP-1):
//   - The launch-window key flows in via the constructor only.
//   - The struct holding the key uses a private field with no getter/Stringer.
//   - Close() zeros the underlying SDK handler reference for GC reachability.
//   - The key is never serialized, logged, or written to state.
//   - GetServiceEnvKeys / GetProjectEnvKeys return EnvKey (no Value field) —
//     P-LP-5 invariant: ZCP never reads external secret values.
type ProjectAdminClient interface {
	// CreateAndImportProject wraps PostClientProjectImport — single API call
	// that creates the project and imports services from import YAML in one
	// shot. Returns synchronously with the new project ID + per-service
	// stack IDs + per-service async processes to poll. Per-service `Error`
	// surfaces import-time validation issues without aborting the whole
	// import.
	CreateAndImportProject(ctx context.Context, yaml string, opts CreateOpts) (*ImportResult, error)

	// ListServices for the target project (read-only). Used to verify
	// external-secret presence post-import + to discover service IDs for
	// further calls.
	ListServices(ctx context.Context, projectID string) ([]ServiceStack, error)

	// GetServiceEnvKeys returns env entry keys + sensitive flag for a
	// service. Intentionally omits Value field per P-LP-5. Used to verify
	// that the user has set external secrets in Zerops UI without ZCP
	// reading those values.
	GetServiceEnvKeys(ctx context.Context, serviceID string) ([]EnvKey, error)

	// GetProjectEnvKeys returns project-level env entry keys + sensitive
	// flag. Same omit-Value semantics.
	GetProjectEnvKeys(ctx context.Context, projectID string) ([]EnvKey, error)

	// GetProcess fetches an async process state. Wraps existing infra.
	GetProcess(ctx context.Context, processID string) (*Process, error)

	// DeleteProject initiates an async project delete. Returns the
	// delete-process; caller polls via GetProcess.
	DeleteProject(ctx context.Context, projectID string) (*Process, error)

	// Close zeros internal references so the GC reclaims the SDK handler
	// (which holds the launch-window key inside its authenticated transport).
	// Caller MUST `defer admin.Close()`.
	Close()

	// ClientUserID returns the launching user's clientUserId — the link
	// between user and client. Used by GrantSelfRole to authorize
	// follow-up env reads against the freshly-created project. Captured
	// at construction via the same GetUserInfo call that validates the
	// launch key. Empty if the underlying token's clientUserList is
	// empty (a shape ZCP refuses at startup, but the field is opt-in
	// defensive against future SDK shape changes). A.10 spike finding.
	ClientUserID() string

	// GrantSelfRole assigns the launching user's clientUserId the
	// given role on the target project. Required after
	// CreateAndImportProject because `project.userRoles[]` in the
	// import yaml is silently dropped by the platform — A.10 finding
	// empirically verified 2026-05-11.
	//
	// Reads existing roles, appends the new entry, writes back the
	// merged list — PutClientUserRoles is a full replace, so a naive
	// write would wipe other project roles.
	//
	// Role values match enum.ClientUserRoleCodeEnum: OWNER, ADMIN,
	// BASIC_USER, READ_ONLY, NO_ACCESS. Launch-production grants ADMIN
	// so the workflow can read envs + manage the project.
	GrantSelfRole(ctx context.Context, projectID string, roleCode string) error

	// GetServiceStackIntegrationStatus reads the pipeline-integration state
	// of a runtime service-stack. Used by launch-production's
	// configuring-pipeline status to verify that the user has wired
	// ongoing CD via the Zerops dashboard.
	//
	// Maps the SDK's
	// GetServiceStackExternalRepositoryIntegrationStatus call. Phase A
	// B.1 finding: the platform expresses "not configured" as HTTP 400
	// with `code: noExternalRepositoryIntegration`. This wrapper treats
	// that code as a state read (IntegrationNotConfigured), NOT a
	// failure to propagate. Any other 4xx/5xx propagates as platform
	// error.
	//
	// Path B v1: ZCP only reads; user configures via Zerops dashboard.
	// Path A close-loop is in backlog
	// (plans/backlog/launch-pipeline-close-loop-oauth.md).
	GetServiceStackIntegrationStatus(ctx context.Context, serviceStackID string) (IntegrationStatus, error)
}

// CreateOpts holds project-creation options NOT derived from the import yaml.
// Project name + envVariables + service stack list all come from the import
// yaml body. CreateOpts carries the dimensions the yaml doesn't naturally
// express (region, tags appended at create-time for organization).
type CreateOpts struct {
	// Location is the region code. Default if empty: derived from yaml or
	// platform default. Verified values include "eu-central". See spec
	// docs/spec-launch-production-platform-spike.md §A.4.
	Location string
	// Tags applied to the project at creation; appended to whatever the
	// import yaml declares (under project.tags).
	Tags []string
}

// EnvKey is an environment variable entry surfaced WITHOUT its value.
//
// Distinct from ProjectEnvVar / ServiceEnvVar (which carry Content) by design — used when ZCP
// verifies the presence of user-set external secrets in a target prod
// project without ever reading those secrets through MCP. Pinned by
// P-LP-5 invariant.
type EnvKey struct {
	ID        string
	Key       string
	Sensitive bool
}

// ErrEmptyLaunchKey is returned by NewProjectAdminClient when launchKey is "".
var ErrEmptyLaunchKey = errors.New("project admin: launch-window API key required")

// ErrNoClientResolved is returned when the supplied key authenticates but
// resolves to no client / org. Account-wide scope is required for
// CreateAndImportProject.
var ErrNoClientResolved = errors.New("project admin: launch-window key resolves to no client (org-wide access required)")

// ErrClientClosed is returned by ProjectAdminClient methods after Close().
var ErrClientClosed = errors.New("project admin: client closed")

// NewProjectAdminClient constructs a ProjectAdminClient from a launch-window
// key. The key is held internally by the wrapped ZeropsClient (inside its
// SDK handler's authenticated transport); this struct never copies it into
// a separately addressable field.
//
// Behavior:
//   - Empty launchKey → ErrEmptyLaunchKey.
//   - Validates the key by calling GetUserInfo (one cheap GET). Invalid or
//     expired keys surface the SDK's mapped error.
//   - Discovers clientID from the response — needed for CreateAndImportProject.
//   - Returns ErrNoClientResolved if the key authenticates but lacks org access.
//
// Caller MUST defer Close() on the returned client. Empty apiHost falls
// back to platform.defaultAPIHost via the underlying NewZeropsClient.
func NewProjectAdminClient(launchKey, apiHost string) (ProjectAdminClient, error) {
	if launchKey == "" {
		return nil, ErrEmptyLaunchKey
	}
	z, err := NewZeropsClient(launchKey, apiHost)
	if err != nil {
		return nil, fmt.Errorf("project admin: construct client: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	info, err := z.GetUserInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("project admin: validate key: %w", err)
	}
	if info.ID == "" {
		return nil, ErrNoClientResolved
	}
	return &projectAdminClient{
		zerops:       z,
		clientID:     info.ID,
		clientUserID: info.ClientUserID,
	}, nil
}

// projectAdminClient implements ProjectAdminClient against the live SDK.
//
// The wrapped *ZeropsClient holds the launch-window key inside its
// authenticated SDK handler — there is no separate token field on this
// struct, so the key is unreachable from any reflection / String() /
// JSON path on projectAdminClient.
type projectAdminClient struct {
	zerops       *ZeropsClient
	clientID     string
	clientUserID string
}

// ClientUserID implements ProjectAdminClient. Returns the captured
// clientUserId from construction-time validation.
func (p *projectAdminClient) ClientUserID() string {
	return p.clientUserID
}

// GrantSelfRole implements ProjectAdminClient. Reads existing roles,
// appends the new entry, writes back the merged list. PutClientUserRoles
// is a full-replace endpoint; a naive write would wipe other project
// roles, hence the read-merge-write pattern.
func (p *projectAdminClient) GrantSelfRole(ctx context.Context, projectID string, roleCode string) error {
	if p.zerops == nil {
		return ErrClientClosed
	}
	if p.clientUserID == "" {
		return errors.New("project admin: clientUserID empty; cannot grant self role")
	}
	if projectID == "" {
		return errors.New("project admin: projectID empty; cannot grant self role")
	}

	pathParam := path.ClientUserId{Id: uuid.ClientUserId(p.clientUserID)}

	// 1) Read existing roles to preserve them.
	getResp, err := p.zerops.handler.GetClientUserRoles(ctx, pathParam)
	if err != nil {
		return fmt.Errorf("get current roles: %w", mapSDKError(err, "client-user"))
	}
	current, err := getResp.Output()
	if err != nil {
		return fmt.Errorf("get current roles output: %w", mapSDKError(err, "client-user"))
	}

	// 2) Build merged list: existing roles + (projectID, roleCode).
	merged := make(body.ClientUserProjectRoleListProjectRoleList, 0, len(current.ProjectRoleList)+1)
	already := false
	for _, r := range current.ProjectRoleList {
		if r.ProjectId.TypedString().String() == projectID {
			already = true
			// Replace the entry with the new role.
			merged = append(merged, body.ClientUserProjectRole{
				ProjectId: uuid.ProjectId(projectID),
				RoleCode:  enum.ClientUserRoleCodeEnum(roleCode),
			})
			continue
		}
		merged = append(merged, body.ClientUserProjectRole{
			ProjectId: r.ProjectId,
			RoleCode:  r.RoleCode,
		})
	}
	if !already {
		merged = append(merged, body.ClientUserProjectRole{
			ProjectId: uuid.ProjectId(projectID),
			RoleCode:  enum.ClientUserRoleCodeEnum(roleCode),
		})
	}

	// 3) Write back the merged list. The SDK reports API-level failures
	// (4xx/5xx) only through the response's Output()/Err() — the function's
	// error return covers transport failures alone. Checking both mirrors
	// the GetClientUserRoles step above; without the Output() check a
	// rejected role write returns nil and the caller believes ADMIN was
	// granted.
	bodyParam := body.ClientUserProjectRoleList{ProjectRoleList: merged}
	putResp, err := p.zerops.handler.PutClientUserRoles(ctx, pathParam, bodyParam)
	if err != nil {
		return fmt.Errorf("put roles: %w", mapSDKError(err, "client-user"))
	}
	if _, err := putResp.Output(); err != nil {
		return fmt.Errorf("put roles output: %w", mapSDKError(err, "client-user"))
	}
	return nil
}

// CreateAndImportProject implements ProjectAdminClient.
func (p *projectAdminClient) CreateAndImportProject(ctx context.Context, yaml string, opts CreateOpts) (*ImportResult, error) {
	if p.zerops == nil {
		return nil, ErrClientClosed
	}
	_ = opts // CreateOpts.Location / Tags are encoded into the yaml body by the caller (LaunchBundleBuilder); kept on the signature for future expansion if the API gains separate fields.

	pathParam := path.ClientId{Id: uuid.ClientId(p.clientID)}
	bodyParam := body.ProjectImport{
		Yaml: zgotypes.Text(yaml),
	}
	resp, err := p.zerops.handler.PostClientProjectImport(ctx, pathParam, bodyParam)
	if err != nil {
		return nil, mapSDKError(err, "project")
	}
	out, err := resp.Output()
	if err != nil {
		return nil, mapSDKError(err, "project")
	}

	result := &ImportResult{
		ProjectID:   out.ProjectId.TypedString().String(),
		ProjectName: out.ProjectName.String(),
	}
	for _, stack := range out.ServiceStacks {
		imported := ImportedServiceStack{
			ID:   stack.Id.TypedString().String(),
			Name: stack.Name.String(),
		}
		if stack.Error != nil {
			imported.Error = &APIError{
				Code:    stack.Error.Code.String(),
				Message: stack.Error.Message.String(),
				Meta:    decodeAPIMetaJSON(stack.Error.Meta.Native()),
			}
		}
		for _, proc := range stack.Processes {
			imported.Processes = append(imported.Processes, mapProcess(proc))
		}
		result.ServiceStacks = append(result.ServiceStacks, imported)
	}
	return result, nil
}

// ListServices implements ProjectAdminClient — same semantics as Client.ListServices
// but uses the admin transport.
func (p *projectAdminClient) ListServices(ctx context.Context, projectID string) ([]ServiceStack, error) {
	if p.zerops == nil {
		return nil, ErrClientClosed
	}
	return p.zerops.ListServices(ctx, projectID)
}

// GetServiceEnvKeys implements ProjectAdminClient — returns EnvKey entries
// stripped of values. Wraps the existing GetServiceEnv but discards Content.
func (p *projectAdminClient) GetServiceEnvKeys(ctx context.Context, serviceID string) ([]EnvKey, error) {
	if p.zerops == nil {
		return nil, ErrClientClosed
	}
	vars, err := p.zerops.GetServiceEnv(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	return stripServiceEnvValues(vars), nil
}

// GetProjectEnvKeys implements ProjectAdminClient — returns project-level
// EnvKey entries stripped of values.
func (p *projectAdminClient) GetProjectEnvKeys(ctx context.Context, projectID string) ([]EnvKey, error) {
	if p.zerops == nil {
		return nil, ErrClientClosed
	}
	vars, err := p.zerops.GetProjectEnv(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return stripProjectEnvValues(vars), nil
}

// GetProcess implements ProjectAdminClient — same semantics as Client.GetProcess.
func (p *projectAdminClient) GetProcess(ctx context.Context, processID string) (*Process, error) {
	if p.zerops == nil {
		return nil, ErrClientClosed
	}
	return p.zerops.GetProcess(ctx, processID)
}

// DeleteProject implements ProjectAdminClient. Wraps SDK DeleteProject.
func (p *projectAdminClient) DeleteProject(ctx context.Context, projectID string) (*Process, error) {
	if p.zerops == nil {
		return nil, ErrClientClosed
	}
	pathParam := path.ProjectId{Id: uuid.ProjectId(projectID)}
	resp, err := p.zerops.handler.DeleteProject(ctx, pathParam)
	if err != nil {
		return nil, mapSDKError(err, "project")
	}
	out, err := resp.Output()
	if err != nil {
		return nil, mapSDKError(err, "project")
	}
	proc := mapProcess(out)
	return &proc, nil
}

// Close zeros the wrapped client reference so the SDK handler (which holds
// the launch-window key) becomes unreachable and eligible for GC. Subsequent
// method calls return ErrClientClosed.
func (p *projectAdminClient) Close() {
	p.zerops = nil
	p.clientID = ""
}

// GetServiceStackIntegrationStatus implements ProjectAdminClient. Maps
// HTTP 400 with `code: noExternalRepositoryIntegration` to
// IntegrationNotConfigured per Phase A B.1 finding. Other errors propagate.
func (p *projectAdminClient) GetServiceStackIntegrationStatus(ctx context.Context, serviceStackID string) (IntegrationStatus, error) {
	if p.zerops == nil {
		return IntegrationStatus{}, ErrClientClosed
	}
	if serviceStackID == "" {
		return IntegrationStatus{}, errors.New("project admin: serviceStackID empty; cannot read integration status")
	}

	pathParam := path.ServiceStackId{Id: uuid.ServiceStackId(serviceStackID)}
	resp, err := p.zerops.handler.GetServiceStackExternalRepositoryIntegrationStatus(ctx, pathParam)
	if err != nil {
		return IntegrationStatus{}, fmt.Errorf("get integration status: %w", mapSDKError(err, "service-stack"))
	}
	out, err := resp.Output()
	if err != nil {
		mapped := mapSDKError(err, "service-stack")
		// Phase A B.1: the platform expresses "no integration yet" as HTTP 400
		// with code noExternalRepositoryIntegration. Treat as state read.
		var pe *PlatformError
		if errors.As(mapped, &pe) && pe.APICode == apiCodeNoExternalRepositoryIntegration {
			return IntegrationStatus{State: IntegrationNotConfigured}, nil
		}
		return IntegrationStatus{}, fmt.Errorf("get integration status output: %w", mapped)
	}
	return mapIntegrationOutput(out.GithubIntegration, out.GitlabIntegration), nil
}

// mapIntegrationOutput maps the SDK's external-repository-integration output
// (one of github / gitlab non-nil) onto IntegrationStatus. If both are nil
// (defensive — should never happen on a 200 response), returns
// IntegrationNotConfigured.
func mapIntegrationOutput(gh *output.GithubIntegration, gl *output.GitlabIntegration) IntegrationStatus {
	if gh != nil {
		branchName, _ := gh.BranchName.Get()
		tagRegex, _ := gh.TagRegex.Get()
		zSetup, _ := gh.ZeropsYamlSetup.Get()
		return IntegrationStatus{
			State:              IntegrationConfigured,
			Provider:           IntegrationProviderGitHub,
			RepositoryFullName: gh.RepositoryFullName.Native(),
			EventType:          IntegrationEventType(gh.EventType.Native()),
			BranchName:         branchName.Native(),
			TagRegex:           tagRegex.Native(),
			ZeropsYamlSetup:    zSetup.Native(),
			IsActive:           gh.IsActive.Native(),
		}
	}
	if gl != nil {
		branchName, _ := gl.BranchName.Get()
		tagRegex, _ := gl.TagRegex.Get()
		zSetup, _ := gl.ZeropsYamlSetup.Get()
		return IntegrationStatus{
			State:              IntegrationConfigured,
			Provider:           IntegrationProviderGitLab,
			RepositoryFullName: gl.RepositoryFullName.Native(),
			EventType:          IntegrationEventType(gl.EventType.Native()),
			BranchName:         branchName.Native(),
			TagRegex:           tagRegex.Native(),
			ZeropsYamlSetup:    zSetup.Native(),
			IsActive:           gl.IsActive.Native(),
		}
	}
	return IntegrationStatus{State: IntegrationNotConfigured}
}

// stripEnvValues maps []EnvVar (with Content) to []EnvKey (without Content).
// Centralizing the strip ensures GetServiceEnvKeys / GetProjectEnvKeys can
// never accidentally surface values — P-LP-5 invariant. Scope-specific
// since Phase 1a (SDK ProjectEnv carries Editable; ServiceStackEnv does
// not — see plans/research/env-types-investigation-2026-05-14.md). The
// Sensitive field is now sourced directly from the SDK rather than the
// Phase B stub.
func stripServiceEnvValues(vars []ServiceEnvVar) []EnvKey {
	out := make([]EnvKey, 0, len(vars))
	for _, v := range vars {
		out = append(out, EnvKey{
			ID:        v.ID,
			Key:       v.Key,
			Sensitive: v.Sensitive,
		})
	}
	return out
}

func stripProjectEnvValues(vars []ProjectEnvVar) []EnvKey {
	out := make([]EnvKey, 0, len(vars))
	for _, v := range vars {
		out = append(out, EnvKey{
			ID:        v.ID,
			Key:       v.Key,
			Sensitive: v.Sensitive,
		})
	}
	return out
}
