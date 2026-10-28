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
	return func(o *envOptions) { o.vaultAsks = stateDir }
}

// envRequestResult is the answer to action=request: Requested when the
// person was asked, AlreadySet when the key is in that vault already.
// Readers are the services whose deployed run.envVariables read the key now;
// RestartAfterSave the ones the agent restarts once a message says saved —
// the save writes the vault from the person's own client, restarts nothing,
// and zcp never sees it.
type envRequestResult struct {
	Requested        *ops.EnvRequestResult `json:"requested,omitempty"`
	AlreadySet       *ops.EnvRequestResult `json:"alreadySet,omitempty"`
	Readers          []string              `json:"readers,omitempty"`
	RestartAfterSave []string              `json:"restartAfterSave,omitempty"`
	NextActions      string                `json:"nextActions"`
}

// requestVaultValue answers action=request. It writes nothing to the
// platform. With WithVaultAsks it refuses a key and vault whose ask is still
// open (ErrAlreadyAsked, an error so Mate opens no second ask), counts an ask
// answered once the vault holds the key written since, and records the ask it
// opens. A record that cannot be read or written never stops the ask: a
// second prompt is the lesser harm than none.
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
	if options.vaultAsks != "" {
		if open, found, _ := ops.ReadVaultAsk(options.vaultAsks, req); found {
			if !open.AnsweredBy(req) && !input.Again.Bool() {
				plan := planRestart(ctx, client, projectID, input, selfHostname, []string{req.Key}, false)
				return alreadyAsked(open, req, restartAfterSaveWords(req.Key, plan, input, selfHostname))
			}
			_ = ops.ForgetVaultAsk(options.vaultAsks, req)
		}
	}
	if req.AlreadySet {
		return jsonResult(envRequestResult{AlreadySet: req, NextActions: alreadySetWords(req)})
	}
	plan := planRestart(ctx, client, projectID, input, selfHostname, []string{req.Key}, false)
	if options.vaultAsks != "" {
		held, claimed, err := ops.ClaimVaultAsk(options.vaultAsks, req, ops.NewVaultAsk(req, time.Now()))
		if err == nil && !claimed {
			return alreadyAsked(held, req, restartAfterSaveWords(req.Key, plan, input, selfHostname))
		}
	}
	return jsonResult(envRequestResult{
		Requested:        req,
		Readers:          plan.readers[req.Key],
		RestartAfterSave: targetNames(plan.targets),
		NextActions:      requestedWords(req, restartAfterSaveWords(req.Key, plan, input, selfHostname)),
	})
}

// vaultPlace is where a requested value goes, as the answer message names it:
// Shared/KEY, or api/KEY.
func vaultPlace(req *ops.EnvRequestResult) string {
	if req.Scope == ops.EnvRequestScopeService {
		return req.ServiceHostname + "/" + req.Key
	}
	return "Shared/" + req.Key
}

// alreadySetWords: nothing was asked, the key is referenced by name — and a
// wrong saved value is replaced by asking with replace=true.
func alreadySetWords(req *ops.EnvRequestResult) string {
	where := "the Shared vault"
	if req.Scope == ops.EnvRequestScopeService {
		where = req.ServiceHostname + "'s vault"
	}
	return fmt.Sprintf("%[1]s is already in %[2]s; nothing was asked. Reference it by name (`NAME: ${%[1]s}` in run.envVariables) — its value is never read. "+
		"If the saved value is wrong (a placeholder, an invalid or expired value), ask the person to replace it: the same request with replace=true and replaceWhy, "+
		"one line they read on what is wrong with it (it is not in Google's format).", req.Key, where)
}

// requestedWords is the agent's next step on an ask: say why in one sentence,
// never ask in the chat, wait for the message, then restart what reads it.
func requestedWords(req *ops.EnvRequestResult, restart string) string {
	waiting := fmt.Sprintf("Now waiting on the person for the %s (%s).", req.Label, req.Key)
	if req.Replace {
		waiting = fmt.Sprintf("Now waiting on the person for a new %s (%s) to replace the saved one; the saved value stays until they save.", req.Label, req.Key)
	}
	return fmt.Sprintf("%s Tell them in one sentence why you need it, and nothing about how they give it. Never ask for the value in the chat. "+
		"A message of its own tells you how it ended: saved to %s, or declined. %s Continue with what does not need it, or end your turn.",
		waiting, vaultPlace(req), restart)
}

// alreadyAsked refuses a request whose key and vault have an open ask: the
// person already has the prompt. It is an error, so neither Mate's engine nor
// its card opens a second ask from it.
func alreadyAsked(open ops.VaultAsk, req *ops.EnvRequestResult, restart string) *mcp.CallToolResult {
	what := cmp.Or(open.Label, req.Label)
	return convertError(platform.NewPlatformError(platform.ErrAlreadyAsked,
		fmt.Sprintf("The ask for the %s (%s), made %s, is still open, so no second one was shown.",
			what, req.Key, open.AskedAt.UTC().Format("2006-01-02 15:04 UTC")),
		fmt.Sprintf("Nothing was asked again: the person answers the open ask in Mate. Tell them in one sentence that it waits for them, never how they give it. "+
			"A message of its own tells you how it ended: saved to %s, or declined. If such a message already said declined and they now want to give it, ask again with again=true. %s",
			vaultPlace(req), restart)))
}

// restartAfterSaveWords names what the agent restarts once a message says the
// value was saved. zcp cannot restart then itself: the person's client writes
// the vault directly and Mate's engine words the message, so zcp sees neither
// — and the save can come days later, to another zcp. It names the readers'
// restarts as calls, never the service running this session; a key nothing
// reads says how a service reads it.
func restartAfterSaveWords(key string, plan restartPlan, input EnvInput, selfHostname string) string {
	switch {
	case plan.poolOwner:
		return "Saving restarts nothing, and nothing needs it: " + poolEnvHint([]string{key}, input.ServiceHostname)
	case plan.boxOwner:
		return "Saving restarts nothing, and nothing needs it: " + boxEnvHint([]string{key}, input.ServiceHostname)
	case plan.unlisted:
		return fmt.Sprintf("Saving restarts nothing, and zcp could not list the services to name the ones that read %s: when it says saved, restart each that does (zerops_manage action=\"restart\").", key)
	}
	parts := []string{"Saving restarts nothing."}
	if len(plan.targets) > 0 {
		calls := make([]string, 0, len(plan.targets))
		for _, t := range plan.targets {
			calls = append(calls, fmt.Sprintf("zerops_manage action=\"restart\" serviceHostname=%q", t.hostname))
		}
		parts = []string{fmt.Sprintf("Saving restarts nothing: when it says saved, restart each service that reads %s, since a running process keeps the env it started with — %s.",
			key, strings.Join(calls, "; "))}
		if plan.uncertain {
			parts = append(parts, "zcp could not read every service's deployed env, so this names each runtime that might read it.")
		}
	}
	if plan.selfReads {
		parts = append(parts, fmt.Sprintf("%s reads it too but runs this session: never restart it; it takes the value at its next deploy.", selfHostname))
	}
	for _, name := range plan.idle {
		parts = append(parts, fmt.Sprintf("%s reads it but is not running: it takes the value when it starts.", name))
	}
	if len(plan.unreadKeys) > 0 {
		parts = append(parts, nothingReadsHint(plan.unreadKeys, input), "That deploy takes the saved value.")
	}
	return strings.Join(parts, " ")
}
