package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// The Mate's Gitea wiring, written onto the `zcp` service by the app when the
// Mate is made (docs/vocabulary.md, "A Mate's environment"). zcp reads these
// three and nothing else; at sign-up they can land minutes after the Mate is
// up, so every reader treats absence as "not yet", never as "never".
const (
	// GiteaURLEnvKey is the account's Gitea origin. It is also the single
	// signal that makes a remote recognisable as that Gitea
	// (topology.ClassifyGitHost).
	GiteaURLEnvKey = "GITEA_URL"

	// MateBrokerURLEnvKey is the broker's origin — where a Mate asks for the
	// repository of a dev/stage pair. A Mate never creates one itself: the
	// broker owns which org it lands in, its protection, and the bot's
	// collaboration on it.
	MateBrokerURLEnvKey = "MATE_BROKER_URL"
)

// GiteaWiring is the three values together. The zero value is the honest
// state of a container the app has not written them onto yet.
type GiteaWiring struct {
	GiteaURL  string
	BrokerURL string
	Token     string
}

// Ready reports whether all three are present. A partial set is NOT usable:
// the broker call needs the token and the URL, and the identity/pull-request
// calls need the Gitea origin, so acting on two of three would half-configure
// a pair and leave the third failure to surface somewhere unrelated.
func (w GiteaWiring) Ready() bool {
	return w.GiteaURL != "" && w.BrokerURL != "" && w.Token != ""
}

// MissingKeys names the variables that have not arrived, in a fixed order, so
// a "waiting for Gitea" report says exactly what it is waiting for. Never the
// values — only the keys.
func (w GiteaWiring) MissingKeys() []string {
	var missing []string
	for _, kv := range []struct {
		key   string
		value string
	}{
		{GiteaURLEnvKey, w.GiteaURL},
		{MateBrokerURLEnvKey, w.BrokerURL},
		{GiteaTokenEnvKey, w.Token},
	} {
		if kv.value == "" {
			missing = append(missing, kv.key)
		}
	}
	return missing
}

// ReadGiteaWiring pulls the three values out of a lookup. The lookup is a
// PARAMETER because a running container's process environment is not the live
// answer: the platform rewrites the container's env store within seconds of a
// service env change, but a process keeps the environment it started with
// until it restarts (measured 2026-09-16). A caller that only consulted
// os.Getenv would wait forever for variables that are already there.
func ReadGiteaWiring(lookup func(string) string) GiteaWiring {
	if lookup == nil {
		return GiteaWiring{}
	}
	return GiteaWiring{
		GiteaURL:  strings.TrimSpace(lookup(GiteaURLEnvKey)),
		BrokerURL: strings.TrimSpace(lookup(MateBrokerURLEnvKey)),
		Token:     strings.TrimSpace(lookup(GiteaTokenEnvKey)),
	}
}

// Broker outcomes a caller must be able to tell apart. Both are REPORTABLE,
// never fatal: a Mate that cannot get a repository still has a working dev
// pair, and the answer may be different on the next pass (a group gets
// registered; a name gets freed).
var (
	// ErrRepositoryTaken is the broker's 409: a repository of that name
	// exists in the org and this bot is not a collaborator on it. Another
	// Mate, or a person, owns it.
	ErrRepositoryTaken = errors.New("gitea: repository name already taken in the group's org")

	// ErrNotRegistered is the broker's 403: this Mate's project is not a
	// `mate` entry of the registry, so the broker will not act for it. At
	// sign-up this resolves itself when the app registers the group.
	ErrNotRegistered = errors.New("gitea: the Mate's project is not registered with the broker")
)

// MateRepository is the broker's answer to POST /mate/repository.
type MateRepository struct {
	FullName      string `json:"fullName"`
	CloneURL      string `json:"cloneUrl"`
	DefaultBranch string `json:"defaultBranch"`
	Created       bool   `json:"created"`
}

