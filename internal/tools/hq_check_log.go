package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/runtime"
)

// CheckLogInput names a change and, optionally, the check and lines to read.
type CheckLogInput struct {
	Repo   string  `json:"repo"            jsonschema:"Repository name as shown by status and the notice."`
	Change int     `json:"change"          jsonschema:"The change's number."`
	Check  *int64  `json:"check,omitempty" jsonschema:"GitHub check run id; omit to list failing checks."`
	From   *int64  `json:"from,omitempty"  jsonschema:"First log line, numbered from 1."`
	To     *int64  `json:"to,omitempty"    jsonschema:"Last log line, inclusive."`
	Match  *string `json:"match,omitempty" jsonschema:"RE2 pattern selecting log lines."`
}

// RegisterCheckLog lets a Mate read its own change's checks through HQ.
func RegisterCheckLog(srv *mcp.Server, httpClient hq.Doer, rt runtime.Info) {
	if !rt.InContainer || !rt.MateEnabled {
		return
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if _, ok := openHQ(httpClient, rt); !ok {
		return
	}
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_check_log",
		Description: "Read through HQ which GitHub checks failed on your open change and part of one failing check's log. Without check, list them; with check, read the last lines, the lines from..to, or lines matching match. The bounded answer says how many lines the log has.",
		Annotations: &mcp.ToolAnnotations{Title: "Read failing checks and logs", ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: boolPtr(false)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in CheckLogInput) (*mcp.CallToolResult, any, error) {
		client, ok := openHQ(httpClient, rt)
		if !ok {
			return observeFailure("GitHub or HQ did not answer; nothing to fix on your side, ask again later."), nil, nil
		}
		client = client.Once().Bounded(3*time.Second, 10*time.Second)
		if in.Check == nil {
			checks, err := client.FailingChecks(ctx, in.Repo, in.Change)
			if err != nil {
				return observeFailure(checkLogRefusal(err)), nil, nil
			}
			return textResult(checkLogText(in.Repo, in.Change, checks, nil)), nil, nil
		}
		log, err := client.CheckLog(ctx, in.Repo, in.Change, *in.Check, hq.CheckLogQuery{From: in.From, To: in.To, Match: in.Match})
		if err != nil {
			return observeFailure(checkLogRefusal(err)), nil, nil
		}
		return textResult(checkLogText(in.Repo, in.Change, hq.ChecksAnswer{}, &log)), nil, nil
	})
}

// checkLogText renders HQ's checks or log without choosing among its lines.
func checkLogText(repo string, change int, checks hq.ChecksAnswer, log *hq.CheckLogAnswer) string {
	if log == nil {
		head := checkLogValue(checks.Head)
		if len(checks.Checks) == 0 {
			return fmt.Sprintf("No check run is failing on commit %s.", head)
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Failing checks of change #%d in %s at commit %s:", change, repo, head)
		for _, check := range checks.Checks {
			fmt.Fprintf(&b, "\n- %s: %s, check run %d, %s", check.Name, checkLogValue(check.Conclusion), check.ID, check.URL)
		}
		return b.String()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s), check run %d, commit %s: %d lines read, %d selected, %d shown.",
		log.Check.Name, checkLogValue(log.Check.Conclusion), log.Check.ID, checkLogValue(log.Head), log.Lines, log.Selected, len(log.Entries))
	if !log.Whole {
		b.WriteString(" The log is longer than HQ reads: these are of its first part.")
	}
	for _, entry := range log.Entries {
		fmt.Fprintf(&b, "\n%d: %s", entry.N, entry.Text)
		if entry.Cut {
			b.WriteString(" …[cut]")
		}
	}
	return b.String()
}

// checkLogValue keeps an absent HQ value distinct from an empty string.
func checkLogValue(value *string) string {
	const absent = "null"
	if value == nil {
		return absent
	}
	return *value
}

// checkLogRefusal says what HQ's refusal means for another read.
func checkLogRefusal(err error) string {
	const conflict = "conflict"
	if hq.IsUnavailable(err) {
		return "GitHub or HQ did not answer; nothing to fix on your side, ask again later."
	}
	var refusal *hq.RefusedError
	if !errors.As(err, &refusal) {
		return fmt.Sprintf("HQ could not answer: %v.", err)
	}
	switch {
	case refusal.Status == http.StatusConflict && refusal.Code == conflict && refusal.Reason == "actions_not_granted":
		return "HQ's GitHub app may not read Actions logs for this repository (it lacks Actions: read). Nothing reads the log until a person grants it on GitHub: tell your person. The checks' pages are in the list."
	case refusal.Status == http.StatusConflict && refusal.Code == conflict && refusal.Reason == "check_has_no_log":
		return "GitHub keeps no log for this check run: it is not a GitHub Actions job, is still running, or its log expired."
	case refusal.Status == http.StatusNotFound && refusal.Code == "not_found" && refusal.Reason == "check_not_found":
		return "No such check run on this change's commit. Call the tool without `check` for the ones failing now."
	case refusal.Status == http.StatusForbidden && refusal.Code == "forbidden" && refusal.Reason == "not_your_change",
		refusal.Status == http.StatusConflict && refusal.Code == conflict && refusal.Reason == "change_not_open",
		refusal.Status == http.StatusNotFound && refusal.Code == "change_not_found":
		return "This Mate reads the checks of its own open changes only; `status` lists them."
	case refusal.Status == http.StatusBadRequest && refusal.Code == "invalid":
		return fmt.Sprintf("HQ says `%s` is not valid.", refusal.Reason)
	case refusal.Status == http.StatusConflict && refusal.Code == conflict && refusal.Reason == "github_unavailable",
		refusal.Status == http.StatusServiceUnavailable:
		return "GitHub or HQ did not answer; nothing to fix on your side, ask again later."
	case (refusal.Status == http.StatusNotFound || refusal.Status == http.StatusMethodNotAllowed) && refusal.Reason == "" &&
		(refusal.Code == "" || refusal.Code == "not_found" || refusal.Code == "method_not_allowed"):
		return "This HQ has no check reads yet."
	default:
		return fmt.Sprintf("HQ refused the check read (%v).", refusal)
	}
}
