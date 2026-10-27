package hq

import (
	"context"
	"fmt"
	"net/http"
)

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
	// changes, newest first: from an HQ that keeps an open change per branch
	// (OpenPerBranch), every open one and the latest MateChangesPerRepo
	// settled ones; from an older HQ, the latest MateChangesPerRepo in any
	// state — an open one older than those is not listed.
	Changes []MateChange `json:"changes"`
	// OpenPerBranch says HQ keeps a Mate an open change per branch it names,
	// beside its one change opened naming none, and lists every open change
	// in Changes. false from an HQ that keeps one open change per repository.
	// A capability, read before any call; an answer is still read by its
	// shape, since an HQ can roll back.
	OpenPerBranch bool `json:"openPerBranch,omitempty"`
	// Repos are the application's repositories in HQ, by name. nil from an HQ
	// older than repositories in the Mate's state.
	Repos []MateRepo `json:"repos"`
	// Production is whether that application has a production, of any
	// target, released or not. nil from an HQ older than it.
	Production *bool `json:"production"`
	// Chief is the Chief this Mate answers to: itself if it is a registered
	// Chief, otherwise its owner's usable Chief; nil while it has none.
	Chief *Chief `json:"chief"`
}

// Chief is the project of the Chief a Mate answers to, one person's Chief
// registered in HQ; a Mate compares it with its own project
// (docs/spec-mate.md §3.11).
type Chief struct {
	ProjectID string `json:"projectId"`
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
	// Branch is the branch the change is bound to, by its short name: one
	// its Mate named, or `mate/<project id>/<number>` of the Mate that opened
	// it. "" from an HQ older than branches in the Mate's state.
	Branch string `json:"branch,omitempty"`
	// Handed is a change a person handed this Mate from a Mate that is gone:
	// the Mate's own now, on the branch it was on.
	Handed bool `json:"handed,omitempty"`
	// Ready is whether the change is described at its head — asks for the
	// person's review — rather than a draft; nil from an HQ older than it.
	Ready *bool `json:"ready,omitempty"`
	// Mergeability is how the change merges into `main` as HQ last judged it
	// (clean, conflict, empty, already_merged, unrelated, unknown); "" from
	// an HQ older than it.
	Mergeability string `json:"mergeability,omitempty"`
	// Behind is whether `main` moved past the change's merge base, judged
	// with Mergeability; nil from an HQ older than it.
	Behind *bool `json:"behind,omitempty"`
	// UpdatedAt is when HQ last changed its record of the change, RFC 3339;
	// "" from an HQ older than it.
	UpdatedAt string `json:"updatedAt,omitempty"`
	// Verdict is Core's refusal to land the change at its head, as Core last
	// judged it — a recipe change Core lands by itself: head_moved (a push
	// arrived while it judged), recipe_changes_environments,
	// files_past_bound, or a git failure's kind; "" while Core refused
	// nothing, and from an HQ that names none.
	Verdict string `json:"verdict,omitempty"`
}

// MateChangesPerRepo is how many of a Mate's changes in a repository its own
// state carries: from an older HQ the latest in any state, from one that
// keeps an open change per branch the latest settled beside every open one
// (hqChanges.ts MATE_CHANGES_PER_REPO).
const MateChangesPerRepo = 10

// The ways HQ judges a change merges into `main` (hqChanges.ts
// MergeabilityKind) that zcp tells the agent about.
const (
	MergeClean    = "clean"
	MergeConflict = "conflict"
)

// Self reads this Mate's state from its HQ.
func (c Client) Self(ctx context.Context) (MateState, error) {
	var state MateState
	if err := c.call.json(ctx, http.MethodGet, "/api/mate/self", c.authorization(), nil, &state); err != nil {
		return MateState{}, fmt.Errorf("mate state: %w", err)
	}
	return state, nil
}

// IsChief reports whether the Chief this Mate answers to is its own project,
// that is, the Mate is a Chief: it then serves org mode (docs/spec-mate.md §2.11).
func (c Client) IsChief(ctx context.Context) (bool, error) {
	state, err := c.Self(ctx)
	if err != nil {
		return false, err
	}
	return state.Chief != nil && state.Chief.ProjectID == c.id.ProjectID, nil
}
