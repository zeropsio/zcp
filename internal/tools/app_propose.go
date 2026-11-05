package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// A Mate acts on its application's stage and production only by proposing
// (docs/spec-mate.md §2.11): its token reaches neither, so the person
// approves a card and their own client makes the change.

// AppProposeServiceInput is zerops_propose_service's input.
type AppProposeServiceInput struct {
	Action  string `json:"action"  jsonschema:"restart, start or stop."`
	Project string `json:"project" jsonschema:"The environment: its projectId or name from zerops_observe action=environments."`
	Service string `json:"service" jsonschema:"The service's hostname, from zerops_observe action=status."`
	Reason  string `json:"reason"  jsonschema:"Why, in one sentence, for the person deciding."`
}

// AppProposeEnvInput is zerops_propose_env's input: a plain value, never a
// secret — the person keeps secrets in Mate's Vault.
type AppProposeEnvInput struct {
	Project string `json:"project"           jsonschema:"The environment: its projectId or name from zerops_observe action=environments."`
	Scope   string `json:"scope"             jsonschema:"project (every app there reads it) or service (one service's own)."`
	Service string `json:"service,omitempty" jsonschema:"The service's hostname when scope is service."`
	Key     string `json:"key"               jsonschema:"The variable's name."`
	Value   string `json:"value"             jsonschema:"A plain value, never a secret."`
	Reason  string `json:"reason"            jsonschema:"Why, in one sentence, for the person deciding."`
}

// AppProposeRevealInput is zerops_propose_reveal's input: where a hidden
// value lives and why the person wants it, never the value.
type AppProposeRevealInput struct {
	Environment string `json:"environment"       jsonschema:"stage, production, or mate for this Mate's own project."`
	Project     string `json:"project,omitempty" jsonschema:"Which stage or production, by its projectId or name from zerops_observe action=environments, when there is more than one."`
	Service     string `json:"service,omitempty" jsonschema:"The service's hostname when the value is that service's own; omit for a project value."`
	Key         string `json:"key"               jsonschema:"The variable's name only."`
	Reason      string `json:"reason"            jsonschema:"Why the person wants to see it, in one sentence, for the card. Never a value."`
}

// appEnvRestartNote follows a variable's proposal: a running service keeps
// the environment it started with.
const appEnvRestartNote = " A running service reads the new value only after a restart."

// RegisterAppPropose registers a Mate's proposal tools for its application's
// stage and production, checked against HQ's observation, and its reveal of a
// hidden value there or in its own project. A Mate's client
// draws the card, so they are Mate-gated like every mate-shaped effect (§2.0).
func RegisterAppPropose(srv *mcp.Server, httpClient hq.Doer, projectID string, rt runtime.Info) {
	if !rt.InContainer || !rt.MateEnabled {
		return
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	src := observedApp{doer: httpClient, projectID: projectID, rt: rt}

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_propose_service",
		Description: "Propose restarting, starting or stopping a service in your application's stage or production. Runs nothing: the person approves or declines it on a card, and their client does it.",
		Annotations: orgPropose("Propose a service action"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in AppProposeServiceInput) (*mcp.CallToolResult, any, error) {
		kind, ok := orgServiceActions[strings.TrimSpace(in.Action)]
		if !ok {
			return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
				fmt.Sprintf("%q is not a service action a Mate can propose", in.Action),
				"action is restart, start or stop")), nil, nil
		}
		return appAnswer(ops.ProposeAppAction(ctx, src, uuid.NewString, ops.OrgActionInput{
			Kind: kind, Project: in.Project, Service: in.Service, Reason: in.Reason,
		}))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_propose_env",
		Description: "Propose setting a plain value in your application's stage or production, for every app there or one service's own. Runs nothing: the person approves or declines it on a card, and their client sets it.",
		Annotations: orgPropose("Propose a variable"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in AppProposeEnvInput) (*mcp.CallToolResult, any, error) {
		result, err := ops.ProposeAppAction(ctx, src, uuid.NewString, ops.OrgActionInput{
			Kind: ops.ProposalEnvSet, Project: in.Project, Service: in.Service, Reason: in.Reason,
			Scope: in.Scope, Key: in.Key, Value: in.Value,
		})
		if result != nil {
			result.Note += appEnvRestartNote
		}
		return appAnswer(result, err)
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_propose_reveal",
		Description: "Propose showing the person a hidden value you cannot read: a password or key in your application's stage or production, or in this Mate. Shows you nothing: the person reveals it on a card, on their own screen, and you are not told. Never print or paste a secret value.",
		Annotations: orgPropose("Propose a reveal"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in AppProposeRevealInput) (*mcp.CallToolResult, any, error) {
		return appAnswer(ops.ProposeReveal(ctx, src, uuid.NewString, projectID, ops.RevealInput{
			Environment: in.Environment, Project: in.Project, Service: in.Service, Key: in.Key, Reason: in.Reason,
		}))
	})
}

// appAnswer renders a proposal, an observation HQ refused or could not make
// in zerops_observe's words, or a plain refusal.
func appAnswer(result *ops.OrgProposalResult, err error) (*mcp.CallToolResult, any, error) {
	var unavailable *hq.UnavailableError
	var refused *hq.RefusedError
	if errors.As(err, &unavailable) || errors.As(err, &refused) || errors.Is(err, hq.ErrNoObserveCredential) {
		return readFailure(err, func(message string) *mcp.CallToolResult {
			return observeFailure(message + "; fix it and propose again.")
		}), nil, nil
	}
	return orgAnswer(result, err)
}

// observedApp reads the Mate's application through HQ's observation, one
// bounded request at a time (HQ limits a Mate's concurrent observations).
type observedApp struct {
	doer      hq.Doer
	projectID string
	rt        runtime.Info
}

func (a observedApp) observe(ctx context.Context, in hq.ObservationRequest, out any) error {
	id := hqIdentity(a.rt)
	if id.ProjectID != a.projectID {
		return errors.New("the configured HQ source belongs to another project")
	}
	client, err := hq.OpenObservation(a.doer, id)
	if err != nil {
		return fmt.Errorf("HQ could not be addressed: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	body, err := client.Observe(ctx, in)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("HQ's observation could not be read: %w", err)
	}
	return nil
}

func (a observedApp) Environments(ctx context.Context) ([]ops.AppEnvironment, error) {
	var out struct {
		Environments []struct {
			ProjectID string `json:"projectId"`
			Name      string `json:"name"`
			Kind      string `json:"kind"`
		} `json:"environments"`
	}
	if err := a.observe(ctx, hq.ObservationRequest{Action: observeEnvironments}, &out); err != nil {
		return nil, err
	}
	envs := make([]ops.AppEnvironment, 0, len(out.Environments))
	for _, e := range out.Environments {
		envs = append(envs, ops.AppEnvironment{ProjectID: e.ProjectID, Name: e.Name, Kind: e.Kind})
	}
	return envs, nil
}

func (a observedApp) Services(ctx context.Context, projectID string) ([]platform.ServiceStack, error) {
	var out struct {
		Services []struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"services"`
	}
	if err := a.observe(ctx, hq.ObservationRequest{Action: observeStatus, ProjectID: projectID}, &out); err != nil {
		return nil, err
	}
	services := make([]platform.ServiceStack, 0, len(out.Services))
	for _, s := range out.Services {
		services = append(services, platform.ServiceStack{ID: s.ID, Name: s.Name, Status: s.Status})
	}
	return services, nil
}
