//go:build e2e

// Tests for: e2e — the Zerops API recorder. TestRecord is the one producer of
// the Zerops response shapes in e2e/recordings/: zcp's mock platform
// (integration/) and z3's pinned copy load them instead of hand-written
// responses. A new endpoint is one entry in recordedEndpoints (z3 adds one by
// a two-repo change).
//
// Run against the e2e project (a project-scoped ZCP_API_KEY):
//
//	ZCP_RECORD=1 go test ./e2e -tags e2e -run TestRecord -count=1
//
// The recorder never prints a response: failures name the endpoint and the
// HTTP status only, and the leak check reports a count.

package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/platform"
)

// recordingsDir is where TestRecord writes and integration/ reads.
const recordingsDir = "recordings"

// recordScope is what an endpoint's path and body are filled from: the
// key's project and org, and the first service of the project's direct list.
type recordScope struct {
	projectID string
	clientID  string
	serviceID string
}

// recordedEndpoint is one listed read. schema is the endpoint's method and
// path template, the recording's $provenance.schema.
type recordedEndpoint struct {
	file   string
	method string
	schema string
	path   func(recordScope) string
	body   func(recordScope) any
}

// recordedEndpoints lists every read the recorder calls, once each, in order:
// the service by-id read needs a service id from the direct list before it.
var recordedEndpoints = []recordedEndpoint{
	{
		file:   "user-info.json",
		method: http.MethodGet,
		schema: "GET /api/rest/public/user/info",
		path:   func(recordScope) string { return "/api/rest/public/user/info" },
	},
	{
		file:   "project.json",
		method: http.MethodGet,
		schema: "GET /api/rest/public/project/{id}",
		path:   func(s recordScope) string { return "/api/rest/public/project/" + s.projectID },
	},
	{
		file:   "project-service-stack.json",
		method: http.MethodGet,
		schema: "GET /api/rest/public/project/{id}/service-stack",
		path: func(s recordScope) string {
			return "/api/rest/public/project/" + s.projectID + "/service-stack?limit=1000"
		},
	},
	{
		file:   "service-stack.json",
		method: http.MethodGet,
		schema: "GET /api/rest/public/service-stack/{id}",
		path:   func(s recordScope) string { return "/api/rest/public/service-stack/" + s.serviceID },
	},
	{
		file:   "service-stack-search.json",
		method: http.MethodPost,
		schema: "POST /api/rest/public/service-stack/search",
		path:   func(recordScope) string { return "/api/rest/public/service-stack/search" },
		body: func(s recordScope) any {
			return map[string]any{
				"search": []map[string]string{
					{"name": "clientId", "operator": "eq", "value": s.clientID},
					{"name": "projectId", "operator": "eq", "value": s.projectID},
				},
				"sort": []any{},
			}
		},
	},
}

// recording is the file shape: who produced the value, and the value.
type recording struct {
	Provenance provenance `json:"$provenance"`
	Value      any        `json:"value"`
}

type provenance struct {
	Producer string `json:"producer"`
	Revision string `json:"revision"`
	Via      string `json:"via"`
	Schema   string `json:"schema"`
}

