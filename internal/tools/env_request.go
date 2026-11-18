package tools

import (
	"cmp"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// WithVaultAsks records each ask zerops_env action=request opens under
// stateDir, so a second request for a key and vault whose ask is still open
// shows the person no second prompt (docs/spec-mate.md §5.8). Only a Mate's
// client draws an ask, so only a Mate's zcp records them.
func WithVaultAsks(stateDir string) EnvOption {
	return func(o *envOptions) { o.vaultAsks, o.asks = stateDir, true }
}

// WithAskCard offers request: this zcp serves a Mate, whose client draws the
// card that asks the person. WithVaultAsks implies it.
func WithAskCard() EnvOption {
	return func(o *envOptions) { o.asks = true }
}

// noAskCard answers a request where no Mate's client draws the card — a
// project outside a Mate, plain zcp: nothing was asked and nothing recorded,
// so the agent must not speak of a card or a request to decline.
func noAskCard(key string) *mcp.CallToolResult {
	name := strings.TrimSpace(key)
	if name == "" {
		name = "the value"
	}
	return convertError(platform.NewPlatformError(platform.ErrPrerequisiteMissing,
		"Nothing was asked: no Mate's card can ask the person here, so there is no request for them to answer or decline",
		fmt.Sprintf("Ask the person in one sentence to add %s themselves where this project's values are set (its variables in Zerops), and never to paste it into the chat; then reference it by name.", name)))
}

// WithCardRestarts tells a request whether this Mate's ask card restarts the
// readers once the person saves (mate.CarriesCardRestart), asked at each
// request so a Mate update counts at once.
func WithCardRestarts(carries func() bool) EnvOption {
	return func(o *envOptions) { o.cardRestarts = carries }
}

// envRequestResult is the answer to action=request: Requested when the
// person was asked, AlreadySet when the key is in that vault already.
// AlreadyAsked (RFC 3339) is when the open ask for this key and vault was
// made, on a repeat while it is open: the person's prompt is that one — Mate
// opens no second card for a key and vault whose card is open.
// Readers are the services whose deployed run.envVariables read the key now;
// RestartAfterSave the running ones restarted once it saves — by the person's
// card where it restarts (mate.CarriesCardRestart), else by the agent once a
// message says saved; zcp sees neither the save nor the message.
type envRequestResult struct {
	Requested        *ops.EnvRequestResult `json:"requested,omitempty"`
	AlreadySet       *ops.EnvRequestResult `json:"alreadySet,omitempty"`
	AlreadyAsked     string                `json:"alreadyAsked,omitempty"`
	Readers          []string              `json:"readers,omitempty"`
	RestartAfterSave []string              `json:"restartAfterSave,omitempty"`
	NextActions      string                `json:"nextActions"`
}

// requestVaultValue answers action=request. It writes nothing to the
// platform. With WithVaultAsks it records the ask it opens, answers a repeat
// for a key and vault whose ask is still open as requested with AlreadyAsked
// and tells the agent not to ask again, and counts an ask answered once the
// vault holds the key written since. A record that cannot be read or written
// never stops the ask: a second prompt is the lesser harm than none.
func requestVaultValue(ctx context.Context, client platform.Client, projectID, selfHostname string, options envOptions, input EnvInput) *mcp.CallToolResult {
	req, err := ops.EnvRequest(ctx, client, projectID, ops.EnvRequestInput{
		Hostname:   input.ServiceHostname,
		Project:    input.Project.Bool(),
		Key:        input.Key,
		Label:      input.Label,
		Why:        cmp.Or(input.Why, input.Reason),
		Sensitive:  input.Sensitive.Ptr(),
		Replace:    input.Replace.Bool(),
		ReplaceWhy: input.ReplaceWhy,
	})
	if err != nil {
		return convertError(err)
	}
	var open *ops.VaultAsk
	if options.vaultAsks != "" {
		if recorded, found, _ := ops.ReadVaultAsk(options.vaultAsks, req); found {
			if !recorded.AnsweredBy(req) && !input.Again.Bool() {
				open = &recorded
			} else {
				_ = ops.ForgetVaultAsk(options.vaultAsks, req)
			}
		}
	}
	if open != nil && req.AlreadySet {
		// The open ask replaces the held value: a repeat names that ask.
		req.AlreadySet, req.Replace = false, true
	}
	if req.AlreadySet {
		return jsonResult(envRequestResult{AlreadySet: req, NextActions: alreadySetWords(req, input.ownVault)})
	}
	plan := planRestart(ctx, client, projectID, input, selfHostname, []string{req.Key}, false)
	if open == nil && options.vaultAsks != "" {
		held, claimed, err := ops.ClaimVaultAsk(options.vaultAsks, req, ops.NewVaultAsk(req, time.Now()))
		if err == nil && !claimed {
			open = &held
		}
	}
	cardRestarts := options.cardRestarts != nil && options.cardRestarts()
	restart := restartAfterSaveWords(req.Key, plan, input, selfHostname, cardRestarts)
	answer := envRequestResult{
		Requested:        req,
		Readers:          plan.readers[req.Key],
		RestartAfterSave: targetNames(plan.targets),
		NextActions:      requestedWords(req, restart),
	}
	if open != nil {
		answer.AlreadyAsked = open.AskedAt.UTC().Format(time.RFC3339)
		answer.NextActions = alreadyAskedWords(req, answer.AlreadyAsked, restart)
	}
	return jsonResult(answer)
}

// vaultPlace is where a requested value goes, as the answer message names it:
// Shared/KEY, or api/KEY.
func vaultPlace(req *ops.EnvRequestResult) string {
	if req.Scope == ops.EnvRequestScopeService {
		return req.ServiceHostname + "/" + req.Key
	}
	return "Shared/" + req.Key
}

// alreadySetWords: nothing was asked, the key is referenced by name — a key
// of the Mate's own vault (ownVault) used with `zcp with`, since no app
// reads it — and a wrong saved value is replaced by asking with replace=true.
func alreadySetWords(req *ops.EnvRequestResult, ownVault bool) string {
	where := "the Shared vault"
	use := fmt.Sprintf("Reference it by name (`NAME: ${%s}` in run.envVariables) — its value is never read.", req.Key)
	switch {
	case ownVault:
		where = "your own vault"
		use = fmt.Sprintf("Run the command that needs it as `zcp with %s -- <command>` — its value is never read, and never print it.", req.Key)
	case req.Scope == ops.EnvRequestScopeService:
		where = req.ServiceHostname + "'s vault"
	}
	return fmt.Sprintf("%s is already in %s; nothing was asked. %s "+
		"If the saved value is wrong (a placeholder, an invalid or expired value), ask the person to replace it: the same request with replace=true and replaceWhy, "+
		"one line they read on what is wrong with it (it is not in Google's format).", req.Key, where, use)
}

// requestedWords is the agent's next step on an ask: say why in one sentence,
// never ask in the chat, wait for the message; then the readers restart.
func requestedWords(req *ops.EnvRequestResult, restart string) string {
	waiting := fmt.Sprintf("Now waiting on the person for the %s (%s).", req.Label, req.Key)
	if req.Replace {
		waiting = fmt.Sprintf("Now waiting on the person for a new %s (%s) to replace the saved one; the saved value stays until they save.", req.Label, req.Key)
	}
	return fmt.Sprintf("%s Tell them in one sentence why you need it, and nothing about how they give it. Never ask for the value in the chat. "+
		"A message of its own tells you how it ended: saved to %s, or declined. %s Continue with what does not need it, or end your turn.",
		waiting, vaultPlace(req), restart)
}

// alreadyAskedWords is the agent's next step on a repeat while the ask is
// open: the person's prompt is that one, so it does not ask again.
func alreadyAskedWords(req *ops.EnvRequestResult, askedAt, restart string) string {
	return fmt.Sprintf("The person's prompt for the %s (%s) is already open, asked %s: do not ask again. "+
		"Tell them in one sentence that it waits for them, never how they give it. Never ask for the value in the chat. "+
		"A message of its own tells you how it ended: saved to %s, or declined; if one already said declined and they now want to give it, ask again with again=true. "+
		"%s Continue with what does not need it, or end your turn.",
		req.Label, req.Key, askedAt, vaultPlace(req), restart)
}

// restartAfterSaveWords names what restarts once the person saves. A card
// that restarts (cardRestarts, mate.CarriesCardRestart) restarts the running
// readers from the person's own client and the saved message names them, so
// the agent restarts nothing; an older card restarts nothing, so the agent
// restarts each once a message says saved — zcp sees neither the save nor the
// message, and the save can come days later, to another zcp. Never the
// service running this session; a key nothing references says how a service
// reads it and names the services that have it by name, never restarted for it.
// A key of the Mate's own vault restarts nothing and says how it is used.
func restartAfterSaveWords(key string, plan restartPlan, input EnvInput, selfHostname string, cardRestarts bool) string {
	switch {
	case plan.ownVault:
		return fmt.Sprintf("Saving restarts nothing, and nothing needs it: %s goes to your own vault, which only you read — no app does. ", key) +
			ownVaultUseWords([]string{key}, "Once the message says saved it is in this container about two seconds later")
	case plan.poolOwner:
		return "Saving restarts nothing, and nothing needs it: " + poolEnvHint([]string{key}, input.ServiceHostname)
	case plan.boxOwner:
		return "Saving restarts nothing, and nothing needs it: " + boxEnvHint([]string{key}, input.ServiceHostname)
	case plan.unlisted && cardRestarts:
		return fmt.Sprintf("Once saved, the person's card restarts the running services that read %s; the message names those it restarted and one that did not.", key)
	case plan.unlisted:
		return fmt.Sprintf("Saving restarts nothing, and zcp could not list the services to name the ones that read %s: when it says saved, restart each that does (zerops_manage action=\"restart\").", key)
	}
	parts := []string{"Saving restarts nothing."}
	if len(plan.targets) > 0 && cardRestarts {
		parts = []string{fmt.Sprintf("Once saved, the person's card restarts %s, since a running process keeps the env it started with; the message names those it restarted and one that did not. Restart nothing yourself.",
			strings.Join(targetNames(plan.targets), ", "))}
	} else if len(plan.targets) > 0 {
		calls := make([]string, 0, len(plan.targets))
		for _, t := range plan.targets {
			calls = append(calls, fmt.Sprintf("zerops_manage action=\"restart\" serviceHostname=%q", t.hostname))
		}
		parts = []string{fmt.Sprintf("Saving restarts nothing: when it says saved, restart each service that reads %s, since a running process keeps the env it started with — %s.",
			key, strings.Join(calls, "; "))}
	}
	if len(plan.targets) > 0 && plan.uncertain {
		parts = append(parts, "zcp could not read every service's deployed env, so this names each runtime that might read it.")
	}
	if plan.selfReads {
		parts = append(parts, fmt.Sprintf("%s reads it too but runs this session: never restart it; it takes the value at its next deploy.", selfHostname))
	}
	for _, name := range plan.idle {
		parts = append(parts, fmt.Sprintf("%s reads it but is not running: it takes the value when it starts.", name))
	}
	if len(plan.unreadKeys) > 0 {
		parts = append(parts, nothingReadsHint(plan.unreadKeys, input), "That deploy takes the saved value.", byNameHint(plan.byName))
	}
	return joinSentences(parts...)
}
