package ops

import (
	"context"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

// SecretAskInput is what a Mate asks the person to give in its application's
// stage or production: where the secret goes (Project names the environment,
// Scope project or service, Service the service's own), its Key, why, and
// the services that read it — never the value.
type SecretAskInput struct {
	Project string
	Scope   string
	Service string
	Key     string
	Value   string
	Reason  string
	Restart []string
	// Generate is a preprocessor expression the person's browser expands
	// into a random value at their press (SecretGenerate's allow-list).
	Generate string
}

// SecretAskEnvironment is the stage or production a secret goes to, named
// as Mate's menu names it.
type SecretAskEnvironment struct {
	Kind      string `json:"kind"`
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
}

// SecretAskService is a service the person's card restarts once a secret is
// saved or a plain value set: a running process reads a changed value only
// after a restart.
type SecretAskService struct {
	ServiceID string `json:"serviceId"`
	Hostname  string `json:"hostname"`
}

// SecretAsk is the ask Mate's engine opens on the call (docs/spec-mate.md
// §5.8): env request's `requested` shape plus the environment and what
// restarts. It never carries a value: the person types or generates it on the
// card, which writes it hidden with their own access.
type SecretAsk struct {
	Key             string               `json:"key"`
	Label           string               `json:"label"`
	Why             string               `json:"why"`
	Scope           EnvRequestScope      `json:"scope"`
	ServiceHostname string               `json:"serviceHostname,omitempty"`
	Sensitive       bool                 `json:"sensitive"`
	Environment     SecretAskEnvironment `json:"environment"`
	Restart         []SecretAskService   `json:"restart,omitempty"`
	// NotRunning are the named services that run nothing now (stopped, or
	// waiting for their first deploy): nothing restarts them, and their next
	// start or release reads the value.
	NotRunning []string `json:"notRunning,omitempty"`
	// Generate is the random value the card makes in the person's browser
	// when they press, as a preprocessor expression in its canonical form;
	// absent, the person types or pastes the value.
	Generate string `json:"generate,omitempty"`
}

// SecretAskResult is zerops_propose_env's answer for a secret.
type SecretAskResult struct {
	Requested   SecretAsk `json:"requested"`
	NextActions string    `json:"nextActions"`
}

// ProposeSecretAsk checks an ask for a secret in the Mate's application's
// stage or production against HQ's observation and returns the ask Mate's
// engine opens a card on. It writes nothing and reads nothing of the vault
// there: the person's card writes the value as them and restarts what reads
// it, and the engine tells the agent only saved or declined. A value, a key
// that is no name or a reason holding a value is refused before anything is
// observed, without repeating it.
func ProposeSecretAsk(ctx context.Context, src AppSource, in SecretAskInput) (*SecretAskResult, error) {
	if in.Value != "" {
		return nil, invalid("a secret is never in the call",
			"Drop value: the person types or generates it on the card, and you never see it.")
	}
	key := strings.TrimSpace(in.Key)
	if !envKeyPattern.MatchString(key) {
		return nil, invalid("key is not a variable name",
			"Pass the variable's name only: letters, digits and `_`, never KEY=value.")
	}
	if err := validateRequestKey(key); err != nil {
		return nil, err
	}
	reason, err := proposalReason(in.Reason)
	if err != nil {
		return nil, err
	}
	if looksLikeValue(reason, key) {
		return nil, invalid("the reason holds what looks like a value",
			"An ask carries the name and why, never a value: the person gives it on the card.")
	}
	generate, err := SecretGenerate(in.Generate)
	if err != nil {
		return nil, err
	}
	scope := strings.TrimSpace(in.Scope)
	hostname := strings.TrimSpace(in.Service)
	switch {
	case scope == OrgScopeService && hostname == "":
		return nil, invalid("a service's own secret needs the service", "Pass service, or scope project")
	case scope == OrgScopeProject && hostname != "":
		return nil, invalid("scope project names no service", "Drop service, or scope service")
	case scope != OrgScopeService && scope != OrgScopeProject:
		return nil, invalid(fmt.Sprintf("unknown scope %q", scope), "scope is project or service")
	}

	env, err := appEnvironment(ctx, src, in.Project)
	if err != nil {
		return nil, err
	}
	p := &orgProject{Project: platform.Project{ID: env.ProjectID, Name: env.Name}}
	if len(secretAskRestarts(hostname, in.Restart)) > 0 {
		if p.Services, err = src.Services(ctx, env.ProjectID); err != nil {
			return nil, fmt.Errorf("read the services of %s: %w", env.Name, err)
		}
	}
	restart, notRunning, err := readerRestarts(p, hostname, in.Restart)
	if err != nil {
		return nil, err
	}
	ask := SecretAsk{
		Key:         key,
		Label:       LabelOfKey(key),
		Why:         reason,
		Scope:       EnvRequestScopeShared,
		Sensitive:   true,
		Environment: SecretAskEnvironment{Kind: env.Kind, ProjectID: env.ProjectID, Name: env.Name},
	}
	if scope == OrgScopeService {
		ask.Scope, ask.ServiceHostname = EnvRequestScopeService, hostname
	}
	if len(restart) > 0 {
		ask.Restart = restart
	}
	ask.NotRunning = notRunning
	ask.Generate = generate
	return &SecretAskResult{Requested: ask, NextActions: secretAskWords(ask)}, nil
}

// The preprocessor directives a person's browser expands exactly as zParser
// does (mate's apps/web/src/components/zerops/vault/secretGenerate.ts):
// generateRandomString(<N>) — N bytes, each mapped onto zParser's 65
// characters — and generateRandomInt(<min>, <max>), uniform in [min, max].
// generateRandomBytes is left out: raw bytes are no text a browser writes.
var (
	generateString = regexp.MustCompile(`^<@generateRandomString\(<(\d+)>\)>$`)
	generateInt    = regexp.MustCompile(`^<@generateRandomInt\(<(-?\d+)>,\s*<(-?\d+)>\)>$`)
)

// The lengths a generated string takes: the recipe's shortest secret, and
// zParser's own limit (maxRandBytesLen).
const (
	minSecretGenerate = 16
	maxSecretGenerate = 1024
)

// SecretGenerate checks a generate expression against the allow-list and
// returns it canonical (no spaces); "" for none. Anything else — a
// modifier, a literal beside the directive, another function, a length out
// of bounds — is refused without repeating it.
func SecretGenerate(expr string) (string, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return "", nil
	}
	const allowed = "generate is one directive and nothing else: <@generateRandomString(<32>)> (16 to 1024 characters) or <@generateRandomInt(<min>, <max>)>."
	if m := generateString.FindStringSubmatch(expr); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil || n < minSecretGenerate || n > maxSecretGenerate {
			return "", invalid(fmt.Sprintf("a generated secret is %d to %d characters", minSecretGenerate, maxSecretGenerate), allowed)
		}
		return fmt.Sprintf("<@generateRandomString(<%d>)>", n), nil
	}
	if m := generateInt.FindStringSubmatch(expr); m != nil {
		low, errLow := strconv.ParseInt(m[1], 10, 64)
		high, errHigh := strconv.ParseInt(m[2], 10, 64)
		if errLow != nil || errHigh != nil || high <= low {
			return "", invalid("a generated number needs min below max, each a 64-bit integer", allowed)
		}
		// zParser draws from max-min+1, which must stay a 64-bit integer.
		if new(big.Int).Sub(big.NewInt(high), big.NewInt(low)).Cmp(big.NewInt(1<<62)) >= 0 {
			return "", invalid("a generated number's range is too wide", allowed)
		}
		return fmt.Sprintf("<@generateRandomInt(<%d>, <%d>)>", low, high), nil
	}
	return "", invalid("generate is not a directive the card can make", allowed)
}

