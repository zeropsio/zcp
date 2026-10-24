package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// Org mode's tools (docs/spec-mate.md §2.11): the Chief looks across the
// whole organization through HQ's overview, and acts only by
// proposing — a proposal runs nothing; the person approves or declines it on
// a card and their own client makes the change.

// OrgProjectInput names a project.
type OrgProjectInput struct {
	Project string `json:"project" jsonschema:"The project: its id, its full name, or its Mate's name."`
}

// OrgProposeServiceInput is zerops_org_propose_service's input.
type OrgProposeServiceInput struct {
	Action  string `json:"action"  jsonschema:"restart, start or stop."`
	Project string `json:"project" jsonschema:"The project: its id, its full name, or its Mate's name."`
	Service string `json:"service" jsonschema:"The service's hostname."`
	Reason  string `json:"reason"  jsonschema:"Why, in one sentence, for the person deciding."`
}

// OrgProposeEnvInput is zerops_org_propose_env's input.
type OrgProposeEnvInput struct {
	Project string `json:"project"           jsonschema:"The project: its id, its full name, or its Mate's name."`
	Scope   string `json:"scope"             jsonschema:"project or service."`
	Service string `json:"service,omitempty" jsonschema:"The service's hostname when scope is service."`
	Key     string `json:"key"               jsonschema:"The variable's name."`
	Value   string `json:"value"             jsonschema:"The value to set."`
	Secret  bool   `json:"secret,omitempty"  jsonschema:"Mark the value secret (masked for read-only roles)."`
	Reason  string `json:"reason"            jsonschema:"Why, in one sentence, for the person deciding."`
}

// OrgProposeMessageInput is zerops_org_propose_message's input.
type OrgProposeMessageInput struct {
	To              string `json:"to"                        jsonschema:"The Mate: its name or its project id."`
	Words           string `json:"words"                     jsonschema:"What to tell the Mate — the ask, in the person's terms."`
	AttachmentTitle string `json:"attachmentTitle,omitempty" jsonschema:"A title for an attached excerpt, e.g. 'medusa errors, last hour'."`
	AttachmentText  string `json:"attachmentText,omitempty"  jsonschema:"The excerpt itself (a log excerpt, a finding); the lines that matter."`
	Reason          string `json:"reason"                    jsonschema:"Why this belongs to that Mate, in one sentence."`
}

// OrgProposeDomainInput is zerops_org_propose_domain's input.
type OrgProposeDomainInput struct {
	Project string   `json:"project" jsonschema:"The project, usually an application's production: its id, its full name, or its Mate's name."`
	Service string   `json:"service" jsonschema:"The hostname of the service that serves the web."`
	Domains []string `json:"domains" jsonschema:"The person's domain names, bare (shop.example.com), its www. name when they want both, and *.shop.example.com for every subdomain."`
	Port    int      `json:"port"    jsonschema:"The service's web port the domains point at (zerops_org_services lists its ports)."`
	Reason  string   `json:"reason"  jsonschema:"Why, in one sentence, for the person deciding."`
}

// OrgProposeAccessInput is zerops_org_propose_access's input.
type OrgProposeAccessInput struct {
	Project string                `json:"project" jsonschema:"An application's name (every one of its projects at once: production, stage and its Mates), or one project: its id, its full name, or its Mate's name."`
	People  []ops.OrgAccessPerson `json:"people"  jsonschema:"Each person once, with the level they get there."`
	Reason  string                `json:"reason"  jsonschema:"Why, in one sentence, for the person deciding."`
}

var orgServiceActions = map[string]string{
	"restart": ops.ProposalServiceRestart,
	"start":   ops.ProposalServiceStart,
	"stop":    ops.ProposalServiceStop,
}

func orgLook(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: boolPtr(false)}
}

// A proposal changes nothing on the platform, so it reads as read-only; it
// is not idempotent — each call is a new card.
func orgPropose(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, DestructiveHint: boolPtr(false)}
}

func orgAnswer[T any](result T, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		return convertError(err), nil, nil
	}
	return jsonResult(result), nil, nil
}

