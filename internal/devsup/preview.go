package devsup

import (
	"fmt"
	"hash/fnv"
	"html"
	"io"
	"net/http"
	"strings"
	"time"
)

// HoldPath is the path the holding page's one held request asks for on the
// preview port, naming the page it came from (?page=pageID). The preview
// forward answers it itself while the preview server is not running; a
// running server gets it like any other path, and the page reloads on its
// answer (docs/spec-mate.md §2.8a.2).
const HoldPath = "/.zcp-devsup/hold"

// holdWait bounds a held request: it answers once the port would serve
// another page than the one the request came from (holdDone), or after
// holdWait with holdAgain, on which the page asks again without reloading.
const holdWait = 25 * time.Second

// holdHeader carries holdAgain on the answer a held request gets when
// holdWait passed with nothing changed; every other answer reloads the page.
// The page reads it across origins (its sandboxed frame gives it an opaque
// one), so the port's own answers to the held request allow any origin.
const (
	holdHeader = "X-Zcp-Devsup"
	holdAgain  = "again"
)

// previewKind is what the preview port does with a connection.
type previewKind int

const (
	// previewForward: the bytes go to the server and back.
	previewForward previewKind = iota
	// previewHold: the holding page that says the server is starting or
	// restarting.
	previewHold
	// previewFinal: the holding page that says why the server is down and
	// that nothing will start it; it stays until someone does.
	previewFinal
)

// previewAnswer is what the preview port does for the preview server in
// status, retryPending its one more start still to come: a running server
// gets the bytes; a starting server, or a crashed one that starts again,
// gets the page that says it is coming; a crashed or stopped one that nothing
// will start gets the page that says so. Either page holds one request and
// opens the server once it runs (holdDone).
func previewAnswer(status Status, retryPending bool) previewKind {
	switch status {
	case StatusRunning:
		return previewForward
	case StatusStarting:
		return previewHold
	case StatusCrashed:
		if retryPending {
			return previewHold
		}
	case StatusStopped:
	}
	return previewFinal
}

// previewView is the preview server as the preview port answers for it.
type previewView struct {
	kind   previewKind
	name   string
	status Status
	exit   *Exit
	// reason is why the supervisor last restarted it, "" before its first
	// restart.
	reason string
}

// previewForwarded reports whether the preview port listens for the
// declaration's preview server, whose facts are f and whose preview port is
// listen (0 for none): while the supervisor has not shut down (closed) and
// the server has a port — whatever its status, so its own page answers there
// before its first start, while it starts, after an exit and while stopped,
// and never the platform's error page.
func previewForwarded(closed bool, listen int, f statusFacts) bool {
	return !closed && listen != 0 && f.port > 0
}

// onceMore is why a server's exit is followed by one more start, "" when it
// is not: a start crash (startCrash), or an exit after it answered of the
// preview server (previewed: the preview port reaches it), whom a person
// watches through that port — unless this start was already that one more
// start (retry).
func onceMore(retry, startCrash, previewed bool) string {
	switch {
	case retry:
		return ""
	case startCrash:
		return reasonRetry
	case previewed:
		return reasonExited
	}
	return ""
}

// pageID names the holding page v gives: two views with one ID give the same
// page. The page sends it with its held request.
func pageID(v previewView) string {
	title, says := pageText(v)
	h := fnv.New32a()
	fmt.Fprintf(h, "%d\x00%s\x00%s", v.kind, title, strings.Join(says, "\x00"))
	return fmt.Sprintf("%08x", h.Sum32())
}

// holdDone reports whether a held request that came from the page served
// (its pageID) answers, now being what the port answers: once the server is
// forwarded, or the page it would give differs — a page that says the server
// stopped opens it once someone starts it, one that says it is starting shows
// its crash. While the page would be the same it holds, so no page reloads
// into itself.
func holdDone(served string, now previewView) bool {
	return now.kind == previewForward || pageID(now) != served
}

