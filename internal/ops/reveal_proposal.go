package ops

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

// ProposalEnvReveal shows the person a hidden value the agent cannot read.
const ProposalEnvReveal = "env.reveal"

// AppEnvironmentMate is the Mate's own project, beside its application's
// stage and production.
const AppEnvironmentMate = "mate"

// RevealNote is what a reveal's result says: the agent never sees the value
// and is never told it was shown.
const RevealNote = "Nothing is shown to you, now or later: the person reveals the value on the card, on their own screen and with their own access, and you are not told they did. Never restate it, never ask them to paste the value into the chat, and don't propose it again unless they ask."

// mateProjectTitle names the Mate's own project on a card.
const mateProjectTitle = "This Mate"

// RevealInput is what a reveal names: where the value lives (Environment,
// with Project picking one stage or production by id or name, and Service a
// service's own value), its Key, and why — never the value.
type RevealInput struct {
	Environment string
	Project     string
	Service     string
	Key         string
	Reason      string
}

// RevealParams are env.reveal's params: the value's name and its scope.
type RevealParams struct {
	Scope string `json:"scope"`
	Key   string `json:"key"`
}

// revealWhyMax bounds the reason to the one line the card shows whole.
const revealWhyMax = requestWhyMax

// valueLike is a run that reads as a secret rather than words: twenty or more
// characters a token is made of, with letters and digits both.
var valueLike = regexp.MustCompile(`[A-Za-z0-9+/=_\-.~]{20,}`)

// looksLikeValue reports whether reason holds what reads as a value: the key
// followed by `=` or `:` and something, or a token-like run.
func looksLikeValue(reason, key string) bool {
	for rest := reason; ; {
		at := strings.Index(rest, key)
		if at < 0 {
			break
		}
		rest = strings.TrimLeft(rest[at+len(key):], " ")
		if (strings.HasPrefix(rest, "=") || strings.HasPrefix(rest, ":")) && strings.TrimSpace(rest[1:]) != "" {
			return true
		}
	}
	for _, run := range valueLike.FindAllString(reason, -1) {
		if strings.ContainsAny(run, "0123456789") && strings.IndexFunc(run, isLetter) >= 0 {
			return true
		}
	}
	return false
}

// ProposeReveal checks a reveal against where its value lives and returns it
// as an env.reveal proposal (docs/spec-mate.md §2.11). Nothing is read but the
// place: the person's client reveals the value with their own access, only on
// their screen, and the agent never holds it. A key or reason that carries a
// value is refused before anything is read.
func ProposeReveal(ctx context.Context, src AppSource, newID func() string, mateProjectID string, in RevealInput) (*OrgProposalResult, error) {
	key := strings.TrimSpace(in.Key)
	// A key that is no name may hold its value: refused without repeating it.
	if !envKeyPattern.MatchString(key) {
		return nil, invalid("key is not a variable name",
			"Pass the variable's name only: letters, digits and `_`, never KEY=value.")
	}
	if err := validateRequestKey(key); err != nil {
		return nil, err
	}
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		return nil, invalid("a reveal needs a reason", "Say in one sentence why the person wants to see it")
	}
	if err := validateRequestLine("reason", reason, revealWhyMax, "reason: one line the card shows whole; never a value."); err != nil {
		return nil, err
	}
	if looksLikeValue(reason, key) {
		return nil, invalid("the reason holds what looks like a value",
			"A reveal carries the name and why, never a value: you cannot read it, and the person sees it on the card.")
	}
	scope, hostname := OrgScopeProject, strings.TrimSpace(in.Service)
	if hostname != "" {
		scope = OrgScopeService
	}
	params := RevealParams{Scope: scope, Key: key}

	environment := strings.ToLower(strings.TrimSpace(in.Environment))
	switch environment {
	case AppEnvironmentMate:
		p := &orgProject{Project: platform.Project{ID: mateProjectID, Name: mateProjectTitle}}
		var s *platform.ServiceStack
		if hostname != "" {
			// This Mate's own services are its vault's: the card finds the service by name there.
			s = &platform.ServiceStack{Name: hostname}
		}
		return revealResult(newID, reason, p, s, fmt.Sprintf("Show %s from this Mate", key), params)
	case AppEnvironmentStage, AppEnvironmentProduction:
	default:
		return nil, invalid(fmt.Sprintf("%q is not where a reveal can show a value", in.Environment),
			"environment is stage, production or mate (this Mate's own project)")
	}
	ref := strings.TrimSpace(in.Project)
	if ref == "" {
		ref = environment
	}
	env, err := appEnvironment(ctx, src, ref)
	if err != nil {
		return nil, err
	}
	if env.Kind != environment {
		return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("%q is your application's %s, not its %s", ref, env.Kind, environment),
			"zerops_observe action=environments lists your application's stage and production")
	}
	p := &orgProject{Project: platform.Project{ID: env.ProjectID, Name: env.Name}}
	var s *platform.ServiceStack
	if hostname != "" {
		if p.Services, err = src.Services(ctx, env.ProjectID); err != nil {
			return nil, fmt.Errorf("read the services of %s: %w", env.Name, err)
		}
		if s, err = appService(p, hostname); err != nil {
			return nil, err
		}
	}
	return revealResult(newID, reason, p, s, fmt.Sprintf("Show %s from %s", key, env.Name), params)
}

// revealResult is the proposal with the reveal's own note.
func revealResult(newID func() string, reason string, p *orgProject, s *platform.ServiceStack, summary string, params RevealParams) (*OrgProposalResult, error) {
	result, err := proposalResult(newID, ProposalEnvReveal, reason, p, s, summary, params)
	if result != nil {
		result.Note = RevealNote
	}
	return result, err
}
