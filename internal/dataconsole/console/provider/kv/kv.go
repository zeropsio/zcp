// Package kv is the Valkey (redis-shape) provider. Safety is an ALLOWLIST by
// construction: the provider only ever issues the handful of typed commands its
// methods call (SCAN/TYPE/GET/HSCAN/LRANGE/SSCAN/ZRANGE/TTL/STRLEN; SET/
// HSET/HDEL/LSET/SADD/SREM/ZADD/ZREM/EXPIRE/PERSIST/DEL) — there is no path for
// an arbitrary command, so KEYS/FLUSHALL/EVAL/CONFIG/MODULE/etc. simply cannot be
// reached. Keys are browsed via SCAN, never KEYS. (KeyDB was removed from the
// platform — Valkey is the only redis-shape family.)
package kv

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

const (
	opTimeout = 15 * time.Second
	// defaultLimit is the page cap when callers omit or zero Page.Limit.
	defaultLimit = 200
	maxLimit     = 1000
	scanCount    = 400
)

const (
	listCursorPrefix  = "list1:"
	tableCursorPrefix = "table1:"
)

// redis collection type names (TYPE result).
const (
	typeHash   = "hash"
	typeList   = "list"
	typeSet    = "set"
	typeZset   = "zset"
	typeString = "string"
	// typeNone is TYPE's result for a nonexistent key.
	typeNone = "none"
)

// Config is the resolved Valkey connection.
type Config struct {
	Addr           string // host:port
	Password       string
	ReadOnly       bool
	MaxInlineBytes int64
}

// Provider browses + edits one Valkey keyspace (db 0).
type Provider struct {
	cli  *redis.Client
	caps provider.Capabilities
}

// New builds the provider.
func New(cfg Config) (*Provider, error) {
	if cfg.Addr == "" {
		return nil, fmt.Errorf("kv: %w: missing addr", provider.ErrInvalid)
	}
	maxInline := cfg.MaxInlineBytes
	if maxInline <= 0 {
		maxInline = 1 << 20
	}
	cli := redis.NewClient(&redis.Options{Addr: cfg.Addr, Password: cfg.Password})
	return &Provider{
		cli: cli,
		caps: provider.Capabilities{
			Family: provider.FamilyKV, Support: provider.SupportFull,
			EditBlob: !cfg.ReadOnly, EditTabular: !cfg.ReadOnly, TTL: true,
			MaxInlineBytes: maxInline, ReadOnly: cfg.ReadOnly,
		},
	}, nil
}

func (p *Provider) Kind() string                { return "valkey" }
func (p *Provider) Caps() provider.Capabilities { return p.caps }
func (p *Provider) Close() error                { return p.cli.Close() }

// Health forces an authenticated PING.
func (p *Provider) Health(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	if err := p.cli.Ping(ctx).Err(); err != nil {
		return provider.HealthErr("kv: ping", err)
	}
	return nil
}

func keyOf(path provider.Path) string { return strings.Join(path.Segments, ":") }

