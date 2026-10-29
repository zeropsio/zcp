package hq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// ObservationRequest is the closed read surface shared with HQ's
// wire/observation.ts. No authority, arbitrary path or URL comes from the model.
type ObservationRequest struct {
	Action    string `json:"action,omitempty"    jsonschema:"environments (default), status, deployments, processes, process, versions, version, logs, build_logs"`
	ProjectID string `json:"projectId,omitempty" jsonschema:"Target project ID from environments; required for platform reads; optional for deployments"`
	ServiceID string `json:"serviceId,omitempty" jsonschema:"Service ID from status; required for versions and logs"`
	ProcessID string `json:"processId,omitempty" jsonschema:"Exact process ID; required for process"`
	VersionID string `json:"versionId,omitempty" jsonschema:"Exact version ID; required for version and build_logs"`
	Limit     int    `json:"limit,omitempty"     jsonschema:"Maximum records: 1 to 100; default 100"`
}

const (
	observeActionEnvironments = "environments"
	observeActionStatus       = "status"
	observeActionDeployments  = "deployments"
	observeActionProcesses    = "processes"
	observeActionProcess      = "process"
	observeActionVersions     = "versions"
	observeActionVersion      = "version"
	observeActionLogs         = "logs"
	observeActionBuildLogs    = "build_logs"
)

// Normalize validates once for both the tool and direct Go callers. HQ still
// validates ownership and current native permissions on every HTTP request.
func (in ObservationRequest) Normalize() (ObservationRequest, error) {
	if in.Action == "" {
		in.Action = observeActionEnvironments
	}
	switch in.Action {
	case observeActionEnvironments, observeActionStatus, observeActionDeployments, observeActionProcesses, observeActionProcess, observeActionVersions, observeActionVersion, observeActionLogs, observeActionBuildLogs:
	default:
		return in, errors.New("unknown observation action")
	}
	id := regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	for _, field := range []struct {
		name, value string
		required    bool
	}{
		{"projectId", in.ProjectID, in.Action != observeActionEnvironments && in.Action != observeActionDeployments},
		{"serviceId", in.ServiceID, in.Action == observeActionVersions || in.Action == observeActionLogs},
		{"processId", in.ProcessID, in.Action == observeActionProcess},
		{"versionId", in.VersionID, in.Action == observeActionVersion || in.Action == observeActionBuildLogs},
	} {
		if field.required && field.value == "" || field.value != "" && !id.MatchString(field.value) {
			return in, fmt.Errorf("a valid %s is required for this observation", field.name)
		}
	}
	if in.Limit == 0 {
		in.Limit = 100
	}
	if in.Limit < 1 || in.Limit > 100 {
		return in, errors.New("observation limit must be 1 to 100")
	}
	return in, nil
}

// ObservationClient has only Observe authority. Project keys are neither
// required nor sent. HQ binds the credential to its issued source/app/org.
type ObservationClient struct {
	doer   Doer
	origin string
}

func OpenObservation(doer Doer, id Identity) (ObservationClient, error) {
	origin, err := url.Parse(id.Origin)
	if err != nil || origin.Host == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || origin.Path != "" && origin.Path != "/" || origin.Scheme != "https" && origin.Scheme != "http" || id.ProjectID == "" {
		return ObservationClient{}, fmt.Errorf("open HQ observation: %w", ErrNoIdentity)
	}
	if doer == nil {
		return ObservationClient{}, errors.New("open HQ observation: no HTTP client")
	}
	// As with OpenProject, a custom Doer owns its redirect policy.
	if client, ok := doer.(*http.Client); ok {
		isolated := *client
		isolated.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		doer = &isolated
	}
	return ObservationClient{doer: doer, origin: strings.TrimRight(id.Origin, "/")}, nil
}

// ObserveOutputLimit bounds HQ facts and the tool's final text, including own metadata.
const ObserveOutputLimit = 512 * 1024