// brokerError is the broker's error body, `{"error": "<code>", "message": "…"}`.
type brokerError struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// RequestMateRepository asks the broker for the service repository of one
// dev/stage pair (gitea-mate docs/broker-api.md, guide 2.1). The Mate
// authenticates as its own bot with Gitea's `token` scheme; the broker
// resolves that token against Gitea to learn which Mate is calling, so the
// request body carries only the name.
//
// Idempotent by contract: a repository this bot already collaborates on comes
// back with created:false, which is what makes "one repository per pair" hold
// across retries rather than depending on the caller remembering.
//
// Errors are coarse by design — they reach agent-facing text, and neither a
// broker error page nor the token is safe to reflect there.
func RequestMateRepository(ctx context.Context, httpClient HTTPDoer, brokerURL, token, name string) (MateRepository, error) {
	if httpClient == nil {
		return MateRepository{}, fmt.Errorf("no HTTP client configured")
	}
	if strings.TrimSpace(brokerURL) == "" {
		return MateRepository{}, fmt.Errorf("%s is not set", MateBrokerURLEnvKey)
	}
	if strings.TrimSpace(name) == "" {
		return MateRepository{}, fmt.Errorf("repository name is empty")
	}

	payload, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		return MateRepository{}, fmt.Errorf("encode repository request failed")
	}
	endpoint := strings.TrimRight(strings.TrimSpace(brokerURL), "/") + "/mate/repository"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return MateRepository{}, fmt.Errorf("build repository request failed")
	}
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return MateRepository{}, fmt.Errorf("broker /mate/repository request failed (transport error)")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return MateRepository{}, fmt.Errorf("read broker /mate/repository response failed")
	}

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
	case http.StatusConflict:
		return MateRepository{}, fmt.Errorf("%w (%s)", ErrRepositoryTaken, name)
	case http.StatusForbidden:
		return MateRepository{}, ErrNotRegistered
	default:
		var be brokerError
		if json.Unmarshal(body, &be) == nil && be.Error != "" {
			// The `error` field is a CODE from the broker's own closed set,
			// not free text a caller controls — safe to echo; `message` is
			// not echoed.
			return MateRepository{}, fmt.Errorf("broker /mate/repository returned status %d (%s)", resp.StatusCode, be.Error)
		}
		return MateRepository{}, fmt.Errorf("broker /mate/repository returned status %d", resp.StatusCode)
	}

	var repo MateRepository
	if jsonErr := json.Unmarshal(body, &repo); jsonErr != nil {
		return MateRepository{}, fmt.Errorf("broker /mate/repository response was not valid JSON")
	}
	if repo.CloneURL == "" || repo.FullName == "" {
		return MateRepository{}, fmt.Errorf("broker /mate/repository response named no repository")
	}
	if repo.DefaultBranch == "" {
		repo.DefaultBranch = "main"
	}
	return repo, nil
}

// GiteaMateBranch is the branch a Mate works on: `mate/{bot login}`. Every
// repository's `main` is protected and takes merges only through a pull
// request (docs/vocabulary.md, "protected branches"), so this is the only
// ref a Mate ever pushes — and naming it after the bot is what keeps two
// Mates on one repository out of each other's way.
func GiteaMateBranch(login string) string {
	if login == "" {
		return ""
	}
	return "mate/" + login
}

// giteaPullRequest is the subset of Gitea's pull-request object the
// idempotence check needs.
type giteaPullRequest struct {
	Number int    `json:"number"`
	State  string `json:"state"`
}

