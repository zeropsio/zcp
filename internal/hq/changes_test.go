package hq

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// hqCall is one request the fake HQ saw.
type hqCall struct {
	Method, Path, Authorization, ContentType, Body string
}

// answering is a fake HQ that answers every request with body, recording
// what it was asked; the client it returns holds the credential "good".
func answering(t *testing.T, body string) (Client, *[]hqCall) {
	t.Helper()
	var calls []hqCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		calls = append(calls, hqCall{r.Method, r.URL.EscapedPath(), r.Header.Get("Authorization"), r.Header.Get("Content-Type"), string(raw)})
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return enrolledClient(t, srv), &calls
}

// enrolledClient is a client of srv, enrolled as the Mate "p-mate" with the
// credential "good".
func enrolledClient(t *testing.T, srv *httptest.Server) Client {
	t.Helper()
	path := filepath.Join(t.TempDir(), "enrollment.json")
	if err := SaveEnrollment(path, Enrollment{HQ: srv.URL, HQProjectID: "hq1", ProjectID: "p-mate", Credential: "good"}); err != nil {
		t.Fatal(err)
	}
	client, err := Open(srv.Client(), path)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestClient_EnsureRepo_AsksHQForTheRepositoryInTheMatesApplication(t *testing.T) {
	t.Parallel()
	client, calls := answering(t, `{"appId":"0b6e7c1e-8f7a-4a52-9d1c-2f1f5d0a3c11","name":"appdev"}`)

	repo, err := client.EnsureRepo(context.Background(), "appdev")
	if err != nil {
		t.Fatal(err)
	}
	if repo != (Repo{AppID: "0b6e7c1e-8f7a-4a52-9d1c-2f1f5d0a3c11", Name: "appdev"}) {
		t.Errorf("repo = %+v", repo)
	}
	want := hqCall{http.MethodPost, "/api/mate/repos", "Mate good", "application/json", `{"name":"appdev"}`}
	if len(*calls) != 1 || (*calls)[0] != want {
		t.Errorf("calls = %+v, want [%+v]", *calls, want)
	}
}

// openSample is HQ's answer to POST /api/mate/changes for a change opened
// now: nothing pushed yet, nothing said about it.
const openSample = `{"change":{"appId":"0b6e7c1e-8f7a-4a52-9d1c-2f1f5d0a3c11","repo":"appdev","number":3,` +
	`"mateProjectId":"p-mate","title":"Add a todo list","body":"","state":"open","head":null,"mergedSha":null,` +
	`"landedHead":null,"openedAt":"2026-10-02T09:12:44.512Z","mergedAt":null,"closedAt":null,` +
	`"updatedAt":"2026-10-02T09:12:44.512Z","mergeability":"unknown","behind":false},"created":true}`

func TestClient_OpenChange_DecodesTheChangeHQOpenedOrHeld(t *testing.T) {
	t.Parallel()
	client, calls := answering(t, openSample)

	opened, err := client.OpenChange(context.Background(), "appdev", "Add a todo list")
	if err != nil {
		t.Fatal(err)
	}
	want := OpenedChange{Created: true, Change: Change{
		AppID: "0b6e7c1e-8f7a-4a52-9d1c-2f1f5d0a3c11", Repo: "appdev", Number: 3, MateProjectID: "p-mate",
		Title: "Add a todo list", State: ChangeOpen, OpenedAt: "2026-10-02T09:12:44.512Z",
		UpdatedAt: "2026-10-02T09:12:44.512Z", Mergeability: "unknown",
	}}
	if !reflect.DeepEqual(opened, want) {
		t.Errorf("opened = %+v\nwant %+v", opened, want)
	}
	wantCall := hqCall{http.MethodPost, "/api/mate/changes", "Mate good", "application/json", `{"repo":"appdev","title":"Add a todo list"}`}
	if len(*calls) != 1 || (*calls)[0] != wantCall {
		t.Errorf("calls = %+v, want [%+v]", *calls, wantCall)
	}
}

// describedSample is HQ's answer to PATCH /api/mate/changes/appdev/3: the
// change, pushed, with the words the Mate wrote about it.
const describedSample = `{"appId":"0b6e7c1e-8f7a-4a52-9d1c-2f1f5d0a3c11","repo":"appdev","number":3,` +
	`"mateProjectId":"p-mate","title":"Add a todo list","body":"Adds a list.\n\n![the list](https://hq.example/a.png)",` +
	`"state":"open","head":"4b825dc642cb6eb9a060e54bf8d69288fbee4904","mergedSha":null,"landedHead":null,` +
	`"openedAt":"2026-10-02T09:12:44.512Z","mergedAt":null,"closedAt":null,"updatedAt":"2026-10-02T09:20:01.000Z",` +
	`"mergeability":"clean","behind":false}`

func TestClient_EditChange_SendsOnlyWhatChanges(t *testing.T) {
	t.Parallel()
	title, body := "Add a todo list", "Adds a list."
	tests := []struct {
		name     string
		edit     ChangeEdit
		wantBody string
	}{
		{"its words", ChangeEdit{Body: &body}, `{"body":"Adds a list."}`},
		{"its title", ChangeEdit{Title: &title}, `{"title":"Add a todo list"}`},
		{"both", ChangeEdit{Title: &title, Body: &body}, `{"title":"Add a todo list","body":"Adds a list."}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client, calls := answering(t, describedSample)
			change, err := client.EditChange(context.Background(), "appdev", 3, tt.edit)
			if err != nil {
				t.Fatal(err)
			}
			if change.Number != 3 || change.Head == nil || *change.Head != "4b825dc642cb6eb9a060e54bf8d69288fbee4904" || change.Mergeability != "clean" {
				t.Errorf("change = %+v", change)
			}
			want := hqCall{http.MethodPatch, "/api/mate/changes/appdev/3", "Mate good", "application/json", tt.wantBody}
			if len(*calls) != 1 || (*calls)[0] != want {
				t.Errorf("calls = %+v, want [%+v]", *calls, want)
			}
		})
	}
}

func TestClient_Attach_SendsThePictureItselfAsAPNG(t *testing.T) {
	t.Parallel()
	client, calls := answering(t,
		`{"id":"7f1c2a90-3d4e-4b5f-8a6b-9c0d1e2f3a4b","path":"/api/apps/0b6e7c1e-8f7a-4a52-9d1c-2f1f5d0a3c11/changes/appdev/3/attachments/7f1c2a90-3d4e-4b5f-8a6b-9c0d1e2f3a4b"}`)
	png := "\x89PNG\r\n\x1a\nthe picture"

	got, err := client.Attach(context.Background(), "appdev", 3, []byte(png))
	if err != nil {
		t.Fatal(err)
	}
	want := Attachment{
		ID:   "7f1c2a90-3d4e-4b5f-8a6b-9c0d1e2f3a4b",
		Path: "/api/apps/0b6e7c1e-8f7a-4a52-9d1c-2f1f5d0a3c11/changes/appdev/3/attachments/7f1c2a90-3d4e-4b5f-8a6b-9c0d1e2f3a4b",
	}
	if got != want {
		t.Errorf("attachment = %+v, want %+v", got, want)
	}
	wantCall := hqCall{http.MethodPost, "/api/mate/changes/appdev/3/attachments", "Mate good", "image/png", png}
	if len(*calls) != 1 || (*calls)[0] != wantCall {
		t.Errorf("calls = %+v, want [%+v]", *calls, wantCall)
	}
}

// TestClient_Unavailable_IsTryAgain: only the HQ that leads answers, and a
// standby says so with 503 and Retry-After while a deploy runs two side by
// side — a wait, never a refusal. The client waits it out within a bound,
// then says HQ is unavailable; an HQ that does not answer at all is
// unavailable at once.
func TestClient_Unavailable_IsTryAgain(t *testing.T) {
	t.Parallel()
	const repoAnswer = `{"appId":"a1","name":"appdev"}`
	tests := []struct {
		name       string
		answers    []string // status:retry-after:body, in turn; the last repeats
		wantErr    func(error) bool
		wantPauses []time.Duration
	}{
		{"a standby, then the leader", []string{"503:5:{\"code\":\"not_active\"}", "200::" + repoAnswer},
			nil, []time.Duration{5 * time.Second}},
		{"Zerops not answering HQ", []string{"503:5:{\"code\":\"zerops_unavailable\"}", "200::" + repoAnswer},
			nil, []time.Duration{5 * time.Second}},
		{"no Retry-After waits the default", []string{"503::{\"code\":\"unavailable\"}", "200::" + repoAnswer},
			nil, []time.Duration{5 * time.Second}},
		{"a long Retry-After is cut to the cap", []string{"503:60:{\"code\":\"not_active\"}", "200::" + repoAnswer},
			nil, []time.Duration{10 * time.Second}},
		{"a standby throughout", []string{"503:5:{\"code\":\"not_active\"}"},
			func(err error) bool {
				var unavailable *UnavailableError
				return errors.As(err, &unavailable) && unavailable.Code == "not_active"
			},
			[]time.Duration{5 * time.Second, 5 * time.Second, 5 * time.Second, 5 * time.Second}},
		{"a refusal is final", []string{"403::{\"code\":\"forbidden\",\"reason\":\"not_in_app\"}"},
			func(err error) bool {
				var refused *RefusedError
				return errors.As(err, &refused) && refused.Status == 403 && refused.Code == "forbidden" && refused.Reason == "not_in_app"
			}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var mu sync.Mutex
			asked := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				mu.Lock()
				answer := tt.answers[min(asked, len(tt.answers)-1)]
				asked++
				mu.Unlock()
				status, rest, _ := strings.Cut(answer, ":")
				retryAfter, body, _ := strings.Cut(rest, ":")
				if retryAfter != "" {
					w.Header().Set("Retry-After", retryAfter)
				}
				code, _ := strconv.Atoi(status)
				w.WriteHeader(code)
				_, _ = io.WriteString(w, body)
			}))
			t.Cleanup(srv.Close)
			client := enrolledClient(t, srv)
			var pauses []time.Duration
			client.call.pause = func(_ context.Context, d time.Duration) error {
				pauses = append(pauses, d)
				return nil
			}

			_, err := client.EnsureRepo(context.Background(), "appdev")
			if tt.wantErr == nil && err != nil || tt.wantErr != nil && !tt.wantErr(err) {
				t.Fatalf("err = %v", err)
			}
			if !reflect.DeepEqual(pauses, tt.wantPauses) {
				t.Errorf("pauses = %v, want %v", pauses, tt.wantPauses)
			}
		})
	}
}

func TestClient_Unreachable_IsUnavailableAtOnce(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.NotFoundHandler())
	client := enrolledClient(t, srv)
	srv.Close()
	client.call.pause = func(context.Context, time.Duration) error {
		t.Error("an HQ that does not answer is not waited for")
		return nil
	}

	_, err := client.EnsureRepo(context.Background(), "appdev")
	var unavailable *UnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("err = %v, want an UnavailableError", err)
	}
}

// TestClient_Addresses: what zcp hands git and a person is HQ's own address
// followed by the contract's paths (hqChanges.ts changeUrl, attachmentPath),
// whether or not the enrollment kept a trailing slash.
func TestClient_Addresses(t *testing.T) {
	t.Parallel()
	for _, address := range []string{"https://hq.example", "https://hq.example/"} {
		path := filepath.Join(t.TempDir(), "enrollment.json")
		if err := SaveEnrollment(path, Enrollment{HQ: address, ProjectID: "p-mate", Credential: "good"}); err != nil {
			t.Fatal(err)
		}
		client, err := Open(http.DefaultClient, path)
		if err != nil {
			t.Fatal(err)
		}
		for got, want := range map[string]string{
			client.Address():                    "https://hq.example",
			client.RepoURL("a1", "appdev"):      "https://hq.example/git/a1/appdev.git",
			client.ChangeURL("a1", "appdev", 3): "https://hq.example/changes/a1/appdev/3",
			client.ChangeBranch(3):              "mate/p-mate/3",
			client.AttachmentURL("/api/apps/a1/changes/appdev/3/attachments/x"): "https://hq.example/api/apps/a1/changes/appdev/3/attachments/x",
		} {
			if got != want {
				t.Errorf("%s: got %q, want %q", address, got, want)
			}
		}
	}
}

func TestClient_OpenChange_KeepsHQsNothingToDeliverVerdict(t *testing.T) {
	t.Parallel()
	client, _ := answering(t, `{"change":null,"created":false,"reason":"nothing_to_deliver"}`)
	opened, err := client.OpenChange(t.Context(), "appdev", "Next task")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(opened)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"reason":"nothing_to_deliver"`) {
		t.Fatalf("HQ verdict lost: %s", raw)
	}
}
