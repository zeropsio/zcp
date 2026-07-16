// Command dcseed is a THROWAWAY seeder: it fills every managed service in the
// adopted project with a little sample data so the Data Console shows something
// in each. It reuses the Data Console's verified connection resolution
// (zcpadapter) — same credentials path as `zcp studio console serve` — and seeds
// each family with its driver. Idempotent + resilient (per-service log+continue).
// Run it FROM the zcp container (internal network reaches every service).
package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	_ "github.com/ClickHouse/clickhouse-go/v2"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl/plain"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
	"github.com/zeropsio/zcp/internal/dataconsole/zcpadapter"
	"github.com/zeropsio/zcp/internal/platform"
)

func main() {
	ctx := context.Background()
	creds, err := auth.ResolveCredentials()
	must("resolve creds", err)
	client, err := platform.NewZeropsClient(creds.Token, creds.APIHost)
	must("client", err)
	info, err := auth.Resolve(ctx, client)
	must("auth resolve", err)
	ad := zcpadapter.New(client, info)
	svcs, err := ad.ManagedServices(ctx)
	must("list managed", err)

	fmt.Printf("project=%s services=%d\n", info.ProjectName, len(svcs))
	for _, s := range svcs {
		ci, err := ad.ConnectionInfo(ctx, s.ID)
		if err != nil {
			fmt.Printf("[%s] connInfo: %v\n", s.Hostname, err)
			continue
		}
		fmt.Printf("--- %s (%s) ---\n", s.Hostname, ci.Type)
		if os.Getenv("DCSEED_DEBUG") != "" {
			fmt.Printf("    descriptor %s\n", redactedDescriptor(ci.Descriptor))
		}
		if err := seed(ctx, ci.Descriptor); err != nil {
			fmt.Printf("[%s] seed: %v\n", s.Hostname, err)
		} else {
			fmt.Printf("[%s] seeded ✓\n", s.Hostname)
		}
	}
}

func seed(ctx context.Context, desc provider.ConnectionDescriptor) error {
	switch conn := desc.(type) {
	case provider.SQLConn:
		switch conn.Dialect {
		case "postgresql":
			return seedPostgres(ctx, conn)
		case "mariadb", "mysql":
			return seedMySQL(ctx, conn)
		case "clickhouse":
			return seedClickhouse(ctx, conn)
		}
	case provider.KVConn:
		return seedValkey(ctx, conn)
	case provider.ObjectConn:
		return seedObject(ctx, conn)
	case provider.DocumentConn:
		switch conn.Engine {
		case "elasticsearch":
			return seedElastic(ctx, conn)
		case "meilisearch":
			return seedMeili(ctx, conn)
		case "typesense":
			return seedTypesense(ctx, conn)
		case "qdrant":
			return seedQdrant(ctx, conn)
		}
	case provider.StreamConn:
		switch conn.Engine {
		case "kafka":
			return seedKafka(ctx, conn)
		case "nats":
			return seedNats(ctx, conn)
		}
	default:
		fmt.Printf("    (no seeder for %T — skipped)\n", desc)
		return nil
	}
	fmt.Printf("    (no seeder for %T — skipped)\n", desc)
	return nil
}

// ---------- tabular ----------

