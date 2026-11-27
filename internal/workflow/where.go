package workflow

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zeropsio/zcp/internal/topology"
)

// WhereBudgetBytes bounds the text placed in front of a turn's message.
const WhereBudgetBytes = 1024

// EditUnknown is a step whose inputs were not read.
const EditUnknown = "unknown"

// EditHandedOver is a step whose change is ready at the published head.
const EditHandedOver = "handed over"

// WhereFacts holds the facts read for a turn, including reasons for missed reads.
type WhereFacts struct {
	ReadAt    time.Time
	Lease     string
	EditSteps bool
	Copies    []WhereCopy
	Stages    []WhereStage
	Changes   []HQChangeLine // open changes without a working copy
}

// WhereCopy is one lease or non-pool dev half and what its readers found.
type WhereCopy struct {
	Name, Part, Branch string
	Pool, Path         string
	Lease              bool
	State              string
	Unverified         bool
	Unsaved            *topology.LeaseUnsaved
	Dirty              bool
	WorkUnread         string
	DevServers         []topology.DevServerState
	ServersUnread      string
	HasHQ              bool
	Change             *HQChangeLine
	Step               *EditStep
	BranchRef          *topology.BranchRef
}

// EditCopy is the checkout and branch ref as read for this call.
type EditCopy struct {
	Name   string
	Dirty  bool
	Unread string
	Ref    *topology.BranchRef
}

// EditChange is HQ's open change, as answered for this branch now.
type EditChange struct {
	Number int
	Head   string
	Ready  *bool
}

// EditStep is the next action and the copy that needs it.
type EditStep struct {
	Action string
	Copy   string
	Reason string
	Head   string
	Number int
}

// PlanEdit decides the next step from the copies' own reads and HQ's answer.
func PlanEdit(copies []EditCopy, change *EditChange, fresh bool) EditStep {
	if len(copies) == 0 {
		return EditStep{Action: "acquire"}
	}
	if workcopy := copies[0]; workcopy.Unread != "" {
		return EditStep{Action: EditUnknown, Copy: workcopy.Name, Reason: workcopy.Unread}
	} else if workcopy.Ref == nil {
		return EditStep{Action: EditUnknown, Copy: workcopy.Name, Reason: "branch ref was not read"}
	}
	if !fresh {
		return EditStep{Action: EditUnknown, Reason: "HQ was not read"}
	}
	for _, workcopy := range copies {
		if workcopy.Unread == "" && workcopy.Ref != nil && workcopy.Dirty {
			return EditStep{Action: "commit", Copy: workcopy.Name}
		}
	}
	for _, workcopy := range copies {
		if workcopy.Unread != "" || workcopy.Ref == nil {
			continue
		}
		if workcopy.Ref.HistoryUnknown {
			return EditStep{Action: "pull then push if needed", Copy: workcopy.Name}
		}
		if workcopy.Ref.Behind > 0 && workcopy.Ref.Ahead > 0 {
			return EditStep{Action: "pull then push", Copy: workcopy.Name}
		}
		if workcopy.Ref.Behind > 0 {
			return EditStep{Action: "pull", Copy: workcopy.Name}
		}
	}
	for _, workcopy := range copies {
		if workcopy.Unread != "" || workcopy.Ref == nil {
			continue
		}
		if !workcopy.Ref.Published || workcopy.Ref.Ahead > 0 {
			return EditStep{Action: "push", Copy: workcopy.Name}
		}
	}
	for _, workcopy := range copies[1:] {
		if workcopy.Unread != "" || workcopy.Ref == nil {
			return EditStep{Action: "read", Copy: workcopy.Name}
		}
	}
	if change == nil {
		return EditStep{Action: "open change with a description", Head: copies[0].Ref.Head}
	}
	if change.Head == "" || change.Head != copies[0].Ref.Head || change.Ready == nil {
		return EditStep{Action: EditUnknown, Reason: "HQ's change head or readiness was not read at this head"}
	}
	if !*change.Ready {
		return EditStep{Action: "describe change", Head: change.Head, Number: change.Number}
	}
	return EditStep{Action: EditHandedOver, Head: change.Head, Number: change.Number}
}

