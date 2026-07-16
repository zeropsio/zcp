// Package object is the S3 / object-storage provider — the v1 spine and the
// only Tier-1 (no-VPN) family: the Zerops object-storage apiUrl is a public
// HTTPS endpoint, so this provider works without the project VPN. It implements
// provider.ObjectProvider over a single fixed bucket (Zerops gives one bucket
// per service), browsing prefixes as folders and objects as leaves.
package object

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// Config is the resolved object-storage connection, built from the core typed
// descriptor at the composition root.
type Config struct {
	Endpoint       string // host[:port], no scheme
	AccessKey      string
	SecretKey      string
	Bucket         string
	Secure         bool
	MaxInlineBytes int64
	ReadOnly       bool
}

// Provider browses + edits one S3 bucket.
type Provider struct {
	cli    *minio.Client
	bucket string
	caps   provider.Capabilities
}

// New builds the provider from a resolved Config.
func New(cfg Config) (*Provider, error) {
	if cfg.Endpoint == "" || cfg.Bucket == "" {
		return nil, fmt.Errorf("object: %w: missing endpoint/bucket", provider.ErrInvalid)
	}
	cli, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.Secure,
	})
	if err != nil {
		return nil, fmt.Errorf("object: client: %w", provider.ErrUpstream)
	}
	maxInline := cfg.MaxInlineBytes
	if maxInline <= 0 {
		maxInline = 16 << 20 // 16 MiB
	}
	return &Provider{
		cli:    cli,
		bucket: cfg.Bucket,
		caps: provider.Capabilities{
			Family:         provider.FamilyObject,
			Support:        provider.SupportFull,
			EditBlob:       !cfg.ReadOnly,
			Upload:         !cfg.ReadOnly,
			MaxInlineBytes: maxInline,
			ReadOnly:       cfg.ReadOnly,
		},
	}, nil
}

func (p *Provider) Kind() string                { return "object-storage" }
func (p *Provider) Caps() provider.Capabilities { return p.caps }
func (p *Provider) Close() error                { return nil }

// Health lists one object to prove the endpoint + credentials work (the
// per-family preflight: reachable + authorized, not just a TCP open).
func (p *Provider) Health(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	for range p.cli.ListObjects(ctx, p.bucket, minio.ListObjectsOptions{MaxKeys: 1}) {
		break
	}
	// A ListObjects error surfaces on the channel item's Err; do a cheap
	// BucketExists to force an authenticated round-trip.
	if _, err := p.cli.BucketExists(ctx, p.bucket); err != nil {
		return provider.HealthErr("object: health", err)
	}
	return nil
}

// prefix joins the path segments into an S3 key prefix.
func (p *Provider) prefix(path provider.Path) string {
	return strings.Join(path.Segments, "/")
}

// List returns the prefixes (folders) + objects (leaves) directly under path,
// using a delimiter so the listing is one level deep. The cursor is the last
// key seen (StartAfter) so "load more" pages forward.
func (p *Provider) List(ctx context.Context, path provider.Path, page provider.Page) ([]provider.Node, string, error) {
	pre := p.prefix(path)
	if pre != "" && !strings.HasSuffix(pre, "/") {
		pre += "/"
	}
	limit := page.Limit
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	opts := minio.ListObjectsOptions{Prefix: pre, Recursive: false, StartAfter: page.Cursor}
	var nodes []provider.Node
	var last string
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	for info := range p.cli.ListObjects(ctx, p.bucket, opts) {
		if info.Err != nil {
			return nil, "", fmt.Errorf("object: list: %w", provider.ErrUpstream)
		}
		if info.Key == pre {
			continue
		}
		last = info.Key
		if strings.HasSuffix(info.Key, "/") {
			name := strings.TrimSuffix(strings.TrimPrefix(info.Key, pre), "/")
			nodes = append(nodes, provider.Node{
				Name:        name,
				Kind:        provider.KindContainer,
				Path:        childPath(path, name),
				HasChildren: true,
			})
		} else {
			name := strings.TrimPrefix(info.Key, pre)
			nodes = append(nodes, provider.Node{
				Name: name,
				Kind: provider.KindBlob,
				Path: childPath(path, name),
				Meta: map[string]any{"size": info.Size, "modified": info.LastModified},
			})
		}
		if len(nodes) >= limit {
			break
		}
	}
	next := ""
	if len(nodes) >= limit {
		next = last
	}
	return nodes, next, nil
}

// Stat returns metadata for one object.
func (p *Provider) Stat(ctx context.Context, path provider.Path) (provider.Node, error) {
	key := p.prefix(path)
	info, err := p.cli.StatObject(ctx, p.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return provider.Node{}, mapErr(err)
	}
	return provider.Node{
		Name: lastSegment(path),
		Kind: provider.KindBlob,
		Path: path,
		Meta: map[string]any{"size": info.Size, "contentType": info.ContentType, "etag": info.ETag},
	}, nil
}

