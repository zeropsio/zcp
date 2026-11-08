package mate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const updateLoopback = "http://127.0.0.1:3773/api/mate/update/"

// UpdateProgressWindow is how long a restarted Mate may go without a receipt of
// boot progress before its wait ends as failed. Each advance it reports starts the
// window again, so a slow boot that keeps advancing is never failed by elapsed
// time. The window covers the stretch before a candidate first answers and a
// whole boot of a release that predates progress receipts (rollback, recovery):
// Eli's longest such boot measured 109 s.
const UpdateProgressWindow = 2 * time.Minute

// UpdateReadiness is the loopback updater API payload.
type UpdateReadiness struct {
	Protocol int    `json:"protocol"`
	Version  string `json:"version"`
	BootID   string `json:"bootId"`
	Ready    bool   `json:"ready"`
	Drained  bool   `json:"drained"`
}

// BootFilePath is where a booting Mate reports its progress until it accepts
// commands (ZCP_MATE_BOOT_FILE, §2.1d). Its HTTP server answers nothing before
// then, so this file is the only receipt of a boot still on its way.
func BootFilePath() string { return filepath.Join(Prefix(), "boot.json") }

// bootFile is one receipt in BootFilePath.
type bootFile struct {
	Protocol int    `json:"protocol"`
	PID      int    `json:"pid"`
	Progress int64  `json:"progress"`
	Error    string `json:"error,omitempty"`
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

// WaitReady observes whether version started healthy (the proof of probeReady) until
// deadline; a boot's progress never extends it. A boot that reports its failure ends
// the observation at once.
func WaitReady(version, oldBoot string, deadline time.Time) error {
	return waitReady(version, oldBoot, deadline, 0, nil)
}

// readinessVerdict is one observation of a restarted Mate.
type readinessVerdict int

const (
	// verdictBooting: nothing proves the outcome yet; advanced tells whether this
	// observation is a receipt of boot progress.
	verdictBooting readinessVerdict = iota
	// verdictReady: the exact version proved readiness, engine/database and C-4 on a
	// new boot.
	verdictReady
	// verdictFailed: the unit exited, the new boot runs another version, it reported
	// a failed startup, or another boot or process replaced it.
	verdictFailed
)

// bootReceipts follows the one new boot a wait attributes to its restart: its
// HTTP boot id, and the process id and progress of its boot file.
type bootReceipts struct {
	boot     string
	pid      int
	progress int64
}

// observeFile reads the boot file a restart's new boot writes; ObserveRestart
// removed the previous one, so any receipt there is this restart's. A missing or
// unreadable file is no receipt.
func (b *bootReceipts) observeFile() (readinessVerdict, bool, error) {
	f, ok := readBootFile()
	if !ok {
		return verdictBooting, false, nil
	}
	if b.pid != 0 && f.PID != b.pid {
		return verdictFailed, false, fmt.Errorf("booting process %d was replaced by process %d", b.pid, f.PID)
	}
	if f.Error != "" {
		return verdictFailed, false, fmt.Errorf("booting process %d reported: %s", f.PID, f.Error)
	}
	advanced := b.pid == 0 || f.Progress > b.progress
	b.pid, b.progress = f.PID, max(b.progress, f.Progress)
	return verdictBooting, advanced, nil
}

// readBootFile is the boot file's receipt, when one of protocol 1 is there.
func readBootFile() (bootFile, bool) {
	var f bootFile
	raw, err := os.ReadFile(BootFilePath())
	if err == nil {
		err = json.Unmarshal(raw, &f)
	}
	return f, err == nil && f.Protocol == 1 && f.PID > 0
}

// observe judges one readiness answer. Only the new boot's answers count: the old
// process may still answer while it stops, and an unanswered probe proves nothing.
func (b *bootReceipts) observe(ctx context.Context, version, oldBoot string) (readinessVerdict, bool, error) {
	var r UpdateReadiness
	if err := UpdateRequest(ctx, "readiness", nil, &r); err != nil {
		return verdictBooting, false, fmt.Errorf("probe readiness: %w", err)
	}
	if r.Protocol != 1 || r.BootID == "" || r.BootID == oldBoot {
		return verdictBooting, false, fmt.Errorf("protocol=%d ready=%t version=%q boot=%q", r.Protocol, r.Ready, r.Version, r.BootID)
	}
	if b.boot != "" && r.BootID != b.boot {
		return verdictFailed, false, fmt.Errorf("boot %q was replaced by boot %q", b.boot, r.BootID)
	}
	if r.Version != version {
		return verdictFailed, false, fmt.Errorf("boot %q runs version %q", r.BootID, r.Version)
	}
	advanced := b.boot == ""
	b.boot = r.BootID
	if !r.Ready {
		return verdictBooting, advanced, fmt.Errorf("boot %q not ready", r.BootID)
	}
	if err := environmentReady(ctx); err != nil {
		return verdictBooting, advanced, fmt.Errorf("probe environment readiness: %w", err)
	}
	return verdictReady, advanced, nil
}

// waitReady ends ready, or failed on an attributed failure or once horizon passes.
// With window > 0 every receipt of boot progress moves horizon to window from then.
// Only a wait after a restart reads the boot file: a cold boot's file may be a
// previous boot's.
func waitReady(version, oldBoot string, horizon time.Time, window time.Duration, restart *RestartReadiness) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var receipts bootReceipts
	for {
		if err := exitedReadinessUnit(context.Background(), restart); err != nil {
			return fmt.Errorf("readiness for %s: %w", version, err)
		}
		// One startup connection must not consume the whole wait; only a factual
		// answer of the new boot decides it.
		probeDeadline := time.Now().Add(time.Second)
		if horizon.Before(probeDeadline) {
			probeDeadline = horizon
		}
		probeCtx, probeCancel := context.WithDeadline(context.Background(), probeDeadline)
		verdict, advanced, observation := receipts.observe(probeCtx, version, oldBoot)
		probeCancel()
		if restart != nil && verdict == verdictBooting {
			fileVerdict, fileAdvanced, fileErr := receipts.observeFile()
			if fileVerdict == verdictFailed {
				verdict, observation = fileVerdict, fileErr
			}
			advanced = advanced || fileAdvanced
		}
		if err := exitedReadinessUnit(context.Background(), restart); err != nil {
			return fmt.Errorf("readiness for %s: %w", version, err)
		}
		if advanced && window > 0 {
			horizon = time.Now().Add(window)
		}
		switch {
		case verdict == verdictFailed:
			return fmt.Errorf("readiness for %s: %w", version, observation)
		case verdict == verdictReady && time.Now().Before(horizon):
			return nil
		}
		if !time.Now().Before(horizon) {
			return fmt.Errorf("readiness for %s: no boot progress in time: %w; last observation: %w", version, context.DeadlineExceeded, observation)
		}
		<-ticker.C
	}
}

// probeReady is the readiness proof for a status observation of a restored
// process after its updater has exited.
func probeReady(ctx context.Context, version, oldBoot string) error {
	var receipts bootReceipts
	verdict, _, observation := receipts.observe(ctx, version, oldBoot)
	if verdict != verdictReady {
		return observation
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
