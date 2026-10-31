package topology

import (
	"errors"
	"regexp"
	"strconv"
)

// Invocation is immutable provenance captured for one actual native tool call.
// An empty ID is only the ordinary CLI/manual path, never a managed task call.
type Invocation struct {
	ID    string          `json:"id,omitempty"`
	Scope InvocationScope `json:"scope"`
}
type InvocationScope struct {
	Kind       string `json:"kind"`
	TaskID     string `json:"taskId,omitempty"`
	Assignment string `json:"assignment,omitempty"`
}

var invocationID = regexp.MustCompile(`^[0-9a-f]{64}$`)
var invocationTaskID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func (i Invocation) Validate() error {
	if i.ID != "" && !invocationID.MatchString(i.ID) {
		return errors.New("invalid invocation identity")
	}
	switch i.Scope.Kind {
	case "manual":
		if i.Scope.TaskID == "" && i.Scope.Assignment == "" {
			return nil
		}
	case "task":
		n, err := strconv.ParseInt(i.Scope.Assignment, 10, 64)
		if i.ID != "" && invocationTaskID.MatchString(i.Scope.TaskID) && err == nil && n > 0 && strconv.FormatInt(n, 10) == i.Scope.Assignment {
			return nil
		}
	}
	return errors.New("invocation scope is unknown or malformed")
}
