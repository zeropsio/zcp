package ops

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakeChrome is Chrome on the DevTools protocol, in process: its cookie jar,
// each origin's local storage, the page each tab is on, the tabs opened
// through it and the DevTools sessions still attached. A navigation to a
// site is answered only once the client fulfils the request Chrome paused
// for it, as Chrome holds it; one to about:blank pauses nothing; a navigation to an origin in stall is never answered, and
// a storage write on one in refuse throws, one in fail is answered with a
// navigation error, one in upgrade lands on its https:// twin (Chrome's
// HTTPS upgrade), and dies makes Chrome die at the first navigation: the
// connection closes. Storage.setCookies refuses a batch
// holding the cookie named refuseCookie, and that cookie alone.
type fakeChrome struct {
	mu           sync.Mutex
	out          chan []byte
	closed       chan struct{}
	closeOnce    sync.Once
	cookies      []map[string]any
	storage      map[string][]browserStorageItem
	stall        map[string]bool
	refuse       map[string]bool
	fail         map[string]bool
	upgrade      map[string]bool
	dies         bool
	refuseCookie string
	created      []string
	sessions     map[string]string
	history      map[string][]string
	pages        map[string]string
	paused       map[string]pausedNavigation
	visited      []string
	nextRequest  int
}

type pausedNavigation struct {
	id      json.RawMessage
	session string
	origin  string
}

func newFakeChrome() *fakeChrome {
	return &fakeChrome{
		out:      make(chan []byte, 1024),
		closed:   make(chan struct{}),
		storage:  map[string][]browserStorageItem{},
		stall:    map[string]bool{},
		refuse:   map[string]bool{},
		fail:     map[string]bool{},
		upgrade:  map[string]bool{},
		sessions: map[string]string{},
		history:  map[string][]string{},
		pages:    map[string]string{baseTarget: "about:blank"},
		paused:   map[string]pausedNavigation{},
	}
}

// dialedBy makes f the Chrome every DevTools dial of the test reaches.
func (f *fakeChrome) dialedBy(t *testing.T) *fakeChrome {
	t.Helper()
	old := browserCDPDial
	browserCDPDial = func(context.Context, string) (browserCDPConn, error) { return f, nil }
	t.Cleanup(func() { browserCDPDial = old })
	return f
}

func (f *fakeChrome) ReadMessage() ([]byte, error) {
	select {
	case msg := <-f.out:
		return msg, nil
	case <-f.closed:
		return nil, errors.New("closed")
	}
}

func (f *fakeChrome) Close() error {
	f.closeOnce.Do(func() { close(f.closed) })
	return nil
}

func (f *fakeChrome) send(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		data = []byte(`{"method":"unencodable"}`)
	}
	f.out <- data
}

func (f *fakeChrome) reply(id json.RawMessage, result any) {
	f.send(map[string]any{"id": id, "result": result})
}

func (f *fakeChrome) deny(id json.RawMessage, message string) {
	f.send(map[string]any{"id": id, "error": map[string]any{"code": -32000, "message": message}})
}

