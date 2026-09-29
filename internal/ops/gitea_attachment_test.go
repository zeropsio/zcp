// Tests for: ops/gitea_attachment.go — a picture attached to a pull request.
package ops

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAttachGiteaPicture pins the upload Gitea 1.27.2 takes: POST
// /repos/{repo}/issues/{number}/assets, the file as the multipart field
// "attachment", its name in ?name=, in the bot's token scheme — and its
// refusal of a token without write:issue, which a caller must tell apart
// from Gitea being away.
func TestAttachGiteaPicture(t *testing.T) {
	t.Parallel()
	picture := []byte("\x89PNG\r\n\x1a\n-a-picture-")
	const served = "https://gitea.example.invalid/attachments/3f2a9c1e-5b7d-4e8f-9a0b-1c2d3e4f5a6b"

	tests := []struct {
		name        string
		status      int
		answer      string
		wantURL     string
		wantScope   bool
		wantErrText string
	}{
		{name: "gitea takes it", status: http.StatusCreated, answer: `{"id":1,"uuid":"3f2a9c1e-5b7d-4e8f-9a0b-1c2d3e4f5a6b","browser_download_url":"` + served + `"}`, wantURL: served},
		{
			name: "the token may not attach", status: http.StatusForbidden,
			answer:    `{"message":"token does not have at least one of required scope(s), required=[write:issue], token scope=write:repository,read:user"}`,
			wantScope: true, wantErrText: "required=[write:issue]",
		},
		{name: "too large for this Gitea", status: http.StatusRequestEntityTooLarge, answer: `{}`, wantErrText: "status 413"},
		{name: "gitea answers no address", status: http.StatusCreated, answer: `{"id":1}`, wantErrText: "no address"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var gotMethod, gotPath, gotName, gotAuth, gotField, gotFile string
			var gotBytes []byte
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath, gotName, gotAuth = r.Method, r.URL.Path, r.URL.Query().Get("name"), r.Header.Get("Authorization")
				if reader, err := r.MultipartReader(); err == nil {
					if part, err := reader.NextPart(); err == nil {
						gotField, gotFile = part.FormName(), part.FileName()
						gotBytes, _ = io.ReadAll(part)
					}
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.answer))
			}))
			defer srv.Close()

			url, err := AttachGiteaPicture(context.Background(), srv.Client(), srv.URL, "bot-token", "acme/appdev", 9, "shot-3.png", picture)
			if tt.wantErrText != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrText) {
					t.Fatalf("err = %v, want one saying %q", err, tt.wantErrText)
				}
				if errors.Is(err, ErrGiteaCannotAttach) != tt.wantScope {
					t.Errorf("errors.Is(ErrGiteaCannotAttach) = %v, want %v", !tt.wantScope, tt.wantScope)
				}
				return
			}
			if err != nil {
				t.Fatalf("AttachGiteaPicture: %v", err)
			}
			if url != tt.wantURL {
				t.Errorf("url = %q, want %q", url, tt.wantURL)
			}
			if gotMethod != http.MethodPost || gotPath != "/api/v1/repos/acme/appdev/issues/9/assets" || gotName != "shot-3.png" {
				t.Errorf("request = %s %s ?name=%s", gotMethod, gotPath, gotName)
			}
			if gotAuth != "token bot-token" {
				t.Errorf("Authorization = %q, want Gitea's token scheme", gotAuth)
			}
			if gotField != "attachment" || gotFile != "shot-3.png" || !bytes.Equal(gotBytes, picture) {
				t.Errorf("multipart = field %q file %q (%d bytes), want the picture as \"attachment\"", gotField, gotFile, len(gotBytes))
			}
		})
	}
}

func TestAttachGiteaPicture_NeedsAClientARequestAndAPicture(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("no request should be made")
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(srv.Close)
	for _, tc := range []struct {
		name     string
		client   HTTPDoer
		fullName string
		number   int
		content  []byte
	}{
		{name: "no HTTP client", fullName: "acme/appdev", number: 9, content: []byte("x")},
		{name: "no repository", client: srv.Client(), number: 9, content: []byte("x")},
		{name: "no request", client: srv.Client(), fullName: "acme/appdev", content: []byte("x")},
		{name: "no picture", client: srv.Client(), fullName: "acme/appdev", number: 9},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := AttachGiteaPicture(context.Background(), tc.client, srv.URL, "tok", tc.fullName, tc.number, "shot-1.png", tc.content); err == nil {
				t.Error("want an error, got none")
			}
		})
	}
}
