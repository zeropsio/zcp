package hq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

const (
	orgID     = "org-1"
	projectID = "p-mate"
	hqProject = "hq1"
)

type envVar struct {
	ID, Key, Content string
	Sensitive        bool
}

// zeropsStub is the platform as this container's key reaches it: the org's
// member list, and its own project's env — written and deleted directly,
// listed through the project search.
type zeropsStub struct {
	mu      sync.Mutex
	members []platform.OrgMember
	env     []envVar
	written []envVar
	nextID  int
}

func (s *zeropsStub) handler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/rest/public/client/"+orgID+"/user/list":
		rows := make([]map[string]any, 0, len(s.members))
		for _, m := range s.members {
			rows = append(rows, map[string]any{
				"id": m.ID, "userId": m.UserID, "status": m.Status, "roleCode": m.RoleCode,
				"user": map[string]any{"email": m.Email, "fullName": m.FullName},
			})
		}
		writeJSON(w, map[string]any{"clientUserList": rows})
	case r.Method == http.MethodGet && r.URL.Path == "/api/rest/public/user/info":
		_, _ = w.Write([]byte(`{"id":"u-zcp","clientUserList":[{"id":"cu-zcp","clientId":"` + orgID + `"}]}`))
	case r.Method == http.MethodPost && r.URL.Path == "/api/rest/public/project/search":
		list := make([]map[string]any, 0, len(s.env))
		for _, e := range s.env {
			list = append(list, map[string]any{"id": e.ID, "key": e.Key, "content": e.Content, "sensitive": e.Sensitive})
		}
		writeJSON(w, map[string]any{"items": []any{map[string]any{"id": projectID, "envList": list}}})
	case r.Method == http.MethodPost && r.URL.Path == "/api/rest/public/project/"+projectID+"/env":
		var body struct {
			Key       string `json:"key"`
			Content   string `json:"content"`
			Sensitive bool   `json:"sensitive"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.nextID++
		e := envVar{ID: fmt.Sprintf("env-%d", s.nextID), Key: body.Key, Content: body.Content, Sensitive: body.Sensitive}
		s.env = append(s.env, e)
		s.written = append(s.written, e)
		_, _ = w.Write([]byte(`{"id":"proc-1","status":"FINISHED","actionName":"project.env.create"}`))
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/rest/public/project-env/"):
		id := strings.TrimPrefix(r.URL.Path, "/api/rest/public/project-env/")
		s.env = slices.DeleteFunc(s.env, func(e envVar) bool { return e.ID == id })
		_, _ = w.Write([]byte(`{"id":"proc-2","status":"FINISHED","actionName":"project.env.delete"}`))
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"notFound","message":"` + r.Method + " " + r.URL.Path + `"}}`))
	}
}

func writeJSON(w http.ResponseWriter, body any) {
	if err := json.NewEncoder(w).Encode(body); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// challengeValue is what HQ's own read of the project's env-file would see.
func (s *zeropsStub) challengeValue() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.env {
		if e.Key == ChallengeEnv {
			if e.Sensitive {
				return "REDACTED", true
			}
			return e.Content, true
		}
	}
	return "", false
}

// hqStub is HQ's Mate door over the zeropsStub's project env.
type hqStub struct {
	mu         sync.Mutex
	zerops     *zeropsStub
	nonces     map[string]string
	credential map[string]string
	refuse     string
	// lagging answers env_mismatch this many times before reading the env.
	lagging int
	calls   []string
}

func newHQStub(z *zeropsStub) *hqStub {
	return &hqStub{zerops: z, nonces: map[string]string{}, credential: map[string]string{}}
}

func (h *hqStub) handler(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, r.Method+" "+r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	answer := func(status int, body any) {
		w.WriteHeader(status)
		writeJSON(w, body)
	}
	var body struct {
		ProjectID string `json:"projectId"`
		Nonce     string `json:"nonce"`
	}
	switch r.Method + " " + r.URL.Path {
	case "POST /api/mate/challenge":
		_ = json.NewDecoder(r.Body).Decode(&body)
		nonce := fmt.Sprintf("nonce-%d", len(h.nonces)+1)
		h.nonces[nonce] = body.ProjectID
		answer(http.StatusOK, map[string]any{"nonce": nonce, "expiresIn": 120})
	case "POST /api/mate/credential":
		_ = json.NewDecoder(r.Body).Decode(&body)
		if h.refuse != "" {
			answer(http.StatusUnauthorized, map[string]string{"code": h.refuse})
			return
		}
		if h.lagging > 0 {
			h.lagging--
			answer(http.StatusUnauthorized, map[string]string{"code": "env_mismatch"})
			return
		}
		if h.nonces[body.Nonce] != body.ProjectID {
			answer(http.StatusUnauthorized, map[string]string{"code": "unknown_nonce"})
			return
		}
		if value, _ := h.zerops.challengeValue(); value != body.Nonce {
			answer(http.StatusUnauthorized, map[string]string{"code": "env_mismatch"})
			return
		}
		delete(h.nonces, body.Nonce)
		credential := fmt.Sprintf("credential-%d", len(h.credential)+1)
		for c, p := range h.credential {
			if p == body.ProjectID {
				delete(h.credential, c)
			}
		}
		h.credential[credential] = body.ProjectID
		answer(http.StatusOK, map[string]string{"credential": credential})
	case "GET /api/mate/whoami":
		project, ok := h.credential[strings.TrimPrefix(r.Header.Get("Authorization"), "Mate ")]
		if !ok {
			answer(http.StatusUnauthorized, map[string]string{"code": "mate_credential_required"})
			return
		}
		answer(http.StatusOK, map[string]string{"projectId": project})
	default:
		answer(http.StatusNotFound, map[string]string{"code": "not_found"})
	}
}

