package farm

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// ErrObjectExists reports that a conditional create lost its race. Callers
// must preserve the existing object and may not retry with an unconditional
// PUT.
var ErrObjectExists = fmt.Errorf("farm: object already exists")

// awsRegion and awsService are fixed: the farm bucket lives in one region,
// on S3 (docs/spec-eval-farm.md §1: "AWS SigV4, region us-east-1, service
// s3").
const (
	awsRegion           = "us-east-1"
	awsService          = "s3"
	sinkRequestTimeout  = 30 * time.Second
	sinkMaxResponseSize = 64 << 20
	sinkMaxListSize     = 8 << 20
)

// ErrObjectNotFound is returned by SinkClient.Get and reported via
// SinkClient.Head when the bucket has no object at the given key. It
// satisfies errors.Is(err, fs.ErrNotExist) so a bucket-backed Bundle
// (observer/sinkbundle.go) reads exactly like a local-directory one to the
// observer's optional-file loaders (observer/bundle.go), which check
// errors.Is(err, os.ErrNotExist) to render a missing optional file as
// "(not recorded)" rather than failing the whole observation.
var ErrObjectNotFound = fmt.Errorf("farm: object not found: %w", fs.ErrNotExist)

// Config is the farm bucket configuration, read from env only
// (docs/spec-eval-farm.md §2.4): ZCP_FARM_S3_URL, ZCP_FARM_S3_BUCKET,
// ZCP_FARM_S3_KEY, ZCP_FARM_S3_SECRET.
type Config struct {
	URL    string
	Bucket string
	Key    string
	Secret string
}

// ConfigFromEnv reads the farm bucket config from ZCP_FARM_S3_* via
// os.Getenv. A thin wrapper over ConfigFromLookup for callers with no
// farm-service resolution to overlay (docs/spec-eval-farm.md §3.1 FM-17;
// cmd/zcp/eval_farm_console.go is the one remaining direct caller).
func ConfigFromEnv() (Config, error) {
	return ConfigFromLookup(os.Getenv)
}

// ConfigFromLookup reads the farm bucket config from ZCP_FARM_S3_* through
// lookup — os.Getenv for ConfigFromEnv, or the environment-first/
// farm-service-resolved overlay `zcp eval farm` builds once per invocation
// (docs/spec-eval-farm.md §3.1 FM-17). It refuses with a single error
// naming every missing variable rather than failing on the first one,
// since a caller with none set needs the whole list at once.
func ConfigFromLookup(lookup func(string) string) (Config, error) {
	cfg := Config{
		URL:    lookup("ZCP_FARM_S3_URL"),
		Bucket: lookup("ZCP_FARM_S3_BUCKET"),
		Key:    lookup("ZCP_FARM_S3_KEY"),
		Secret: lookup("ZCP_FARM_S3_SECRET"),
	}
	var missing []string
	if cfg.URL == "" {
		missing = append(missing, "ZCP_FARM_S3_URL")
	}
	if cfg.Bucket == "" {
		missing = append(missing, "ZCP_FARM_S3_BUCKET")
	}
	if cfg.Key == "" {
		missing = append(missing, "ZCP_FARM_S3_KEY")
	}
	if cfg.Secret == "" {
		missing = append(missing, "ZCP_FARM_S3_SECRET")
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("farm: missing env var(s): %s", strings.Join(missing, ", "))
	}
	return cfg, nil
}

// SinkClient is a stdlib-only (net/http + crypto/hmac + crypto/sha256)
// path-style S3 client, signed with AWS SigV4. Path-style is load-bearing:
// virtual-host URLs do not resolve on Zerops object storage
// (docs/spec-eval-farm.md §1, FM-8).
type SinkClient struct {
	cfg    Config
	client *http.Client
	now    func() time.Time
}

// NewSinkClient creates a SinkClient with its own finite request deadline.
// Several CLI call sites intentionally use context.Background; the client
// deadline keeps an unreachable object store from blocking them forever.
func NewSinkClient(cfg Config) *SinkClient {
	return &SinkClient{cfg: cfg, client: &http.Client{Timeout: sinkRequestTimeout}, now: time.Now}
}

