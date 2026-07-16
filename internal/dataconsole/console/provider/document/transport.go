package document

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// transport is the shared HTTP round-tripper for a single engine: it owns the
// base URL + the engine's auth header and maps every non-2xx / transport failure
// to a sanitized provider sentinel. The endpoint, the api-key and the response
// body never escape into a returned error.
type transport struct {
	base    string
	client  *http.Client
	setAuth func(*http.Request)
}

// request performs one HTTP call and returns the raw response body on 2xx.
func (t *transport) request(ctx context.Context, method, urlPath string, body []byte) ([]byte, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, t.base+urlPath, rdr)
	if err != nil {
		return nil, fmt.Errorf("doc: request: %w", provider.ErrInvalid)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	t.setAuth(req)

	resp, err := t.client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, fmt.Errorf("doc: transport: %w", provider.ErrUpstream)
	}
	defer func() { _ = resp.Body.Close() }()

	data, rerr := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, statusErr(resp.StatusCode)
	}
	if rerr != nil {
		return nil, fmt.Errorf("doc: read: %w", provider.ErrUpstream)
	}
	return data, nil
}

// requestJSON performs a call and decodes a 2xx body into out.
func (t *transport) requestJSON(ctx context.Context, method, urlPath string, body []byte, out any) error {
	data, err := t.request(ctx, method, urlPath, body)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("doc: decode: %w", provider.ErrUpstream)
	}
	return nil
}

// statusErr maps an HTTP status to a sanitized sentinel (never the body/URL/key).
func statusErr(code int) error {
	switch code {
	case http.StatusNotFound:
		return provider.ErrNotFound
	case http.StatusConflict:
		// A create-only write (es _create, typesense action=create) answers an
		// existing id with 409 — a conflict the caller can act on (the id is
		// taken), not an opaque outage (S17).
		return fmt.Errorf("doc: conflict: %w", provider.ErrConflict)
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("doc: auth: %w", provider.ErrUpstream)
	default:
		return fmt.Errorf("doc: upstream status %d: %w", code, provider.ErrUpstream)
	}
}