type rig struct {
	zerops   *zeropsStub
	hq       *hqStub
	hqURL    string
	enroller Enroller
	path     string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	z := &zeropsStub{}
	zSrv := httptest.NewServer(http.HandlerFunc(z.handler))
	t.Cleanup(zSrv.Close)
	h := newHQStub(z)
	hSrv := httptest.NewServer(http.HandlerFunc(h.handler))
	t.Cleanup(hSrv.Close)
	z.members = []platform.OrgMember{
		{ID: "cu-owner", UserID: "u-owner", Status: "ACTIVE", RoleCode: "OWNER", Email: "owner@example.com", FullName: "Owner"},
		{ID: "cu-anchor", UserID: "u-anchor", Status: "ACTIVE", RoleCode: "ADMIN", Email: "token-anchor@zerops.io",
			FullName: anchorPrefix + hqProject + ":" + hSrv.URL},
	}
	client, err := platform.NewZeropsClient("zcp-key", zSrv.URL)
	if err != nil {
		t.Fatalf("NewZeropsClient: %v", err)
	}
	path := filepath.Join(t.TempDir(), "hq", "enrollment.json")
	return &rig{
		zerops: z, hq: h, hqURL: hSrv.URL, path: path,
		enroller: Enroller{
			Zerops: client, HTTP: hSrv.Client(), OrgID: orgID, ProjectID: projectID,
			Path: path, Poll: 5 * time.Millisecond,
		},
	}
}

func TestEnroll_OfficialHQ_ProvesThroughUnmarkedEnvAndKeepsCredential(t *testing.T) {
	t.Parallel()
	r := newRig(t)

	got, err := r.enroller.Enroll(context.Background())
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if got.HQ != r.hqURL || !got.Changed {
		t.Errorf("Enroll = %+v, want a fresh enrollment with %s", got, r.hqURL)
	}
	saved, found, err := LoadEnrollment(r.path)
	if err != nil || !found {
		t.Fatalf("enrollment not kept: found %v, err %v", found, err)
	}
	want := Enrollment{HQ: r.hqURL, HQProjectID: hqProject, ProjectID: projectID, Credential: "credential-1"}
	if saved != want {
		t.Errorf("kept %+v, want %+v", saved, want)
	}
	if len(r.zerops.written) != 1 || r.zerops.written[0].Key != ChallengeEnv || r.zerops.written[0].Sensitive {
		t.Errorf("env written = %+v, want one unmarked %s", r.zerops.written, ChallengeEnv)
	}
	if _, left := r.zerops.challengeValue(); left {
		t.Error("the challenge env outlived the enrollment")
	}
}

func TestEnroll_NoSingleOfficialHQ_WritesNothing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		anchors []platform.OrgMember
		want    Verdict
	}{
		{"no anchor", nil, VerdictNone},
		{"anchors naming two projects", []platform.OrgMember{
			{ID: "a1", Status: "ACTIVE", RoleCode: "ADMIN", Email: "token-a1@zerops.io", FullName: "mate-hq:hq1:https://a.example"},
			{ID: "a2", Status: "ACTIVE", RoleCode: "ADMIN", Email: "token-a2@zerops.io", FullName: "mate-hq:hq2:https://b.example"},
		}, VerdictUnclear},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newRig(t)
			r.zerops.members = append(r.zerops.members[:1], tt.anchors...)

			_, err := r.enroller.Enroll(context.Background())
			var noHQ *NoHQError
			if !errors.As(err, &noHQ) || noHQ.Official.Verdict != tt.want {
				t.Fatalf("Enroll err = %v, want no HQ (%s)", err, tt.want)
			}
			if len(r.zerops.written) != 0 || len(r.hq.calls) != 0 {
				t.Errorf("wrote %d env vars and called HQ %v; want neither", len(r.zerops.written), r.hq.calls)
			}
			if _, found, _ := LoadEnrollment(r.path); found {
				t.Error("an enrollment was kept")
			}
		})
	}
}

