package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/hq"
)

// ObserveInput uses HQ's project and service IDs, learned from environments
// and status. No Zerops credential or application ID comes from the model.
type ObserveInput struct {
	Action    string `json:"action,omitempty"    jsonschema:"environments (default), status, logs, or ci"`
	ProjectID string `json:"projectId,omitempty" jsonschema:"Environment project ID from environments; required for status and logs"`
	ServiceID string `json:"serviceId,omitempty" jsonschema:"Service ID from status; required for logs"`
	Limit     int    `json:"limit,omitempty"     jsonschema:"Log entries: 1 to 100; defaults to 100"`
}

// RegisterObserve is Mate-gated by the server. Observation needs no SSH:
// HQ reads Zerops with each environment's deploy key and exposes only facts.
func RegisterObserve(srv *mcp.Server, httpClient hq.Doer, projectID string) {
	registerObserve(srv, httpClient, hq.EnrollmentPath(), projectID)
}

const (
	observeEnvironments = "environments"
	observeStatus       = "status"
	observeLogs         = "logs"
	observeCI           = "ci"
)

var observationID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func registerObserve(srv *mcp.Server, httpClient hq.Doer, path, projectID string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_observe",
		Description: "Observe this project's stage and production through HQ. environments lists project IDs; status reads service IDs, status and active versions; logs reads up to 100 service entries; ci reads the newest push and CI where Mate delivers to Git, which environments omits. Read only: no sibling Zerops grants or secrets. Failures say: fix input, retry later, or accept HQ's refusal.",
		Annotations: &mcp.ToolAnnotations{Title: "Observe stage and production", ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: boolPtr(false)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ObserveInput) (*mcp.CallToolResult, any, error) {
		failure := func(message string) *mcp.CallToolResult {
			return observeFailure(message + "; fix it and call zerops_observe again.")
		}
		if in.Action == "" {
			in.Action = observeEnvironments
		}
		if in.Action != observeEnvironments && in.Action != observeStatus && in.Action != observeLogs && in.Action != observeCI {
			return failure("Unknown observation action"), nil, nil
		}
		if (in.Action == observeStatus || in.Action == observeLogs) && !observationID.MatchString(in.ProjectID) {
			return failure("A valid environment projectId from environments is required"), nil, nil
		}
		if in.Action == observeLogs && !observationID.MatchString(in.ServiceID) {
			return failure("A valid serviceId from status is required for logs"), nil, nil
		}
		if in.Limit == 0 {
			in.Limit = 100
		}
		if in.Limit < 1 || in.Limit > 100 {
			return failure("Log limit must be 1 to 100"), nil, nil
		}
		client, err := hq.Open(httpClient, path)
		if err != nil {
			return failure(fmt.Sprintf("HQ enrollment could not be opened: %v", err)), nil, nil
		}
		if client.ProjectID() != projectID {
			return failure("HQ enrollment belongs to another project"), nil, nil
		}
		ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		var body []byte
		switch in.Action {
		case observeEnvironments:
			out, readErr := client.Environments(ctx)
			if readErr != nil {
				return readFailure(readErr, failure), nil, nil
			}
			body, err = json.Marshal(out)
		case observeStatus:
			out, readErr := client.EnvironmentStatus(ctx, in.ProjectID)
			if readErr != nil {
				return readFailure(readErr, failure), nil, nil
			}
			body, err = json.Marshal(out)
		case observeLogs:
			out, readErr := client.EnvironmentLogs(ctx, in.ProjectID, in.ServiceID, in.Limit)
			if readErr != nil {
				return readFailure(readErr, failure), nil, nil
			}
			body, err = json.Marshal(out)
		case observeCI:
			state, readErr := client.Once().Self(ctx)
			if readErr != nil {
				return readFailure(readErr, failure), nil, nil
			}
			body = []byte(gitPushesCI(state.GitPushes))
		}
		if err != nil {
			return failure(fmt.Sprintf("Observation could not be rendered: %v", err)), nil, nil
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(body)}}}, nil, nil
	})
}