func (f *fakeChrome) WriteMessage(_ context.Context, data []byte) error {
	select {
	case <-f.closed:
		return errors.New("closed")
	default:
	}
	var msg struct {
		ID        json.RawMessage `json:"id"`
		Method    string          `json:"method"`
		SessionID string          `json:"sessionId"`
		Params    json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	reply := func(result any) { f.reply(msg.ID, result) }
	refuse := func(message string) { f.deny(msg.ID, message) }
	switch msg.Method {
	case "Storage.getCookies":
		cookies := f.cookies
		if cookies == nil {
			cookies = []map[string]any{}
		}
		reply(map[string]any{"cookies": cookies})
	case "Storage.setCookies":
		var p struct {
			Cookies []map[string]any `json:"cookies"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		if slices.ContainsFunc(p.Cookies, func(c map[string]any) bool { return c["name"] == f.refuseCookie }) {
			refuse("Invalid cookie fields")
			return nil
		}
		f.cookies = append(f.cookies, p.Cookies...)
		reply(map[string]any{})
	case "Target.createTarget":
		var p struct {
			URL string `json:"url"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		id := "NEW" + strconv.Itoa(len(f.created)+1)
		f.created = append(f.created, id)
		f.pages[id] = p.URL
		reply(map[string]any{"targetId": id})
	case "Target.attachToTarget":
		var p struct {
			TargetID string `json:"targetId"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		if _, ok := f.pages[p.TargetID]; !ok {
			refuse("No target with given id found")
			return nil
		}
		session := "S" + strconv.Itoa(len(f.sessions)+1) + "-" + p.TargetID
		f.sessions[session] = p.TargetID
		reply(map[string]any{"sessionId": session})
	case "Target.getTargetInfo":
		var p struct {
			TargetID string `json:"targetId"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		if _, ok := f.pages[p.TargetID]; !ok {
			refuse("No target with given id found")
			return nil
		}
		reply(map[string]any{"targetInfo": map[string]any{"targetId": p.TargetID, "type": "page"}})
	case "Page.resetNavigationHistory":
		target, ok := f.sessions[msg.SessionID]
		if !ok {
			refuse("Session with given id not found.")
			return nil
		}
		f.history[target] = nil
		reply(map[string]any{})
	case "Target.detachFromTarget":
		var p struct {
			SessionID string `json:"sessionId"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		delete(f.sessions, p.SessionID)
		reply(map[string]any{})
	case "Page.navigate":
		f.navigate(msg.ID, msg.SessionID, msg.Params)
	case "Fetch.fulfillRequest":
		var p struct {
			RequestID string `json:"requestId"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		nav, ok := f.paused[p.RequestID]
		if !ok {
			refuse("Invalid InterceptionId.")
			return nil
		}
		delete(f.paused, p.RequestID)
		f.pages[f.sessions[nav.session]] = nav.origin
		f.history[f.sessions[nav.session]] = append(f.history[f.sessions[nav.session]], nav.origin)
		reply(map[string]any{})
		f.send(map[string]any{"id": nav.id, "result": map[string]any{"frameId": "F1", "loaderId": "L1"}})
	case "Runtime.evaluate":
		return f.evaluate(msg.ID, msg.SessionID, msg.Params)
	default:
		reply(map[string]any{})
	}
	return nil
}

// navigate answers a Page.navigate of session; f.mu is held.
func (f *fakeChrome) navigate(id json.RawMessage, session string, params json.RawMessage) {
	var p struct {
		URL string `json:"url"`
	}
	_ = json.Unmarshal(params, &p)
	if p.URL == "about:blank" {
		f.pages[f.sessions[session]] = p.URL
		f.history[f.sessions[session]] = append(f.history[f.sessions[session]], p.URL)
		f.reply(id, map[string]any{"frameId": "F1"})
		f.send(map[string]any{"method": "Page.frameNavigated", "sessionId": session, "params": map[string]any{"frame": map[string]any{"id": "F1", "url": "about:blank"}}})
		return
	}
	if f.dies {
		f.closeOnce.Do(func() { close(f.closed) })
		return
	}
	u, _ := url.Parse(p.URL)
	origin := u.Scheme + "://" + u.Host
	f.visited = append(f.visited, origin)
	if f.upgrade[origin] {
		origin = "https://" + u.Host
	}
	if f.stall[origin] {
		return
	}
	if f.fail[origin] {
		f.reply(id, map[string]any{"frameId": "F1", "errorText": "net::ERR_BLOCKED_BY_CLIENT"})
		return
	}
	f.nextRequest++
	rid := "R" + string(rune('0'+f.nextRequest))
	f.paused[rid] = pausedNavigation{id: id, session: session, origin: origin}
	f.send(map[string]any{"method": "Fetch.requestPaused", "sessionId": session, "params": map[string]any{"requestId": rid, "request": map[string]any{"url": p.URL}}})
}

// evaluate answers a Runtime.evaluate of session; f.mu is held.
func (f *fakeChrome) evaluate(id json.RawMessage, session string, params json.RawMessage) error {
	var p struct {
		Expression string `json:"expression"`
	}
	_ = json.Unmarshal(params, &p)
	origin := f.pages[f.sessions[session]]
	call := func(fn string, args any) bool {
		if !strings.HasPrefix(p.Expression, fn+"(") {
			return false
		}
		_ = json.Unmarshal([]byte("["+strings.TrimSuffix(strings.TrimPrefix(p.Expression, fn+"("), ")")+"]"), args)
		return true
	}
	var asked string
	var items []browserStorageItem
	switch {
	case call(browserStorageRead, &[]any{&asked}):
		answer := map[string]any{"elsewhere": origin}
		if origin == asked {
			stored := f.storage[origin]
			if stored == nil {
				stored = []browserStorageItem{}
			}
			answer = map[string]any{"localStorage": stored}
		}
		page, err := json.Marshal(answer)
		if err != nil {
			return err
		}
		f.reply(id, map[string]any{"result": map[string]any{"type": "string", "value": string(page)}})
	case call(browserStorageWrite, &[]any{&asked, &items}):
		if origin != asked {
			f.reply(id, map[string]any{"result": map[string]any{"type": "string", "value": origin}})
			return nil
		}
		if f.refuse[origin] {
			f.reply(id, map[string]any{"result": map[string]any{"type": "object"}, "exceptionDetails": map[string]any{"text": "Uncaught", "exception": map[string]any{"description": "QuotaExceededError"}}})
			return nil
		}
		f.storage[origin] = append(f.storage[origin], items...)
		f.reply(id, map[string]any{"result": map[string]any{"type": "string", "value": ""}})
	default:
		f.deny(id, "unknown expression")
	}
	return nil
}

// leftBehind says what the kept sign-in left in the browser: tabs it
// opened, DevTools sessions still attached, the base tab's page when it is
// anything but about:blank, and the pages in the base tab's history, each of
// which Chrome's back/forward cache may keep alive. Nothing, for one that leaves the browser as
// agent-browser had it.
func (f *fakeChrome) leftBehind() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var left []string
	for _, id := range f.created {
		left = append(left, "tab "+id)
	}
	for session := range f.sessions {
		left = append(left, "session "+session)
	}
	if page := f.pages[baseTarget]; page != "about:blank" {
		left = append(left, "base tab on "+page)
	}
	for _, page := range f.history[baseTarget] {
		left = append(left, "base tab history "+page)
	}
	return left
}

// sites is every origin a navigation went to.
func (f *fakeChrome) sites() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.visited)
}

func (f *fakeChrome) storageOf(origin string) []browserStorageItem {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.storage[origin])
}

func (f *fakeChrome) cookieNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	names := make([]string, 0, len(f.cookies))
	for _, c := range f.cookies {
		names = append(names, c["name"].(string))
	}
	return names
}
