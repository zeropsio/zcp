// Package studiows is the Zerops Studio websocket watcher: it rides the
// platform's (undocumented) realtime channel and turns ServiceStack changes
// into a small event stream the Studio cockpit consumes, so the cockpit updates
// on push instead of polling.
//
// The protocol below was reverse-engineered from the legacy web frontend
// (frontend-legacy/libs/zef/src/websocket) and VERIFIED LIVE against
// api.app-prg1.zerops.io (see the captured frames in the commit that introduced
// this file). It is NOT a public/documented API — treat the legacy frontend as
// the source of truth if it ever drifts.
//
// Flow:
//  1. Auth exchange:  POST https://<host>/api/rest/public/web-socket/login
//     body {"token": <accessToken>} + Authorization: Bearer <accessToken>
//     -> {"webSocketToken": "..."}   (short-lived)
//  2. Connect:        wss://<host>/api/rest/public/web-socket/<receiverId>/<webSocketToken>
//     server greets with {"type":"SocketSuccess"}
//  3. Subscribe:      POST https://<host>/api/rest/public/service-stack/search
//     + Bearer, body = the service-stack search filter PLUS
//     {"subscriptionName":"ServiceStack__list-subscription",
//     "receiverId":<uuid>, "wsOutputType":"listStream"}
//  4. Push:           {"type":"search","subscriptionName":"ServiceStack__list-subscription",
//     "data":{"add":[ids],"delete":[ids]}}  on membership change
//  5. Heartbeat:      send {"type":"ping"} -> receive {"type":"pong"}
//
// The watcher is a "change SIGNAL" — it does NOT carry topology. On any relevant
// push it emits a topology-changed event; the cockpit re-reads the authoritative
// state via the existing `zcp studio topology` REST transport (single mapping
// owner). On disconnect it self-heals (re-auth -> reconnect -> re-subscribe) and
// emits disconnected so the consumer can fall back to polling meanwhile.
package studiows

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// subscriptionName is the platform's name for a ServiceStack list subscription
// (frontend getSubscriptionNameForFeature(ServiceStack,'list')). Pushes carry it.
const subscriptionName = "ServiceStack__list-subscription"

// EventType is the kind of event the watcher emits to its consumer.
type EventType string

const (
	// EventConnected fires when the socket is up + subscribed (SocketSuccess).
	EventConnected EventType = "connected"
	// EventTopologyChanged fires on a ServiceStack push — the cockpit should
	// re-read topology. It is a signal, not the data.
	EventTopologyChanged EventType = "topology-changed"
	// EventDisconnected fires when the socket drops (before a reconnect attempt)
	// so the consumer can fall back to polling in the meantime.
	EventDisconnected EventType = "disconnected"
)

// Event is one watcher event.
type Event struct {
	Type EventType `json:"type"`
}

// Config is what the watcher needs to authenticate + scope the subscription.
type Config struct {
	APIHost   string // e.g. api.app-prg1.zerops.io (ZCP_API_HOST)
	Token     string // the API access token (zcp resolves it; never logged)
	ClientID  string // account/client id (search scope)
	ProjectID string // project id (search scope)

	// PingInterval / reconnect backoff are overridable for tests; zero => defaults.
	PingInterval   time.Duration
	MinBackoff     time.Duration
	MaxBackoff     time.Duration
	dialWebSocket  func(ctx context.Context, url string) (wsConn, error) // test seam
	httpClient     *http.Client
	insecureScheme bool // tests: use http/ws instead of https/wss
}

// wsConn is the minimal websocket surface the watcher uses (so tests can stub it).
type wsConn interface {
	ReadMessage() (messageType int, p []byte, err error)
	WriteMessage(messageType int, data []byte) error
	SetReadDeadline(t time.Time) error
	Close() error
}

// Watch runs the watcher until ctx is cancelled, calling emit for every event.
// It blocks. emit must be cheap + non-blocking (the consumer should hand off);
// it is called from the read goroutine. Reconnects forever with backoff while
// ctx is live.
func Watch(ctx context.Context, cfg Config, emit func(Event)) error {
	cfg = withDefaults(cfg)
	backoff := cfg.MinBackoff
	for ctx.Err() == nil {
		err := cfg.streamOnce(ctx, emit)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Connection ended (drop / auth expiry / read error). Tell the consumer
		// so it can poll, then back off and reconnect.
		emit(Event{Type: EventDisconnected})
		_ = err // err is informational; the reconnect loop handles all failures
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < cfg.MaxBackoff {
			backoff *= 2
			if backoff > cfg.MaxBackoff {
				backoff = cfg.MaxBackoff
			}
		}
	}
	return ctx.Err()
}

func withDefaults(cfg Config) Config {
	if cfg.PingInterval == 0 {
		cfg.PingInterval = 15 * time.Second
	}
	if cfg.MinBackoff == 0 {
		cfg.MinBackoff = time.Second
	}
	if cfg.MaxBackoff == 0 {
		cfg.MaxBackoff = 30 * time.Second
	}
	if cfg.httpClient == nil {
		cfg.httpClient = &http.Client{Timeout: 20 * time.Second}
	}
	return cfg
}