// List scans one keyspace level with virtual ':' grouping. Keys with a deeper
// ':' segment become synthetic containers; terminal keys are TYPE-routed leaves.
//
// SCAN is iterated internally until enough nodes are collected (or the keyspace
// is exhausted). If a SCAN batch exceeds the requested limit, the overflow is
// kept in the opaque cursor so the next page resumes without dropping entries.
func (p *Provider) List(ctx context.Context, path provider.Path, page provider.Page) ([]provider.Node, string, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	prefix := keyOf(path)
	match := "*"
	if prefix != "" {
		match = prefix + ":*"
	}
	limit := clampLimit(page.Limit)
	state := decodeListCursor(page.Cursor)
	seen := stringSet(state.Seen)
	nodes := make([]provider.Node, 0, limit)
	emit := func(entry listCursorEntry) {
		if seen[entry.id()] {
			return
		}
		if entry.Container {
			nodes = append(nodes, provider.Node{
				Name: entry.Name, Kind: provider.KindContainer,
				Path: child(path, entry.Name), HasChildren: true,
			})
		} else {
			nodes = append(nodes, p.leaf(ctx, path, entry.Name, entry.FullKey))
		}
		seen[entry.id()] = true
		state.Seen = append(state.Seen, entry.id())
	}
	for len(state.Pending) > 0 && len(nodes) < limit {
		entry := state.Pending[0]
		state.Pending = state.Pending[1:]
		emit(entry)
	}
	if len(nodes) == limit {
		return nodes, encodeListCursorIfMore(state), nil
	}
	if state.Exhausted {
		return nodes, "", nil
	}
	for {
		keys, next, err := p.cli.Scan(ctx, state.Scan, match, scanCount).Result()
		if err != nil {
			return nil, "", fmt.Errorf("kv: scan: %w", provider.ErrUpstream)
		}
		state.Scan = next
		batch := listEntries(prefix, keys, seen)
		for len(batch) > 0 && len(nodes) < limit {
			emit(batch[0])
			batch = batch[1:]
		}
		if len(batch) > 0 {
			state.Pending = batch
			state.Exhausted = state.Scan == 0
			return nodes, encodeListCursor(state), nil
		}
		if state.Scan == 0 {
			return nodes, "", nil // keyspace exhausted — no more pages
		}
		if len(nodes) == limit {
			return nodes, encodeListCursor(state), nil
		}
	}
}

func (p *Provider) leaf(ctx context.Context, parent provider.Path, name, fullKey string) provider.Node {
	kind := provider.KindBlob
	if t, err := p.cli.Type(ctx, fullKey).Result(); err == nil && t != typeString && t != typeNone {
		kind = provider.KindTabular
	}
	return provider.Node{Name: name, Kind: kind, Path: child(parent, name)}
}

// Stat reports a key's type + TTL. ttlSeconds is nil for a key with no
// expiry — Redis TTL replies -1 "exists, no expiry" / -2 "missing" (the
// latter can't happen here: typeNone already returned ErrNotFound above), and
// either must never surface as the literal 0, which the SPA reads as "expires
// in 0s" instead of "no expiry" (KV-AUD-02).
func (p *Provider) Stat(ctx context.Context, path provider.Path) (provider.Node, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	key := keyOf(path)
	t, err := p.cli.Type(ctx, key).Result()
	if err != nil {
		return provider.Node{}, fmt.Errorf("kv: type: %w", provider.ErrUpstream)
	}
	if t == typeNone {
		return provider.Node{}, provider.ErrNotFound
	}
	ttl, _ := p.cli.TTL(ctx, key).Result()
	kind := provider.KindBlob
	if t != typeString {
		kind = provider.KindTabular
	}
	var ttlSeconds any
	if ttl > 0 {
		ttlSeconds = int64(ttl.Seconds())
	}
	return provider.Node{Name: lastSeg(path), Kind: kind, Path: path,
		Meta: map[string]any{"type": t, "ttlSeconds": ttlSeconds}}, nil
}

// ReadBlob returns a string value, head-sliced over the guard, with TTL.
func (p *Provider) ReadBlob(ctx context.Context, path provider.Path) ([]byte, provider.BlobMeta, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	key := keyOf(path)
	n, err := p.cli.StrLen(ctx, key).Result()
	if err != nil {
		return nil, provider.BlobMeta{}, fmt.Errorf("kv: strlen: %w", provider.ErrUpstream)
	}
	meta := provider.BlobMeta{ContentType: "text/plain", Size: n}
	var val string
	if n > p.caps.MaxInlineBytes {
		meta.Truncated = true
		val, err = p.cli.GetRange(ctx, key, 0, p.caps.MaxInlineBytes-1).Result()
	} else {
		val, err = p.cli.Get(ctx, key).Result()
	}
	if errors.Is(err, redis.Nil) {
		return nil, meta, provider.ErrNotFound
	}
	if err != nil {
		return nil, meta, fmt.Errorf("kv: get: %w", provider.ErrUpstream)
	}
	if ttl, terr := p.cli.TTL(ctx, key).Result(); terr == nil && ttl > 0 {
		s := int64(ttl.Seconds())
		meta.TTLSeconds = &s
	}
	return []byte(val), meta, nil
}

