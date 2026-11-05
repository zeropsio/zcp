package watch

import (
	"context"
	"net/http"
	"net/http/httptest"
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
}

func newStub() *stubConn { return &stubConn{msgs: make(chan []byte, 16), done: make(chan struct{})} }

func (s *stubConn) ReadMessage() (int, []byte, error) {
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
		Token:          "tok",
		ClientID:       "client",
		ProjectID:      "proj",
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
		Token:          "t",
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
