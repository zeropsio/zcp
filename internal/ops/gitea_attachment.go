package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
)

// ErrGiteaCannotAttach is Gitea refusing a token an attachment: its
// attachment routes are issue-scope, and a token without write:issue — a
// Mate bot's generation minted before the broker gave bots that scope — is
// answered 403 "required=[write:issue]" (measured on Gitea 1.27.2).
var ErrGiteaCannotAttach = errors.New("this Mate's Gitea token cannot attach pictures yet")

// AttachGiteaPicture attaches a PNG to issue or pull request number on
// fullName — POST /repos/{fullName}/issues/{number}/assets, the file as the
// multipart field "attachment", named by ?name= — and returns the address
// Gitea serves it at (browser_download_url, {ROOT_URL}attachments/{uuid}).
// A pull request's attachments are its issue's; Gitea's own editor attaches a
// pasted screenshot the same way.
func AttachGiteaPicture(ctx context.Context, httpClient HTTPDoer, giteaURL, token, fullName string, number int, name string, content []byte) (string, error) {
	if httpClient == nil {
		return "", fmt.Errorf("no HTTP client configured")
	}
	apiBase, err := giteaAPIBase(giteaURL)
	if err != nil {
		return "", err
	}
	if fullName == "" || number <= 0 || len(content) == 0 {
		return "", fmt.Errorf("an attachment needs a repository, a request and a picture")
	}

	var form bytes.Buffer
	writer := multipart.NewWriter(&form)
	part, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {fmt.Sprintf(`form-data; name="attachment"; filename=%q`, name)},
		"Content-Type":        {"image/png"},
	})
	if err != nil {
		return "", fmt.Errorf("encode the attachment failed")
	}
	if _, err := part.Write(content); err != nil {
		return "", fmt.Errorf("encode the attachment failed")
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("encode the attachment failed")
	}

	endpoint := fmt.Sprintf("%s/repos/%s/issues/%d/assets?name=%s", apiBase, fullName, number, url.QueryEscape(name))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &form)
	if err != nil {
		return "", fmt.Errorf("build the Gitea attachment request failed")
	}
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request to the Gitea API failed (transport error)")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read the Gitea attachment response failed")
	}

	switch resp.StatusCode {
	case http.StatusCreated, http.StatusOK:
	case http.StatusForbidden:
		var refusal struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(body, &refusal)
		return "", fmt.Errorf("%w (Gitea: %s)", ErrGiteaCannotAttach, refusal.Message)
	default:
		return "", fmt.Errorf("the Gitea attachment upload onto %s#%d returned status %d", fullName, number, resp.StatusCode)
	}
	var attached struct {
		BrowserDownloadURL string `json:"browser_download_url"` //nolint:tagliatelle // Gitea's wire schema
	}
	if err := json.Unmarshal(body, &attached); err != nil || attached.BrowserDownloadURL == "" {
		return "", fmt.Errorf("the Gitea attachment upload onto %s#%d answered no address for it", fullName, number)
	}
	return attached.BrowserDownloadURL, nil
}
