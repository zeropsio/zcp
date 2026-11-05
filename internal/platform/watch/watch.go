// Package watch is the Zerops websocket watcher: it rides the platform's
// (undocumented) realtime channel and turns changes of the project's services
// and processes into a small event stream — the Studio cockpit's topology
// signal and the Mate's waits — so they follow on push instead of polling.
//
// The protocol below was reverse-engineered from the legacy web frontend
// (frontend-legacy/libs/zef/src/websocket) and VERIFIED LIVE against
// api.app-prg1.zerops.io, under a Mate's project-scoped key as well
// (ZEROPS-078, ZEROPS-079). It is NOT a public/documented API — treat the
// legacy frontend as the source of truth if it ever drifts.
//
// Flow:
//  1. Auth exchange:  POST https://<host>/api/rest/public/web-socket/login
//     body {"token": <accessToken>} + Authorization: Bearer <accessToken>
//     -> {"webSocketToken": "..."}   (short-lived)
//  2. Connect:        wss://<host>/api/rest/public/web-socket/<receiverId>/<webSocketToken>
//     server greets with {"type":"SocketSuccess"}
//  3. Subscribe:      per stream, POST https://<host>/api/rest/public/<entity>/search
//     + Bearer, body = the search filtered by the org (clientId; a projectId
//     filter answers 400 for every key, ZEROPS-079 — the key's own scope keeps
//     the stream to its project) PLUS {"subscriptionName":<stream>,
//     "receiverId":<uuid>, "wsOutputType":"listStream"|"updateStream",
//     "disableOutput":true}
//  4. Push:           {"type":"search","subscriptionName":<stream>,
//     "data":{"add":[...],"update":[...],"delete":[...]}}  on a change
//  5. Heartbeat:      send {"type":"ping"} -> receive {"type":"pong"}
//
// The watcher is a "change SIGNAL" — it does NOT carry state. On a push it
// emits its stream's event; the consumer re-reads the authoritative state over
// REST. On disconnect it self-heals (re-auth -> reconnect -> re-subscribe) and
// emits disconnected; the next connected event tells the consumer to read
// again for whatever changed in between.
package watch

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

// Stream is one subscription a watcher holds, by the name its pushes carry
// (frontend getSubscriptionNameForFeature).
type Stream string

const (
	// ServiceList is the project's services coming and going (ZEROPS-079).
	ServiceList Stream = "ServiceStack__list-subscription"
	// ServiceUpdate is a change of a service's row, its status among them
	// (ZEROPS-079).
	ServiceUpdate Stream = "ServiceStack__update-subscription"
	// ProcessUpdate is a process changing state (ZEROPS-078).
	ProcessUpdate Stream = "Process__update-subscription"
)

// ProjectStreams are the streams a wait on the project's services and their
// processes holds.
func ProjectStreams() []Stream { return []Stream{ServiceList, ServiceUpdate, ProcessUpdate} }

// subscription is how stream is subscribed: its search entity, output mode,
// the terms beside the org's, and the event its push emits.
func (s Stream) subscription() (entity, mode string, terms []searchTerm, event EventType) {
	switch s {
	case ServiceUpdate:
		return "service-stack", "updateStream", nil, EventServiceChanged
	case ProcessUpdate:
		// The frontend's process filter, with the failed and cancelled ends
		// added so a process's every end is pushed.
		return "process", "updateStream", []searchTerm{
			{Name: "status", Operator: "in", Value: []string{"PENDING", "RUNNING", "FINISHED", "FAILED", "CANCELLED"}},
			{Name: "executorTag", Operator: "ne", Value: "L7_MASTER"},
		}, EventProcessChanged
	case ServiceList:
	}
	return "service-stack", "listStream", nil, EventTopologyChanged
}

// EventType is the kind of event the watcher emits to its consumer.
type EventType string

const (
	// EventConnected fires when the socket is up + subscribed (SocketSuccess).
	EventConnected EventType = "connected"
	// EventTopologyChanged fires on a ServiceList push — the cockpit should
	// re-read topology. It is a signal, not the data.
	EventTopologyChanged EventType = "topology-changed"
	// EventServiceChanged fires on a ServiceUpdate push.
	EventServiceChanged EventType = "service-changed"
	// EventProcessChanged fires on a ProcessUpdate push.
	EventProcessChanged EventType = "process-changed"
	// EventDisconnected fires when the socket drops (before a reconnect attempt)
	// so the consumer can fall back to polling in the meantime.
	EventDisconnected EventType = "disconnected"
)

