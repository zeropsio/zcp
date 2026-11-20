package ops

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// The kept sign-in's own way into Chrome (docs/spec-mate.md §2.6c): zcp
// speaks Chrome's DevTools protocol to the warm browser's Chrome itself, on
// the browser target, never through agent-browser's state save or state load.
// Cookies are read and written browser-wide (Storage.getCookies,
// Storage.setCookies); local storage, which Chrome hands out only to a page of
// its origin, through one stub tab whose every request zcp answers itself with
// an empty page, so no site is ever loaded.

// browserStubCloseTimeout bounds closing the stub tab, which runs after the
// save's or restore's own bound may have run out.
const browserStubCloseTimeout = 2 * time.Second

// browserStubPage is the page every request of the stub tab is answered with.
var browserStubPage = base64.StdEncoding.EncodeToString([]byte("<!doctype html><title>zcp kept sign-in</title>"))

// browserCDPConn is one websocket to Chrome's DevTools protocol, whole JSON
// messages each way.
type browserCDPConn interface {
	ReadMessage() ([]byte, error)
	WriteMessage(ctx context.Context, data []byte) error
	Close() error
}

// browserCDPDial opens the websocket to url. Tests replace it with an
// in-process Chrome.
var browserCDPDial = dialBrowserCDP

// dialBrowserCDP opens a websocket to a DevTools URL on this machine; any
// other host is refused, since the cookies of the kept sign-in travel on it.
func dialBrowserCDP(ctx context.Context, raw string) (browserCDPConn, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("devtools url: %w", err)
	}
	if u.Scheme != "ws" || !browserLoopbackHost(u.Hostname()) {
		return nil, fmt.Errorf("devtools url %q is not a websocket on this machine", u.Redacted())
	}
	conn, resp, err := websocket.DefaultDialer.DialContext(ctx, raw, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("devtools dial: %w", err)
	}
	return gorillaCDPConn{conn}, nil
}

func browserLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

type gorillaCDPConn struct{ conn *websocket.Conn }

func (c gorillaCDPConn) ReadMessage() ([]byte, error) {
	_, data, err := c.conn.ReadMessage()
	return data, err
}

func (c gorillaCDPConn) WriteMessage(ctx context.Context, data []byte) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Time{}
	}
	if err := c.conn.SetWriteDeadline(deadline); err != nil {
		return fmt.Errorf("devtools write: %w", err)
	}
	return c.conn.WriteMessage(websocket.TextMessage, data)
}

func (c gorillaCDPConn) Close() error { return c.conn.Close() }

// browserCDPURL asks agent-browser for the DevTools URL of the Chrome it
// drives now. Its daemon is the only one that knows it: a Chrome it
// relaunched has a new port and a new profile folder.
func browserCDPURL(ctx context.Context, run browserStepExec) (string, error) {
	out, _, err := run(ctx, []string{"get", "cdp-url"})
	var answer struct {
		Success bool `json:"success"`
		Data    struct {
			CDPURL string `json:"cdpUrl"`
		} `json:"data"`
		Error *string `json:"error"`
	}
	if jsonErr := json.Unmarshal([]byte(strings.TrimSpace(out)), &answer); jsonErr != nil || !answer.Success || answer.Data.CDPURL == "" {
		reason := "no url in its answer"
		if answer.Error != nil {
			reason = *answer.Error
		}
		return "", fmt.Errorf("get cdp-url: %s: %w", reason, errors.Join(err, jsonErr))
	}
	return answer.Data.CDPURL, nil
}

// browserCDP is a DevTools session on Chrome's browser target. One reader
// hands each answer to its call and answers every paused request of a stub
// tab with browserStubPage.
type browserCDP struct {
	conn    browserCDPConn
	writeMu sync.Mutex

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan browserCDPMessage
	readErr error
	done    chan struct{}
}

type browserCDPMessage struct {
	ID        int64            `json:"id,omitempty"`
	Method    string           `json:"method,omitempty"`
	Params    json.RawMessage  `json:"params,omitempty"`
	SessionID string           `json:"sessionId,omitempty"`
	Result    json.RawMessage  `json:"result,omitempty"`
	Error     *browserCDPError `json:"error,omitempty"`
}

