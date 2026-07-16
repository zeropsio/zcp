package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/zeropsio/zcp/internal/dataconsole/console"
	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
	"github.com/zeropsio/zcp/internal/dataconsole/console/provider/document"
	"github.com/zeropsio/zcp/internal/dataconsole/console/provider/kv"
	"github.com/zeropsio/zcp/internal/dataconsole/console/provider/object"
	"github.com/zeropsio/zcp/internal/dataconsole/console/provider/stream"
	"github.com/zeropsio/zcp/internal/dataconsole/console/provider/tabular"
	"github.com/zeropsio/zcp/internal/dataconsole/console/safety"
	"github.com/zeropsio/zcp/internal/dataconsole/console/server"
	"github.com/zeropsio/zcp/internal/dataconsole/console/webui"
	"github.com/zeropsio/zcp/internal/dataconsole/zcpadapter"
)

// runStudioConsole boots the Data Console — a long-lived LOCAL HTTP server that
// serves the embedded SPA + a same-origin JSON API. It is the second studio
// transport shape (the one-shot `zcp studio <verb>` verbs stay as they are): a
// persistent process is required for interactive paging / blob streaming a
// one-shot JSON emit cannot serve.
//
// Contract: it prints ONE ready-line JSON object
// {url, sessionToken, writeToken, pid, allowWrites} to stdout — a PRIVATE pipe /
// secret channel the parent reads via the spawn stdout pipe, NEVER a log — then
// logs only to stderr and serves over HTTP. The Studio console handler consumes
// that ready-line: it hands the READ bearer (sessionToken) to the SPA via the URL
// fragment, and keeps the writeToken host-side (the broker attaches it, per
// mutating request, once the user confirms writes). writeToken is minted only under
// --allow-writes (empty otherwise). Neither token is ever logged.
func runStudioConsole(args []string) {
	fs := flag.NewFlagSet("studio console serve", flag.ExitOnError)
	host := fs.String("host", "127.0.0.1", "bind address (localhost only)")
	port := fs.Int("port", 0, "bind port (0 = random)")
	allowWrites := fs.Bool("allow-writes", false, "enable basic edits (default read-only)")
	// Accept an optional leading "serve" subverb for `zcp studio console serve`.
	rest := args
	if len(rest) > 0 && rest[0] == "serve" {
		rest = rest[1:]
	}
	_ = fs.Parse(rest)

	client, authInfo, ctx := studioInit()

	// Two independent per-process secrets: the read bearer (authorizes all /api/*)
	// and the write token (the caller-bound write capability the embed host presents,
	// per mutating request, to authorize a write). The write token is minted ONLY
	// under --allow-writes; a read-only process mints none and can authorize no write.
	token := newToken()
	var writeToken string
	if *allowWrites {
		writeToken = newToken()
	}
	policy := safety.NewPolicy(*allowWrites, writeToken, "")
	adapter := zcpadapter.New(client, authInfo)
	factories := map[provider.Family]console.Factory{
		provider.FamilyObject:   objectFactory,
		provider.FamilyTabular:  tabularFactory, // postgresql/mariadb/mysql/clickhouse
		provider.FamilyKV:       kvFactory,
		provider.FamilyDocument: documentFactory, // elasticsearch/meilisearch/typesense/qdrant
		provider.FamilyStream:   streamFactory,   // kafka/nats (read-only inspector)
	}
	engine := console.NewEngine(adapter, policy, factories)
	if err := engine.Refresh(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "discover: %v\n", err)
		os.Exit(1)
	}

	srv := server.New(engine, token, webui.FS())

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", fmt.Sprintf("%s:%d", *host, *port))
	if err != nil {
		engine.Close()
		fmt.Fprintf(os.Stderr, "listen: %v\n", err)
		os.Exit(1)
	}
	url := fmt.Sprintf("http://%s", ln.Addr().String())

	if err := emitReady(os.Stdout, os.Stderr, url, token, writeToken, os.Getpid(), *allowWrites); err != nil {
		fmt.Fprintf(os.Stderr, "ready: %v\n", err)
		os.Exit(1)
	}

	httpSrv := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		_ = httpSrv.Close()
	}()
	// Lifecycle backstop: when spawned by the Studio extension host our stdin is a
	// pipe. If that parent dies WITHOUT killing us (a crash), the pipe EOFs — shut
	// down so no orphaned console server lingers. A standalone run from a terminal
	// has a char-device (TTY) stdin and is unaffected.
	if fi, statErr := os.Stdin.Stat(); statErr == nil && fi.Mode()&os.ModeCharDevice == 0 {
		go func() {
			_, _ = io.Copy(io.Discard, os.Stdin)
			_ = httpSrv.Close()
		}()
	}
	serr := httpSrv.Serve(ln)
	engine.Close()
	if serr != nil && serr != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "serve: %v\n", serr)
		os.Exit(1)
	}
}

