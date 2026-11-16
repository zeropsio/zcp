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
	// Class and Cause are a failure met on the way ("" for none).
	Class platform.ErrorClass
	Cause string
}

// StandupRowOf is the one derivation of a service's verdict row.
func StandupRowOf(in StandupRowInput) mate.StandupRow {
	return mate.StandupRow{}
}

// StandupHeadlineOf is the one derivation of the verdict's headline from its
// rows: failed, done, ready (to work) or building (mate.StandupHeadline).
func StandupHeadlineOf(rows []mate.StandupRow) mate.StandupHeadline {
	return ""
}