// ReadTable renders a collection (hash/list/set/zset) as a grid of entries.
func (p *Provider) ReadTable(ctx context.Context, path provider.Path, page provider.Page) (provider.TablePage, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	key := keyOf(path)
	t, err := p.cli.Type(ctx, key).Result()
	if err != nil {
		return provider.TablePage{}, fmt.Errorf("kv: type: %w", provider.ErrUpstream)
	}
	limit := clampLimit(page.Limit)
	switch t {
	case typeNone:
		// A missing key is a genuinely absent resource, not "the wrong
		// shape" — Stat already special-cases this (kv.go Stat); ReadTable's
		// switch used to fall through to the generic default: below and
		// report ErrUnsupported (422) for a deleted/never-existed key the
		// same way it does for an existing-but-wrong-type key, which reads
		// as "unsupported operation" instead of "not found" (KV-AUD-09).
		return provider.TablePage{}, provider.ErrNotFound
	case typeHash:
		tp, err := readScannedTable(page, cols2("field", "value"), []string{"field"}, func(cursor uint64) ([][]string, uint64, error) {
			vals, next, herr := p.cli.HScan(ctx, key, cursor, "*", scanCount).Result()
			if herr != nil {
				return nil, 0, fmt.Errorf("kv: hscan: %w", provider.ErrUpstream)
			}
			return pairs(vals), next, nil
		})
		return tp, err
	case typeList:
		offset := parseOffset(page.Cursor)
		vals, lerr := p.cli.LRange(ctx, key, int64(offset), int64(offset+limit)).Result()
		if lerr != nil {
			return provider.TablePage{}, fmt.Errorf("kv: lrange: %w", provider.ErrUpstream)
		}
		rows := make([][]any, 0, min(len(vals), limit))
		for i, v := range vals {
			if i == limit {
				break
			}
			rows = append(rows, []any{offset + i, v})
		}
		tp := provider.TablePage{Columns: cols2("index", "value"), Rows: rows}
		if len(vals) > limit {
			tp.NextCursor = strconv.Itoa(offset + limit)
		}
		return tp, nil
	case typeSet:
		tp, err := readScannedTable(page, cols1("member"), []string{"member"}, func(cursor uint64) ([][]string, uint64, error) {
			vals, next, serr := p.cli.SScan(ctx, key, cursor, "*", scanCount).Result()
			if serr != nil {
				return nil, 0, fmt.Errorf("kv: sscan: %w", provider.ErrUpstream)
			}
			rows := make([][]string, len(vals))
			for i, v := range vals {
				rows[i] = []string{v}
			}
			return rows, next, nil
		})
		return tp, err
	case typeZset:
		offset := parseOffset(page.Cursor)
		zs, zerr := p.cli.ZRangeWithScores(ctx, key, int64(offset), int64(offset+limit)).Result()
		if zerr != nil {
			return provider.TablePage{}, fmt.Errorf("kv: zrange: %w", provider.ErrUpstream)
		}
		rows := make([][]any, 0, min(len(zs), limit))
		for i, z := range zs {
			if i == limit {
				break
			}
			rows = append(rows, []any{z.Member, z.Score})
		}
		tp := provider.TablePage{Columns: cols2("member", "score"), Rows: rows, RowKeyCols: []string{"member"}}
		if len(zs) > limit {
			tp.NextCursor = strconv.Itoa(offset + limit)
		}
		return tp, nil
	default:
		return provider.TablePage{}, fmt.Errorf("kv: %q not a collection: %w", t, provider.ErrUnsupported)
	}
}