// ReadBlob returns the object bytes; objects larger than MaxInlineBytes come
// back as a head-slice with Truncated=true (view-only).
func (p *Provider) ReadBlob(ctx context.Context, path provider.Path) ([]byte, provider.BlobMeta, error) {
	key := p.prefix(path)
	info, err := p.cli.StatObject(ctx, p.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return nil, provider.BlobMeta{}, mapErr(err)
	}
	meta := provider.BlobMeta{ContentType: info.ContentType, Size: info.Size}
	getOpts := minio.GetObjectOptions{}
	if info.Size > p.caps.MaxInlineBytes {
		meta.Truncated = true
		if rerr := getOpts.SetRange(0, p.caps.MaxInlineBytes-1); rerr != nil {
			return nil, meta, fmt.Errorf("object: range: %w", provider.ErrInvalid)
		}
	}
	obj, err := p.cli.GetObject(ctx, p.bucket, key, getOpts)
	if err != nil {
		return nil, meta, mapErr(err)
	}
	defer func() { _ = obj.Close() }()
	limit := info.Size
	if meta.Truncated {
		limit = p.caps.MaxInlineBytes
	}
	data, err := io.ReadAll(io.LimitReader(obj, limit))
	if err != nil {
		return nil, meta, fmt.Errorf("object: read: %w", provider.ErrUpstream)
	}
	return data, meta, nil
}

// WriteBlob uploads/replaces an object (PutObject; multipart handled by the
// SDK). contentType becomes the object's S3 Content-Type metadata — minio-go
// itself defaults an empty ContentType to "application/octet-stream" (see
// PutObjectOptions.Header), which is exactly the degraded value OBJ-AUD-01
// found on every console-originated write: the next read's isTextual/isImage
// classification (dc-format.js) never matches, so an uploaded image loses its
// preview and an edited text file loses its own editability. The caller
// (server.go) is responsible for never handing this an empty string when a
// real type is knowable.
func (p *Provider) WriteBlob(ctx context.Context, path provider.Path, data []byte, contentType string) error {
	if p.caps.ReadOnly {
		return provider.ErrReadOnly
	}
	key := p.prefix(path)
	_, err := p.cli.PutObject(ctx, p.bucket, key, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return mapErr(err)
	}
	return nil
}

// Delete removes an object. S3's DELETE is spec-idempotent — a 204 whether or
// not the key ever existed — so without an existence check first, Delete would
// report {"ok":true} for a key that was never there, indistinguishable from a
// real deletion (OBJ-AUD-02). Stat-first makes this honest and matches
// Rename's existing 404-on-missing behavior on the identical condition. This
// is a check-then-act (a concurrent delete between the two calls is possible,
// same as Rename's existing copy-then-remove), an accepted tradeoff for
// turning a silent no-op into an honest error.
func (p *Provider) Delete(ctx context.Context, path provider.Path) error {
	if p.caps.ReadOnly {
		return provider.ErrReadOnly
	}
	key := p.prefix(path)
	if _, err := p.cli.StatObject(ctx, p.bucket, key, minio.StatObjectOptions{}); err != nil {
		return mapErr(err)
	}
	if err := p.cli.RemoveObject(ctx, p.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return mapErr(err)
	}
	return nil
}

// Rename copies an object to a new key then removes the source (S3 has no atomic
// rename). Gated by the read-only capability.
func (p *Provider) Rename(ctx context.Context, from, to provider.Path) error {
	if p.caps.ReadOnly {
		return provider.ErrReadOnly
	}
	src, dst := p.prefix(from), p.prefix(to)
	if _, err := p.cli.CopyObject(ctx,
		minio.CopyDestOptions{Bucket: p.bucket, Object: dst},
		minio.CopySrcOptions{Bucket: p.bucket, Object: src}); err != nil {
		return mapErr(err)
	}
	if err := p.cli.RemoveObject(ctx, p.bucket, src, minio.RemoveObjectOptions{}); err != nil {
		return mapErr(err)
	}
	return nil
}

func childPath(parent provider.Path, name string) provider.Path {
	seg := make([]string, len(parent.Segments)+1)
	copy(seg, parent.Segments)
	seg[len(parent.Segments)] = name
	return provider.Path{Service: parent.Service, Segments: seg}
}

func lastSegment(path provider.Path) string {
	if len(path.Segments) == 0 {
		return ""
	}
	return path.Segments[len(path.Segments)-1]
}

// mapErr collapses an S3 error to a sanitized sentinel — never leaks the
// endpoint/key/credentials embedded in the driver's error text.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	resp := minio.ToErrorResponse(err)
	switch resp.Code {
	case "NoSuchKey", "NoSuchBucket":
		return provider.ErrNotFound
	case "AccessDenied", "SignatureDoesNotMatch", "InvalidAccessKeyId":
		return fmt.Errorf("object: access denied: %w", provider.ErrUpstream)
	}
	if errors.Is(err, context.Canceled) {
		// A canceled request surfaces from net/http as a *url.Error carrying the
		// full request URL (bucket/key/query) in its Error() text — re-wrap the
		// sentinel instead of returning err verbatim, so cancellation stays
		// distinguishable via errors.Is without leaking that URL.
		return fmt.Errorf("object: %w", context.Canceled)
	}
	return fmt.Errorf("object: %w", provider.ErrUpstream)
}
