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
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// CheckLogInput names a change and, optionally, the check and lines to read.
type CheckLogInput struct {
	Repo   string  `json:"repo"            jsonschema:"Repository name as shown by status and the notice."`
	Change int     `json:"change"          jsonschema:"The change's number."`
	Check  *int64  `json:"check,omitempty" jsonschema:"GitHub check run id; omit to list failing checks."`
	Setup  string  `json:"setup,omitempty" jsonschema:"Review build setup; omit to list failing checks."`
	From   *int64  `json:"from,omitempty"  jsonschema:"First log line, numbered from 1."`
	To     *int64  `json:"to,omitempty"    jsonschema:"Last log line, inclusive."`
	Match  *string `json:"match,omitempty" jsonschema:"RE2 pattern selecting log lines."`
}

// RegisterCheckLog lets a Mate read its own change's checks through HQ.
func RegisterCheckLog(srv *mcp.Server, httpClient hq.Doer, rt runtime.Info, platformClient platform.Client, logFetcher platform.LogFetcher, projectID string) {
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
		Description: "Read failing GitHub checks and Review build parts on your change. Without check or setup, list both; with check, read its GitHub log, optionally from..to or matching match; with setup, read that Review build's platform log or health observation.",
		Annotations: &mcp.ToolAnnotations{Title: "Read failing checks and logs", ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: boolPtr(false)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in CheckLogInput) (*mcp.CallToolResult, any, error) {
		if in.Check != nil && in.Setup != "" {
			return observeFailure("Choose either `check` or `setup`."), nil, nil
		}
		if in.Setup != "" && (in.From != nil || in.To != nil || in.Match != nil) {
			return observeFailure("`from`, `to` and `match` select GitHub check logs; a Review build returns its last lines."), nil, nil
		}
		client, ok := openHQ(httpClient, rt)
		if !ok {
			return observeFailure("GitHub or HQ did not answer; nothing to fix on your side, ask again later."), nil, nil
		}
		client = client.Once().Bounded(3*time.Second, 10*time.Second)
		var log *hq.CheckLogAnswer
		if in.Check != nil {
			answer, err := client.CheckLog(ctx, in.Repo, in.Change, *in.Check, hq.CheckLogQuery{From: in.From, To: in.To, Match: in.Match})
			if err != nil {
				return observeFailure(checkLogRefusal(in.Change, err)), nil, nil
			}
			log = &answer
		}
		// The log names the run's head. Read the change's current head from
		// HQ in this call, after the log, rather than from the Mate state.
		checks, err := client.FailingChecks(ctx, in.Repo, in.Change)
		if err != nil {
			return observeFailure(checkLogRefusal(in.Change, err)), nil, nil
		}
		if in.Setup != "" {
			return textResult(reviewBuildLog(ctx, platformClient, logFetcher, projectID, in.Setup, checks)), nil, nil
		}
		return textResult(checkLogText(in.Repo, in.Change, checks, log)), nil, nil
	})
}

// checkLogText renders HQ's checks or log without choosing among its lines.
func checkLogText(repo string, change int, checks hq.ChecksAnswer, log *hq.CheckLogAnswer) string {
	if log == nil {
		head := checkLogValue(checks.Head)
		if len(checks.Checks) == 0 && (checks.ReviewBuild == nil || len(checks.ReviewBuild.Parts) == 0) {
			return fmt.Sprintf("No check run is failing on commit %s.", head)
		}
		var b strings.Builder
		if len(checks.Checks) > 0 {
			fmt.Fprintf(&b, "Failing checks of change #%d in %s at commit %s:", change, repo, head)
			for _, check := range checks.Checks {
				fmt.Fprintf(&b, "\n- %s: %s, check run %d, %s", check.Name, checkLogValue(check.Conclusion), check.ID, check.URL)
			}
		}
		if checks.ReviewBuild != nil && len(checks.ReviewBuild.Parts) > 0 {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			fmt.Fprintf(&b, "Review build failed for change #%d in %s at commit %s:", change, repo, checks.ReviewBuild.Head)
			if checks.Head != nil && checks.ReviewBuild.Head != *checks.Head {
				fmt.Fprintf(&b, " This Review build is for commit %s; the change is now at %s.", checks.ReviewBuild.Head, *checks.Head)
			}
			for _, part := range checks.ReviewBuild.Parts {
				fmt.Fprintf(&b, "\n- setup %s, stage %s, phase %s, app version %s: %s", part.Setup, part.Stage, part.Phase, part.AppVersionID, part.Reason)
			}
			b.WriteString("\nCall zerops_check_log with setup to read a part.")
		}
		return b.String()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s), check run %d, commit %s: %d lines read, %d selected, %d shown.",
		log.Check.Name, checkLogValue(log.Check.Conclusion), log.Check.ID, checkLogValue(log.Head), log.Lines, log.Selected, len(log.Entries))
	if log.Head != nil && checks.Head != nil && *log.Head != *checks.Head {
		fmt.Fprintf(&b, " This log is for commit %s; the change is now at %s.", *log.Head, *checks.Head)
	}
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

