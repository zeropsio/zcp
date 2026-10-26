package mate_test

import (
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/mate"
)

// non-parallel: the tests replace http.DefaultTransport.
type updateProofTransport struct {
	invalid                          string
	readinessCalls, environmentCalls int
}

func (p *updateProofTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	body := `{"protocol":1,"version":"0.15.111","bootId":"new","ready":true}`
	contentType := "application/json; charset=utf-8"
	if req.URL.Path == "/api/mate/update/readiness" {
		p.readinessCalls++
		if p.readinessCalls == 1 {
			switch p.invalid {
			case "protocol":
				body = `{"protocol":0,"version":"0.15.111","bootId":"new","ready":true}`
			case "version":
				body = `{"protocol":1,"version":"0.14.0","bootId":"new","ready":true}`
			case "old boot":
				body = `{"protocol":1,"version":"0.15.111","bootId":"old","ready":true}`
			case "empty boot":
				body = `{"protocol":1,"version":"0.15.111","bootId":"","ready":true}`
			case "engine not ready":
				body = `{"protocol":1,"version":"0.15.111","bootId":"new","ready":false}`
			}
		}
	} else {
		p.environmentCalls++
		body = `{"basePath":"/mate"}`
		if p.environmentCalls == 1 {
			switch p.invalid {
			case "C4 html":
				contentType = "text/html"
				body = "<html></html>"
			case "C4 base path":
				body = `{"basePath":"/"}`
			}
		}
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func TestMateUpdate_ReadinessProof_Result(t *testing.T) {
	for _, invalid := range []string{"protocol", "version", "old boot", "empty boot", "engine not ready", "C4 html", "C4 base path"} {
		t.Run(invalid, func(t *testing.T) {
			transport := &updateProofTransport{invalid: invalid}
			original := http.DefaultTransport
			http.DefaultTransport = transport
			t.Cleanup(func() { http.DefaultTransport = original })
			if err := mate.WaitReady("0.15.111", "old", time.Now().Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			if transport.readinessCalls != 2 {
				t.Fatalf("accepted incomplete readiness proof, requests=%d", transport.readinessCalls)
			}
		})
	}
}

type startupReadinessTransport struct {
	stallAlways bool
	calls       atomic.Int32
}

func (p *startupReadinessTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Path == "/api/mate/update/readiness" && (p.calls.Add(1) == 1 || p.stallAlways) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	}
	body := `{"protocol":1,"version":"0.15.111","bootId":"new","ready":true}`
	if req.URL.Path == "/.well-known/t3/environment" {
		body = `{"basePath":"/mate"}`
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func TestMateUpdate_ReadinessDuringStartup(t *testing.T) {
	for _, tc := range []struct {
		name        string
		stallAlways bool
		budget      time.Duration
		wantReady   bool
	}{
		{"stalled old connection does not hide a healthy new boot", false, 2 * time.Second, true},
		{"unavailable readiness cannot extend the explicit budget", true, 100 * time.Millisecond, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport := &startupReadinessTransport{stallAlways: tc.stallAlways}
			original := http.DefaultTransport
			http.DefaultTransport = transport
			t.Cleanup(func() { http.DefaultTransport = original })
			start := time.Now()
			err := mate.WaitReady("0.15.111", "old", start.Add(tc.budget))
			if (err == nil) != tc.wantReady {
				t.Fatalf("ready=%v, error=%v", err == nil, err)
			}
			if elapsed := time.Since(start); elapsed > tc.budget+time.Second {
				t.Fatalf("readiness exceeded its explicit budget: %s", elapsed)
			}
		})
	}
}
