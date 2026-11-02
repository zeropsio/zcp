package ops

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/zeropsio/zcp/internal/platform"
)

// The kinds of environment HQ lets a Mate propose a change in.
const (
	AppEnvironmentStage      = "stage"
	AppEnvironmentProduction = "production"
)

// AppEnvironment is one of the Mate's application's environments as HQ
// observes it: its project, its name and its kind.
type AppEnvironment struct {
	ProjectID string
	Name      string
	Kind      string
}

// AppSource is what a Mate reads of its application to check a proposal:
// HQ's observation of the environments, and of one environment's services.
type AppSource interface {
	Environments(ctx context.Context) ([]AppEnvironment, error)
	Services(ctx context.Context, projectID string) ([]platform.ServiceStack, error)
}

// ProposeAppAction validates a change to the Mate's application's stage or
// production against what HQ observes there and returns it as a proposal of
// the Chief's shape (docs/spec-mate.md §2.11). It runs nothing: the person
// approves it on a card and their client makes the change with their own
// token. Only the target environment's services are read.
func ProposeAppAction(ctx context.Context, src AppSource, newID func() string, in OrgActionInput) (*OrgProposalResult, error) {
	switch in.Kind {
	case ProposalServiceRestart, ProposalServiceStart, ProposalServiceStop, ProposalEnvSet:
	default:
		return nil, invalid(fmt.Sprintf("%q is not something a Mate can propose", in.Kind),
			"Propose service.restart, service.start, service.stop or env.set")
	}
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		return nil, invalid("a proposal needs a reason", "Say in one sentence why, for the person deciding")
	}
	env, err := appEnvironment(ctx, src, in.Project)
	if err != nil {
		return nil, err
	}
	services, err := src.Services(ctx, env.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("read the services of %s: %w", env.Name, err)
	}
	p := &orgProject{Project: platform.Project{ID: env.ProjectID, Name: env.Name}, Services: services}
	service := func(ref string) (*platform.ServiceStack, error) { return appService(p, ref) }

	var (
		s       *platform.ServiceStack
		summary string
		params  any = struct{}{}
	)
	if in.Kind == ProposalEnvSet {
		if summary, s, params, err = envProposal(p, in, service); err != nil {
			return nil, err
		}
	} else {
		if s, err = service(in.Service); err != nil {
			return nil, err
		}
		if summary, err = serviceProposal(in.Kind, p, s); err != nil {
			return nil, err
		}
	}
	return proposalResult(newID, in.Kind, reason, p, s, summary, params)
}

// appEnvironment is the application's stage or production that ref names: by
// project id, name or kind.
func appEnvironment(ctx context.Context, src AppSource, ref string) (AppEnvironment, error) {
	const listed = "zerops_observe action=environments lists your application's stage and production"
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return AppEnvironment{}, platform.NewPlatformError(platform.ErrInvalidParameter, "project is required", "Pass an environment's projectId or name; "+listed)
	}
	envs, err := src.Environments(ctx)
	if err != nil {
		return AppEnvironment{}, fmt.Errorf("read your application's environments: %w", err)
	}
	for _, env := range envs {
		if env.Kind != AppEnvironmentStage && env.Kind != AppEnvironmentProduction {
			continue
		}
		title := appEnvironmentTitle(env)
		if env.ProjectID == ref || strings.EqualFold(env.Name, ref) || strings.EqualFold(env.Kind, ref) || strings.EqualFold(title, ref) {
			env.Name = title
			return env, nil
		}
	}
	return AppEnvironment{}, platform.NewPlatformError(platform.ErrInvalidParameter,
		fmt.Sprintf("%q is not your application's stage or production", ref), listed)
}

// hqStageName is a stage name HQ made from the application's slug and the tier.
var hqStageName = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*-stage$`)

// appEnvironmentTitle names an environment as Mate's menu does, so a card
// reads like the place the person sees: "Production"; a stage by its own name
// with its first letter capitalised, or "Stage" where HQ made the name.
func appEnvironmentTitle(env AppEnvironment) string {
	if env.Kind == AppEnvironmentProduction {
		return "Production"
	}
	own := strings.TrimSpace(env.Name)
	if own == "" || hqStageName.MatchString(own) {
		return "Stage"
	}
	r, size := utf8.DecodeRuneInString(own)
	return string(unicode.ToUpper(r)) + own[size:]
}

// appService is one of the environment's services by hostname or id. HQ's
// observation leaves the platform's own services out.
func appService(p *orgProject, ref string) (*platform.ServiceStack, error) {
	listed := fmt.Sprintf("zerops_observe action=status projectId=%q lists its services", p.ID)
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, platform.NewPlatformError(platform.ErrServiceRequired, "service is required", "Pass the service's hostname; "+listed)
	}
	for i := range p.Services {
		if s := &p.Services[i]; s.Name == ref || s.ID == ref {
			return s, nil
		}
	}
	return nil, platform.NewPlatformError(platform.ErrServiceNotFound, fmt.Sprintf("%s has no service %q", p.Name, ref), listed)
}