// EnsureGiteaPullRequest opens a pull request from head to base on fullName
// when no open one exists, and reports its number either way. Idempotent: the
// Mate runs it after every push to its branch, and a second call must find
// the first request rather than pile up duplicates. Gitea does answer 409 on
// a duplicate, but relying on that would make the ordinary path an error
// path — so the open list is read first.
//
// head is a plain branch name (a same-repository pull request): the bot is a
// collaborator on its own repositories and never works from a fork.
func EnsureGiteaPullRequest(ctx context.Context, httpClient HTTPDoer, giteaURL, token, fullName, head, base, title string) (number int, created bool, err error) {
	if httpClient == nil {
		return 0, false, fmt.Errorf("no HTTP client configured")
	}
	apiBase, err := giteaAPIBase(giteaURL)
	if err != nil {
		return 0, false, err
	}
	if fullName == "" || head == "" || base == "" {
		return 0, false, fmt.Errorf("pull request needs a repository, a head and a base")
	}
	repoRoot := apiBase + "/repos/" + fullName

	existing, err := giteaOpenPullRequest(ctx, httpClient, repoRoot, token, head, base)
	if err != nil {
		return 0, false, err
	}
	if existing != 0 {
		return existing, false, nil
	}

	payload, err := json.Marshal(map[string]string{"head": head, "base": base, "title": title})
	if err != nil {
		return 0, false, fmt.Errorf("encode pull-request body failed")
	}
	body, status, err := giteaAPICall(ctx, httpClient, http.MethodPost, repoRoot+"/pulls", token, payload)
	if err != nil {
		return 0, false, err
	}
	if status == http.StatusConflict {
		// Someone (a concurrent pass, or a person) opened it between the
		// list and the create. Not an error — re-read and report it.
		if n, reErr := giteaOpenPullRequest(ctx, httpClient, repoRoot, token, head, base); reErr == nil && n != 0 {
			return n, false, nil
		}
		return 0, false, nil
	}
	if status != http.StatusCreated && status != http.StatusOK {
		return 0, false, fmt.Errorf("Gitea pull-request create returned status %d", status)
	}
	var pr giteaPullRequest
	if jsonErr := json.Unmarshal(body, &pr); jsonErr != nil {
		return 0, false, fmt.Errorf("Gitea pull-request response was not valid JSON")
	}
	return pr.Number, true, nil
}

// giteaOpenPullRequest returns the number of the open pull request from head
// to base, or 0 when there is none. Gitea's list endpoint takes no head/base
// filter that can be relied on across versions, so the open page is read and
// matched here.
func giteaOpenPullRequest(ctx context.Context, httpClient HTTPDoer, repoRoot, token, head, base string) (int, error) {
	body, status, err := giteaAPICall(ctx, httpClient, http.MethodGet, repoRoot+"/pulls?state=open&limit=50", token, nil)
	if err != nil {
		return 0, err
	}
	if status != http.StatusOK {
		return 0, fmt.Errorf("Gitea pull-request list returned status %d", status)
	}
	var open []struct {
		giteaPullRequest
		Head struct {
			Ref string `json:"ref"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
	}
	if jsonErr := json.Unmarshal(body, &open); jsonErr != nil {
		return 0, fmt.Errorf("Gitea pull-request list was not valid JSON")
	}
	for _, pr := range open {
		if pr.Head.Ref == head && pr.Base.Ref == base {
			return pr.Number, nil
		}
	}
	return 0, nil
}

// giteaAPICall is the one place a Gitea API request is built, so the auth
// scheme (`token`, never `Bearer` — a bot token sent as Bearer reads as
// anonymous) and the coarse error vocabulary cannot drift between callers.
func giteaAPICall(ctx context.Context, httpClient HTTPDoer, method, url, token string, payload []byte) ([]byte, int, error) {
	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, 0, fmt.Errorf("build Gitea API request failed")
	}
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("Gitea API request failed (transport error)")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read Gitea API response failed")
	}
	return body, resp.StatusCode, nil
}

// giteaAPIBase is the `/api/v1` root of a Gitea origin.
func giteaAPIBase(giteaURL string) (string, error) {
	endpoint, err := giteaUserAPIURL(giteaURL)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(endpoint, giteaUserAPIPath) + "/api/v1", nil
}
