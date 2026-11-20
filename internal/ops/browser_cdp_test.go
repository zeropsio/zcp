package ops

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
)

// fakeChrome is Chrome on the DevTools protocol, in process: its cookie jar,
// each origin's local storage and its open tabs. A stub tab's navigation is
// answered only once the client fulfils the request Chrome paused for it, as
// Chrome holds it; a navigation to an origin in stall is never answered, and
// a storage write on one in refuse throws, and one in fail is answered with
// a navigation error. Storage.setCookies refuses a batch
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
	refuseCookie string
	tabs         []string
	pages        map[string]string
	paused       map[string]pausedNavigation
	visited      []string
	nextTarget   int
	nextRequest  int
}

type pausedNavigation struct {
	id      json.RawMessage
	session string
	origin  string
}

func newFakeChrome() *fakeChrome {
	return &fakeChrome{
		out:     make(chan []byte, 1024),
		closed:  make(chan struct{}),
		storage: map[string][]browserStorageItem{},
		stall:   map[string]bool{},
		refuse:  map[string]bool{},
		fail:    map[string]bool{},
		pages:   map[string]string{},
		paused:  map[string]pausedNavigation{},
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
	reply := func(result any) { f.send(map[string]any{"id": msg.ID, "result": result}) }
	refuse := func(message string) {
		f.send(map[string]any{"id": msg.ID, "error": map[string]any{"code": -32000, "message": message}})
	}
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
		f.nextTarget++
		id := "STUB" + string(rune('0'+f.nextTarget))
		f.tabs = append(f.tabs, id)
		reply(map[string]any{"targetId": id})
	case "Target.attachToTarget":
		var p struct {
			TargetID string `json:"targetId"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		reply(map[string]any{"sessionId": "S-" + p.TargetID})
	case "Target.closeTarget":
		var p struct {
			TargetID string `json:"targetId"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		f.tabs = slices.DeleteFunc(f.tabs, func(id string) bool { return id == p.TargetID })
		reply(map[string]any{"success": true})
	case "Page.navigate":
		var p struct {
			URL string `json:"url"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		u, _ := url.Parse(p.URL)
		origin := u.Scheme + "://" + u.Host
		f.visited = append(f.visited, origin)
		if f.stall[origin] {
			return nil
		}
		if f.fail[origin] {
			reply(map[string]any{"frameId": "F1", "errorText": "net::ERR_BLOCKED_BY_CLIENT"})
			return nil
		}
		f.nextRequest++
		rid := "R" + string(rune('0'+f.nextRequest))
		f.paused[rid] = pausedNavigation{id: msg.ID, session: msg.SessionID, origin: origin}
		f.send(map[string]any{"method": "Fetch.requestPaused", "sessionId": msg.SessionID, "params": map[string]any{"requestId": rid, "request": map[string]any{"url": p.URL}}})
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
		f.pages[nav.session] = nav.origin
		reply(map[string]any{})
		f.send(map[string]any{"id": nav.id, "result": map[string]any{"frameId": "F1", "loaderId": "L1"}})
	case "Runtime.evaluate":
		var p struct {
			Expression string `json:"expression"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		origin := f.pages[msg.SessionID]
		switch {
		case p.Expression == browserStorageRead:
			items := f.storage[origin]
			if items == nil {
				items = []browserStorageItem{}
			}
			page, err := json.Marshal(struct {
				Origin       string               `json:"origin"`
				LocalStorage []browserStorageItem `json:"localStorage"`
			}{origin, items})
			if err != nil {
				return err
			}
			reply(map[string]any{"result": map[string]any{"type": "string", "value": string(page)}})
		case strings.HasPrefix(p.Expression, browserStorageWrite+"("):
			if f.refuse[origin] {
				reply(map[string]any{"result": map[string]any{"type": "object"}, "exceptionDetails": map[string]any{"text": "Uncaught", "exception": map[string]any{"description": "QuotaExceededError"}}})
				return nil
			}
			var items []browserStorageItem
			_ = json.Unmarshal([]byte(strings.TrimSuffix(strings.TrimPrefix(p.Expression, browserStorageWrite+"("), ")")), &items)
			f.storage[origin] = append(f.storage[origin], items...)
			reply(map[string]any{"result": map[string]any{"type": "string", "value": origin}})
		default:
			refuse("unknown expression")
		}
	default:
		reply(map[string]any{})
	}
	return nil
}

// openTabs is every stub tab still open.
func (f *fakeChrome) openTabs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.tabs)
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
