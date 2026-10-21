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
	// Repos are the application's repositories in HQ, by name. nil from an HQ
	// older than repositories in the Mate's state.
	Repos []MateRepo `json:"repos"`
	// Production is whether that application has a production, of any
	// target, released or not. nil from an HQ older than it.
	Production *bool `json:"production"`
}

// MateRepo is one of the application's repositories in HQ, with the origin
// connected to it, nil while none is.
type MateRepo struct {
	Name   string      `json:"name"`
	Origin *RepoOrigin `json:"origin"`
}

// RepoOrigin is a connected origin: its https address as HQ stores it, which
// never carries a credential, and which way the person keeps the code.
type RepoOrigin struct {
	URL string `json:"url"`
	// Mode is OriginOnGitHub or OriginFollowed; "" from an HQ older than it.
	Mode string `json:"mode,omitempty"`
}

// The ways a person keeps a repository's code: on GitHub, GitHub being the
// checkout's origin; or in Mate, HQ the origin and GitHub only followed as a
// fetch-only upstream.
const (
	OriginOnGitHub = "github"
	OriginFollowed = "follow"
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