// browserCDPError is Chrome's own refusal of a command, as against a
// connection that failed or a bound that ran out.
type browserCDPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *browserCDPError) Error() string { return e.Message }

// connectBrowserCDP opens a DevTools session on the Chrome agent-browser
// drives now.
func connectBrowserCDP(ctx context.Context, run browserStepExec) (*browserCDP, error) {
	raw, err := browserCDPURL(ctx, run)
	if err != nil {
		return nil, err
	}
	conn, err := browserCDPDial(ctx, raw)
	if err != nil {
		return nil, err
	}
	c := &browserCDP{conn: conn, pending: map[int64]chan browserCDPMessage{}, done: make(chan struct{})}
	// The reader lives as long as the connection, not as ctx.
	go c.read(context.WithoutCancel(ctx))
	return c, nil
}

// close ends the session and waits for its reader.
func (c *browserCDP) close() {
	_ = c.conn.Close()
	<-c.done
}

func (c *browserCDP) read(ctx context.Context) {
	defer close(c.done)
	for {
		data, err := c.conn.ReadMessage()
		if err != nil {
			c.mu.Lock()
			c.readErr = err
			for id, ch := range c.pending {
				close(ch)
				delete(c.pending, id)
			}
			c.mu.Unlock()
			return
		}
		var msg browserCDPMessage
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		if msg.ID != 0 {
			c.mu.Lock()
			ch, ok := c.pending[msg.ID]
			delete(c.pending, msg.ID)
			c.mu.Unlock()
			if ok {
				ch <- msg
			}
			continue
		}
		if msg.Method == "Fetch.requestPaused" && msg.SessionID != "" {
			var paused struct {
				RequestID string `json:"requestId"`
			}
			if json.Unmarshal(msg.Params, &paused) == nil {
				// No answer is awaited: the navigation it holds up is.
				go c.post(ctx, msg.SessionID, "Fetch.fulfillRequest", map[string]any{
					"requestId":       paused.RequestID,
					"responseCode":    200,
					"responseHeaders": []map[string]string{{"name": "content-type", "value": "text/html"}},
					"body":            browserStubPage,
				})
			}
		}
	}
}

// post sends a command whose answer nobody waits for.
func (c *browserCDP) post(ctx context.Context, sessionID, method string, params any) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	c.mu.Unlock()
	data, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params, "sessionId": sessionID})
	if err != nil {
		return
	}
	wctx, cancel := context.WithTimeout(ctx, browserStubCloseTimeout)
	defer cancel()
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.conn.WriteMessage(wctx, data)
}

// call sends method to the browser target (sessionID "") or to a session and
// decodes its answer into result (nil drops it), bounded by ctx.
func (c *browserCDP) call(ctx context.Context, sessionID, method string, params, result any) error {
	if params == nil {
		params = struct{}{}
	}
	ch := make(chan browserCDPMessage, 1)
	c.mu.Lock()
	if c.readErr != nil {
		err := c.readErr
		c.mu.Unlock()
		return fmt.Errorf("%s: %w", method, err)
	}
	c.nextID++
	id := c.nextID
	c.pending[id] = ch
	c.mu.Unlock()
	forget := func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}
	msg := map[string]any{"id": id, "method": method, "params": params}
	if sessionID != "" {
		msg["sessionId"] = sessionID
	}
	data, err := json.Marshal(msg)
	if err != nil {
		forget()
		return fmt.Errorf("%s: %w", method, err)
	}
	c.writeMu.Lock()
	err = c.conn.WriteMessage(ctx, data)
	c.writeMu.Unlock()
	if err != nil {
		forget()
		return fmt.Errorf("%s: %w", method, err)
	}
	select {
	case <-ctx.Done():
		forget()
		return fmt.Errorf("%s: %w", method, ctx.Err())
	case answer, ok := <-ch:
		if !ok {
			return fmt.Errorf("%s: the devtools connection closed", method)
		}
		if answer.Error != nil {
			return fmt.Errorf("%s: %w", method, answer.Error)
		}
		if result == nil {
			return nil
		}
		if err := json.Unmarshal(answer.Result, result); err != nil {
			return fmt.Errorf("%s: %w", method, err)
		}
		return nil
	}
}