// objectURL returns the path-style URL for key ("" for the bucket root,
// used by List). It never produces a virtual-host URL
// (<bucket>.<host>/...) — the bucket is always a literal path segment.
func (c *SinkClient) objectURL(key string) string {
	u := strings.TrimRight(c.cfg.URL, "/") + "/" + awsURIEncodePath(c.cfg.Bucket)
	if key != "" {
		u += "/" + awsURIEncodePath(key)
	}
	return u
}

// signAndDo builds, signs, and sends one S3 request. rawQuery must already
// be AWS-canonical (sorted, percent-encoded) — buildQuery produces it.
func (c *SinkClient) signAndDo(ctx context.Context, method, key, rawQuery string, body []byte) (*http.Response, error) {
	return c.signAndDoWithHeaders(ctx, method, key, rawQuery, body, nil)
}

func (c *SinkClient) signAndDoWithHeaders(ctx context.Context, method, key, rawQuery string, body []byte, extra http.Header) (*http.Response, error) {
	rawURL := c.objectURL(key)
	if rawQuery != "" {
		rawURL += "?" + rawQuery
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("farm: build request: %w", err)
	}

	payloadHash := sha256Hex(body)
	now := c.now()
	amzDate := now.UTC().Format("20060102T150405Z")
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	for name, values := range extra {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	req.ContentLength = int64(len(body))
	signedHeaders := map[string]string{
		"host":                 req.URL.Host,
		"x-amz-content-sha256": payloadHash,
		"x-amz-date":           amzDate,
	}
	if value := req.Header.Get("If-None-Match"); value != "" {
		signedHeaders["if-none-match"] = value
	}

	sigReq := SigV4Request{
		Method:        method,
		Path:          req.URL.EscapedPath(),
		Query:         rawQuery,
		Headers:       signedHeaders,
		PayloadSHA256: payloadHash,
	}
	req.Header.Set("Authorization", SignV4(sigReq, c.cfg.Key, c.cfg.Secret, awsRegion, awsService, now))

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("farm: %s %s: %w", method, rawURL, err)
	}
	return resp, nil
}

// sinkAttempts and sinkRetryBackoff bound signAndDoRetrying below: the
// farm's own writes are a batch's evidence, and one dropped keep-alive
// connection must not lose a run's done.json or a batch's summary (seen
// live as "transport connection broken" while writing summary.json).
const (
	sinkAttempts     = 3
	sinkRetryBackoff = 200 * time.Millisecond
)

// signAndDoRetrying is signAndDo plus one reliability rule: a request whose
// connection failed, or that the store answered with 429 or 5xx, is signed
// and sent again (at most sinkAttempts times, with a short backoff). Every
// request this client makes is idempotent — a GET, a HEAD, a LIST, or a PUT
// of one fixed key and body — so a retry can only repeat the same effect. A
// 4xx answer is final. The context ends the wait.
func (c *SinkClient) signAndDoRetrying(ctx context.Context, method, key, rawQuery string, body []byte) (*http.Response, error) {
	var lastErr error
	for attempt := range sinkAttempts {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(sinkRetryBackoff * time.Duration(attempt)):
			}
		}
		resp, err := c.signAndDo(ctx, method, key, rawQuery, body)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return nil, err
			}
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode/100 == 5 {
			lastErr = fmt.Errorf("farm: %s %s: %s", method, key, statusError(resp))
			_ = resp.Body.Close()
			continue
		}
		return resp, nil
	}
	return nil, lastErr
}

// Put uploads body to key, replacing any existing object there.
func (c *SinkClient) Put(ctx context.Context, key string, body []byte) error {
	resp, err := c.signAndDoRetrying(ctx, http.MethodPut, key, "", body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("farm: PUT %s: %s", key, statusError(resp))
	}
	return nil
}

