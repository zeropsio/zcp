package ops

import (
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

// MirrorRequest asks the person to connect one of the application's
// repositories in HQ to GitHub (docs/spec-mate.md §5.11): Mate draws a card
// with the person's GitHub accounts and repositories, and the person connects
// it through HQ themselves, so no token or address crosses the conversation.
type MirrorRequest struct {
	// Repo is the repository in HQ to connect: its name in the application,
	// a dev hostname.
	Repo string `json:"repo"`
	// Why is the one line the card shows, "" when the agent gives none.
	Why string `json:"why,omitempty"`
}

// MirrorConnection is a repository that already has a remote in HQ: nothing
// is asked of the person.
type MirrorConnection struct {
	Repo string `json:"repo"`
	// URL is the remote's https address as HQ lists it, never with a
	// credential.
	URL string `json:"url"`
}

// MirrorWithdrawal is a mirror ask the agent takes back: the repository the
// withdrawn ask named.
type MirrorWithdrawal struct {
	Repo string `json:"repo"`
}

// NewMirrorRequest checks a mirror request and writes nothing: repo names a
// repository in HQ, and why, when given, is one line Mate shows whole.
func NewMirrorRequest(repo, why string) (MirrorRequest, error) {
	repo, err := mirrorRepo(repo)
	if err != nil {
		return MirrorRequest{}, err
	}
	if strings.TrimSpace(why) != "" {
		if err := validateRequestLine("why", why, requestWhyMax,
			"why: why the code should be on GitHub, one line Mate shows whole; or leave it out."); err != nil {
			return MirrorRequest{}, err
		}
	}
	return MirrorRequest{Repo: repo, Why: strings.TrimSpace(why)}, nil
}

// NewMirrorWithdrawal checks a withdrawal and writes nothing.
func NewMirrorWithdrawal(repo string) (MirrorWithdrawal, error) {
	repo, err := mirrorRepo(repo)
	if err != nil {
		return MirrorWithdrawal{}, err
	}
	return MirrorWithdrawal{Repo: repo}, nil
}

// mirrorRepo is repo checked as the name of a repository in HQ, which names
// each after its pair's dev hostname.
func mirrorRepo(repo string) (string, error) {
	repo = strings.TrimSpace(repo)
	if perr := platform.ValidateHostname(repo); perr != nil {
		perr.Suggestion = "repo is the repository in HQ to connect, by its name in the application — the dev service's hostname (app)."
		return "", perr
	}
	return repo, nil
}
