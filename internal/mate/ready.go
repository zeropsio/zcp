package mate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

const updateLoopback = "http://127.0.0.1:3773/api/mate/update/"

// UpdateProgressWindow is how long a Mate's boot may go without a receipt of
// progress before its wait ends as failed. Every receipt starts the window
// again, so a boot that keeps advancing is never failed by elapsed time: the
// launcher's own steps (launch.json: its start, the install pass, about one a
// second of the state copy, the server's process started), the first answer of
// the new boot, and every boot-file receipt whose progress grew. While the unit
// stops its previous process the window does not run: systemd's stop timeout
// bounds that stretch.
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

// ErrNotAnswering is a drain that got no HTTP answer at all: nothing listened or
// the connection broke. A Mate server answers no request before command
// readiness, so a server that does not answer has admitted no work in its boot:
// it is no running Mate to drain. A request that ran into its deadline reached a
// server that may hold work, so it is not this verdict.
var ErrNotAnswering = errors.New("mate does not answer")

// DrainUpdate asks the running Mate to prove idle behind its admission barrier.
// The caller cancels on failure and switches only after this proof succeeds; a
// failure wrapping ErrNotAnswering has no barrier to cancel.
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
		// net/http reports a request that got no response as *url.Error; an
		// answer it could not use (a status, a payload) is not one.
		var unanswered *url.Error
		if errors.As(err, &unanswered) && !unanswered.Timeout() {
			return drain, fmt.Errorf("drain Mate update: %w: %w", ErrNotAnswering, err)
		}
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

// bootReceipts follows the one boot a wait attributes to its launch: the
// launcher's process and progress in the launch file, the HTTP boot id, and the
// progress in the boot file of the server the launch file names.
type bootReceipts struct {
	launchPID      int
	launchProgress int64
	server         int
	boot           string
	progress       int64
}

// observeLaunch reads the launch file the launcher of the boot writes. A
// launcher that is gone ended its server's process with it; another launcher
// in the same wait is a later launch of the unit. ObserveRestart removed the
// previous launch's file, so any receipt there is this launch's.
func (b *bootReceipts) observeLaunch() (readinessVerdict, bool, error) {
	l, ok := readLaunchFile()
	if !ok {
		return verdictBooting, false, nil
	}
	if b.launchPID != 0 && l.PID != b.launchPID {
		return verdictFailed, false, fmt.Errorf("launch by process %d was replaced by process %d", b.launchPID, l.PID)
	}
	if !l.alive() {
		return verdictFailed, false, fmt.Errorf("launching process %d exited", l.PID)
	}
	advanced := b.launchPID == 0 || l.Progress > b.launchProgress
	b.launchPID, b.launchProgress, b.server = l.PID, max(b.launchProgress, l.Progress), l.Server
	return verdictBooting, advanced, nil
}

// observeFile reads the boot file the launch's server writes. Only a receipt
// of the server the launch file names is this launch's: a previous server
// still ending may write one more after its file was removed. A missing or
// unreadable file, or another process's receipt, is no receipt.
func (b *bootReceipts) observeFile() (readinessVerdict, bool, error) {
	f, ok := readBootFile()
	if !ok || b.server == 0 || f.PID != b.server {
		return verdictBooting, false, nil
	}
	if f.Error != "" {
		return verdictFailed, false, fmt.Errorf("booting process %d reported: %s", f.PID, f.Error)
	}
	advanced := f.Progress > b.progress
	b.progress = max(b.progress, f.Progress)
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

// bootCheck reports what the wait learns about the boot's process outside its
// receipts: err once the process ended, stopping while systemd stops the
// unit's previous process (a stretch its stop timeout bounds).
type bootCheck func() (stopping bool, err error)

// waitReady is the one wait for a Mate's boot: it ends ready, or failed on an
// attributed failure or once window passes without a receipt of progress.
// Every receipt, and every observation of the previous process stopping,
// starts the window again; while that process stops, nothing is observed.
func waitReady(version, oldBoot string, window time.Duration, check bootCheck) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var receipts bootReceipts
	horizon := time.Now().Add(window)
	for {
		stopping, err := check()
		if err != nil {
			return fmt.Errorf("readiness for %s: %w", version, err)
		}
		if stopping {
			// Whatever answers or writes while the previous process stops is
			// that process's, never this boot's.
			horizon = time.Now().Add(window)
			<-ticker.C
			continue
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
		for _, observeFile := range []func() (readinessVerdict, bool, error){receipts.observeLaunch, receipts.observeFile} {
			if verdict != verdictBooting {
				break
			}
			fileVerdict, fileAdvanced, fileErr := observeFile()
			if fileVerdict == verdictFailed {
				verdict, observation = fileVerdict, fileErr
			}
			advanced = advanced || fileAdvanced
		}
		stoppingAfter, err := check()
		if err != nil {
			return fmt.Errorf("readiness for %s: %w", version, err)
		}
		if advanced || stoppingAfter {
			horizon = time.Now().Add(window)
		}
		switch {
		case verdict == verdictFailed:
			return fmt.Errorf("readiness for %s: %w", version, observation)
		case verdict == verdictReady && time.Now().Before(horizon):
			return nil
		}
		if !time.Now().Before(horizon) {
			return fmt.Errorf("readiness for %s: no boot progress for %s: %w; last observation: %w", version, window, context.DeadlineExceeded, observation)
		}
		<-ticker.C
	}
}

// WaitRunningBoot waits for the running launch's server, which gave no HTTP
// answer, to become ready as version: a server answers nothing before command
// readiness, so a silent server may be booting. It fails wrapping
// ErrNotAnswering when no live launch is on record, or the launch ends or
// stops progressing: then no server holds work.
func WaitRunningBoot(version string) error {
	if l, ok := readLaunchFile(); !ok || !l.alive() {
		return fmt.Errorf("%w: no live launch of the Mate", ErrNotAnswering)
	}
	if err := waitReady(version, "", UpdateProgressWindow, func() (bool, error) { return false, nil }); err != nil {
		return fmt.Errorf("%w: %w", ErrNotAnswering, err)
	}
	return nil
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
