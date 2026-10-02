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
// it in with its changes there.
type MateState struct {
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	Face      string `json:"face"`
	// StandupRequestedBy is the person who asked for the stand-up, or nil.
	StandupRequestedBy *string `json:"standupRequestedBy"`
	// ClosedOff is the project closed off: its runtimes may be imported.
	ClosedOff bool `json:"closedOff"`
	// AppID is the application HQ holds the Mate in, nil for none.
	AppID *string `json:"appId"`
	// AppName is that application's name, nil while HQ names it nothing.
	AppName *string `json:"appName"`
	// Changes are, in each of that application's repositories, the Mate's
	// latest changes, newest first: its open one, if any, is the newest.
	Changes []MateChange `json:"changes"`
}

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

// ClosedOffReader reads whether this Mate's project is closed off: what an
// import of its runtimes waits for. An error is "not known", never "open".
type ClosedOffReader func(ctx context.Context) (bool, error)

// ReadClosedOff reads it from the HQ the enrollment at path names.
func ReadClosedOff(httpClient Doer, path string) ClosedOffReader {
	return func(ctx context.Context) (bool, error) {
		client, err := Open(httpClient, path)
		if err != nil {
			return false, err
		}
		state, err := client.Self(ctx)
		if err != nil {
			return false, err
		}
		return state.ClosedOff, nil
	}
}
