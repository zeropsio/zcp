package console

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// ServiceSummary exposes only display-safe connection facts, never a descriptor or env.
type ServiceSummary struct {
	MaskedConnection string `json:"maskedConnection"`
	Bucket           string `json:"bucket,omitempty"`
}

func (e *Engine) Summary(ctx context.Context, hostname string) (ServiceSummary, error) {
	e.mu.Lock()
	view, ok := findView(e.services, hostname)
	e.mu.Unlock()
	if !ok {
		return ServiceSummary{}, provider.ErrNotFound
	}
	ci, err := e.host.ConnectionInfo(ctx, view.ID)
	if err != nil {
		return ServiceSummary{}, fmt.Errorf("summary: %w", provider.ErrUpstream)
	}
	switch d := ci.Descriptor.(type) {
	case provider.SQLConn:
		scheme := "postgresql"
		if d.Driver == "mysql" {
			scheme = "mysql"
		}
		return ServiceSummary{MaskedConnection: scheme + "://••••:••••@" + net.JoinHostPort(d.Host, d.Port) + "/" + url.PathEscape(d.Database)}, nil
	case provider.KVConn:
		return ServiceSummary{MaskedConnection: "redis://:••••@" + net.JoinHostPort(d.Host, d.Port)}, nil
	case provider.ObjectConn:
		endpoint := d.Endpoint
		if !strings.Contains(endpoint, "://") {
			if d.Secure {
				endpoint = "https://" + endpoint
			} else {
				endpoint = "http://" + endpoint
			}
		}
		parsed, err := url.Parse(endpoint)
		if err != nil {
			return ServiceSummary{}, provider.ErrInvalid
		}
		// Rebuild from protocol + host; discard userinfo, query/signatures and fragments.
		if parsed.Scheme != "https" && parsed.Scheme != "http" {
			return ServiceSummary{}, provider.ErrInvalid
		}
		return ServiceSummary{MaskedConnection: parsed.Scheme + "://" + parsed.Host + "/" + url.PathEscape(d.Bucket), Bucket: d.Bucket}, nil
	default:
		return ServiceSummary{}, provider.ErrUnsupported
	}
}