// browserCookie is a kept cookie: the fields of Chrome's own cookie
// agent-browser's state keeps, and its partition (CHIPS), which a cookie set
// back without it would lose.
type browserCookie struct {
	Name         string          `json:"name"`
	Value        string          `json:"value"`
	Domain       string          `json:"domain"`
	Path         string          `json:"path"`
	Expires      float64         `json:"expires"`
	Size         int64           `json:"size"`
	HTTPOnly     bool            `json:"httpOnly"`
	Secure       bool            `json:"secure"`
	Session      bool            `json:"session"`
	SameSite     string          `json:"sameSite,omitempty"`
	PartitionKey json.RawMessage `json:"partitionKey,omitempty"`
}

// cookies reads every cookie of the browser's default context.
func (c *browserCDP) cookies(ctx context.Context) ([]browserCookie, error) {
	var answer struct {
		Cookies []browserCookie `json:"cookies"`
	}
	if err := c.call(ctx, "", "Storage.getCookies", nil, &answer); err != nil {
		return nil, err
	}
	if answer.Cookies == nil {
		answer.Cookies = []browserCookie{}
	}
	return answer.Cookies, nil
}

// cookieParam is the cookie as Storage.setCookies takes it: a session
// cookie, or one with no expiry, is set without one.
func (k browserCookie) cookieParam() map[string]any {
	p := map[string]any{"name": k.Name, "value": k.Value, "domain": k.Domain, "path": k.Path, "secure": k.Secure, "httpOnly": k.HTTPOnly}
	if k.SameSite != "" {
		p["sameSite"] = k.SameSite
	}
	if !k.Session && k.Expires > 0 {
		p["expires"] = k.Expires
	}
	if len(k.PartitionKey) > 0 && string(k.PartitionKey) != "null" {
		p["partitionKey"] = k.PartitionKey
	}
	return p
}

// setCookies sets cookies into the browser's default context, all in one
// command; when Chrome refuses that, one by one, so a cookie it refuses costs
// only itself. refused counts those; err is a connection that failed or ctx's
// end.
func (c *browserCDP) setCookies(ctx context.Context, cookies []browserCookie) (refused int, err error) {
	if len(cookies) == 0 {
		return 0, nil
	}
	params := make([]map[string]any, 0, len(cookies))
	for _, k := range cookies {
		params = append(params, k.cookieParam())
	}
	err = c.call(ctx, "", "Storage.setCookies", map[string]any{"cookies": params}, nil)
	var refusal *browserCDPError
	if !errors.As(err, &refusal) {
		return 0, err
	}
	for _, p := range params {
		err := c.call(ctx, "", "Storage.setCookies", map[string]any{"cookies": []map[string]any{p}}, nil)
		if errors.As(err, &refusal) {
			refused++
			continue
		}
		if err != nil {
			return refused, err
		}
	}
	return refused, nil
}

// browserStub is a tab zcp opened on the browser for the kept sign-in: every
// request of it is answered with browserStubPage, never by a site, and no
// service worker answers it.
type browserStub struct {
	cdp             *browserCDP
	target, session string
}

// withStub runs use on a stub tab it opens, and closes the tab whatever
// use did, its bound run out included: a stub tab left open would sit beside
// the call's tabs.
func (c *browserCDP) withStub(ctx context.Context, use func(*browserStub) error) error {
	stub, err := c.openStub(ctx)
	if err != nil {
		return fmt.Errorf("open the stub tab: %w", err)
	}
	defer stub.close(ctx)
	return use(stub)
}

// openStub opens the stub tab; withStub closes it.
func (c *browserCDP) openStub(ctx context.Context) (*browserStub, error) {
	var created struct {
		TargetID string `json:"targetId"`
	}
	if err := c.call(ctx, "", "Target.createTarget", map[string]any{"url": "about:blank"}, &created); err != nil {
		return nil, err
	}
	s := &browserStub{cdp: c, target: created.TargetID}
	var attached struct {
		SessionID string `json:"sessionId"`
	}
	if err := c.call(ctx, "", "Target.attachToTarget", map[string]any{"targetId": s.target, "flatten": true}, &attached); err != nil {
		s.close(ctx)
		return nil, err
	}
	s.session = attached.SessionID
	for _, step := range []struct {
		method string
		params any
	}{
		// Chrome takes the bypass only once the Network domain is on.
		{"Network.enable", nil},
		{"Network.setBypassServiceWorker", map[string]any{"bypass": true}},
		{"Fetch.enable", map[string]any{"patterns": []map[string]string{{"urlPattern": "*", "requestStage": "Request"}}}},
	} {
		if err := c.call(ctx, s.session, step.method, step.params, nil); err != nil {
			s.close(ctx)
			return nil, err
		}
	}
	return s, nil
}