// reviewBuildLog reads the setup HQ named, from the platform rather than HQ.
func reviewBuildLog(ctx context.Context, client platform.Client, fetcher platform.LogFetcher, projectID, setup string, checks hq.ChecksAnswer) string {
	if checks.ReviewBuild == nil {
		return fmt.Sprintf("No failing Review build part for setup %s.", setup)
	}
	for _, part := range checks.ReviewBuild.Parts {
		if part.Setup != setup {
			continue
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Review build setup %s, stage %s, commit %s, phase %s, app version %s: %s.", part.Setup, part.Stage, checks.ReviewBuild.Head, part.Phase, part.AppVersionID, part.Reason)
		if checks.Head != nil && checks.ReviewBuild.Head != *checks.Head {
			fmt.Fprintf(&b, " This Review build is for commit %s; the change is now at %s.", checks.ReviewBuild.Head, *checks.Head)
		}
		if part.Phase == "health" {
			if part.Probe != nil {
				fmt.Fprintf(&b, " Probe %s at %s", part.Probe.URL, part.Probe.At)
				if part.Probe.Status != nil {
					fmt.Fprintf(&b, ", HTTP %d", *part.Probe.Status)
				}
				b.WriteByte('.')
			}
			return b.String()
		}
		lines, err := ops.FetchVersionBuildLogs(ctx, client, fetcher, projectID, part.Stage, part.AppVersionID, 100)
		switch {
		case err != nil:
			fmt.Fprintf(&b, " The platform log could not be read: %v.", err)
		case len(lines) == 0:
			b.WriteString(" The platform keeps no log for this version.")
		default:
			b.WriteString(" Last platform log lines:")
			lines = reviewLogWithoutBanner(lines)
			for _, line := range lines {
				fmt.Fprintf(&b, "\n%s", line)
			}
		}
		return b.String()
	}
	return fmt.Sprintf("No failing Review build part for setup %s.", setup)
}

// Remove the complete builder startup banner only when it begins the read;
// a partial tail or unfamiliar output must remain available for diagnosis.
func reviewLogWithoutBanner(entries []string) []string {
	lines := strings.Split(strings.Join(entries, "\n"), "\n")
	title := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "ZEROPS BUILD CONTAINER STARTED") {
			title = i
			break
		}
		if trimmed != "" && !strings.HasPrefix(trimmed, "BUILD appVersionId:") && !strings.HasPrefix(trimmed, "┏") {
			return entries
		}
	}
	if title < 0 {
		return entries
	}
	for i := title + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "┏") {
			return entries
		}
		if len(strings.ReplaceAll(trimmed, " ", "")) < 10 || strings.Trim(trimmed, "@ ") != "" {
			continue
		}
		start := i
		for i < len(lines) && strings.Trim(strings.TrimSpace(lines[i]), "@ ") == "" && strings.Contains(lines[i], "@") {
			i++
		}
		if i-start < 2 || strings.Trim(strings.TrimSpace(lines[i-1]), "@") != "" {
			return entries
		}
		for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
			i++
		}
		return lines[i:]
	}
	return entries
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
func checkLogRefusal(change int, err error) string {
	const conflict = "conflict"
	if hq.IsUnavailable(err) {
		return "GitHub or HQ did not answer; nothing to fix on your side, ask again later."
	}
	var refusal *hq.RefusedError
	if !errors.As(err, &refusal) {
		return fmt.Sprintf("HQ could not answer: %v.", err)
	}
	switch {
	case refusal.Status == http.StatusConflict && refusal.Code == conflict && refusal.Reason == "change_not_open":
		switch refusal.State {
		case "merged", "closed":
			return fmt.Sprintf("change #%d was %s.", change, refusal.State)
		default:
			return fmt.Sprintf("change #%d is not open.", change)
		}
	case refusal.Status == http.StatusConflict && refusal.Code == conflict && refusal.Reason == "actions_not_granted":
		return "HQ's GitHub app may not read Actions logs for this repository (it lacks Actions: read). Nothing reads the log until a person grants it on GitHub: tell your person. The checks' pages are in the list."
	case refusal.Status == http.StatusConflict && refusal.Code == conflict && refusal.Reason == "check_has_no_log":
		return "GitHub keeps no log for this check run: it is not a GitHub Actions job, is still running, or its log expired."
	case refusal.Status == http.StatusNotFound && refusal.Code == "not_found" && refusal.Reason == "check_not_found":
		return "No such check run on this change's commit. Call the tool without `check` for the ones failing now."
	case refusal.Status == http.StatusForbidden && refusal.Code == "forbidden" && refusal.Reason == "not_your_change",
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
