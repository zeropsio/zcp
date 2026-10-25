package hq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ProjectIdentity is HQ's authoritative placement of the project its key names.
type ProjectIdentity struct {
	OrgID     string `json:"orgId"`
	ProjectID string `json:"projectId"`
	AppID     string `json:"appId"`
}

// ProjectClient speaks HQ's plain-project API. Its key stays in the existing
// client's in-memory identity and travels only as an Authorization header.
type ProjectClient struct{ client Client }

// OpenProject opens a plain-project client. Every call is attempted once;
// a refused key triggers one explicit recheck without repeating the call.
func OpenProject(doer Doer, id Identity) (ProjectClient, error) {
	// Redirects would send a second request, potentially forwarding the key
	// to a different origin. Leave the caller's shared HTTP client untouched.
	// A custom Doer owns its transport and redirect policy.
	if httpClient, ok := doer.(*http.Client); ok {
		isolated := *httpClient
		isolated.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		doer = &isolated
	}
	client, err := Open(doer, id)
	if err != nil {
		return ProjectClient{}, fmt.Errorf("open HQ project: %w", err)
	}
	return ProjectClient{client: client.Once()}, nil
}

// Self enrolls the project whose key is presented. The local project ID is
// only an assertion; the returned organisation and application come from HQ.
func (c ProjectClient) Self(ctx context.Context) (ProjectIdentity, error) {
	var identity ProjectIdentity
	path := "/api/project/self?" + url.Values{"projectId": {c.client.id.ProjectID}}.Encode()
	err := c.client.call.json(ctx, http.MethodGet, path, c.client.authorization(), nil, &identity)
	if err != nil {
		return ProjectIdentity{}, c.report(ctx, "project self", err)
	}
	return identity, nil
}

// EnsureRepo creates or returns this project's repository named after its
// development service hostname.
func (c ProjectClient) EnsureRepo(ctx context.Context, name string) (Repo, error) {
	var repo Repo
	body := struct {
		Name      string `json:"name"`
		ProjectID string `json:"projectId"`
	}{name, c.client.id.ProjectID}
	err := c.client.call.json(ctx, http.MethodPost, "/api/project/repos", c.client.authorization(), body, &repo)
	if err != nil {
		return Repo{}, c.report(ctx, "project repository", err)
	}
	return repo, nil
}

// RecheckKey asks HQ for its own verdict once, without recursively rechecking
// a refusal of the recheck itself.
func (c ProjectClient) RecheckKey(ctx context.Context) (KeyVerdict, error) {
	err := c.client.call.json(ctx, http.MethodPost, "/api/project/key-recheck", c.client.authorization(), nil, nil)
	if err == nil {
		return KeyHeld, nil
	}
	var refused *RefusedError
	if errors.As(err, &refused) && refused.Status == http.StatusUnauthorized && refused.Code == keyRefusedCode {
		return KeyRefused, nil
	}
	return "", fmt.Errorf("project key recheck: %w", err)
}

// report preserves the original refusal even when the required recheck
// fails or confirms the key. Only a 401 key_refused requests that recheck.
func (c ProjectClient) report(ctx context.Context, op string, err error) error {
	var refused *RefusedError
	if errors.As(err, &refused) && refused.Status == http.StatusUnauthorized && refused.Code == keyRefusedCode {
		_, _ = c.RecheckKey(ctx)
	}
	return fmt.Errorf("%s: %w", op, err)
}

// GitEnv presents the project's key only to Git HTTP requests under HQ's
// origin, through the command environment rather than persistent Git config.
func (c ProjectClient) GitEnv() []string { return c.client.gitEnv() }

// RepoURL addresses the placement HQ returned from Self, never caller input
// used to select the key's organisation or application.
func (c ProjectClient) RepoURL(orgID, appID, repo string) (string, error) {
	if orgID == "" {
		return "", fmt.Errorf("project repository URL: %w", ErrNoOrganisation)
	}
	// HQ's project-door contract encodes each segment with encodeURIComponent.
	// QueryEscape encodes its reserved characters, but uses + for spaces and
	// also escapes these five characters JavaScript leaves untouched.
	unescape := strings.NewReplacer("+", "%20", "%21", "!", "%27", "'", "%28", "(", "%29", ")", "%2A", "*")
	escape := func(segment string) string { return unescape.Replace(url.QueryEscape(segment)) }
	return c.client.Address() + "/git/" + escape(orgID) + "/" + escape(appID) + "/" + escape(repo) + ".git", nil
}

// ProbeRepo verifies Git access using the project header before a Git command
// runs. It accepts only repository paths at the configured HQ origin.
func (c ProjectClient) ProbeRepo(ctx context.Context, remote string) error {
	target, err := url.Parse(remote)
	if err != nil {
		return fmt.Errorf("project Git URL: %w", err)
	}
	origin, err := url.Parse(c.client.Address())
	if err != nil {
		return fmt.Errorf("project HQ URL: %w", err)
	}
	if target.Scheme != origin.Scheme || target.Host != origin.Host || target.User != nil || target.RawQuery != "" || target.Fragment != "" || !strings.HasPrefix(target.Path, "/git/") || !strings.HasSuffix(target.Path, ".git") {
		return fmt.Errorf("project Git URL must name a repository at the configured HQ origin")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, remote+"/info/refs?service=git-upload-pack", nil)
	if err != nil {
		return fmt.Errorf("project Git probe: %w", err)
	}
	req.Header.Set("Authorization", c.client.authorization())
	response, err := c.client.call.http.Do(req)
	if err != nil {
		return fmt.Errorf("project Git probe: %w", &UnavailableError{Err: err})
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, answerLimit))
	if err != nil {
		return fmt.Errorf("project Git probe: %w", &UnavailableError{Err: err})
	}
	if response.StatusCode == http.StatusOK {
		return nil
	}
	var refusal struct {
		Code   string `json:"code"`
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal(raw, &refusal)
	if response.StatusCode == http.StatusServiceUnavailable {
		return fmt.Errorf("project Git probe: %w", &UnavailableError{Code: refusal.Code})
	}
	return c.report(ctx, "project Git probe", &RefusedError{Status: response.StatusCode, Code: refusal.Code, Reason: refusal.Reason})
}