func seedPostgres(ctx context.Context, conn provider.SQLConn) error {
	dsn := fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=disable",
		url.QueryEscape(conn.User), url.QueryEscape(conn.Password),
		net.JoinHostPort(conn.Host, conn.Port), conn.Database)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	// Dedicated demo tables (dc_*) so we never collide with or drop the project's
	// own tables (some have dependents). Idempotent: insert only when empty.
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS dc_customers (id serial PRIMARY KEY, name text NOT NULL, email text, created timestamptz DEFAULT now())`,
		`CREATE TABLE IF NOT EXISTS dc_orders (id serial PRIMARY KEY, customer_id int, total numeric(10,2), status text DEFAULT 'pending')`,
		`INSERT INTO dc_customers (name, email) SELECT * FROM (VALUES ('Alice','alice@example.io'),('Bob','bob@example.io'),('Carol','carol@example.io')) v(n,e) WHERE NOT EXISTS (SELECT 1 FROM dc_customers)`,
		`INSERT INTO dc_orders (customer_id, total, status) SELECT * FROM (VALUES (1,49.90,'paid'),(2,19.50,'pending'),(1,4.25,'shipped')) v(c,t,s) WHERE NOT EXISTS (SELECT 1 FROM dc_orders)`,
	}
	return execAll(ctx, db, stmts)
}

func seedMySQL(ctx context.Context, dbConn provider.SQLConn) error {
	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s?parseTime=true", dbConn.User, dbConn.Password, net.JoinHostPort(dbConn.Host, dbConn.Port), dbConn.Database)
	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		return err
	}
	defer conn.Close()
	stmts := []string{
		"CREATE TABLE IF NOT EXISTS users (id INT AUTO_INCREMENT PRIMARY KEY, name VARCHAR(120) NOT NULL, email VARCHAR(190))",
		"CREATE TABLE IF NOT EXISTS products (id INT AUTO_INCREMENT PRIMARY KEY, title VARCHAR(190) NOT NULL, price DECIMAL(10,2))",
		"INSERT INTO users (name,email) SELECT 'Alice','alice@example.io' FROM DUAL WHERE NOT EXISTS (SELECT 1 FROM users)",
		"INSERT INTO users (name,email) SELECT 'Bob','bob@example.io' FROM DUAL WHERE (SELECT COUNT(*) FROM users) < 3",
		"INSERT INTO products (title,price) SELECT 'Widget',9.99 FROM DUAL WHERE NOT EXISTS (SELECT 1 FROM products)",
	}
	return execAll(ctx, conn, stmts)
}

func seedClickhouse(ctx context.Context, conn provider.SQLConn) error {
	hostPort := net.JoinHostPort(conn.Host, conn.Port)
	// NO readonly flag here — we are seeding (the provider opens readonly=1).
	dsn := fmt.Sprintf("clickhouse://%s:%s@%s/%s", url.QueryEscape(conn.User), url.QueryEscape(conn.Password), hostPort, conn.Database)
	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	stmts := []string{
		"CREATE TABLE IF NOT EXISTS events (id UInt64, name String, ts DateTime DEFAULT now()) ENGINE = MergeTree ORDER BY id",
		"INSERT INTO events (id,name) SELECT number, concat('event-', toString(number)) FROM numbers(20) WHERE (SELECT count() FROM events) = 0",
	}
	return execAll(ctx, db, stmts)
}

func execAll(ctx context.Context, db *sql.DB, stmts []string) error {
	cx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := db.PingContext(cx); err != nil {
		return err
	}
	for _, q := range stmts {
		if _, err := db.ExecContext(cx, q); err != nil {
			return fmt.Errorf("%.50q: %w", q, err)
		}
	}
	return nil
}

// ---------- valkey ----------

func seedValkey(ctx context.Context, conn provider.KVConn) error {
	cli := redis.NewClient(&redis.Options{Addr: net.JoinHostPort(conn.Host, conn.Port), Password: conn.Password})
	defer cli.Close()
	cx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := cli.Ping(cx).Err(); err != nil {
		return err
	}
	cli.Set(cx, "greeting", "hello from seed", 0)
	cli.Set(cx, "session:abc123", "user-1", time.Hour)
	cli.Set(cx, "session:def456", "user-2", 30*time.Minute)
	cli.HSet(cx, "user:1", "name", "Alice", "email", "alice@example.io", "role", "admin")
	cli.HSet(cx, "user:2", "name", "Bob", "email", "bob@example.io", "role", "member")
	cli.RPush(cx, "queue:jobs", "job-1", "job-2", "job-3")
	cli.SAdd(cx, "tags", "go", "zerops", "data", "console")
	cli.ZAdd(cx, "leaderboard", redis.Z{Score: 100, Member: "alice"}, redis.Z{Score: 250, Member: "bob"}, redis.Z{Score: 175, Member: "carol"})
	return nil
}

// ---------- object-storage ----------

func seedObject(ctx context.Context, conn provider.ObjectConn) error {
	cli, err := minio.New(conn.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(conn.AccessKey, conn.SecretKey, ""),
		Secure: conn.Secure,
	})
	if err != nil {
		return err
	}
	cx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	// text/json objects
	objs := map[string]struct{ body, ct string }{
		"readme.txt":          {"Hello from the Data Console seed.\nThis bucket has sample objects + images.\n", "text/plain"},
		"data/config.json":    {`{"feature":"data-console","enabled":true,"limit":100}`, "application/json"},
		"data/users.csv":      {"id,name,email\n1,Alice,alice@example.io\n2,Bob,bob@example.io\n", "text/csv"},
		"logs/2026-06-26.log": {"INFO seed ran\nINFO ok\n", "text/plain"},
	}
	for key, v := range objs {
		if _, err := cli.PutObject(cx, conn.Bucket, key, strings.NewReader(v.body), int64(len(v.body)), minio.PutObjectOptions{ContentType: v.ct}); err != nil {
			return fmt.Errorf("put %s: %w", key, err)
		}
	}

	// real PNG images (generated) so the console can preview them inline.
	imgs := map[string][]byte{
		"images/red.png":      pngImage(96, 96, func(x, y int) color.Color { return color.RGBA{220, 50, 47, 255} }),
		"images/gradient.png": pngImage(160, 96, func(x, y int) color.Color { return color.RGBA{uint8(x * 255 / 160), uint8(y * 255 / 96), 160, 255} }),
		"images/checker.png": pngImage(128, 128, func(x, y int) color.Color {
			if (x/16+y/16)%2 == 0 {
				return color.RGBA{38, 139, 210, 255}
			}
			return color.RGBA{253, 246, 227, 255}
		}),
		"images/circle.png": pngImage(120, 120, func(x, y int) color.Color {
			dx, dy := float64(x-60), float64(y-60)
			if dx*dx+dy*dy <= 50*50 {
				return color.RGBA{133, 153, 0, 255}
			}
			return color.RGBA{0, 43, 54, 255}
		}),
	}
	for key, b := range imgs {
		if _, err := cli.PutObject(cx, conn.Bucket, key, bytes.NewReader(b), int64(len(b)), minio.PutObjectOptions{ContentType: "image/png"}); err != nil {
			return fmt.Errorf("put %s: %w", key, err)
		}
	}
	return nil
}

// ---------- document engines (HTTP) ----------

// pngImage renders a w×h PNG by sampling fn per pixel.
func pngImage(w, h int, fn func(x, y int) color.Color) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, fn(x, y))
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

func seedElastic(ctx context.Context, conn provider.DocumentConn) error {
	docs := []struct {
		idx, id, body string
	}{
		{"products", "1", `{"name":"Widget","price":9.99,"tags":["tools","new"]}`},
		{"products", "2", `{"name":"Gadget","price":19.50,"tags":["tools"]}`},
		{"products", "3", `{"name":"Gizmo","price":4.25,"tags":["misc"]}`},
		{"articles", "a1", `{"title":"Hello Zerops","author":"alice","words":420}`},
	}
	for _, d := range docs {
		if err := httpReq(ctx, http.MethodPut, conn.BaseURL+"/"+d.idx+"/_doc/"+d.id, []byte(d.body), basicAuth(conn.User, conn.APIKey), ""); err != nil {
			return err
		}
	}
	return httpReq(ctx, http.MethodPost, conn.BaseURL+"/_refresh", nil, basicAuth(conn.User, conn.APIKey), "")
}

func seedMeili(ctx context.Context, conn provider.DocumentConn) error {
	body := `[{"id":1,"title":"Widget","price":9.99},{"id":2,"title":"Gadget","price":19.5},{"id":3,"title":"Gizmo","price":4.25}]`
	return httpReq(ctx, http.MethodPost, conn.BaseURL+"/indexes/products/documents", []byte(body), bearer(conn.APIKey), "")
}

func seedTypesense(ctx context.Context, conn provider.DocumentConn) error {
	schema := `{"name":"products","fields":[{"name":"title","type":"string"},{"name":"price","type":"float"}]}`
	_ = httpReq(ctx, http.MethodPost, conn.BaseURL+"/collections", []byte(schema), tsKey(conn.APIKey), "") // ignore "already exists"
	// import documents (JSONL)
	docs := "{\"id\":\"1\",\"title\":\"Widget\",\"price\":9.99}\n{\"id\":\"2\",\"title\":\"Gadget\",\"price\":19.5}\n"
	return httpReq(ctx, http.MethodPost, conn.BaseURL+"/collections/products/documents/import?action=upsert", []byte(docs), tsKey(conn.APIKey), "text/plain")
}

func seedQdrant(ctx context.Context, conn provider.DocumentConn) error {
	hdr := func(r *http.Request) {
		if conn.APIKey != "" {
			r.Header.Set("api-key", conn.APIKey)
		}
	}
	_ = httpReq(ctx, http.MethodPut, conn.BaseURL+"/collections/items", []byte(`{"vectors":{"size":4,"distance":"Cosine"}}`), hdr, "")
	points := `{"points":[{"id":1,"vector":[0.1,0.2,0.3,0.4],"payload":{"name":"alpha"}},{"id":2,"vector":[0.2,0.1,0.4,0.3],"payload":{"name":"beta"}},{"id":3,"vector":[0.9,0.8,0.7,0.6],"payload":{"name":"gamma"}}]}`
	return httpReq(ctx, http.MethodPut, conn.BaseURL+"/collections/items/points?wait=true", []byte(points), hdr, "")
}

// ---------- streams ----------

func seedKafka(ctx context.Context, streamConn provider.StreamConn) error {
	addr := net.JoinHostPort(streamConn.Host, streamConn.Port)
	var mech plain.Mechanism
	d := &kafka.Dialer{Timeout: 12 * time.Second}
	if streamConn.User != "" {
		mech = plain.Mechanism{Username: streamConn.User, Password: streamConn.Password}
		d.SASLMechanism = mech
	}
	cx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	// Explicitly create the topic via the controller — the broker has
	// auto-create disabled (a write to a missing topic returns UnknownTopic).
	conn, err := d.DialContext(cx, "tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	ctrl, err := conn.Controller()
	if err != nil {
		return err
	}
	cc, err := d.DialContext(cx, "tcp", net.JoinHostPort(ctrl.Host, fmt.Sprint(ctrl.Port)))
	if err != nil {
		return err
	}
	defer cc.Close()
	if err := cc.CreateTopics(kafka.TopicConfig{Topic: "orders", NumPartitions: 1, ReplicationFactor: 1}); err != nil {
		return err
	}

	transport := &kafka.Transport{}
	if streamConn.User != "" {
		transport.SASL = mech
	}
	w := &kafka.Writer{Addr: kafka.TCP(addr), Topic: "orders", Balancer: &kafka.LeastBytes{}, Transport: transport}
	defer w.Close()
	msgs := []kafka.Message{
		{Key: []byte("1"), Value: []byte(`{"order":1,"item":"Widget"}`)},
		{Key: []byte("2"), Value: []byte(`{"order":2,"item":"Gadget"}`)},
		{Key: []byte("3"), Value: []byte(`{"order":3,"item":"Gizmo"}`)},
	}
	return w.WriteMessages(cx, msgs...)
}

func seedNats(ctx context.Context, conn provider.StreamConn) error {
	opts := []nats.Option{nats.Timeout(12 * time.Second)}
	if conn.User != "" {
		opts = append(opts, nats.UserInfo(conn.User, conn.Password))
	}
	nc, err := nats.Connect("nats://"+net.JoinHostPort(conn.Host, conn.Port), opts...)
	if err != nil {
		return err
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		return err
	}
	cx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, err = js.CreateStream(cx, jetstream.StreamConfig{Name: "EVENTS", Subjects: []string{"events.>"}})
	if err != nil && !strings.Contains(err.Error(), "already") {
		return err
	}
	for i := 1; i <= 5; i++ {
		if _, err := js.Publish(cx, "events.seed", fmt.Appendf(nil, `{"seq":%d,"msg":"hello"}`, i)); err != nil {
			return err
		}
	}
	return nil
}

// ---------- http helpers ----------

func httpReq(ctx context.Context, method, u string, body []byte, hdr func(*http.Request), ctype string) error {
	cx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var r *http.Request
	var err error
	if body != nil {
		r, err = http.NewRequestWithContext(cx, method, u, bytes.NewReader(body))
	} else {
		r, err = http.NewRequestWithContext(cx, method, u, nil)
	}
	if err != nil {
		return err
	}
	if ctype == "" {
		ctype = "application/json"
	}
	r.Header.Set("Content-Type", ctype)
	if hdr != nil {
		hdr(r)
	}
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var b bytes.Buffer
		_, _ = b.ReadFrom(resp.Body)
		return fmt.Errorf("%s %s -> %d: %.160s", method, u, resp.StatusCode, b.String())
	}
	return nil
}

func basicAuth(u, p string) func(*http.Request) {
	return func(r *http.Request) {
		if u != "" || p != "" {
			r.SetBasicAuth(u, p)
		}
	}
}
func bearer(k string) func(*http.Request) {
	return func(r *http.Request) {
		if k != "" {
			r.Header.Set("Authorization", "Bearer "+k)
		}
	}
}
func tsKey(k string) func(*http.Request) {
	return func(r *http.Request) {
		if k != "" {
			r.Header.Set("X-TYPESENSE-API-KEY", k)
		}
	}
}

func redactedDescriptor(desc provider.ConnectionDescriptor) string {
	if desc == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%T family=%s", desc, desc.ConnectionFamily())
}

// ---------- misc ----------

func must(what string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", what, err)
		os.Exit(1)
	}
}