func TestEnroll_HQRefuses_ChallengeEnvStillRemoved(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.hq.refuse = "project_not_in_org"

	_, err := r.enroller.Enroll(context.Background())
	if !refusedAs(err, "project_not_in_org") {
		t.Fatalf("Enroll err = %v, want HQ's project_not_in_org", err)
	}
	if len(r.zerops.written) != 1 {
		t.Errorf("env written %d times, want once", len(r.zerops.written))
	}
	if _, left := r.zerops.challengeValue(); left {
		t.Error("the challenge env outlived a refused enrollment")
	}
	if _, found, _ := LoadEnrollment(r.path); found {
		t.Error("an enrollment was kept")
	}
}

func TestEnroll_HQSeesTheNonceLate_PresentsItAgain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		lagging int
		wantErr bool
	}{
		{"seen on the second presentation", 1, false},
		{"seen on the last presentation", mismatchTries - 1, false},
		{"never seen", mismatchTries, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newRig(t)
			r.hq.lagging = tt.lagging

			_, err := r.enroller.Enroll(context.Background())
			if tt.wantErr != refusedAs(err, "env_mismatch") || (!tt.wantErr && err != nil) {
				t.Fatalf("Enroll err = %v, wantErr %v", err, tt.wantErr)
			}
			if _, found, _ := LoadEnrollment(r.path); found == tt.wantErr {
				t.Errorf("enrollment kept = %v, want %v", found, !tt.wantErr)
			}
			if _, left := r.zerops.challengeValue(); left {
				t.Error("the challenge env outlived the enrollment")
			}
		})
	}
}

func TestEnroll_ChallengeLeftByAnEarlierRun_RemovedFirst(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.zerops.env = []envVar{{ID: "env-stale", Key: ChallengeEnv, Content: "nonce-of-a-crashed-run"}}

	if _, err := r.enroller.Enroll(context.Background()); err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if _, left := r.zerops.challengeValue(); left {
		t.Error("a challenge env is left behind")
	}
}

func TestEnroll_AlreadyEnrolled_IssuesOnlyWhenHQNoLongerKnowsIt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		known       bool
		wantChanged bool
	}{
		{"HQ knows the credential", true, false},
		{"HQ revoked it", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newRig(t)
			if _, err := r.enroller.Enroll(context.Background()); err != nil {
				t.Fatalf("first Enroll: %v", err)
			}
			if !tt.known {
				r.hq.credential = map[string]string{}
			}
			r.zerops.written, r.hq.calls = nil, nil

			got, err := r.enroller.Enroll(context.Background())
			if err != nil {
				t.Fatalf("Enroll: %v", err)
			}
			if got.Changed != tt.wantChanged {
				t.Errorf("Changed = %v, want %v", got.Changed, tt.wantChanged)
			}
			if wrote := len(r.zerops.written) > 0; wrote != tt.wantChanged {
				t.Errorf("challenge written = %v, want %v", wrote, tt.wantChanged)
			}
			saved, _, _ := LoadEnrollment(r.path)
			if _, live := r.hq.credential[saved.Credential]; !live {
				t.Errorf("kept credential %q is not the one HQ knows", saved.Credential)
			}
		})
	}
}

func TestStatus_OfficialHQAndEnrollment_NeverTheCredential(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		arrange      func(r *rig)
		wantVerdict  Verdict
		wantEnrolled bool
	}{
		{"no HQ", func(r *rig) { r.zerops.members = r.zerops.members[:1] }, VerdictNone, false},
		{"the member list unreadable", func(r *rig) { r.enroller.OrgID = "org-unknown" }, VerdictUnknown, false},
		{"an HQ, not enrolled", func(*rig) {}, VerdictOfficial, false},
		{"enrolled, HQ knows it", func(r *rig) { mustEnroll(t, r) }, VerdictOfficial, true},
		{"enrolled, HQ revoked it", func(r *rig) {
			mustEnroll(t, r)
			r.hq.credential = map[string]string{}
		}, VerdictOfficial, false},
		{"enrolled with an HQ that is no longer the official one", func(r *rig) {
			mustEnroll(t, r)
			r.zerops.members[1].FullName = anchorPrefix + "hq2:https://elsewhere.example"
		}, VerdictOfficial, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newRig(t)
			tt.arrange(r)

			got := r.enroller.Status(context.Background())
			if got.HQ.Verdict != tt.wantVerdict || got.Enrolled != tt.wantEnrolled ||
				(got.Error != "") != (tt.wantVerdict == VerdictUnknown) {
				t.Errorf("Status = %+v, want %s, enrolled %v", got, tt.wantVerdict, tt.wantEnrolled)
			}
			raw, _ := json.Marshal(got)
			if strings.Contains(string(raw), "credential-") {
				t.Errorf("status carries the credential: %s", raw)
			}
		})
	}
}

func mustEnroll(t *testing.T, r *rig) {
	t.Helper()
	if _, err := r.enroller.Enroll(context.Background()); err != nil {
		t.Fatalf("Enroll: %v", err)
	}
}
