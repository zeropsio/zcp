package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/zeropsio/zcp/internal/platform"
)

// Proposal kinds — the only changes org mode can propose. There is no delete,
// no token, no scaling: what is not a kind here cannot be proposed.
const (
	ProposalServiceRestart = "service.restart"
	ProposalServiceStart   = "service.start"
	ProposalServiceStop    = "service.stop"
	ProposalEnvSet         = "env.set"
	ProposalMateMessage    = "mate.message"
	ProposalDomainAdd      = "domain.add"
	ProposalAccessSet      = "access.set"
)

// ProposalNote is what every proposal result says about itself.
const ProposalNote = "Nothing has run. The person approves or declines it on the card, which carries your reason and how it ends: never restate it or ask them to approve, and don't propose it again unless they ask. You are not told the outcome: read the state again before saying it ran."

// The scopes a variable lives at.
const (
	OrgScopeProject = "project"
	OrgScopeService = "service"
)

// orgAttachmentMax bounds an attachment's text: an excerpt for a card, not a
// whole log.
const orgAttachmentMax = 64 << 10

// OrgActionInput is what an act tool asks to propose.
type OrgActionInput struct {
	Kind    string
	Project string
	Service string
	Reason  string
	// env.set
	Scope  string
	Key    string
	Value  string
	Secret bool
	// mate.message
	To              string
	Words           string
	AttachmentTitle string
	AttachmentText  string
	// domain.add
	Domains []string
	Port    int
	// access.set
	People []OrgAccessPerson
}

// OrgProposal is one change for the person to approve or decline
// (docs/spec-mate.md §2.11). Params is the kind's own object:
// {} for a service action, OrgEnvSetParams, OrgMateMessageParams,
// OrgDomainAddParams or OrgAccessSetParams.
type OrgProposal struct {
	ID          string          `json:"id"`
	Kind        string          `json:"kind"`
	Summary     string          `json:"summary"`
	Reason      string          `json:"reason"`
	ProjectID   string          `json:"projectId"`
	ProjectName string          `json:"projectName"`
	ServiceID   string          `json:"serviceId,omitempty"`
	ServiceName string          `json:"serviceName,omitempty"`
	Params      json.RawMessage `json:"params"`
}

// OrgProposalResult is the whole result of an act tool.
type OrgProposalResult struct {
	Proposal OrgProposal `json:"proposal"`
	Note     string      `json:"note"`
}

// OrgEnvSetParams are env.set's params.
type OrgEnvSetParams struct {
	Scope  string `json:"scope"`
	Key    string `json:"key"`
	Value  string `json:"value"`
	Secret bool   `json:"secret"`
}

// OrgMateAddress names the Mate a message goes to.
type OrgMateAddress struct {
	ProjectID string `json:"projectId"`
	MateName  string `json:"mateName"`
}

// OrgAttachment is an excerpt handed along with a message.
type OrgAttachment struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}

// OrgMateMessageParams are mate.message's params.
type OrgMateMessageParams struct {
	To         OrgMateAddress `json:"to"`
	Words      string         `json:"words"`
	Attachment *OrgAttachment `json:"attachment,omitempty"`
}

// OrgDomainAddParams are domain.add's params: the domains, put on one
// routing to the proposal's service at Port.
type OrgDomainAddParams struct {
	Domains []string `json:"domains"`
	Port    int      `json:"port"`
}