// RegisterOrg registers org mode's look and propose tools over src. newID
// mints a proposal's id.
func RegisterOrg(srv *mcp.Server, src ops.OrgSource, newID func() string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_org_overview",
		Description: "Every project of the organization, grouped: Mates (with each Mate's name), HQ, production and stage environments by application, others, and you. Status, service count, and services stopped or failing; then the organization's people with their roles. Start here.",
		Annotations: orgLook("Organization overview"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		return orgAnswer(ops.OrgOverview(ctx, src))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_org_services",
		Description: "A project's services: hostname, type, status and ports.",
		Annotations: orgLook("Project services"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in OrgProjectInput) (*mcp.CallToolResult, any, error) {
		return orgAnswer(ops.OrgServices(ctx, src, in.Project))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_org_propose_service",
		Description: "Propose restarting, starting or stopping a service. Runs nothing: the person approves or declines it on a card, and their client does it.",
		Annotations: orgPropose("Propose a service action"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in OrgProposeServiceInput) (*mcp.CallToolResult, any, error) {
		kind, ok := orgServiceActions[strings.TrimSpace(in.Action)]
		if !ok {
			return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
				fmt.Sprintf("%q is not a service action org mode can propose", in.Action),
				"action is restart, start or stop")), nil, nil
		}
		return orgAnswer(ops.ProposeOrgAction(ctx, src, newID, ops.OrgActionInput{
			Kind: kind, Project: in.Project, Service: in.Service, Reason: in.Reason,
		}))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_org_propose_env",
		Description: "Propose setting a project or service variable, optionally secret. Runs nothing: the person approves or declines it on a card, and their client sets it.",
		Annotations: orgPropose("Propose a variable"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in OrgProposeEnvInput) (*mcp.CallToolResult, any, error) {
		return orgAnswer(ops.ProposeOrgAction(ctx, src, newID, ops.OrgActionInput{
			Kind: ops.ProposalEnvSet, Project: in.Project, Service: in.Service, Reason: in.Reason,
			Scope: in.Scope, Key: in.Key, Value: in.Value, Secret: in.Secret,
		}))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_org_propose_message",
		Description: "Propose handing work to a project's Mate: your words, optionally with an excerpt such as log lines. Runs nothing: the person approves it on a card, and it lands in that Mate's conversation.",
		Annotations: orgPropose("Propose a message to a Mate"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in OrgProposeMessageInput) (*mcp.CallToolResult, any, error) {
		return orgAnswer(ops.ProposeOrgAction(ctx, src, newID, ops.OrgActionInput{
			Kind: ops.ProposalMateMessage, To: in.To, Words: in.Words, Reason: in.Reason,
			AttachmentTitle: in.AttachmentTitle, AttachmentText: in.AttachmentText,
		}))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_org_propose_domain",
		Description: "Propose putting a project, usually production, on the person's own domain, pointed at a service's web port. Runs nothing: the person approves it on a card, their client adds the domain, and they then add the DNS records it shows at their DNS provider.",
		Annotations: orgPropose("Propose a domain"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in OrgProposeDomainInput) (*mcp.CallToolResult, any, error) {
		return orgAnswer(ops.ProposeOrgAction(ctx, src, newID, ops.OrgActionInput{
			Kind: ops.ProposalDomainAdd, Project: in.Project, Service: in.Service, Reason: in.Reason,
			Domains: in.Domains, Port: in.Port,
		}))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_org_propose_access",
		Description: "Propose who has access to an application or one project: each person (email or name) at full, read, none (hidden) or remove (their own grant dropped). Runs nothing: the person approves it on a card, and their client writes it with their own Zerops account. Zerops refuses it on Approve unless the person may change access.",
		Annotations: orgPropose("Propose access"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in OrgProposeAccessInput) (*mcp.CallToolResult, any, error) {
		return orgAnswer(ops.ProposeOrgAction(ctx, src, newID, ops.OrgActionInput{
			Kind: ops.ProposalAccessSet, Project: in.Project, People: in.People, Reason: in.Reason,
		}))
	})
}
