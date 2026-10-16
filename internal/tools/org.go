package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// Org mode's tools (docs/spec-mate.md §2.11): the Chief looks across the
// whole organization through the org's read-only token, and acts only by
// proposing — a proposal runs nothing; the person approves or declines it on
// a card and their own client makes the change.

// OrgProjectInput names a project.
type OrgProjectInput struct {
	Project string `json:"project" jsonschema:"The project: its id, its full name, or its Mate's name."`
}

// OrgLogsInput is zerops_org_logs's input.
type OrgLogsInput struct {
	Project  string `json:"project"            jsonschema:"The project: its id, its full name, or its Mate's name."`
	Service  string `json:"service"            jsonschema:"The service's hostname."`
	Kind     string `json:"kind,omitempty"     jsonschema:"runtime (default) or build (the service's newest build)."`
	Since    string `json:"since,omitempty"    jsonschema:"How far back: 30m, 6h, 2d or RFC3339. Runtime default 1h; build default its whole log."`
	Severity string `json:"severity,omitempty" jsonschema:"Lowest severity: error, warning, info, debug."`
	Search   string `json:"search,omitempty"   jsonschema:"Only lines containing this text."`
	Limit    int    `json:"limit,omitempty"    jsonschema:"Newest lines to return (default 100, max 1000)."`
}

// OrgFailuresInput is zerops_org_failures's input.
type OrgFailuresInput struct {
	Since string `json:"since,omitempty" jsonschema:"How far back: 30m, 24h (default), 7d or RFC3339."`
}

// OrgEnvInput is zerops_org_env's input.
type OrgEnvInput struct {
	Project string   `json:"project"           jsonschema:"The project: its id, its full name, or its Mate's name."`
	Service string   `json:"service,omitempty" jsonschema:"A service's hostname for its variables; omit for the project's."`
	Reveal  []string `json:"reveal,omitempty"  jsonschema:"Keys whose values to show. Every other value stays masked."`
}

// OrgProcessesInput is zerops_org_processes's input.
type OrgProcessesInput struct {
	Project string `json:"project,omitempty" jsonschema:"One project (id, name or Mate's name); omit for the whole organization."`
	Limit   int    `json:"limit,omitempty"   jsonschema:"Newest processes to return (default 30, max 200)."`
}

// OrgCostsInput is zerops_org_costs's input.
type OrgCostsInput struct {
	Project string `json:"project,omitempty" jsonschema:"One project (id, name or Mate's name) for its services' costs; omit for every project's."`
	From    string `json:"from,omitempty"    jsonschema:"A range's first day (2026-10-01, UTC), for its total, what it is made of and each day's; omit for today, this and last month, this and last year."`
	Till    string `json:"till,omitempty"    jsonschema:"The range's last day, inclusive (default today). At most 31 days from from."`
}

// OrgUsageInput is zerops_org_usage's input.
type OrgUsageInput struct {
	Project string `json:"project"           jsonschema:"The project: its id, its full name, or its Mate's name."`
	Service string `json:"service,omitempty" jsonschema:"One service's hostname, for its use step by step; omit for every service's."`
	Since   string `json:"since,omitempty"   jsonschema:"How far back: 6h, 24h (default), 7d; at most 31d. Hourly up to 48h, daily (whole UTC days) past that."`
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
func RegisterOrg(srv *mcp.Server, src ops.OrgSource, logs platform.LogFetcher, newID func() string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_org_overview",
		Description: "Who you act for (their name, organization role and finance rights), then every project of the organization, grouped: Mates (with each Mate's name), HQ, production and stage environments by application, others, and you. Status, service count, and services stopped or failing. Start here.",
		Annotations: orgLook("Organization overview"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		return orgAnswer(ops.OrgOverview(ctx, src))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_org_services",
		Description: "A project's services: hostname, type, status, ports, domains (subdomain and custom), and public ports on the project's own IP with their firewall.",
		Annotations: orgLook("Project services"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in OrgProjectInput) (*mcp.CallToolResult, any, error) {
		return orgAnswer(ops.OrgServices(ctx, src, in.Project))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_org_logs",
		Description: "A service's log in any project of the organization: its runtime log, or its newest build's log (kind=build). Filter by since, severity and search; newest lines last.",
		Annotations: orgLook("Service logs"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in OrgLogsInput) (*mcp.CallToolResult, any, error) {
		return orgAnswer(ops.OrgLogs(ctx, src, logs, ops.OrgLogsInput{
			Project: in.Project, Service: in.Service, Kind: in.Kind, Since: in.Since,
			Severity: in.Severity, Search: in.Search, Limit: min(in.Limit, platform.LogLimitMax),
		}))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_org_failures",
		Description: "Sweep the whole organization for failures since a time: failed builds, deploys and processes, newest first, then every service stopped or failing now. Spent build containers are left out — they rest stopped by design.",
		Annotations: orgLook("Organization failures"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in OrgFailuresInput) (*mcp.CallToolResult, any, error) {
		return orgAnswer(ops.OrgFailures(ctx, src, in.Since))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_org_env",
		Description: "A project's or a service's variables. Values are masked; name a key in reveal to read its value, and only when the person needs it.",
		Annotations: orgLook("Project and service variables"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in OrgEnvInput) (*mcp.CallToolResult, any, error) {
		return orgAnswer(ops.OrgEnv(ctx, src, in.Project, in.Service, in.Reveal))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_org_processes",
		Description: "Recent platform processes — builds, deploys, restarts, env changes — across the organization or in one project, newest first, with who started each and why one failed.",
		Annotations: orgLook("Platform processes"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in OrgProcessesInput) (*mcp.CallToolResult, any, error) {
		return orgAnswer(ops.OrgProcesses(ctx, src, in.Project, in.Limit))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_org_costs",
		Description: "The organization's costs in its billing currency: totals for today, yesterday, 7 and 30 days, this and last month and year, and each project's; or one project's services'. With from, a range day by day per project, or per service of one project with its parts (CPU, RAM, disk, backups, builds, egress). Outside the org's finances, only the person's projects.",
		Annotations: orgLook("Costs"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in OrgCostsInput) (*mcp.CallToolResult, any, error) {
		return orgAnswer(ops.OrgCosts(ctx, src, ops.OrgCostsInput{Project: in.Project, From: in.From, Till: in.Till, Now: time.Now()}))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_org_usage",
		Description: "A project's services' resource use against their limits: containers, dedicated CPU cores or shared vCPUs, RAM and disk in GB — in the last step, at peak and on average since a time. One service's step by step. For a slow or crashing service, or a cost that grew.",
		Annotations: orgLook("Resource use"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in OrgUsageInput) (*mcp.CallToolResult, any, error) {
		return orgAnswer(ops.OrgUsage(ctx, src, ops.OrgUsageInput{Project: in.Project, Service: in.Service, Since: in.Since, Now: time.Now()}))
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
		Description: "Propose who has access to an application or one project: each person (email or name) at full, read, none (hidden) or remove (their own grant dropped). Runs nothing: the person approves it on a card, and their client writes it with their own Zerops account. Only an organization owner or admin changes access.",
		Annotations: orgPropose("Propose access"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in OrgProposeAccessInput) (*mcp.CallToolResult, any, error) {
		return orgAnswer(ops.ProposeOrgAction(ctx, src, newID, ops.OrgActionInput{
			Kind: ops.ProposalAccessSet, Project: in.Project, People: in.People, Reason: in.Reason,
		}))
	})
}