// witness: internal/auth/auth.go | ProjectID:   project.ID, → ProjectID:   project.Name,
func TestRecord(t *testing.T) {
	if os.Getenv("ZCP_RECORD") != "1" {
		t.Skip("ZCP_RECORD=1 not set: the recorder writes e2e/recordings/ only on request")
	}
	token := os.Getenv("ZCP_API_KEY")
	if token == "" {
		t.Skip("ZCP_API_KEY not set: the recorder reads the e2e project with a project-scoped key")
	}
	apiHost := defaultAPIHost()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	client, err := platform.NewZeropsClient(token, apiHost)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	info, err := auth.Resolve(ctx, client)
	if err != nil {
		t.Fatalf("resolve the key's project: %v", err)
	}

	scope := recordScope{projectID: info.ProjectID, clientID: info.ClientID}
	scrub := newRecordScrubber(token, info)
	revision := time.Now().UTC().Format("2006-01-02")
	values := make(map[string]any, len(recordedEndpoints))

	for _, ep := range recordedEndpoints {
		if ep.file == "service-stack.json" {
			scope.serviceID = firstServiceID(t, values["project-service-stack.json"])
		}
		value, err := readEndpoint(ctx, apiHost, token, ep, scope)
		if err != nil {
			t.Fatalf("%s: %v", ep.schema, err)
		}
		values[ep.file] = value
	}

	if err := os.MkdirAll(recordingsDir, 0o755); err != nil {
		t.Fatalf("create %s: %v", recordingsDir, err)
	}
	for _, ep := range recordedEndpoints {
		scrub.collect("", values[ep.file])
	}
	for _, ep := range recordedEndpoints {
		rec := recording{
			Provenance: provenance{
				Producer: "zerops",
				Revision: revision,
				Via:      "zcp e2e TestRecord",
				Schema:   ep.schema,
			},
			Value: scrub.value("", values[ep.file]),
		}
		data, err := json.MarshalIndent(rec, "", "  ")
		if err != nil {
			t.Fatalf("%s: encode: %v", ep.file, err)
		}
		if err := os.WriteFile(filepath.Join(recordingsDir, ep.file), append(data, '\n'), 0o644); err != nil {
			t.Fatalf("%s: write: %v", ep.file, err)
		}
	}

	leaks, err := countLeaks(recordingsDir, scrub.secrets)
	if err != nil {
		t.Fatalf("leak check: %v", err)
	}
	t.Logf("leak check: %d matches of the account's email, token prefix and %d live values", leaks, len(scrub.secrets))
	if leaks != 0 {
		t.Fatalf("leak check: %d matches in %s; the scrubber misses a field", leaks, recordingsDir)
	}
}