// gitPushesCI answers the ci observation: one line per Git environment's
// newest push of a repository.
func gitPushesCI(pushes []hq.GitPush) string {
	switch {
	case pushes == nil:
		return "This HQ does not report its Git environments' pushes, so there is no CI to read through it."
	case len(pushes) == 0:
		return "No Git environment of this Mate's application has taken a push yet."
	}
	lines := make([]string, 0, len(pushes))
	for _, push := range pushes {
		lines = append(lines, push.Repo+": "+gitPushCI(push))
	}
	return strings.Join(lines, "\n")
}

// gitPushCI is the short form of the origin's CI on one push, as HQ last read
// it, with what the agent does about it: "CI on 1a2b3c4d5e in git-stage:
// failed — check_types <url>; you hold no token …". Only a Mate reads CI
// through HQ, so this is where that guidance lives.
func gitPushCI(push hq.GitPush) string {
	line := fmt.Sprintf("CI on %s in %s: ", shortSHA(push.Sha), push.Environment)
	ci := push.CI
	if ci == nil {
		return line + "not read — HQ holds no CI record for this push"
	}
	switch ci.Verdict {
	case hq.CIRunning:
		line += fmt.Sprintf("running — %d of %d checks done", ci.Done, ci.Total)
	case hq.CIPassed:
		line += fmt.Sprintf("passed — %d of %d checks", ci.Done, ci.Total)
	case hq.CIFailed:
		failed := make([]string, 0, len(ci.Failed))
		for _, check := range ci.Failed {
			if check.URL != nil {
				failed = append(failed, check.Name+" "+*check.URL)
			} else {
				failed = append(failed, check.Name)
			}
		}
		return line + "failed — " + strings.Join(failed, ", ") +
			"; you hold no token for the origin, so run the repository's own test command for each on the dev half, fix it and deliver the fix as a new change"
	case hq.CIUnreadable:
		// The reason says what the token lacks.
		return line + "unreadable — " + reasonOf(ci) + " Tell the person this reason: only they can replace the Git environment's token."
	case hq.CINone:
		line += "none — no check reported"
	default:
		line += ci.Verdict
	}
	switch ci.Watch {
	case hq.CIWatching:
		line += "; HQ reads it until " + ci.Until
		if ci.Verdict == hq.CIRunning {
			line += "; tell the person CI is still running and read it again a few minutes later, never in a loop and not past then"
		}
	case hq.CIStopped:
		line += "; HQ stopped reading: " + reasonOf(ci)
	}
	return line
}

func reasonOf(ci *hq.PushCI) string {
	if ci.Reason == nil || *ci.Reason == "" {
		return "HQ gave no reason"
	}
	return *ci.Reason
}

func observeFailure(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

// readFailure says what the agent can do about HQ's answer: an unavailable
// HQ is waited out, a refusal is HQ's answer to the same call; anything else
// stays the input failure.
func readFailure(err error, failure func(string) *mcp.CallToolResult) *mcp.CallToolResult {
	var unavailable *hq.UnavailableError
	if errors.As(err, &unavailable) {
		if unavailable.Code == "zerops_unavailable" {
			return observeFailure("HQ could not read Zerops right now (zerops_unavailable); try again later — nothing to fix on your side.")
		}
		detail := unavailable.Code
		if detail == "" && unavailable.Err != nil {
			detail = unavailable.Err.Error()
		}
		return observeFailure(fmt.Sprintf("HQ is not serving right now (%s); try again later — nothing to fix on your side.", detail))
	}
	var refused *hq.RefusedError
	if errors.As(err, &refused) {
		refusal := fmt.Sprintf("%d %s", refused.Status, refused.Code)
		if refused.Reason != "" && refused.Reason != refused.Code {
			refusal += " (" + refused.Reason + ")"
		}
		return observeFailure("HQ refused the observation: " + refusal + "; the same call gets the same answer.")
	}
	return failure(err.Error())
}