// Event is one watcher event.
type Event struct {
	Type EventType `json:"type"`
}

// Config is what the watcher needs to authenticate + scope the subscriptions.
type Config struct {
	APIHost string // e.g. api.app-prg1.zerops.io (ZCP_API_HOST)
	// Token is the API access token, read at each (re)connect so a rotated
	// key is the one used (never logged).
	Token    func() string
	ClientID string   // the org owning the project (search scope)
	Streams  []Stream // the subscriptions held

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
	token := cfg.Token()
	wsToken, err := cfg.authExchange(ctx, token)
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
	events := make(map[Stream]EventType, len(cfg.Streams))
	for _, stream := range cfg.Streams {
		event, err := cfg.subscribe(ctx, token, receiverID, stream)
		if err != nil {
			return fmt.Errorf("subscribe %s: %w", stream, err)
		}
		events[stream] = event
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
			if event, ok := events[Stream(msg.SubscriptionName)]; ok {
				emit(Event{Type: event})
			}
		case "pong":
			// heartbeat ack — read deadline already refreshed above
		}
	}
}

// authExchange swaps the API access token for a short-lived webSocketToken.
// VERIFIED: the body field is "token" AND the Bearer header is required.
func (cfg Config) authExchange(ctx context.Context, token string) (string, error) {
	body, err := json.Marshal(struct {
		Token string `json:"token"`
	}{Token: token})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.scheme("https")+"://"+cfg.APIHost+"/api/rest/public/web-socket/login", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
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

// subscribe registers stream against our receiverId by POSTing its entity's
// search, filtered by the org, augmented with the ws fields; it answers the
// event the stream's pushes emit.
func (cfg Config) subscribe(ctx context.Context, token, receiverID string, stream Stream) (EventType, error) {
	entity, mode, terms, event := stream.subscription()
	body, err := json.Marshal(subscribeBody{
		Search:           append([]searchTerm{{Name: "clientId", Operator: "eq", Value: cfg.ClientID}}, terms...),
		Sort:             []string{},
		Limit:            subscribeLimit,
		SubscriptionName: string(stream),
		ReceiverID:       receiverID,
		WSOutputType:     mode,
		DisableOutput:    true,
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.scheme("https")+"://"+cfg.APIHost+"/api/rest/public/"+entity+"/search", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := cfg.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("subscribe status %d", resp.StatusCode)
	}
	return event, nil
}

// subscribeLimit is the frontend's page size for a subscribed search.
const subscribeLimit = 200

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

// subscribeBody is an entity's search payload + the ws-subscription fields
// (the frontend's updateSubscribe/listSubscribe shape, ZEROPS-078/079).
type subscribeBody struct {
	Search           []searchTerm `json:"search"`
	Sort             []string     `json:"sort"`
	Limit            int          `json:"limit"`
	SubscriptionName string       `json:"subscriptionName"`
	ReceiverID       string       `json:"receiverId"`
	WSOutputType     string       `json:"wsOutputType"`
	DisableOutput    bool         `json:"disableOutput"`
}

// searchTerm is one filter of a search; Value is a string, or a list for "in".
type searchTerm struct {
	Name     string `json:"name"`
	Operator string `json:"operator"`
	Value    any    `json:"value"`
}

// Pushes opens a push session for as long as ctx lives and answers its wakes
// (Wakes): the seam a wait on the project holds.
type Pushes func(ctx context.Context) <-chan struct{}

// ProjectPushes is Pushes over the project's streams (ProjectStreams) for the
// org clientID, under the key token answers at each connect.
func ProjectPushes(apiHost string, token func() string, clientID string) Pushes {
	return func(ctx context.Context) <-chan struct{} {
		return Wakes(ctx, Config{APIHost: apiHost, Token: token, ClientID: clientID, Streams: ProjectStreams()})
	}
}

// Wakes watches cfg's streams until ctx ends and answers one wake after each
// connection — a read then covers whatever changed while there was none —
// and after each push, coalesced: a reader away for several finds one.
func Wakes(ctx context.Context, cfg Config) <-chan struct{} {
	wakes := make(chan struct{}, 1)
	go func() {
		_ = Watch(ctx, cfg, func(e Event) {
			if e.Type == EventDisconnected {
				return
			}
			select {
			case wakes <- struct{}{}:
			default:
			}
		})
	}()
	return wakes
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