func emitReady(stdout, stderr io.Writer, url, token, writeToken string, pid int, allowWrites bool) error {
	ready := struct {
		URL          string `json:"url"`
		SessionToken string `json:"sessionToken"`
		WriteToken   string `json:"writeToken"`
		PID          int    `json:"pid"`
		AllowWrites  bool   `json:"allowWrites"`
	}{
		URL:          url,
		SessionToken: token,
		WriteToken:   writeToken,
		PID:          pid,
		AllowWrites:  allowWrites,
	}
	if err := json.NewEncoder(stdout).Encode(ready); err != nil {
		return fmt.Errorf("encode ready-line: %w", err)
	}
	if _, err := fmt.Fprintf(stderr, "data console serving on %s (writes=%v)\n", url, allowWrites); err != nil {
		return fmt.Errorf("write ready log: %w", err)
	}
	return nil
}

// objectFactory builds the S3 provider from a typed object descriptor.
func objectFactory(ci console.ConnectionInfo, policy *safety.Policy) (provider.Provider, error) {
	conn, err := typedConnection[provider.ObjectConn](ci)
	if err != nil {
		return nil, err
	}
	return object.New(object.Config{
		Endpoint:  conn.Endpoint,
		AccessKey: conn.AccessKey,
		SecretKey: conn.SecretKey,
		Bucket:    conn.Bucket,
		Secure:    conn.Secure,
		ReadOnly:  !policy.ArmingPermitted(),
	})
}

// tabularFactory builds the SQL provider from a typed SQL descriptor.
func tabularFactory(ci console.ConnectionInfo, policy *safety.Policy) (provider.Provider, error) {
	conn, err := typedConnection[provider.SQLConn](ci)
	if err != nil {
		return nil, err
	}
	return tabular.New(tabular.Config{Conn: conn, ReadOnly: !policy.ArmingPermitted()})
}

// documentFactory builds a search/vector provider (elasticsearch/meilisearch/
// typesense/qdrant) from a typed document descriptor.
func documentFactory(ci console.ConnectionInfo, policy *safety.Policy) (provider.Provider, error) {
	conn, err := typedConnection[provider.DocumentConn](ci)
	if err != nil {
		return nil, err
	}
	return document.New(document.Config{
		Engine:   conn.Engine,
		BaseURL:  conn.BaseURL,
		User:     conn.User,
		APIKey:   conn.APIKey,
		ReadOnly: !policy.ArmingPermitted(),
	})
}

// streamFactory builds a read-only messaging inspector (kafka/nats).
func streamFactory(ci console.ConnectionInfo, _ *safety.Policy) (provider.Provider, error) {
	conn, err := typedConnection[provider.StreamConn](ci)
	if err != nil {
		return nil, err
	}
	return stream.New(stream.Config{
		Engine:   conn.Engine,
		Addr:     net.JoinHostPort(conn.Host, conn.Port),
		User:     conn.User,
		Password: conn.Password,
	})
}

// kvFactory builds the Valkey provider from a typed KV descriptor.
func kvFactory(ci console.ConnectionInfo, policy *safety.Policy) (provider.Provider, error) {
	conn, err := typedConnection[provider.KVConn](ci)
	if err != nil {
		return nil, err
	}
	return kv.New(kv.Config{
		Addr:     net.JoinHostPort(conn.Host, conn.Port),
		Password: conn.Password,
		ReadOnly: !policy.ArmingPermitted(),
	})
}

func typedConnection[T provider.ConnectionDescriptor](ci console.ConnectionInfo) (T, error) {
	conn, ok := ci.Descriptor.(T)
	if !ok {
		var zero T
		return zero, fmt.Errorf("connection descriptor for %q: got %T", ci.Type, ci.Descriptor)
	}
	return conn, nil
}

func newToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		fmt.Fprintf(os.Stderr, "token: %v\n", err)
		os.Exit(1)
	}
	return hex.EncodeToString(b)
}
