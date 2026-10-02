package hq

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// ErrNotEnrolled is a Mate holding no enrollment yet: nothing to ask HQ with.
var ErrNotEnrolled = errors.New("not enrolled with HQ yet")

// MateState is the Mate as HQ holds it (GET /api/mate/self): its record and
// its birth, written by the client that set it up.
type MateState struct {
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	Face      string `json:"face"`
	// StandupRequestedBy is the person who asked for the stand-up, or nil.
	StandupRequestedBy *string `json:"standupRequestedBy"`
	// ClosedOff is the project closed off: its runtimes may be imported.
	ClosedOff bool `json:"closedOff"`
}

// Self reads this Mate's state from the HQ it enrolled with (the enrollment
// at path).
func Self(ctx context.Context, httpClient *http.Client, path string) (MateState, error) {
	kept, found, err := LoadEnrollment(path)
	if err != nil {
		return MateState{}, err
	}
	if !found {
		return MateState{}, ErrNotEnrolled
	}
	var state MateState
	if err := (hqClient{http: httpClient, address: kept.HQ}).call(ctx, http.MethodGet, "/api/mate/self", "Mate "+kept.Credential, nil, &state); err != nil {
		return MateState{}, fmt.Errorf("mate state: %w", err)
	}
	return state, nil
}

// ClosedOffReader reads whether this Mate's project is closed off: what an
// import of its runtimes waits for. An error is "not known", never "open".
type ClosedOffReader func(ctx context.Context) (bool, error)

// ReadClosedOff reads it from HQ (Self) with the enrollment at path.
func ReadClosedOff(httpClient *http.Client, path string) ClosedOffReader {
	return func(ctx context.Context) (bool, error) {
		state, err := Self(ctx, httpClient, path)
		if err != nil {
			return false, err
		}
		return state.ClosedOff, nil
	}
}
