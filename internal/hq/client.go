package hq

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

// RefusedError is HQ's own no: its HTTP status and the code it answered.
type RefusedError struct {
	Status int
	Code   string
}

func (r *RefusedError) Error() string {
	return fmt.Sprintf("hq refused: %d %s", r.Status, r.Code)
}

// refusedAs reports whether err is HQ's refusal with code.
func refusedAs(err error, code string) bool {
	var refused *RefusedError
	return errors.As(err, &refused) && refused.Code == code
}

// hqClient speaks HQ's Mate door at one address.
type hqClient struct {
	http    *http.Client
	address string
}

func (c hqClient) challenge(ctx context.Context, projectID string) (string, error) {
	var out struct {
		Nonce string `json:"nonce"`
	}
	err := c.call(ctx, http.MethodPost, "/api/mate/challenge", "", map[string]string{"projectId": projectID}, &out)
	return out.Nonce, err
}

func (c hqClient) credential(ctx context.Context, projectID, nonce string) (string, error) {
	var out struct {
		Credential string `json:"credential"`
	}
	err := c.call(ctx, http.MethodPost, "/api/mate/credential", "",
		map[string]string{"projectId": projectID, "nonce": nonce}, &out)
	return out.Credential, err
}

func (c hqClient) whoami(ctx context.Context, credential string) (string, error) {
	var out struct {
		ProjectID string `json:"projectId"`
	}
	err := c.call(ctx, http.MethodGet, "/api/mate/whoami", "Mate "+credential, nil, &out)
	return out.ProjectID, err
}

func (c hqClient) call(ctx context.Context, method, path, authorization string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("hq %s: %w", path, err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.address, "/")+path, body)
	if err != nil {
		return fmt.Errorf("hq %s: %w", path, err)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("hq %s: %w", path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return fmt.Errorf("hq %s: %w", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		var refusal struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(raw, &refusal)
		return &RefusedError{Status: resp.StatusCode, Code: refusal.Code}
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("hq %s: malformed answer: %w", path, err)
	}
	return nil
}