// holdingPage is the page the preview port answers while its server is not
// running. It renders in Mate's sandboxed frame (scripts allowed, same origin
// only for a page on another origin): inline style and script only, nothing
// fetched but the held request. That request reloads the page on any answer
// but holdAgain, on which it asks again, and 2 s after it failed.
func holdingPage(v previewView) []byte {
	title, says := pageText(v)
	var b strings.Builder
	b.WriteString("<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\">")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width,initial-scale=1\">")
	fmt.Fprintf(&b, "<title>%s</title>", html.EscapeString(title))
	b.WriteString("<style>:root{color-scheme:light dark}body{margin:0;min-height:100vh;display:grid;place-items:center;font:15px/1.5 system-ui,sans-serif}main{max-width:32rem;padding:2rem}h1{font-size:1.15rem;margin:0 0 .5rem}p{margin:.25rem 0;opacity:.8}</style>")
	b.WriteString("</head><body><main>")
	fmt.Fprintf(&b, "<h1>%s</h1>", html.EscapeString(title))
	for _, line := range says {
		fmt.Fprintf(&b, "<p>%s</p>", html.EscapeString(line))
	}
	b.WriteString("</main>")
	fmt.Fprintf(&b, "<script>(function(){var u=%q,reload=function(){location.reload()},ask=function(){fetch(u,{cache:\"no-store\"}).then(function(r){if(r.headers.get(%q)===%q){ask()}else{reload()}},function(){setTimeout(reload,2000)})};ask()})()</script>", HoldPath+"?page="+pageID(v), holdHeader, holdAgain)
	b.WriteString("</body></html>")
	return []byte(b.String())
}

// pageText is the holding page's title and lines for v.
func pageText(v previewView) (string, []string) {
	if v.kind == previewHold {
		const soon = "This page opens it as soon as it answers."
		switch {
		case v.status == StatusCrashed:
			return "Restarting the dev server", []string{v.name + " " + exitText(v.exit) + " and starts once more.", soon}
		case v.reason != "":
			return "Restarting the dev server", []string{v.name + " is starting again: " + v.reason + ".", soon}
		}
		return "Starting the dev server", []string{v.name + " is starting.", soon}
	}
	const opens = "This page opens it once it runs."
	if v.status == StatusCrashed {
		return "The dev server stopped", []string{v.name + " " + exitText(v.exit) + ".", "Ask your Mate to start it again.", opens}
	}
	return "The dev server is stopped", []string{v.name + " is stopped.", "Ask your Mate to start it.", opens}
}

// exitText says how a server's last process ended.
func exitText(e *Exit) string {
	if e == nil {
		return "stopped"
	}
	var how string
	switch e.Signal {
	case ExitPortBusy:
		how = "could not start: its port stayed busy"
	case ExitStartFailed:
		how = "could not start"
	case "":
		how = fmt.Sprintf("exited with code %d", e.Code)
	default:
		how = "was ended by " + e.Signal
	}
	if !e.At.IsZero() {
		how += " at " + e.At.UTC().Format("15:04 UTC")
	}
	return how
}

// writeAnswer writes one HTTP/1.1 answer that ends the connection and is never
// cached, with the header lines given: each request the preview port answers
// itself comes on a connection of its own, decided again.
func writeAnswer(w io.Writer, code int, header []string, body []byte) error {
	var head strings.Builder
	fmt.Fprintf(&head, "HTTP/1.1 %d %s\r\nCache-Control: no-store\r\nConnection: close\r\nContent-Length: %d\r\n", code, http.StatusText(code), len(body))
	for _, line := range header {
		head.WriteString(line + "\r\n")
	}
	head.WriteString("\r\n")
	if _, err := io.WriteString(w, head.String()); err != nil {
		return fmt.Errorf("write answer: %w", err)
	}
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("write answer: %w", err)
	}
	return nil
}