// PutIfAbsent creates key exactly once using S3's conditional-write
// precondition. It deliberately does not retry a transport error: after a
// lost response the store may have accepted the write, so ownership is
// ambiguous and the caller must fail closed.
func (c *SinkClient) PutIfAbsent(ctx context.Context, key string, body []byte) error {
	resp, err := c.signAndDoWithHeaders(ctx, http.MethodPut, key, "", body, http.Header{"If-None-Match": {"*"}})
	if err != nil {
		return fmt.Errorf("farm: conditional PUT %s: ambiguous create: %w", key, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusPreconditionFailed {
		return fmt.Errorf("farm: conditional PUT %s: %w", key, ErrObjectExists)
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("farm: conditional PUT %s: %s", key, statusError(resp))
	}
	return nil
}

// Get downloads key's content. It returns ErrObjectNotFound (wrapped) when
// the bucket has no object at key.
func (c *SinkClient) Get(ctx context.Context, key string) ([]byte, error) {
	resp, err := c.signAndDoRetrying(ctx, http.MethodGet, key, "", nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("farm: GET %s: %w", key, ErrObjectNotFound)
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("farm: GET %s: %s", key, statusError(resp))
	}
	data, err := readBodyLimited(resp.Body, resp.ContentLength, sinkMaxResponseSize)
	if err != nil {
		return nil, fmt.Errorf("farm: GET %s: read body: %w", key, err)
	}
	return data, nil
}

// Head reports whether key exists and, if so, its size. exists is false
// with a nil error when the bucket has no object at key.
func (c *SinkClient) Head(ctx context.Context, key string) (exists bool, size int64, err error) {
	resp, err := c.signAndDoRetrying(ctx, http.MethodHead, key, "", nil)
	if err != nil {
		return false, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return false, 0, nil
	}
	if resp.StatusCode/100 != 2 {
		return false, 0, fmt.Errorf("farm: HEAD %s: %s", key, statusError(resp))
	}
	return true, resp.ContentLength, nil
}

// listBucketResult is the subset of the S3 ListObjectsV2 XML body
// (list-type=2) this package reads.
type listBucketResult struct {
	XMLName               xml.Name `xml:"ListBucketResult"`
	IsTruncated           bool     `xml:"IsTruncated"`
	NextContinuationToken string   `xml:"NextContinuationToken"`
	Contents              []struct {
		Key string `xml:"Key"`
	} `xml:"Contents"`
}

// List returns every object key under prefix, following continuation
// tokens until the listing is no longer truncated
// (docs/spec-eval-farm.md §1: "?list-type=2&prefix=, follow continuation
// tokens"). A truncated page without a continuation token is an error,
// never a successful partial listing.
func (c *SinkClient) List(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	continuationToken := ""
	for {
		params := map[string]string{"list-type": "2", "prefix": prefix}
		if continuationToken != "" {
			params["continuation-token"] = continuationToken
		}
		resp, err := c.signAndDoRetrying(ctx, http.MethodGet, "", buildQuery(params), nil)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode/100 != 2 {
			// R10d: read the error body BEFORE any other read touches
			// resp.Body — statusError's own read would otherwise find the
			// body already drained and report an empty message.
			errMsg := statusError(resp)
			_ = resp.Body.Close()
			return nil, fmt.Errorf("farm: LIST prefix=%s: %s", prefix, errMsg)
		}
		body, readErr := readBodyLimited(resp.Body, resp.ContentLength, sinkMaxListSize)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("farm: LIST prefix=%s: read body: %w", prefix, readErr)
		}
		var result listBucketResult
		if err := xml.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("farm: LIST prefix=%s: parse response: %w", prefix, err)
		}
		for _, entry := range result.Contents {
			keys = append(keys, entry.Key)
		}
		if !result.IsTruncated {
			break
		}
		if result.NextContinuationToken == "" {
			return nil, fmt.Errorf("farm: LIST prefix=%s: truncated response has no continuation token", prefix)
		}
		continuationToken = result.NextContinuationToken
	}
	return keys, nil
}

func readBodyLimited(r io.Reader, contentLength, limit int64) ([]byte, error) {
	if contentLength > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return data, nil
}

func statusError(resp *http.Response) string {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Sprintf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
}

// --- AWS SigV4 signing (stdlib only) ---

// SigV4Request is the minimal request shape SignV4 needs. Path and Query
// must already be in their final on-the-wire, AWS-canonical form (Path
// percent-encoded per segment with "/" preserved; Query sorted and
// percent-encoded) — SignV4 does not re-derive them, so the caller's wire
// request and its signature always agree.
type SigV4Request struct {
	Method        string
	Path          string
	Query         string
	Headers       map[string]string // header name (any case) -> single value
	PayloadSHA256 string            // lowercase-hex sha256 of the body ("" body hashes to the empty-string digest)
}