// Words names the action and, when relevant, the copy or change it concerns.
func (step EditStep) Words() string {
	if step.Action == EditUnknown {
		return "unknown: " + step.Reason
	}
	if step.Action == EditHandedOver {
		return fmt.Sprintf("handed over at %s (change #%d)", ShortEditHead(step.Head), step.Number)
	}
	if step.Copy != "" {
		return step.Action + " in " + step.Copy
	}
	return step.Action
}

// ShortEditHead is the commit identifier shown in edit handover replies.
func ShortEditHead(head string) string {
	if len(head) > 7 {
		head = head[:7]
	}
	return head
}

// WhereStage is a stage's active version, or the reason it was not read.
type WhereStage struct {
	Hostname, Part, Version, Unread string
}

// RenderWhere is the one wording of the facts sent to an agent starting work.
func RenderWhere(facts WhereFacts) string {
	if len(facts.Copies)+len(facts.Stages)+len(facts.Changes) == 0 {
		return ""
	}
	copies := slices.Clone(facts.Copies)
	slices.SortStableFunc(copies, func(a, b WhereCopy) int {
		if whereIsNamed(a, facts.Lease) != whereIsNamed(b, facts.Lease) {
			if whereIsNamed(a, facts.Lease) {
				return -1
			}
			return 1
		}
		if a.Lease != b.Lease {
			if a.Lease {
				return -1
			}
			return 1
		}
		return cmp.Compare(a.Name, b.Name)
	})
	stages := slices.Clone(facts.Stages)
	slices.SortStableFunc(stages, func(a, b WhereStage) int { return cmp.Compare(a.Hostname, b.Hostname) })
	changes := slices.Clone(facts.Changes)
	slices.SortStableFunc(changes, func(a, b HQChangeLine) int {
		return cmp.Or(cmp.Compare(a.Branch, b.Branch), cmp.Compare(a.Number, b.Number))
	})

	header := "Where the work stands, read by zcp at " + facts.ReadAt.UTC().Format(time.RFC3339) + ". A tool result you received after that is newer."
	lines := []string{header}
	named := len(copies) > 0 && whereIsNamed(copies[0], facts.Lease)
	for _, worktree := range copies {
		lines = append(lines, whereCopyLine(worktree, whereIsNamed(worktree, facts.Lease), 0))
	}
	for _, stage := range stages {
		version := "runs " + stage.Version
		if stage.Unread != "" {
			version = "version not read (" + stage.Unread + ")"
		} else if stage.Version == "" {
			version = "runs no deployed version"
		}
		lines = append(lines, fmt.Sprintf("stage %s (part %s) %s", stage.Hostname, stage.Part, version))
	}
	for _, change := range changes {
		line := fmt.Sprintf("change #%d %s%s: no working copy", change.Number, change.Branch, whereLabels(change.Labels))
		if facts.EditSteps {
			line += "; step: " + PlanEdit(nil, nil, false).Words()
		}
		lines = append(lines, line)
	}
	if len(strings.Join(lines, "\n")) <= WhereBudgetBytes {
		return strings.Join(lines, "\n")
	}

	// The named line is protected even if its list of servers is oversized.
	// Reserve room for the worst-case count of other lines dropped below.
	if named {
		reserve := 0
		if len(lines) > 2 {
			reserve = 1 + len(whereMoreLines(len(lines)-2))
		}
		limit := WhereBudgetBytes - len(header) - 1 - reserve
		lines[1] = whereCopyLine(copies[0], true, limit)
	}
	protected := 1
	if named {
		protected++
	}
	original := len(lines)
	for len(lines) > protected && len(strings.Join(append(lines, whereMoreLines(original-len(lines))), "\n")) > WhereBudgetBytes {
		lines = lines[:len(lines)-1]
	}
	if dropped := original - len(lines); dropped > 0 {
		lines = append(lines, whereMoreLines(dropped))
	}
	return strings.Join(lines, "\n")
}

func whereIsNamed(worktree WhereCopy, alias string) bool {
	return worktree.Lease && alias != "" && worktree.Name == alias
}