// Observe makes one bounded request, rereading the profile's sole credential
// immediately before it. No retry, cache, project-key fallback or /self call.
func (c ObservationClient) Observe(ctx context.Context, in ObservationRequest) (json.RawMessage, error) {
	in, err := in.Normalize()
	if err != nil {
		return nil, fmt.Errorf("HQ observation input: %w", err)
	}
	credential, err := ReadObserveCredential(ObserveCredentialPath())
	if err != nil {
		return nil, fmt.Errorf("HQ observation: %w", err)
	}
	body, err := json.Marshal(in)
	if err != nil {
		return nil, errors.New("HQ observation: request could not be encoded")
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.origin+"/api/mate/observe", bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("HQ observation: request could not be created")
	}
	req.Header.Set("Authorization", "Observe "+credential)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.doer.Do(req)
	if err != nil {
		// A custom transport error can echo headers; never render its raw error.
		return nil, &UnavailableError{Err: errors.New("HQ observation request did not complete")}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, ObserveOutputLimit+1))
	if err != nil {
		return nil, &UnavailableError{Err: errors.New("HQ observation response could not be read")}
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("HQ refused observe authorization; authorize this profile again: %w", ErrNoObserveCredential)
	}
	if resp.StatusCode != http.StatusOK {
		// Only fixed machine codes survive; arbitrary upstream reason strings may
		// contain credentials. They are not an observation projection.
		var refusal struct {
			Code   string `json:"code"`
			Reason string `json:"reason"`
		}
		_ = json.Unmarshal(raw, &refusal)
		code := "observation_refused"
		switch refusal.Code {
		case "observation_refused", "forbidden", "environment_not_found", "service_not_found", "process_not_found", "version_not_found", "invalid_request", "not_active", "zerops_unavailable", "observe_credential_required", "observe_credential_refused", "too_many_requests", "observation_unavailable", "observation_output_too_large":
			code = refusal.Code
		}
		if resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusTooManyRequests {
			return nil, &UnavailableError{Code: code}
		}
		reason := ""
		switch refusal.Reason {
		case "forbidden", "environment_not_found", "service_not_found", "process_not_found", "version_not_found", "deploy_key_unavailable", "mate_not_in_app":
			reason = refusal.Reason
		}
		return nil, &RefusedError{Status: resp.StatusCode, Code: code, Reason: reason}
	}
	if len(raw) > ObserveOutputLimit || bytes.Contains(raw, []byte(credential)) {
		return nil, errors.New("HQ observation: response exceeds bounds or contains authorization material")
	}
	if err := validateObservation(raw, in); err != nil {
		return nil, fmt.Errorf("HQ observation: %w", err)
	}
	return json.RawMessage(raw), nil
}

// HQ owns the bounded fact DTOs. zcp validates the strict outer envelope and
// action payload, retaining its JSON without interpreting runtime verdicts.
func validateObservation(raw []byte, in ObservationRequest) error {
	var out struct {
		Action    string          `json:"action"`
		AppID     string          `json:"appId"`
		ProjectID json.RawMessage `json:"projectId"`
		Coverage  *struct {
			Source     string `json:"source"`
			ObservedAt string `json:"observedAt"`
			Freshness  string `json:"freshness"`
			Scope      string `json:"scope"`
			Truncated  *bool  `json:"truncated"`
		} `json:"coverage"`
		Environments json.RawMessage `json:"environments"`
		SiblingMates json.RawMessage `json:"siblingMates"`
		Services     json.RawMessage `json:"services"`
		Deployments  json.RawMessage `json:"deployments"`
		Processes    json.RawMessage `json:"processes"`
		Versions     json.RawMessage `json:"versions"`
		Entries      json.RawMessage `json:"entries"`
	}
	invalid := errors.New("invalid observe response envelope")
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&out); err != nil {
		return invalid
	}
	if decoder.Decode(new(any)) != io.EOF || out.Action != in.Action || out.AppID == "" || len(out.ProjectID) == 0 || out.Coverage == nil {
		return invalid
	}
	coverage := out.Coverage
	if coverage.Source != "hq" && coverage.Source != "zerops" && coverage.Source != "zerops_logger" || coverage.Freshness != "fresh" || coverage.Scope == "" || coverage.Truncated == nil {
		return invalid
	}
	if _, err := time.Parse(time.RFC3339Nano, coverage.ObservedAt); err != nil {
		return invalid
	}
	var project *string
	if err := json.Unmarshal(out.ProjectID, &project); err != nil || in.ProjectID != "" && (project == nil || *project != in.ProjectID) {
		return invalid
	}
	var payload json.RawMessage
	switch in.Action {
	case observeActionEnvironments:
		payload = out.Environments
	case observeActionStatus:
		payload = out.Services
	case observeActionDeployments:
		payload = out.Deployments
	case observeActionProcesses, observeActionProcess:
		payload = out.Processes
	case observeActionVersions, observeActionVersion:
		payload = out.Versions
	case observeActionLogs, observeActionBuildLogs:
		payload = out.Entries
	}
	if len(payload) == 0 {
		return invalid
	}
	for _, array := range []json.RawMessage{out.Environments, out.SiblingMates, out.Services, out.Deployments, out.Processes, out.Versions, out.Entries} {
		if len(array) == 0 {
			continue
		}
		var records []json.RawMessage
		if json.Unmarshal(array, &records) != nil || records == nil || len(records) > 100 {
			return invalid
		}
	}
	return nil
}