// SignV4 computes the AWS Signature Version 4 Authorization header value
// for req, signed with accessKey/secretKey for the given region/service at
// time t. Exported (rather than folded into SinkClient) so it can be
// checked directly against AWS's own published test vectors.
func SignV4(req SigV4Request, accessKey, secretKey, region, service string, t time.Time) string {
	scope, sts := stringToSignParts(req, region, service, t)
	signingKey := deriveSigningKey(secretKey, t.UTC().Format("20060102"), region, service)
	signature := hex.EncodeToString(hmacSHA256(signingKey, sts))
	_, signedHeaders := canonicalHeaders(req.Headers)
	return fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		accessKey, scope, signedHeaders, signature)
}

// stringToSignParts returns the credential scope and the full string-to-sign
// for req at time t.
func stringToSignParts(req SigV4Request, region, service string, t time.Time) (scope, stringToSign string) {
	dateStamp := t.UTC().Format("20060102")
	amzDate := t.UTC().Format("20060102T150405Z")
	scope = fmt.Sprintf("%s/%s/%s/aws4_request", dateStamp, region, service)
	hashedCanonicalRequest := sha256Hex([]byte(canonicalRequestString(req)))
	stringToSign = strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		hashedCanonicalRequest,
	}, "\n")
	return scope, stringToSign
}

// canonicalRequestString builds the AWS canonical request string for req.
func canonicalRequestString(req SigV4Request) string {
	headersBlock, signedHeaders := canonicalHeaders(req.Headers)
	return strings.Join([]string{
		req.Method,
		req.Path,
		req.Query,
		headersBlock,
		signedHeaders,
		req.PayloadSHA256,
	}, "\n")
}

// canonicalHeaders lowercases and sorts headers by name, and returns the
// "name:value\n" block (one trailing newline after the last header) plus
// the ";"-joined SignedHeaders list.
func canonicalHeaders(headers map[string]string) (block, signedHeaders string) {
	names := make([]string, 0, len(headers))
	lower := make(map[string]string, len(headers))
	for name, value := range headers {
		lowerName := strings.ToLower(name)
		names = append(names, lowerName)
		lower[lowerName] = strings.TrimSpace(value)
	}
	sort.Strings(names)

	lines := make([]string, 0, len(names))
	for _, name := range names {
		lines = append(lines, name+":"+lower[name])
	}
	return strings.Join(lines, "\n") + "\n", strings.Join(names, ";")
}

// deriveSigningKey performs the AWS4-HMAC-SHA256 key-derivation chain.
func deriveSigningKey(secretKey, dateStamp, region, service string) []byte {
	dateKey := hmacSHA256([]byte("AWS4"+secretKey), dateStamp)
	dateRegionKey := hmacSHA256(dateKey, region)
	dateRegionServiceKey := hmacSHA256(dateRegionKey, service)
	return hmacSHA256(dateRegionServiceKey, "aws4_request")
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(data))
	return mac.Sum(nil)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// awsURIEncodePath percent-encodes one path segment per AWS's UriEncode
// rule: every byte except unreserved characters (A-Za-z0-9-._~) becomes
// %XX (uppercase hex); "/" is preserved as a segment separator so a key
// containing slashes still names nested "directories" in the bucket
// listing, matching AWS's carve-out for the object key name.
func awsURIEncodePath(path string) string {
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		segments[i] = awsURIEncode(segment)
	}
	return strings.Join(segments, "/")
}

// awsURIEncode percent-encodes s per AWS's UriEncode rule with no
// exceptions (used for query keys/values, and for each path segment).
func awsURIEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isUnreserved(c) {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

func isUnreserved(c byte) bool {
	switch {
	case 'A' <= c && c <= 'Z', 'a' <= c && c <= 'z', '0' <= c && c <= '9':
		return true
	case c == '-' || c == '.' || c == '_' || c == '~':
		return true
	default:
		return false
	}
}

// buildQuery returns the AWS-canonical query string for params: keys sorted,
// keys and values percent-encoded with awsURIEncode. The same string is used
// both on the wire and in the signature, so the two can never disagree.
func buildQuery(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, awsURIEncode(k)+"="+awsURIEncode(params[k]))
	}
	return strings.Join(parts, "&")
}
