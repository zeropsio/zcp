package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// BrowserCall is the agent's tool call that asks for the browser: ID is the
// calling client's own tool-call id, verbatim, "" when it sent none; Client
// is the name the client gave at initialize.
type BrowserCall struct {
	ID     string
	Client string
}

// browserCallHolder is the call file's content (browserCallPath on Linux):
// the agent's tool call that holds the warm browser, so the Mate can put the
// browser's live picture on that call's card (docs/spec-mate.md §5.6).
type browserCallHolder struct {
	CallID string `json:"callId"`
	Client string `json:"client"`
	// Since is when the call took the browser, in ms since the epoch.
	Since int64 `json:"since"`
	// TargetID is the CDP target of the call's own tab, absent when
	// agent-browser did not name it.
	TargetID string `json:"targetId,omitempty"`
}

type browserCallKey struct{}

// withBrowserCall carries call to the runner's Acquire, which holds the
// browser for it.
func withBrowserCall(ctx context.Context, call BrowserCall) context.Context {
	return context.WithValue(ctx, browserCallKey{}, call)
}

// browserCallOf is the call withBrowserCall carried, none when it carried
// nothing (zerops_verify's render check).
func browserCallOf(ctx context.Context) BrowserCall {
	call, _ := ctx.Value(browserCallKey{}).(BrowserCall)
	return call
}

// publishBrowserCall writes, while the call holds the browser lock, that
// call holds the browser in its tab target since since, replacing in one
// rename whatever the path held: a reader sees the old file or the new one,
// never a part, and a link at the path is replaced, never followed. A call
// without an id publishes none and removes what is there, so no earlier
// call's id outlives its turn. It answers the withdrawal for the call's
// release.
func publishBrowserCall(path string, call BrowserCall, target string, since time.Time) (func(), error) {
	withdraw := func() { withdrawBrowserCall(path) }
	if call.ID == "" {
		withdraw()
		return func() {}, nil
	}
	holder := browserCallHolder{CallID: call.ID, Client: call.Client, Since: since.UnixMilli(), TargetID: target}
	if err := replaceBrowserCall(path, holder); err != nil {
		withdraw()
		return func() {}, err
	}
	return withdraw, nil
}

// replaceBrowserCall writes holder to a private file beside path and
// renames it over path.
func replaceBrowserCall(path string, holder browserCallHolder) error {
	data, err := json.Marshal(holder)
	if err != nil {
		return fmt.Errorf("browser call: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return fmt.Errorf("browser call: %w", err)
	}
	_, werr := tmp.Write(data)
	if err := errors.Join(werr, tmp.Close()); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("browser call: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("browser call: %w", err)
	}
	return nil
}

// withdrawBrowserCall removes the call file: nobody holds the browser. A
// file a stop already removed with the RuntimeDirectory is no failure.
func withdrawBrowserCall(path string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(os.Stderr, "zerops_browser: browser call: %v\n", err)
	}
}