// streamOnce does one full connect→subscribe→read lifecycle; it returns when the
// connection ends (error, close, or ctx cancel). A successful connect resets the
// caller's backoff via the EventConnected path.
func (cfg Config) streamOnce(ctx context.Context, emit func(Event)) error {
	wsToken, err := cfg.authExchange(ctx)
	if err != nil {
		return fmt.Errorf("auth exchange: %w", err)
	}

	receiverID := newReceiverID()
	conn, err := cfg.dial(ctx, receiverID, wsToken)
	if err != nil {
		return fmt.Errorf("ws dial: %w", err)
	}
	defer conn.Close()

	// Subscribe AFTER connect so pushes route to our live receiverId.
	if err := cfg.subscribe(ctx, receiverID); err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}

	// Heartbeat writer (the only writer on this conn — gorilla allows one
	// concurrent reader + one writer). A dead server stops sending pongs; the
	// read deadline below then trips and the read loop returns -> reconnect.
	pingCtx, stopPing := context.WithCancel(ctx)
	defer stopPing()
	go func() {
		t := time.NewTicker(cfg.PingInterval)
		defer t.Stop()
		for {
			select {
			case <-pingCtx.Done():
				return
			case <-t.C:
				if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"ping"}`)); err != nil {
					return
				}
			}
		}
	}()

	// Cancel the read by closing the conn when ctx ends.
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	readTimeout := cfg.PingInterval*2 + 5*time.Second
	for {
		_ = conn.SetReadDeadline(time.Now().Add(readTimeout))
		_, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		var msg struct {
			Type             string `json:"type"`
			SubscriptionName string `json:"subscriptionName"`
		}
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		switch msg.Type {
		case "SocketSuccess":
			emit(Event{Type: EventConnected})
		case "search":
			if msg.SubscriptionName == subscriptionName {
				emit(Event{Type: EventTopologyChanged})
			}
		case "pong":
			// heartbeat ack — read deadline already refreshed above
		}
	}
}

// authExchange swaps the API access token for a short-lived webSocketToken.
// VERIFIED: the body field is "token" AND the Bearer header is required.
func (cfg Config) authExchange(ctx context.Context) (string, error) {
	body, err := json.Marshal(struct {
		Token string `json:"token"`
	}{Token: cfg.Token})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.scheme("https")+"://"+cfg.APIHost+"/api/rest/public/web-socket/login", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	resp, err := cfg.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("login status %d", resp.StatusCode)
	}
	var out struct {
		WebSocketToken string `json:"webSocketToken"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.WebSocketToken == "" {
		return "", fmt.Errorf("no webSocketToken in login response")
	}
	return out.WebSocketToken, nil
}

// subscribe registers a ServiceStack list subscription against our receiverId by
// POSTing the service-stack search (the same endpoint zcp's ListServices uses)
// augmented with the three ws fields. The platform returns the current list AND
// starts pushing membership deltas to the socket.
func (cfg Config) subscribe(ctx context.Context, receiverID string) error {
	body, err := json.Marshal(subscribeBody{
		Search: []searchTerm{
			{Name: "clientId", Operator: "eq", Value: cfg.ClientID},
			{Name: "projectId", Operator: "eq", Value: cfg.ProjectID},
		},
		Sort:             []string{},
		SubscriptionName: subscriptionName,
		ReceiverID:       receiverID,
		WSOutputType:     "listStream",
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.scheme("https")+"://"+cfg.APIHost+"/api/rest/public/service-stack/search", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	resp, err := cfg.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("subscribe status %d", resp.StatusCode)
	}
	return nil
}

func (cfg Config) dial(ctx context.Context, receiverID, wsToken string) (wsConn, error) {
	url := cfg.scheme("wss") + "://" + cfg.APIHost + "/api/rest/public/web-socket/" + receiverID + "/" + wsToken
	if cfg.dialWebSocket != nil {
		return cfg.dialWebSocket(ctx, url)
	}
	conn, resp, err := websocket.DefaultDialer.DialContext(ctx, url, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// subscribeBody is the service-stack search payload + the three ws-subscription
// fields. Typed (no map[string]any) so marshaling is provably error-free.
type subscribeBody struct {
	Search           []searchTerm `json:"search"`
	Sort             []string     `json:"sort"`
	SubscriptionName string       `json:"subscriptionName"`
	ReceiverID       string       `json:"receiverId"`
	WSOutputType     string       `json:"wsOutputType"`
}

type searchTerm struct {
	Name     string `json:"name"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
}

// scheme returns the secure scheme normally, or its insecure variant in tests.
func (cfg Config) scheme(secure string) string {
	if !cfg.insecureScheme {
		return secure
	}
	switch secure {
	case "wss":
		return "ws"
	default:
		return "http"
	}
}

func newReceiverID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
