package watch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// stubConn is a scripted websocket connection for tests.
type stubConn struct {
	msgs   chan []byte
	mu     sync.Mutex
	closed bool
	done   chan struct{}
	// reading, when set, receives at each read: every frame before it has
	// been handled.
	reading chan struct{}
}

func newStub() *stubConn { return &stubConn{msgs: make(chan []byte, 16), done: make(chan struct{})} }

func (s *stubConn) ReadMessage() (int, []byte, error) {
	if s.reading != nil {
		select {
		case s.reading <- struct{}{}:
		case <-s.done:
		}
	}
	select {
	case m := <-s.msgs:
		return 1, m, nil
	case <-s.done:
		return 0, nil, http.ErrServerClosed
	}
}
func (s *stubConn) WriteMessage(int, []byte) error  { return nil }
func (s *stubConn) SetReadDeadline(time.Time) error { return nil }
func (s *stubConn) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.done)
	}
	return nil
}

// TestWatch_EmitsConnectedAndTopologyChanged drives the full lifecycle against a
// stub socket + httptest API: SocketSuccess -> connected; a ServiceStack list
// push -> topology-changed; a foreign subscription + a pong -> ignored. Also pins
// that auth-exchange + subscribe HTTP are actually issued.
func TestWatch_EmitsConnectedAndTopologyChanged(t *testing.T) {
	var hitLogin, hitSearch bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/rest/public/web-socket/login":
			hitLogin = true
			_, _ = w.Write([]byte(`{"webSocketToken":"wstok"}`))
		case "/api/rest/public/service-stack/search":
			hitSearch = true
			_, _ = w.Write([]byte(`{"items":[],"totalHits":0}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	stub := newStub()
	stub.msgs <- []byte(`{"type":"SocketSuccess","data":{"Success":true}}`)
	stub.msgs <- []byte(`{"type":"search","subscriptionName":"ServiceStack__list-subscription","data":{"add":["x"],"delete":[]}}`)
	stub.msgs <- []byte(`{"type":"search","subscriptionName":"OtherEntity__list-subscription"}`)
	stub.msgs <- []byte(`{"type":"pong"}`)

	cfg := Config{
		APIHost:        host,
		Token:          func() string { return "tok" },
		ClientID:       "client",
		Streams:        []Stream{ServiceList},
		insecureScheme: true,
		httpClient:     srv.Client(),
		PingInterval:   20 * time.Millisecond,
		dialWebSocket:  func(_ context.Context, _ string) (wsConn, error) { return stub, nil },
	}

	evCh := make(chan EventType, 16)
	ctx := t.Context()
	go func() { _ = Watch(ctx, cfg, func(e Event) { evCh <- e.Type }) }()

	want := []EventType{EventConnected, EventTopologyChanged}
	for i, w := range want {
		select {
		case got := <-evCh:
			if got != w {
				t.Fatalf("event %d = %q, want %q", i, got, w)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for event %d (%q)", i, w)
		}
	}

	// The foreign subscription + pong must NOT have produced an event: the next
	// thing on the channel should be nothing for a beat.
	select {
	case extra := <-evCh:
		t.Fatalf("unexpected extra event %q (foreign subscription / pong must be ignored)", extra)
	case <-time.After(150 * time.Millisecond):
	}

	if !hitLogin {
		t.Error("auth-exchange (login) was not called")
	}
	if !hitSearch {
		t.Error("subscribe (service-stack search) was not called")
	}
}

// TestWatch_ReconnectsAfterDrop pins that a dropped socket yields a disconnected
// event and a reconnect (a second connect).
func TestWatch_ReconnectsAfterDrop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/login") {
			_, _ = w.Write([]byte(`{"webSocketToken":"wstok"}`))
			return
		}
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	var dials int
	var mu sync.Mutex
	cfg := Config{
		APIHost:        host,
		Token:          func() string { return "t" },
		Streams:        []Stream{ServiceList},
		insecureScheme: true,
		httpClient:     srv.Client(),
		PingInterval:   20 * time.Millisecond,
		MinBackoff:     10 * time.Millisecond,
		MaxBackoff:     10 * time.Millisecond,
		dialWebSocket: func(_ context.Context, _ string) (wsConn, error) {
			mu.Lock()
			dials++
			mu.Unlock()
			s := newStub()
			s.msgs <- []byte(`{"type":"SocketSuccess"}`)
			// drop almost immediately
			go func() { time.Sleep(15 * time.Millisecond); s.Close() }()
			return s, nil
		},
	}

	var sawDisconnect bool
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	_ = Watch(ctx, cfg, func(e Event) {
		if e.Type == EventDisconnected {
			sawDisconnect = true
		}
	})

	if !sawDisconnect {
		t.Error("expected a disconnected event after the socket dropped")
	}
	mu.Lock()
	d := dials
	mu.Unlock()
	if d < 2 {
		t.Errorf("expected at least 2 dials (reconnect), got %d", d)
	}
}

// projectAPI answers the auth exchange and every subscribe, recording each
// subscribe's path and body.
func projectAPI(t *testing.T) (host string, client *http.Client, subscribed func() []recordedSubscribe) {
	t.Helper()
	var (
		mu  sync.Mutex
		got []recordedSubscribe
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/web-socket/login") {
			_, _ = w.Write([]byte(`{"webSocketToken":"wstok"}`))
			return
		}
		var body recordedSubscribe
		_ = json.NewDecoder(r.Body).Decode(&body)
		body.Path = r.URL.Path
		mu.Lock()
		got = append(got, body)
		mu.Unlock()
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://"), srv.Client(), func() []recordedSubscribe {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(got)
	}
}

type recordedSubscribe struct {
	Path   string `json:"-"`
	Search []struct {
		Name     string `json:"name"`
		Operator string `json:"operator"`
		Value    any    `json:"value"`
	} `json:"search"`
	SubscriptionName string `json:"subscriptionName"`
	WSOutputType     string `json:"wsOutputType"`
	DisableOutput    bool   `json:"disableOutput"`
}

// TestWatch_ProjectStreams_SubscribedByTheOrgOnlyAndPushed: each of the
// project's streams is subscribed by the org alone — a projectId filter
// answers 400 for every key (ZEROPS-079) — and its push emits its own event:
// a service's list or row (ZEROPS-079), a process (ZEROPS-078).
func TestWatch_ProjectStreams_SubscribedByTheOrgOnlyAndPushed(t *testing.T) {
	t.Parallel()
	host, client, subscribed := projectAPI(t)
	stub := newStub()
	stub.msgs <- []byte(`{"type":"SocketSuccess"}`)
	stub.msgs <- []byte(`{"type":"search","subscriptionName":"ServiceStack__update-subscription","data":{"update":[{"status":"ACTIVE"}]}}`)
	stub.msgs <- []byte(`{"type":"search","subscriptionName":"Process__update-subscription","data":{"update":[{"status":"FINISHED"}]}}`)
	stub.msgs <- []byte(`{"type":"search","subscriptionName":"ServiceStack__list-subscription","data":{"add":["s1"]}}`)
	cfg := Config{
		APIHost: host, Token: func() string { return "tok" }, ClientID: "org", Streams: ProjectStreams(),
		insecureScheme: true, httpClient: client, PingInterval: time.Hour,
		dialWebSocket: func(context.Context, string) (wsConn, error) { return stub, nil },
	}
	events := make(chan EventType, 8)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = Watch(ctx, cfg, func(e Event) { events <- e.Type }) }()
	for _, want := range []EventType{EventConnected, EventServiceChanged, EventProcessChanged, EventTopologyChanged} {
		if got := <-events; got != want {
			t.Fatalf("event = %q, want %q", got, want)
		}
	}

	want := map[string]string{
		"ServiceStack__list-subscription":   "/api/rest/public/service-stack/search listStream",
		"ServiceStack__update-subscription": "/api/rest/public/service-stack/search updateStream",
		"Process__update-subscription":      "/api/rest/public/process/search updateStream",
	}
	subs := subscribed()
	if len(subs) != len(want) {
		t.Fatalf("subscribed %d streams, want %d: %+v", len(subs), len(want), subs)
	}
	for _, sub := range subs {
		if got := sub.Path + " " + sub.WSOutputType; got != want[sub.SubscriptionName] {
			t.Errorf("%s subscribed as %q, want %q", sub.SubscriptionName, got, want[sub.SubscriptionName])
		}
		if !sub.DisableOutput {
			t.Errorf("%s asks for the search's output", sub.SubscriptionName)
		}
		if len(sub.Search) == 0 || sub.Search[0].Name != "clientId" || sub.Search[0].Operator != "eq" || sub.Search[0].Value != "org" {
			t.Errorf("%s is not filtered by the org first: %+v", sub.SubscriptionName, sub.Search)
		}
		for _, term := range sub.Search {
			if term.Name == "projectId" {
				t.Errorf("%s filters by projectId, which answers 400: %+v", sub.SubscriptionName, sub.Search)
			}
		}
	}
}

// TestWakes_OneWakeForEveryChangeSinceTheLastRead: the connection wakes the
// reader, and pushes that arrive while it is away leave one wake, not one
// each; a push after it read wakes it again.
func TestWakes_OneWakeForEveryChangeSinceTheLastRead(t *testing.T) {
	t.Parallel()
	host, client, _ := projectAPI(t)
	stub := newStub()
	stub.reading = make(chan struct{})
	cfg := Config{
		APIHost: host, Token: func() string { return "tok" }, ClientID: "org", Streams: ProjectStreams(),
		insecureScheme: true, httpClient: client, PingInterval: time.Hour,
		dialWebSocket: func(context.Context, string) (wsConn, error) { return stub, nil },
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	wakes := Wakes(ctx, cfg)
	frames := func(msgs ...string) {
		for _, m := range msgs {
			<-stub.reading
			stub.msgs <- []byte(m)
		}
		<-stub.reading // every frame before this read was handled
	}
	woken := func() int {
		n := 0
		for {
			select {
			case <-wakes:
				n++
			default:
				return n
			}
		}
	}

	frames(`{"type":"SocketSuccess"}`,
		`{"type":"search","subscriptionName":"Process__update-subscription","data":{}}`,
		`{"type":"search","subscriptionName":"ServiceStack__update-subscription","data":{}}`,
		`{"type":"pong"}`)
	if n := woken(); n != 1 {
		t.Fatalf("the connection and two pushes left %d wakes, want one", n)
	}
	stub.msgs <- []byte(`{"type":"search","subscriptionName":"ServiceStack__list-subscription","data":{}}`)
	<-stub.reading
	if n := woken(); n != 1 {
		t.Fatalf("a push after the read left %d wakes, want one", n)
	}
	stub.msgs <- []byte(`{"type":"pong"}`)
	<-stub.reading
	if n := woken(); n != 0 {
		t.Fatalf("a pong left %d wakes, want none", n)
	}
}