func whereMoreLines(n int) string {
	return fmt.Sprintf("%d more lines: zerops_workflow action=\"status\" lists them.", n)
}

func whereCopyLine(worktree WhereCopy, named bool, limit int) string {
	name := worktree.Name
	if named {
		name += " (this conversation's)"
	}
	work := "clean"
	switch {
	case worktree.Lease && worktree.State != "held":
		work = worktree.State
	case worktree.WorkUnread != "":
		work = "working copy not read (" + worktree.WorkUnread + ")"
	case worktree.Lease && worktree.Unsaved == nil:
		work = "working copy not read (no answer)"
	case worktree.Lease:
		var unsaved []string
		if worktree.Unsaved.FileCount > 0 {
			unsaved = append(unsaved, fmt.Sprintf("%d files uncommitted", worktree.Unsaved.FileCount))
		}
		unpushed := worktree.Unsaved.UnpushedCommits
		if worktree.Step != nil {
			unpushed = 0
			if worktree.BranchRef != nil {
				unpushed = worktree.BranchRef.Ahead
			}
		}
		if unpushed > 0 {
			unsaved = append(unsaved, fmt.Sprintf("%d commits unpushed", unpushed))
		}
		if len(unsaved) > 0 {
			work = strings.Join(unsaved, ", ")
		}
	case worktree.Dirty:
		work = "uncommitted changes"
	}
	if worktree.Unverified {
		work += " (unverified)"
	}
	base := fmt.Sprintf("%s: ", name)
	if worktree.Step != nil {
		base += "step: " + worktree.Step.Words() + "; "
	}
	change := ""
	if worktree.Branch != "main" && worktree.HasHQ {
		switch {
		case worktree.Change == nil:
			change = "no change yet"
		case worktree.Change.Landing() != "":
			change = fmt.Sprintf("change #%d %s", worktree.Change.Number, worktree.Change.Landing())
		default:
			labels := worktree.Change.Labels
			if limit > 0 {
				labels = slices.Clone(labels)
				for i := range labels {
					labels[i] = whereClip(labels[i], 160)
				}
			}
			change = fmt.Sprintf("change #%d%s", worktree.Change.Number, whereLabels(labels))
		}
	}
	if worktree.Step != nil && change != "" {
		base += change + "; "
	}
	branch := worktree.Branch
	if branch == "" {
		branch = "(detached)"
	}
	base += fmt.Sprintf("part %s, branch %s, %s", worktree.Part, branch, work)
	if worktree.Step == nil && change != "" {
		base += ", " + change
	}
	servers := make([]string, 0, len(worktree.DevServers)+1)
	switch {
	case worktree.ServersUnread != "":
		servers = append(servers, "dev servers not read ("+worktree.ServersUnread+")")
	case worktree.DevServers == nil:
		servers = append(servers, "dev servers not read (no answer)")
	case len(worktree.DevServers) == 0:
		servers = append(servers, "no dev server")
	default:
		for _, server := range worktree.DevServers {
			item := "dev server " + server.Name + " " + server.Status
			if server.URL != "" {
				item += " at " + server.URL
			}
			servers = append(servers, item)
		}
	}
	line := base + "; " + strings.Join(servers, ", ")
	if limit <= 0 || len(line) <= limit {
		return line
	}
	if len(worktree.DevServers) == 0 || worktree.ServersUnread != "" {
		return whereClip(line, limit)
	}
	for n := len(servers) - 1; n >= 0; n-- {
		suffix := fmt.Sprintf("+%d more (zcp status)", len(servers)-n)
		items := append(slices.Clone(servers[:n]), suffix)
		line = base + "; " + strings.Join(items, ", ")
		if len(line) <= limit {
			return line
		}
	}
	suffix := fmt.Sprintf("; +%d more (zcp status)", len(servers))
	return whereClip(base, limit-len(suffix)) + suffix
}

func whereLabels(labels []string) string {
	if len(labels) == 0 {
		return ""
	}
	return " (" + strings.Join(labels, " · ") + ")"
}

func whereClip(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	const ellipsis = "…"
	if limit < len(ellipsis) {
		return ""
	}
	cut := limit - len(ellipsis)
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + ellipsis
}
