package hq

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// ErrNotEnrolled is a Mate holding no enrollment yet: nothing to ask HQ with.
var ErrNotEnrolled = errors.New("not enrolled with HQ yet")

// MateState is the Mate as HQ holds it (GET /api/mate/self): its record, its
// birth, written by the client that set it up, and the application HQ holds
// it in with its changes there. Whether its project is closed off is read
// from Zerops (ops.ProjectClosedOff): HQ's closedOff is the press's receipt,
// which zcp never reads (audit N1).
type MateState struct {
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	Face      string `json:"face"`
	// StandupRequestedBy is the person who asked for the stand-up, or nil.
	StandupRequestedBy *string `json:"standupRequestedBy"`
	// AppID is the application HQ holds the Mate in, nil for none.
	AppID *string `json:"appId"`
	// AppName is that application's name, nil while HQ names it nothing.
	AppName *string `json:"appName"`
	// Changes are, in each of that application's repositories, the Mate's
	// latest changes, newest first: its open one, if any, is the newest.
	Changes []MateChange `json:"changes"`
	// GitPushes are, for each Git environment of that application and each
	// repository, the newest push the origin took, in environment order. nil
	// from an HQ older than Git environments in the Mate's state.
	GitPushes []GitPush `json:"gitPushes"`
	// Repos are the application's repositories in HQ, by name. nil from an HQ
	// older than repositories in the Mate's state.
	Repos []MateRepo `json:"repos"`
}

// MateRepo is one of the application's repositories in HQ, with the origin
// connected to it, nil while none is.
type MateRepo struct {
	Name   string      `json:"name"`
	Origin *RepoOrigin `json:"origin"`
}

// RepoOrigin is a connected origin: its https address as HQ stores it, which
// never carries a credential.
type RepoOrigin struct {
	URL string `json:"url"`
}

// GitPush is a Git environment's newest confirmed push of one repository: the
// commit the origin took on the environment's branch, and what the origin's
// CI reported for it as HQ last read it — nil where HQ read none (a host other
// than GitHub).
type GitPush struct {
	Environment string  `json:"environment"`
	Tier        string  `json:"tier"`
	Branch      string  `json:"branch"`
	Repo        string  `json:"repo"`
	Sha         string  `json:"sha"`
	CI          *PushCI `json:"ci"`
}

// PushCI is HQ's record of the origin's CI on a pushed commit. Done of Total
// counted checks completed; Failed names each counted check that did not
// pass; Reason says why it is unreadable or why HQ stopped watching. Required
// lists the branch rules' required checks, which alone count; nil counts
// every check. HQ watches until Until at the latest.
type PushCI struct {
	Sha      string          `json:"sha"`
	Verdict  string          `json:"verdict"`
	Done     int             `json:"done"`
	Total    int             `json:"total"`
	Failed   []PushCIFailure `json:"failed"`
	Required []string        `json:"required"`
	Reason   *string         `json:"reason"`
	Watch    string          `json:"watch"`
	ReadAt   string          `json:"readAt"`
	Until    string          `json:"until"`
}

// PushCIFailure is one counted check that did not pass; URL is nil where the
// origin gave none.
type PushCIFailure struct {
	Name string  `json:"name"`
	URL  *string `json:"url"`
}

// PushCI.Verdict: no check reported, some still running, all passed, some
// failed, or the origin refused HQ the read.
const (
	CINone       = "none"
	CIRunning    = "running"
	CIPassed     = "passed"
	CIFailed     = "failed"
	CIUnreadable = "unreadable"
)

// PushCI.Watch: HQ still reads, every counted check completed, or HQ stopped
// reading before that.
const (
	CIWatching = "watching"
	CIFinished = "finished"
	CIStopped  = "stopped"
)

// MateChange is one of the Mate's own changes, as it reads its outcome.
type MateChange struct {
	Repo   string `json:"repo"`
	Number int    `json:"number"`
	// Title is the change's title; nil from an HQ older than titles in the
	// Mate's state.
	Title      *string `json:"title"`
	State      string  `json:"state"`
	Head       *string `json:"head"`
	MergedSha  *string `json:"mergedSha"`
	LandedHead *string `json:"landedHead"`
}

// Self reads this Mate's state from its HQ.
func (c Client) Self(ctx context.Context) (MateState, error) {
	var state MateState
	if err := c.call.json(ctx, http.MethodGet, "/api/mate/self", c.authorization(), nil, &state); err != nil {
		return MateState{}, fmt.Errorf("mate state: %w", err)
	}
	return state, nil
}