// readerRestarts splits the services a changed value's card restarts — a
// service's own value's service, then the ones named — into the running ones
// it restarts and the ones that run nothing now (stopped, or waiting for their
// first deploy), which read the value when they next start. A name the
// environment lacks is refused.
func readerRestarts(p *orgProject, service string, named []string) ([]SecretAskService, []string, error) {
	var (
		restart    []SecretAskService
		notRunning []string
	)
	for _, ref := range secretAskRestarts(service, named) {
		s, err := appService(p, ref)
		if err != nil {
			return nil, nil, err
		}
		// A restart is for a running process; one that runs nothing reads the value when it starts.
		if !s.IsLive() {
			notRunning = append(notRunning, s.Name)
			continue
		}
		restart = append(restart, SecretAskService{ServiceID: s.ID, Hostname: s.Name})
	}
	return restart, notRunning, nil
}

// secretAskRestarts is each service the card restarts, once: a service's
// own value's service first, then the ones named.
func secretAskRestarts(service string, named []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, name := range append([]string{service}, named...) {
		if name = strings.TrimSpace(name); name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

// secretAskPlace is where the secret goes, as the engine's message names it:
// "Production · Shared/AUTH_SECRET".
func secretAskPlace(ask SecretAsk) string {
	vault := "Shared"
	if ask.Scope == EnvRequestScopeService {
		vault = ask.ServiceHostname
	}
	return fmt.Sprintf("%s · %s/%s", ask.Environment.Name, vault, ask.Key)
}

// secretAskWords is the agent's next step: say why in one sentence, never
// ask in the chat or send the person anywhere, wait for the message, then
// read the environment before saying it works.
func secretAskWords(ask SecretAsk) string {
	restarted := " Nothing restarts by itself: once it is saved, propose restarting what reads it with zerops_propose_service."
	if len(ask.Restart) > 0 {
		names := make([]string, 0, len(ask.Restart))
		for _, s := range ask.Restart {
			names = append(names, s.Hostname)
		}
		restarted = fmt.Sprintf(" The card restarts %s once it is saved.", domainList(names))
	}
	if len(ask.NotRunning) > 0 {
		restarted += fmt.Sprintf(" %s runs nothing now, so nothing restarts it: its next start or release reads the value.", domainList(ask.NotRunning))
	}
	made := "their card takes it, typed or generated, and saves it hidden with their own access."
	if ask.Generate != "" {
		made = "their card makes it in their browser at their press, never shown, and saves it hidden with their own access."
	}
	return fmt.Sprintf("Now waiting on the person for %s in %s: %s%s "+
		"Tell them in one sentence why it is needed, and nothing about how they give it: never send them to a Vault page and never ask for the value in the chat. "+
		"A message of its own tells you how it ended: saved to %s (naming a service that did not restart, and why), or declined. Once saved, read zerops_observe action=logs for %s before you say it works. "+
		"The app reads it only through its service's zerops.yaml run.envVariables, under the name the app reads and never the same name (APP_KEY: ${%s}): the release review sees nothing else.",
		ask.Key, ask.Environment.Name, made, restarted, secretAskPlace(ask), ask.Environment.Name, ask.Key)
}