// readEndpoint calls one endpoint and decodes its 2xx JSON body. An error
// names the status only: a response body is never printed.
func readEndpoint(ctx context.Context, apiHost, token string, ep recordedEndpoint, scope recordScope) (any, error) {
	var reqBody io.Reader
	if ep.body != nil {
		data, err := json.Marshal(ep.body(scope))
		if err != nil {
			return nil, fmt.Errorf("encode body: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, ep.method, "https://"+apiHost+ep.path(scope), reqBody)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errors.New("request failed before a response")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d, want 200", resp.StatusCode)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, errors.New("the body is not JSON")
	}
	return value, nil
}

// firstServiceID picks the by-id read's service: the first, by name, of the
// project's direct service list.
func firstServiceID(t *testing.T, list any) string {
	t.Helper()
	items, _ := asObject(list)["list"].([]any)
	type named struct{ id, name string }
	services := make([]named, 0, len(items))
	for _, item := range items {
		obj := asObject(item)
		id, _ := obj["id"].(string)
		name, _ := obj["name"].(string)
		if id != "" {
			services = append(services, named{id, name})
		}
	}
	if len(services) == 0 {
		t.Fatal("the e2e project holds no service: the by-id read has nothing to record")
	}
	sort.Slice(services, func(i, j int) bool { return services[i].name < services[j].name })
	return services[0].id
}

func asObject(v any) map[string]any {
	obj, _ := v.(map[string]any)
	return obj
}

// Placeholders keep each field's wire type: a placeholder id stays a
// 22-character id, a time an RFC 3339 time, an address an address of its
// family (RFC 5737 / RFC 3849 documentation ranges), a zone label or a
// subdomain host a label of the same length.
const (
	placeholderTime  = "2026-01-01T00:00:00Z"
	placeholderEmail = "user@example.invalid"
	placeholderIPv4  = "192.0.2.1"
	placeholderIPv6  = "2001:db8::1"
	placeholderToken = "redacted"
	placeholderName  = "Recorded User"
)

var (
	rfc3339Pattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$`)
	emailPattern   = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	ipv4Pattern    = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	ipv6Pattern    = regexp.MustCompile(`^[0-9A-Fa-f]{0,4}(?::[0-9A-Fa-f]{0,4}){2,7}(?:/\d{1,3})?$`)
	secretKey      = regexp.MustCompile(`(?i)token|secret|password|passwd|accesskey|apikey|credential|hash`)
	personKey      = regexp.MustCompile(`(?i)^(full|first|last)name$|^phone`)
	idKey          = regexp.MustCompile(`^id$|Id$`)
	zeropsID       = regexp.MustCompile(`^[A-Za-z0-9_-]{22}$`)
)

// recordScrubber replaces ids, times, tokens, emails, names and addresses in
// a decoded response. Ids map consistently across every recording, so a
// service's projectId still names the recorded project. The project's public
// zone label and subdomain host are replaced as substrings like ids, so the
// recorded subdomain URLs name no live project. Every live value it removes is
// a secret the leak check counts.
type recordScrubber struct {
	ids     map[string]string
	labels  map[string]string
	secrets []string
}

func newRecordScrubber(token string, info *auth.Info) *recordScrubber {
	s := &recordScrubber{ids: map[string]string{}, labels: map[string]string{}}
	prefix := token
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	s.secrets = []string{prefix}
	if info.Email != "" {
		s.secrets = append(s.secrets, info.Email)
	}
	for _, id := range []string{info.ProjectID, info.ClientID} {
		if id != "" {
			s.placeholderID(id)
		}
	}
	return s
}

func (s *recordScrubber) placeholderID(id string) string {
	if p, ok := s.ids[id]; ok {
		return p
	}
	p := fmt.Sprintf("REC%019d", len(s.ids)+1)
	s.ids[id] = p
	s.secrets = append(s.secrets, id)
	return p
}

// label maps a live zone label or subdomain host to a same-length placeholder.
func (s *recordScrubber) label(live string) {
	if live == "" || s.labels[live] != "" {
		return
	}
	s.labels[live] = strings.Repeat("0", len(live))
	s.secrets = append(s.secrets, live)
}

// secret registers a removed value with the leak check.
func (s *recordScrubber) secret(v string) {
	if v != "" && v != placeholderToken {
		s.secrets = append(s.secrets, v)
	}
}

// sensitiveEnv reports a userData entry ({key, content, sensitive}) whose
// content is a credential: Zerops marks it sensitive, or its key names one.
func sensitiveEnv(obj map[string]any) bool {
	key, ok := obj["key"].(string)
	if !ok {
		return false
	}
	if _, ok := obj["content"].(string); !ok {
		return false
	}
	sensitive, _ := obj["sensitive"].(bool)
	return sensitive || secretKey.MatchString(key)
}

// collect registers every id-keyed Zerops id, the project's zone label and
// subdomain host, and every credential value before any string is scrubbed,
// so a live value quoted inside another field (a URL, a list) is replaced too.
func (s *recordScrubber) collect(key string, v any) {
	switch x := v.(type) {
	case map[string]any:
		if sensitiveEnv(x) {
			s.secret(x["content"].(string))
		}
		for k, child := range x {
			s.collect(k, child)
		}
	case []any:
		for _, child := range x {
			s.collect(key, child)
		}
	case string:
		switch {
		case idKey.MatchString(key) && zeropsID.MatchString(x):
			s.placeholderID(x)
		case key == "publicZone":
			s.label(strings.SplitN(x, ".", 2)[0])
		case key == "zeropsSubdomainHost":
			s.label(x)
		case secretKey.MatchString(key) || personKey.MatchString(key):
			s.secret(x)
		}
	}
}

func (s *recordScrubber) value(key string, v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, child := range x {
			out[k] = s.value(k, child)
		}
		if sensitiveEnv(x) && x["content"] != "" {
			out["content"] = placeholderToken
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, child := range x {
			out[i] = s.value(key, child)
		}
		return out
	case string:
		return s.str(key, x)
	default:
		return v
	}
}

func (s *recordScrubber) str(key, v string) string {
	switch {
	case v == "":
		return v
	case secretKey.MatchString(key):
		return placeholderToken
	case personKey.MatchString(key):
		return placeholderName
	case idKey.MatchString(key) && zeropsID.MatchString(v):
		return s.placeholderID(v)
	case rfc3339Pattern.MatchString(v):
		return placeholderTime
	case ipv6Pattern.MatchString(v) && strings.Count(v, ":") >= 2:
		return placeholderIPv6
	}
	for id, p := range s.ids {
		v = strings.ReplaceAll(v, id, p)
	}
	for live, p := range s.labels {
		v = strings.ReplaceAll(v, live, p)
	}
	v = emailPattern.ReplaceAllString(v, placeholderEmail)
	return ipv4Pattern.ReplaceAllString(v, placeholderIPv4)
}

// countLeaks counts, over every recording, the occurrences of the account's
// email, the key's prefix and every live value the scrubber replaced.
func countLeaks(dir string, secrets []string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", dir, err)
	}
	count := 0
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return 0, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		for _, secret := range secrets {
			count += bytes.Count(data, []byte(secret))
		}
	}
	return count, nil
}
