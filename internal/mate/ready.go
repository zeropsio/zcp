package mate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"time"
)

const updateLoopback = "http://127.0.0.1:3773/api/mate/update/"

// UpdateStartupBudget bounds candidate, rollback and recovery readiness after a
// restart request, whether init or the update worker performs the switch.
// It covers the observed ~44 s Eli 0.16.12 recovery start with 16 s margin;
// it cannot guarantee readiness and does not replace attributed exit observation.
const UpdateStartupBudget = 60 * time.Second

// UpdateReadiness is the loopback updater API payload.
type UpdateReadiness struct {
	Protocol int    `json:"protocol"`
	Version  string `json:"version"`
	BootID   string `json:"bootId"`
	Ready    bool   `json:"ready"`
	Drained  bool   `json:"drained"`
}

// UpdateRequest calls Mate's loopback updater API; a non-200 answer is an *UpdateHTTPError.
func UpdateRequest(ctx context.Context, path string, body []byte, out *UpdateReadiness) error {
	method := http.MethodGet
	if body != nil {
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(ctx, method, updateLoopback+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("update request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("update %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return &UpdateHTTPError{Path: path, Status: resp.StatusCode}
	}
	if out == nil {
		_, err := io.Copy(io.Discard, resp.Body)
		return err
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(out); err != nil {
		return fmt.Errorf("update %s payload: %w", path, err)
	}
	return nil
}

// DrainUpdate asks the running Mate to prove idle behind its admission barrier.
// The caller cancels on failure and switches only after this proof succeeds.
func DrainUpdate(version string, automatic bool) (UpdateReadiness, error) {
	body, err := json.Marshal(struct {
		Version    string `json:"version"`
		DeadlineMS int    `json:"deadlineMs"`
		Automatic  bool   `json:"automatic"`
	}{version, 600000, automatic})
	if err != nil {
		return UpdateReadiness{}, fmt.Errorf("encode drain request: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute+time.Second)
	defer cancel()
	var drain UpdateReadiness
	if err := UpdateRequest(ctx, "drain", body, &drain); err != nil {
		return drain, fmt.Errorf("drain Mate update: %w", err)
	}
	if drain.Protocol != 1 || !drain.Drained || drain.BootID == "" {
		return drain, fmt.Errorf("mate did not prove idle before drain deadline")
	}
	return drain, nil
}

// UpdateHTTPError is a non-200 answer from the loopback updater API.
type UpdateHTTPError struct {
	Path   string
	Status int
}

func (e *UpdateHTTPError) Error() string {
	return fmt.Sprintf("update %s: HTTP %d", e.Path, e.Status)
}

// WaitReady proves version started healthy: loopback readiness protocol 1, the exact version,
// a boot id different from oldBoot (empty accepts any nonempty id), engine/database readiness
// and C-4, all before deadline.
func WaitReady(version, oldBoot string, deadline time.Time) error {
	return waitReady(version, oldBoot, deadline, nil)
}

func waitReady(version, oldBoot string, deadline time.Time, before *readinessUnitState) error {
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var lastObservation error
	for {
		if err := exitedReadinessUnit(ctx, before); err != nil {
			return fmt.Errorf("readiness for %s: %w", version, err)
		}
		// One startup connection must not consume the whole readiness budget.
		// The capability probe uses the same transport bound; only a factual
		// exact-version/new-boot response can accept the candidate.
		probeCtx, probeCancel := context.WithTimeout(ctx, time.Second)
		lastObservation = probeReady(probeCtx, version, oldBoot)
		probeCancel()
		if err := exitedReadinessUnit(ctx, before); err != nil {
			return fmt.Errorf("readiness for %s: %w", version, err)
		}
		if lastObservation == nil && ctx.Err() == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("readiness for %s: %w; last observation: %w", version, ctx.Err(), lastObservation)
		case <-ticker.C:
		}
	}
}

// probeReady is the shared proof for an updater waiting on a restart and a
// status observation of a restored process after its updater has exited.
func probeReady(ctx context.Context, version, oldBoot string) error {
	var r UpdateReadiness
	if err := UpdateRequest(ctx, "readiness", nil, &r); err != nil {
		return fmt.Errorf("probe readiness: %w", err)
	}
	if r.Protocol != 1 || !r.Ready || r.Version != version || r.BootID == "" || r.BootID == oldBoot {
		return fmt.Errorf("protocol=%d ready=%t version=%q boot=%q", r.Protocol, r.Ready, r.Version, r.BootID)
	}
	if err := environmentReady(ctx); err != nil {
		return fmt.Errorf("probe environment readiness: %w", err)
	}
	return nil
}

func environmentReady(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:3773/.well-known/t3/environment", nil)
	if err != nil {
		return fmt.Errorf("environment request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("environment probe: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var descriptor struct {
		BasePath string `json:"basePath"`
	}
	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if resp.StatusCode != http.StatusOK || mediaType != "application/json" {
		return fmt.Errorf("environment probe not JSON ready")
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&descriptor); err != nil {
		return fmt.Errorf("environment descriptor: %w", err)
	}
	if descriptor.BasePath != BasePath {
		return fmt.Errorf("environment basePath mismatch")
	}
	return nil
}