// ProposeOrgAction validates a change against what the organization holds
// and returns it as a proposal. It runs nothing: the person approves it on
// a card and their client makes the change with their own token.
func ProposeOrgAction(ctx context.Context, src OrgSource, newID func() string, in OrgActionInput) (*OrgProposalResult, error) {
	reason := strings.TrimSpace(in.Reason)
	switch in.Kind {
	case ProposalServiceRestart, ProposalServiceStart, ProposalServiceStop, ProposalEnvSet, ProposalMateMessage, ProposalDomainAdd, ProposalAccessSet:
	default:
		return nil, invalid(fmt.Sprintf("%q is not something org mode can propose", in.Kind),
			"Propose service.restart, service.start, service.stop, env.set, mate.message, domain.add or access.set")
	}
	if reason == "" {
		return nil, invalid("a proposal needs a reason", "Say in one sentence why, for the person deciding")
	}
	snap, err := readOrgSnapshot(ctx, src)
	if err != nil {
		return nil, err
	}

	var (
		p       *orgProject
		s       *platform.ServiceStack
		summary string
		params  any = struct{}{}
		// placeName names where the proposal acts where that is not p
		// alone: an application, for access to every one of its projects.
		placeName string
	)
	switch in.Kind {
	case ProposalServiceRestart, ProposalServiceStart, ProposalServiceStop:
		if p, err = snap.resolve(in.Project); err != nil {
			return nil, err
		}
		if s, err = p.ownService(in.Service); err != nil {
			return nil, err
		}
		if summary, err = serviceProposal(in.Kind, p, s); err != nil {
			return nil, err
		}

	case ProposalEnvSet:
		if p, err = snap.resolve(in.Project); err != nil {
			return nil, err
		}
		if summary, s, params, err = envProposal(p, in, p.ownService); err != nil {
			return nil, err
		}

	case ProposalMateMessage:
		if p, err = snap.resolve(in.To); err != nil {
			return nil, err
		}
		if p.Class.Kind == OrgKindChief {
			return nil, invalid(fmt.Sprintf("%s is the Chief: you, not a Mate to hand work to", p.Name), "zerops_org_overview lists the Mates")
		}
		if p.Class.Kind != OrgKindMate {
			return nil, invalid(fmt.Sprintf("%s is not a Mate", p.Name), "zerops_org_overview lists the Mates")
		}
		words := strings.TrimSpace(in.Words)
		if words == "" {
			return nil, invalid("mate.message needs words", "Say what the Mate should do")
		}
		attachment, err := orgAttachment(in.AttachmentTitle, in.AttachmentText)
		if err != nil {
			return nil, err
		}
		msg := OrgMateMessageParams{To: OrgMateAddress{ProjectID: p.ID, MateName: p.Class.MateName}, Words: words, Attachment: attachment}
		summary = fmt.Sprintf("Message %s in %s", p.Class.MateName, p.Name)
		params = msg

	case ProposalDomainAdd:
		if p, err = snap.resolve(in.Project); err != nil {
			return nil, err
		}
		if s, err = p.ownService(in.Service); err != nil {
			return nil, err
		}
		if err := domainPortFits(s, p.Name, in.Port); err != nil {
			return nil, err
		}
		domains, err := proposedDomains(in.Domains)
		if err != nil {
			return nil, err
		}
		summary = fmt.Sprintf("Put %s on %s", p.Name, domainList(domains))
		params = OrgDomainAddParams{Domains: domains, Port: in.Port}

	case ProposalAccessSet:
		access, err := proposeAccess(snap, in)
		if err != nil {
			return nil, err
		}
		p, summary, params, placeName = access.first, access.summary, access.params, access.name
	}

	result, err := proposalResult(newID, in.Kind, reason, p, s, summary, params)
	if err != nil {
		return nil, err
	}
	if placeName != "" {
		result.Proposal.ProjectName = placeName
	}
	return result, nil
}

// serviceProposal names a service action on p's service s, refusing one its
// state makes pointless.
func serviceProposal(kind string, p *orgProject, s *platform.ServiceStack) (string, error) {
	if err := serviceActionFits(kind, s, p.Name); err != nil {
		return "", err
	}
	verb := map[string]string{ProposalServiceRestart: "Restart", ProposalServiceStart: "Start", ProposalServiceStop: "Stop"}[kind]
	return fmt.Sprintf("%s %s in %s", verb, s.Name, p.Name), nil
}

