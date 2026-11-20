package mate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const updateLoopback = "http://127.0.0.1:3773/api/mate/update/"

// UpdateReadiness is the loopback updater API payload. Blockers (readiness)
// and Why (a drain answered drained: false) are the Mate's own words on what
// it waits on, opaque to zcp but for the one prefix an operator's forced update
// reads (Overridable); an older Mate sends no Why.
type UpdateReadiness struct {
	Protocol int      `json:"protocol"`
	Version  string   `json:"version"`
	BootID   string   `json:"bootId"`
	Ready    bool     `json:"ready"`
	Blockers []string `json:"blockers"`
	Drained  bool     `json:"drained"`
	Why      string   `json:"why"`
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
	body, err := drainBody(version, automatic)
	if err != nil {
		return UpdateReadiness{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), drainBound)
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

// drainBound is a drain request's own bound: the Mate's deadline and a second.
const drainBound = 10*time.Minute + time.Second

// drainBody is a drain request's body for version.
func drainBody(version string, automatic bool) ([]byte, error) {
	body, err := json.Marshal(struct {
		Version    string `json:"version"`
		DeadlineMS int    `json:"deadlineMs"`
		Automatic  bool   `json:"automatic"`
	}{version, 600000, automatic})
	if err != nil {
		return nil, fmt.Errorf("encode drain request: %w", err)
	}
	return body, nil
}

// CloseForUpdate closes the running Mate's intake for an operator's forced
// update (docs/spec-mate.md §2.1d): it posts the attended drain for version
// and returns once that request is written, without waiting for its answer.
// The Mate closes intake as it reads the request and reopens it when the
// request ends unanswered, so intake stays closed only while the request is
// open. ended is closed when the request ends any way: answered, dropped by a
// stopping Mate, or its own bound. A request that could not be written is the
// error, and ended is nil.
func CloseForUpdate(version string) (ended <-chan struct{}, err error) {
	body, err := drainBody(version, false)
	if err != nil {
		return nil, err
	}
	written := make(chan struct{})
	var once sync.Once
	trace := &httptrace.ClientTrace{WroteRequest: func(info httptrace.WroteRequestInfo) {
		if info.Err == nil {
			once.Do(func() { close(written) })
		}
	}}
	ctx, cancel := context.WithTimeout(httptrace.WithClientTrace(context.Background(), trace), drainBound)
	done := make(chan struct{})
	failed := make(chan error, 1)
	go func() {
		defer close(done)
		defer cancel()
		failed <- UpdateRequest(ctx, "drain", body, nil)
	}()
	select {
	case <-written:
		return done, nil
	case err := <-failed:
		// A request that ended at once was still written when the trace says so.
		select {
		case <-written:
			return done, nil
		default:
		}
		if err == nil {
			err = errors.New("drain request ended unwritten")
		}
		return nil, fmt.Errorf("close Mate intake: %w", err)
	}
}

// UpdateHTTPError is a non-200 answer from the loopback updater API.
type UpdateHTTPError struct {
	Path   string
	Status int
}

func (e *UpdateHTTPError) Error() string {
	return fmt.Sprintf("update %s: HTTP %d", e.Path, e.Status)
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

// PostUpdate sends one request to the loopback updater API, bounded to 5 s.
func PostUpdate(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return UpdateRequest(ctx, path, []byte("{}"), nil)
}