// close closes the stub tab under its own bound, ctx's end aside.
func (s *browserStub) close(ctx context.Context) {
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), browserStubCloseTimeout)
	defer cancel()
	if err := s.cdp.call(cctx, "", "Target.closeTarget", map[string]any{"targetId": s.target}, nil); err != nil {
		logBrowserSignIn(fmt.Errorf("close the stub tab: %w", err))
	}
}

// visit puts the stub tab on origin's root.
func (s *browserStub) visit(ctx context.Context, origin string) error {
	var nav struct {
		ErrorText string `json:"errorText"`
	}
	if err := s.cdp.call(ctx, s.session, "Page.navigate", map[string]any{"url": origin + "/"}, &nav); err != nil {
		return err
	}
	if nav.ErrorText != "" {
		return fmt.Errorf("open the stub of %s: %s", origin, nav.ErrorText)
	}
	return nil
}

// eval evaluates expression in the stub's page and answers its string value.
func (s *browserStub) eval(ctx context.Context, expression string) (string, error) {
	var answer struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text      string `json:"text"`
			Exception struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	if err := s.cdp.call(ctx, s.session, "Runtime.evaluate", map[string]any{"expression": expression, "returnByValue": true}, &answer); err != nil {
		return "", err
	}
	if d := answer.ExceptionDetails; d != nil {
		reason := d.Exception.Description
		if reason == "" {
			reason = d.Text
		}
		return "", fmt.Errorf("the stub page threw: %s", reason)
	}
	var value string
	if err := json.Unmarshal(answer.Result.Value, &value); err != nil {
		return "", fmt.Errorf("the stub page answered no string: %w", err)
	}
	return value, nil
}

// browserStorageItem is one local storage entry.
type browserStorageItem struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// browserStorageRead answers the stub page's origin and its local storage.
const browserStorageRead = `JSON.stringify((() => { const items = []; ` +
	`for (let i = 0; i < localStorage.length; i++) { const name = localStorage.key(i); items.push({name, value: localStorage.getItem(name)}); } ` +
	`return {origin: location.origin, localStorage: items}; })())`

// browserStorageWrite, called with the items, sets them into the stub page's
// local storage and answers its origin.
const browserStorageWrite = `((items) => { for (const item of items) localStorage.setItem(item.name, item.value); return location.origin; })`

// readStorage answers origin's local storage, read on the stub tab.
func (s *browserStub) readStorage(ctx context.Context, origin string) ([]browserStorageItem, error) {
	if err := s.visit(ctx, origin); err != nil {
		return nil, err
	}
	raw, err := s.eval(ctx, browserStorageRead)
	if err != nil {
		return nil, err
	}
	var page struct {
		Origin       string               `json:"origin"`
		LocalStorage []browserStorageItem `json:"localStorage"`
	}
	if err := json.Unmarshal([]byte(raw), &page); err != nil {
		return nil, fmt.Errorf("read the local storage of %s: %w", origin, err)
	}
	if page.Origin != origin {
		return nil, fmt.Errorf("read the local storage of %s: the stub page is on %s", origin, page.Origin)
	}
	return page.LocalStorage, nil
}

// writeStorage sets items into origin's local storage on the stub tab.
func (s *browserStub) writeStorage(ctx context.Context, origin string, items []browserStorageItem) error {
	if err := s.visit(ctx, origin); err != nil {
		return err
	}
	// json.Marshal escapes U+2028 and U+2029, so its output is a JavaScript
	// literal as it stands.
	literal, err := json.Marshal(items)
	if err != nil {
		return fmt.Errorf("write the local storage of %s: %w", origin, err)
	}
	got, err := s.eval(ctx, browserStorageWrite+"("+string(literal)+")")
	if err != nil {
		return err
	}
	if got != origin {
		return fmt.Errorf("write the local storage of %s: the stub page is on %s", origin, got)
	}
	return nil
}