// WriteBlob writes a string value (SET) — shares the server's blob-write path.
// The key's current TYPE is checked first: Redis SET is unconditional, so
// without this guard a WriteBlob against an existing hash/list/set/zset would
// silently destroy the whole collection and replace it with a plain string —
// no error, no partial-failure signal, and (short of a maintainer re-seeding
// the fixture) no documented API path back to the original collection
// (KV-AUD-01, live-destroyed a "leaderboard" zset this way). A nonexistent
// key ("none") or an existing string both proceed normally; any other
// existing type is refused.
//
// contentType is accepted only for parity with provider.ObjectProvider's
// WriteBlob — a redis string value has no MIME concept, so it is ignored;
// ReadBlob always reports "text/plain" for a KV string regardless of what a
// caller supplies here.
func (p *Provider) WriteBlob(ctx context.Context, path provider.Path, val []byte, _ string) error {
	if p.caps.ReadOnly {
		return provider.ErrReadOnly
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	key := keyOf(path)
	t, err := p.cli.Type(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("kv: type: %w", provider.ErrUpstream)
	}
	if t != typeNone && t != typeString {
		return fmt.Errorf("kv: %q is a collection, refusing to overwrite with a string value: %w", t, provider.ErrWrongType)
	}
	if err := p.cli.Set(ctx, key, val, 0).Err(); err != nil {
		return fmt.Errorf("kv: set: %w", provider.ErrUpstream)
	}
	return nil
}

// SetTTL sets (EXPIRE) or clears (PERSIST) a key's TTL.
func (p *Provider) SetTTL(ctx context.Context, path provider.Path, seconds *int64) error {
	if p.caps.ReadOnly {
		return provider.ErrReadOnly
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	key := keyOf(path)
	var err error
	if seconds == nil {
		err = p.cli.Persist(ctx, key).Err()
	} else {
		err = p.cli.Expire(ctx, key, time.Duration(*seconds)*time.Second).Err()
	}
	if err != nil {
		return fmt.Errorf("kv: ttl: %w", provider.ErrUpstream)
	}
	return nil
}

// SetEntry edits one collection entry, dispatched by the key's TYPE — HSET for a
// hash field, LSET for a list index, SREM+SADD to rename a set member, ZADD to
// set a zset member's score. Each is a single typed command (allowlist-safe); a
// string key uses WriteBlob, not this path.
//
// Affected reports the real per-command effect wherever Redis's own reply
// answers "did this touch an existing entry" (the set rename: whether the old
// member existed to remove — KV-AUD-06). HSET/LSET/ZADD stay at a fixed 1 on
// success: their own integer reply counts entries *newly created*, not
// "now reflects the edit" (HSET/ZADD report 0 when only an existing
// field/member's value/score changed; LSET has no count reply at all, only
// success/error) — piping that raw count through would make the console's
// own edit-an-existing-field flow (the common case, e.g. D-02) read as
// "0 affected" despite genuinely applying, which is the same class of lie
// KV-AUD-06 exists to close, not a fix for it.
func (p *Provider) SetEntry(ctx context.Context, e provider.KVEntryEdit) (provider.Applied, error) {
	if p.caps.ReadOnly {
		return provider.Applied{}, provider.ErrReadOnly
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	key := keyOf(e.Path)
	t, err := p.cli.Type(ctx, key).Result()
	if err != nil {
		return provider.Applied{}, fmt.Errorf("kv: type: %w", provider.ErrUpstream)
	}
	switch t {
	case typeHash:
		if err := p.cli.HSet(ctx, key, e.Field, e.Value).Err(); err != nil {
			return provider.Applied{}, fmt.Errorf("kv: hset: %w", provider.ErrUpstream)
		}
		return provider.Applied{Statement: "HSET", Affected: 1}, nil
	case typeList:
		idx, cerr := strconv.ParseInt(e.Field, 10, 64)
		if cerr != nil {
			return provider.Applied{}, fmt.Errorf("kv: list index: %w", provider.ErrInvalid)
		}
		if err := p.cli.LSet(ctx, key, idx, e.Value).Err(); err != nil {
			return provider.Applied{}, fmt.Errorf("kv: lset: %w", provider.ErrUpstream)
		}
		return provider.Applied{Statement: "LSET", Affected: 1}, nil
	case typeSet:
		// A set member IS its value; "edit" renames it: remove the old, add the
		// new. Affected reflects SREM's real reply — whether the OLD member
		// existed to remove — so "renaming" an already-gone member (SADD still
		// creates the new value unconditionally) honestly reports Affected:0
		// instead of the previously hardcoded 1.
		removed, err := p.cli.SRem(ctx, key, e.Field).Result()
		if err != nil {
			return provider.Applied{}, fmt.Errorf("kv: srem: %w", provider.ErrUpstream)
		}
		if err := p.cli.SAdd(ctx, key, string(e.Value)).Err(); err != nil {
			return provider.Applied{}, fmt.Errorf("kv: sadd: %w", provider.ErrUpstream)
		}
		return provider.Applied{Statement: "SREM+SADD", Affected: removed}, nil
	case typeZset:
		// Score is mandatory: a nil Score silently defaulted to 0 would ZADD any
		// value-only edit at score 0, zeroing an existing member's real score
		// with no error and no signal (KV-AUD-10) — refuse instead of guessing.
		if e.Score == nil {
			return provider.Applied{}, fmt.Errorf("kv: zset entry requires a score: %w", provider.ErrInvalid)
		}
		if err := p.cli.ZAdd(ctx, key, redis.Z{Score: *e.Score, Member: e.Field}).Err(); err != nil {
			return provider.Applied{}, fmt.Errorf("kv: zadd: %w", provider.ErrUpstream)
		}
		return provider.Applied{Statement: "ZADD", Affected: 1}, nil
	default:
		return provider.Applied{}, fmt.Errorf("kv: %q not an editable collection: %w", t, provider.ErrUnsupported)
	}
}

// DeleteEntry removes one collection entry — HDEL (hash field), SREM (set member),
// ZREM (zset member). A list element has no clean delete-by-index command, so it
// is view-only for delete (honest ErrUnsupported). Affected is the command's real
// integer reply (0 or 1, since each call names exactly one field/member) — not a
// hardcoded 1 — so deleting an already-gone entry honestly reports 0 rather than
// claiming a removal that never happened (KV-AUD-06).
func (p *Provider) DeleteEntry(ctx context.Context, path provider.Path, field string) (provider.Applied, error) {
	if p.caps.ReadOnly {
		return provider.Applied{}, provider.ErrReadOnly
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	key := keyOf(path)
	t, err := p.cli.Type(ctx, key).Result()
	if err != nil {
		return provider.Applied{}, fmt.Errorf("kv: type: %w", provider.ErrUpstream)
	}
	var n int64
	switch t {
	case typeNone:
		// Sibling of ReadTable's KV-AUD-09 fix: deleting an entry from a key
		// that does not exist at all is a genuinely missing resource, not a
		// wrong-shape refusal — distinct from SetEntry's typeNone case, which
		// deliberately stays ErrUnsupported (KV-AUD-03's deferred
		// create-a-collection gap, not a read/delete of something absent).
		return provider.Applied{}, provider.ErrNotFound
	case typeHash:
		n, err = p.cli.HDel(ctx, key, field).Result()
		if err != nil {
			return provider.Applied{}, fmt.Errorf("kv: hdel: %w", provider.ErrUpstream)
		}
	case typeSet:
		n, err = p.cli.SRem(ctx, key, field).Result()
		if err != nil {
			return provider.Applied{}, fmt.Errorf("kv: srem: %w", provider.ErrUpstream)
		}
	case typeZset:
		n, err = p.cli.ZRem(ctx, key, field).Result()
		if err != nil {
			return provider.Applied{}, fmt.Errorf("kv: zrem: %w", provider.ErrUpstream)
		}
	default:
		return provider.Applied{}, fmt.Errorf("kv: %q entry not deletable: %w", t, provider.ErrUnsupported)
	}
	return provider.Applied{Statement: "DEL-ENTRY", Affected: n}, nil
}

// Delete removes a key (DEL).
func (p *Provider) Delete(ctx context.Context, path provider.Path) error {
	if p.caps.ReadOnly {
		return provider.ErrReadOnly
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	if err := p.cli.Del(ctx, keyOf(path)).Err(); err != nil {
		return fmt.Errorf("kv: del: %w", provider.ErrUpstream)
	}
	return nil
}

// ---- helpers ----

type listCursorEntry struct {
	Name      string `json:"name"`
	FullKey   string `json:"fullKey,omitempty"`
	Container bool   `json:"container,omitempty"`
}

func (e listCursorEntry) id() string {
	if e.Container {
		return "container:" + e.Name
	}
	return "leaf:" + e.Name
}

type listCursorState struct {
	Scan      uint64            `json:"scan,omitempty"`
	Pending   []listCursorEntry `json:"pending,omitempty"`
	Seen      []string          `json:"seen,omitempty"`
	Exhausted bool              `json:"exhausted,omitempty"`
}

type tableScanCursorState struct {
	Scan      uint64     `json:"scan,omitempty"`
	Pending   [][]string `json:"pending,omitempty"`
	Seen      []string   `json:"seen,omitempty"`
	Exhausted bool       `json:"exhausted,omitempty"`
}

func listEntries(prefix string, keys []string, seen map[string]bool) []listCursorEntry {
	queued := map[string]bool{}
	entries := make([]listCursorEntry, 0, len(keys))
	for _, k := range keys {
		rest := k
		if prefix != "" {
			rest = strings.TrimPrefix(k, prefix+":")
		}
		if rest == "" {
			continue
		}
		var entry listCursorEntry
		if name, _, found := strings.Cut(rest, ":"); found {
			entry = listCursorEntry{Name: name, Container: true}
		} else {
			entry = listCursorEntry{Name: rest, FullKey: k}
		}
		if seen[entry.id()] || queued[entry.id()] {
			continue
		}
		queued[entry.id()] = true
		entries = append(entries, entry)
	}
	return entries
}

func readScannedTable(page provider.Page, cols []provider.Column, rowKeyCols []string, scan func(uint64) ([][]string, uint64, error)) (provider.TablePage, error) {
	limit := clampLimit(page.Limit)
	state := decodeTableScanCursor(page.Cursor)
	seen := stringSet(state.Seen)
	rows := make([][]any, 0, limit)
	emit := func(row []string) {
		if len(row) == 0 || seen[row[0]] {
			return
		}
		out := make([]any, len(row))
		for i, v := range row {
			out[i] = v
		}
		rows = append(rows, out)
		seen[row[0]] = true
		state.Seen = append(state.Seen, row[0])
	}
	for len(state.Pending) > 0 && len(rows) < limit {
		row := state.Pending[0]
		state.Pending = state.Pending[1:]
		emit(row)
	}
	if len(rows) == limit {
		return provider.TablePage{Columns: cols, Rows: rows, NextCursor: encodeTableCursorIfMore(state), RowKeyCols: rowKeyCols}, nil
	}
	if state.Exhausted {
		return provider.TablePage{Columns: cols, Rows: rows, RowKeyCols: rowKeyCols}, nil
	}
	for {
		scanned, next, err := scan(state.Scan)
		if err != nil {
			return provider.TablePage{}, err
		}
		state.Scan = next
		batch := tableRows(scanned, seen)
		for len(batch) > 0 && len(rows) < limit {
			emit(batch[0])
			batch = batch[1:]
		}
		if len(batch) > 0 {
			state.Pending = batch
			state.Exhausted = state.Scan == 0
			return provider.TablePage{Columns: cols, Rows: rows, NextCursor: encodeTableCursor(state), RowKeyCols: rowKeyCols}, nil
		}
		if state.Scan == 0 {
			return provider.TablePage{Columns: cols, Rows: rows, RowKeyCols: rowKeyCols}, nil
		}
		if len(rows) == limit {
			return provider.TablePage{Columns: cols, Rows: rows, NextCursor: encodeTableCursor(state), RowKeyCols: rowKeyCols}, nil
		}
	}
}

func tableRows(scanned [][]string, seen map[string]bool) [][]string {
	queued := map[string]bool{}
	rows := make([][]string, 0, len(scanned))
	for _, row := range scanned {
		if len(row) == 0 || seen[row[0]] || queued[row[0]] {
			continue
		}
		queued[row[0]] = true
		rows = append(rows, row)
	}
	return rows
}

func pairs(vals []string) [][]string {
	rows := make([][]string, 0, len(vals)/2)
	for i := 0; i+1 < len(vals); i += 2 {
		rows = append(rows, []string{vals[i], vals[i+1]})
	}
	return rows
}

func stringSet(vals []string) map[string]bool {
	out := make(map[string]bool, len(vals))
	for _, v := range vals {
		out[v] = true
	}
	return out
}

func encodeListCursorIfMore(state listCursorState) string {
	if state.Scan == 0 && len(state.Pending) == 0 {
		return ""
	}
	return encodeListCursor(state)
}

func encodeListCursor(state listCursorState) string {
	b, err := json.Marshal(state)
	if err != nil {
		return ""
	}
	return listCursorPrefix + base64.RawURLEncoding.EncodeToString(b)
}

func decodeListCursor(cursor string) listCursorState {
	if raw, ok := strings.CutPrefix(cursor, listCursorPrefix); ok {
		payload, err := base64.RawURLEncoding.DecodeString(raw)
		if err == nil {
			var state listCursorState
			if err := json.Unmarshal(payload, &state); err == nil {
				return state
			}
		}
	}
	scan, err := strconv.ParseUint(cursor, 10, 64)
	if err != nil {
		return listCursorState{}
	}
	return listCursorState{Scan: scan}
}

func encodeTableCursorIfMore(state tableScanCursorState) string {
	if state.Scan == 0 && len(state.Pending) == 0 {
		return ""
	}
	return encodeTableCursor(state)
}

func encodeTableCursor(state tableScanCursorState) string {
	b, err := json.Marshal(state)
	if err != nil {
		return ""
	}
	return tableCursorPrefix + base64.RawURLEncoding.EncodeToString(b)
}

func decodeTableScanCursor(cursor string) tableScanCursorState {
	if raw, ok := strings.CutPrefix(cursor, tableCursorPrefix); ok {
		payload, err := base64.RawURLEncoding.DecodeString(raw)
		if err == nil {
			var state tableScanCursorState
			if err := json.Unmarshal(payload, &state); err == nil {
				return state
			}
		}
	}
	scan, err := strconv.ParseUint(cursor, 10, 64)
	if err != nil {
		return tableScanCursorState{}
	}
	return tableScanCursorState{Scan: scan}
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return defaultLimit
	}
	if limit > maxLimit {
		return maxLimit
	}
	return limit
}

func parseOffset(cursor string) int {
	offset, err := strconv.Atoi(cursor)
	if err != nil || offset < 0 {
		return 0
	}
	return offset
}

func child(parent provider.Path, name string) provider.Path {
	seg := make([]string, len(parent.Segments)+1)
	copy(seg, parent.Segments)
	seg[len(parent.Segments)] = name
	return provider.Path{Service: parent.Service, Segments: seg}
}

func lastSeg(path provider.Path) string {
	if len(path.Segments) == 0 {
		return ""
	}
	return path.Segments[len(path.Segments)-1]
}

func cursorStr(c uint64) string {
	if c == 0 {
		return ""
	}
	return strconv.FormatUint(c, 10)
}

func cols1(a string) []provider.Column { return []provider.Column{{Name: a}} }
func cols2(a, b string) []provider.Column {
	return []provider.Column{{Name: a, PK: true}, {Name: b}}
}