// envProposal checks an env.set on p — the key, the scope and, for a service
// variable, the service found by service — and names it.
func envProposal(p *orgProject, in OrgActionInput, service func(string) (*platform.ServiceStack, error)) (string, *platform.ServiceStack, OrgEnvSetParams, error) {
	key := strings.TrimSpace(in.Key)
	if !envKeyPattern.MatchString(key) {
		return "", nil, OrgEnvSetParams{}, invalid(fmt.Sprintf("%q is not a variable name", key), "Letters, digits and underscores, not starting with a digit")
	}
	scope := strings.TrimSpace(in.Scope)
	hasService := strings.TrimSpace(in.Service) != ""
	switch {
	case scope == OrgScopeService && !hasService:
		return "", nil, OrgEnvSetParams{}, invalid("env.set on a service needs the service", "Pass service, or scope project")
	case scope == OrgScopeProject && hasService:
		return "", nil, OrgEnvSetParams{}, invalid("env.set with scope project names no service", "Drop service, or scope service")
	case scope != OrgScopeService && scope != OrgScopeProject:
		return "", nil, OrgEnvSetParams{}, invalid(fmt.Sprintf("unknown scope %q", scope), "scope is project or service")
	}
	secretWord := ""
	if in.Secret {
		secretWord = " (secret)"
	}
	params := OrgEnvSetParams{Scope: scope, Key: key, Value: in.Value, Secret: in.Secret}
	if scope == OrgScopeProject {
		return fmt.Sprintf("Set %s%s in %s", key, secretWord, p.Name), nil, params, nil
	}
	s, err := service(in.Service)
	if err != nil {
		return "", nil, OrgEnvSetParams{}, err
	}
	return fmt.Sprintf("Set %s%s on %s in %s", key, secretWord, s.Name, p.Name), s, params, nil
}

// proposalResult is the document a card is drawn from.
func proposalResult(newID func() string, kind, reason string, p *orgProject, s *platform.ServiceStack, summary string, params any) (*OrgProposalResult, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("encode proposal params: %w", err)
	}
	proposal := OrgProposal{ID: newID(), Kind: kind, Summary: summary, Reason: reason, ProjectID: p.ID, ProjectName: p.Name, Params: raw}
	if s != nil {
		proposal.ServiceID, proposal.ServiceName = s.ID, s.Name
	}
	return &OrgProposalResult{Proposal: proposal, Note: ProposalNote}, nil
}

// orgAttachment is a message's excerpt: complete (it has text) or absent,
// and an excerpt rather than a whole log.
func orgAttachment(title, text string) (*OrgAttachment, error) {
	title, text = strings.TrimSpace(title), strings.TrimSpace(text)
	if title == "" && text == "" {
		return nil, nil //nolint:nilnil // no attachment is a valid answer
	}
	if text == "" {
		return nil, invalid("the attachment has no text", "Pass attachmentText, or no attachment")
	}
	if utf8.RuneCountInString(text) > orgAttachmentMax {
		return nil, invalid(fmt.Sprintf("the attachment is over %d characters", orgAttachmentMax), "Hand an excerpt: the lines that matter")
	}
	return &OrgAttachment{Title: title, Text: text}, nil
}

// ownService is one of the project's own services — never a system service
// or a build container, which no one starts or stops by hand.
func (p *orgProject) ownService(ref string) (*platform.ServiceStack, error) {
	s, err := p.service(ref)
	if err != nil {
		return nil, err
	}
	if s.IsSystem() {
		return nil, invalid(fmt.Sprintf("%s in %s is the platform's own service", s.Name, p.Name), "Propose changes to the project's own services")
	}
	return s, nil
}

// serviceActionFits refuses an action the service's state makes pointless.
func serviceActionFits(kind string, s *platform.ServiceStack, project string) error {
	stopped := s.Status == platform.ServiceStatusStopped
	running := s.IsLive()
	switch {
	case kind == ProposalServiceStart && running:
		return invalid(fmt.Sprintf("%s in %s is already running", s.Name, project), "Propose service.restart to restart it")
	case kind == ProposalServiceStop && stopped:
		return invalid(fmt.Sprintf("%s in %s is already stopped", s.Name, project), "")
	case kind == ProposalServiceRestart && stopped:
		return invalid(fmt.Sprintf("%s in %s is stopped: propose service.start", s.Name, project), "")
	}
	return nil
}

func invalid(msg, suggestion string) error {
	return platform.NewPlatformError(platform.ErrInvalidParameter, msg, suggestion)
}
