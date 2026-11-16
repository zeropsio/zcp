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

// StandupRowOf is the one derivation of a service's verdict row and the one
// owner of its next step. A failed version wins over every other cause, and
// its Cause is the build's words, not a failure met on the way (a failed
// first pool build leaves its bearer reference literal: one cause, not two).
// A version a newer one replaced (BACKUP) failed too: the commit no longer
// runs. A pool is done only when its version is active and its bearer
// reference resolves, never from the version alone. Next is "" unless the
// row failed.
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
		row.Cause, row.Next = versionWords(in), rebuildStep(in)
	case in.AppVersionStatus == platform.BuildStatusBackup:
		row.State, row.Class = mate.RowFailed, platform.ClassFailed
		row.Cause = versionWords(in) + ": a newer version replaced it"
		row.Next = rerunStep
	case in.Class != "":
		row.State, row.Class = mate.RowFailed, in.Class
		if active && bearerMissing && in.Class == platform.ClassUnreachable {
			row.Cause, row.Next = bearerCause(in)
		} else {
			row.Cause, row.Next = in.Cause, classStep(in.Class)
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
// failed row makes it failed whatever the others are (pools done and a stage
// failed is failed: only failed starts the agent's turn); ready needs a pool
// row; no rows (nothing adopted yet) is building, never done.
func StandupHeadlineOf(rows []mate.StandupRow) mate.StandupHeadline {
	if len(rows) == 0 {
		return mate.HeadlineBuilding
	}
	pools := 0
	poolsDone, allDone := true, true
	for _, r := range rows {
		if r.State == mate.RowFailed {
			return mate.HeadlineFailed
		}
		if r.Kind == mate.RowPool {
			pools++
		}
		if r.State != mate.RowDone {
			allDone = false
			poolsDone = poolsDone && r.Kind != mate.RowPool
		}
	}
	if allDone {
		return mate.HeadlineDone
	}
	if poolsDone && pools > 0 {
		return mate.HeadlineReady
	}
	return mate.HeadlineBuilding
}

// versionFailed reports a version or its build-and-deploy process that ended
// failed or cancelled.
func versionFailed(versionStatus, processStatus string) bool {
	switch versionStatus {
	case platform.BuildStatusBuildFailed, platform.BuildStatusPreparingRuntimeFail, platform.BuildStatusDeployFailed,
		buildStatusValidationFailed, appVersionStatusCancelled:
		return true
	}
	return processStatus == statusFailed || processStatus == statusCanceled
}

// The app-version statuses beyond platform's build constants that end a
// version failed.
const (
	buildStatusValidationFailed = "BUILD_VALIDATION_FAILED"
	appVersionStatusCancelled   = "CANCELLED"
)

// versionWords is the platform's words for the version and its process,
// naming only what is known.
func versionWords(in StandupRowInput) string {
	words := "app version"
	if in.AppVersionID != "" {
		words += " " + in.AppVersionID
	}
	if in.Commit != "" {
		words += " of " + in.Commit
	}
	words += " is " + orNamed(in.AppVersionStatus, "unknown")
	if in.ProcessStatus != "" {
		words += ", process"
		if in.ProcessID != "" {
			words += " " + in.ProcessID
		}
		words += " is " + in.ProcessStatus
	}
	return words
}

// rerunStep ends every step that changes main: no agent tool commit-deploys
// a branch's commit, the stand-up commit-deploys main.
const rerunStep = "run zerops_standup again: it commit-deploys main"

// rebuildStep is how a failed version of in's service is fixed: on a branch,
// merged to main through the HQ change, then the stand-up again. Its evidence
// is the runtime log when the container failed to start (DEPLOY_FAILED), the
// build's log otherwise. A pool whose build failed offers no container to
// work in, so its branch is worked on zcp's own container; a stage's on a
// branch lease of a pool.
func rebuildStep(in StandupRowInput) string {
	evidence := "the build log of " + orNamed(in.ProcessID, "its build")
	if in.AppVersionStatus == platform.BuildStatusDeployFailed {
		evidence = in.Hostname + "'s runtime log"
	}
	where := "a branch lease"
	if in.Kind == mate.RowPool {
		where = "a branch in a checkout on zcp's own container (the pool has no build to lease)"
	}
	return "read " + evidence + ", fix the code on " + where + ", merge it to main through the HQ change, then " + rerunStep
}

// bearerCause names the pool's bearer variable that does not resolve, and
// the call that writes it.
func bearerCause(in StandupRowInput) (cause, next string) {
	variable := in.Hostname + "_" + leaseTokenAliasKey
	cause = variable + " (${" + in.Hostname + "_" + leaseTokenKey + "}) does not resolve on zcp"
	if in.Cause != "" {
		cause += ": " + in.Cause
	}
	return cause, "acquire a lease of " + in.Hostname + " (zerops_lease): the acquire writes " + variable + " again"
}

// classStep is the next step for a failure of class c met outside a build
// (c is never "" here).
func classStep(c platform.ErrorClass) string {
	switch c {
	case platform.ClassRefused:
		return "change what the refusal names, then " + rerunStep
	case platform.ClassUnreachable:
		return "once the target answers, " + rerunStep
	case platform.ClassFailed:
		return "fix what the cause names, then " + rerunStep
	}
	return ""
}

// orNamed is s, or name when s is "".
func orNamed(s, name string) string {
	if s == "" {
		return name
	}
	return s
}
