package ops

import (
	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/platform"
)

// StandupRowInput is what one pooled service's verdict row is derived from:
// its app version named Commit and the process behind it, its bearer, and
// the class and words of a failure met on the way.
type StandupRowInput struct {
	Hostname string
	Kind     mate.StandupRowKind
	Commit   string
	// AppVersionID and AppVersionStatus are the version named Commit; ""
	// when none exists.
	AppVersionID     string
	AppVersionStatus string
	ProcessID        string
	ProcessStatus    string
	// BearerResolves is read for a pool row only.
	BearerResolves bool
	// Class and Cause are a failure met on the way ("" for none): one the
	// caller no longer waits on. A ClassUnreachable failure is passed only
	// once its bounded wait is over; while the bound lasts the caller passes
	// none, and the row stays building.
	Class platform.ErrorClass
	Cause string
}

// StandupRowOf is the one derivation of a service's verdict row. A failed
// version wins over every other cause (a failed first pool build leaves its
// bearer reference literal: one cause, not two); a pool is done only when its
// version is active and its bearer reference resolves, never from the
// version alone. A failed row's Cause is the source's words, then one next
// step after " — next: ".
func StandupRowOf(in StandupRowInput) mate.StandupRow {
	row := mate.StandupRow{
		Hostname:     in.Hostname,
		Kind:         in.Kind,
		Commit:       in.Commit,
		AppVersionID: in.AppVersionID,
		ProcessID:    in.ProcessID,
	}
	active := in.AppVersionStatus == statusActive
	bearerMissing := in.Kind == mate.RowPool && !in.BearerResolves
	switch {
	case versionFailed(in.AppVersionStatus, in.ProcessStatus):
		row.State, row.Class = mate.RowFailed, platform.ClassFailed
		row.Cause = withNextStep(buildWords(in), rebuildStep(in))
	case in.Class != "":
		row.State, row.Class = mate.RowFailed, in.Class
		if active && bearerMissing {
			row.Cause = bearerCause(in)
		} else {
			row.Cause = withNextStep(in.Cause, classStep(in.Class))
		}
	case active && !bearerMissing:
		row.State = mate.RowDone
	default:
		row.State = mate.RowBuilding
	}
	return row
}

// StandupHeadlineOf is the one derivation of the verdict's headline from its
// rows: failed, done, ready (to work) or building (mate.StandupHeadline). A
// failed row makes it failed whatever the others are; no rows (nothing
// adopted yet) is building, never done.
func StandupHeadlineOf(rows []mate.StandupRow) mate.StandupHeadline {
	if len(rows) == 0 {
		return mate.HeadlineBuilding
	}
	poolsDone, allDone := true, true
	for _, r := range rows {
		if r.State == mate.RowFailed {
			return mate.HeadlineFailed
		}
		if r.State != mate.RowDone {
			allDone = false
			if r.Kind == mate.RowPool {
				poolsDone = false
			}
		}
	}
	switch {
	case allDone:
		return mate.HeadlineDone
	case poolsDone:
		return mate.HeadlineReady
	}
	return mate.HeadlineBuilding
}

// versionFailed reports a version or its build-and-deploy process that ended
// failed.
func versionFailed(versionStatus, processStatus string) bool {
	switch versionStatus {
	case platform.BuildStatusBuildFailed, platform.BuildStatusPreparingRuntimeFail, platform.BuildStatusDeployFailed:
		return true
	}
	return processStatus == statusFailed || processStatus == statusCanceled
}

// buildWords is the failed build's cause: the caller's words (a build log
// line) when it has them, else the platform's statuses.
func buildWords(in StandupRowInput) string {
	if in.Cause != "" {
		return in.Cause
	}
	words := "app version " + in.AppVersionID + " of " + in.Commit + " is " + in.AppVersionStatus
	if in.ProcessStatus != "" {
		words += ", process " + in.ProcessID + " is " + in.ProcessStatus
	}
	return words
}

// rebuildStep is how a failed build of in's service is fixed. A pool whose
// build failed offers no container to work in, so its fix is written on
// zcp's own container; a stage's on a leased pool.
func rebuildStep(in StandupRowInput) string {
	where := "a leased pool"
	if in.Kind == mate.RowPool {
		where = "a checkout on zcp's own container"
	}
	return "read process " + in.ProcessID + "'s build log, fix the code in " + where +
		", push it as a branch through HQ and commit-deploy that branch's commit to " + in.Hostname
}

// bearerCause names the pool's bearer variable that does not resolve.
func bearerCause(in StandupRowInput) string {
	variable := in.Hostname + "_" + leaseTokenAliasKey
	words := variable + " (${" + in.Hostname + "_" + leaseTokenKey + "}) does not resolve on zcp"
	if in.Cause != "" {
		words += ": " + in.Cause
	}
	return withNextStep(words, "acquire a lease of "+in.Hostname+" (zerops_lease): the acquire writes "+variable+" again")
}

// classStep is the next step for a failure of class c met outside a build
// (c is never "" here).
func classStep(c platform.ErrorClass) string {
	switch c {
	case platform.ClassRefused:
		return "change what the refusal names, then run the stand-up again"
	case platform.ClassUnreachable:
		return "run the stand-up again once the target answers"
	case platform.ClassFailed:
		return "fix what the words name, then run the stand-up again"
	}
	return ""
}

func withNextStep(words, step string) string {
	return words + " — next: " + step
}
