package seed

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

const (
	objectSeedTimeout    = 60 * time.Second
	objectCleanupTimeout = 60 * time.Second
)

// seedTextObject is one text/JSON fixture object: key + body + content type.
type seedTextObject struct{ key, body, ct string }

// seedImageObject is one generated-PNG fixture object.
type seedImageObject struct {
	key    string
	render func(x, y int) color.Color
	w, h   int
}

func newObjectClient(conn provider.ObjectConn) (*minio.Client, error) {
	cli, err := minio.New(conn.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(conn.AccessKey, conn.SecretKey, ""),
		Secure: conn.Secure,
	})
	if err != nil {
		return nil, fmt.Errorf("client: %w", err)
	}
	return cli, nil
}

// seedObject writes text/JSON/CSV/log objects plus a handful of generated
// PNG images, so the console has something to preview inline for every
// object content-type family. Every key is namespace-derived via
// ObjectName so an empty Namespace reproduces dcseed's original,
// unprefixed object keys exactly. PutObject is an unconditional overwrite
// (S3 has no create-only verb in this call shape), so every write below is
// idempotent by construction — a re-run against an already-seeded key has
// no "already exists" class to hit.
func seedObject(ctx context.Context, conn provider.ObjectConn, opts Options) error {
	cli, err := newObjectClient(conn)
	if err != nil {
		return fmt.Errorf("seed object: %w", err)
	}
	cx, cancel := context.WithTimeout(ctx, objectSeedTimeout)
	defer cancel()

	n := func(base string) string { return ObjectName(opts.Namespace, base) }

	texts := []seedTextObject{
		{n("readme.txt"), "Hello from the Data Console seed.\nThis bucket has sample objects + images.\n", "text/plain"},
		{n("data/config.json"), `{"feature":"data-console","enabled":true,"limit":100}`, "application/json"},
		{n("data/users.csv"), "id,name,email\n1,Alice,alice@example.io\n2,Bob,bob@example.io\n", "text/csv"},
		{n("logs/2026-06-26.log"), "INFO seed ran\nINFO ok\n", "text/plain"},
	}
	for _, o := range texts {
		if _, err := cli.PutObject(cx, conn.Bucket, o.key, strings.NewReader(o.body), int64(len(o.body)), minio.PutObjectOptions{ContentType: o.ct}); err != nil {
			return fmt.Errorf("seed object: put %s: %w", o.key, err)
		}
	}

	imgs := []seedImageObject{
		{n("images/red.png"), func(int, int) color.Color { return color.RGBA{220, 50, 47, 255} }, 96, 96},
		{n("images/gradient.png"), func(x, y int) color.Color { return color.RGBA{uint8(x * 255 / 160), uint8(y * 255 / 96), 160, 255} }, 160, 96},
		{n("images/checker.png"), func(x, y int) color.Color {
			if (x/16+y/16)%2 == 0 {
				return color.RGBA{38, 139, 210, 255}
			}
			return color.RGBA{253, 246, 227, 255}
		}, 128, 128},
		{n("images/circle.png"), func(x, y int) color.Color {
			dx, dy := float64(x-60), float64(y-60)
			if dx*dx+dy*dy <= 50*50 {
				return color.RGBA{133, 153, 0, 255}
			}
			return color.RGBA{0, 43, 54, 255}
		}, 120, 120},
	}
	for _, o := range imgs {
		b := pngImage(o.w, o.h, o.render)
		if _, err := cli.PutObject(cx, conn.Bucket, o.key, bytes.NewReader(b), int64(len(b)), minio.PutObjectOptions{ContentType: "image/png"}); err != nil {
			return fmt.Errorf("seed object: put %s: %w", o.key, err)
		}
	}
	return nil
}

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

// cleanupObject lists every object under the "<namespace>/" prefix (the
// exact shape ObjectName produces) and removes it. ListObjects' own Prefix
// option already restricts the walk to the namespace's own objects;
// ObjectOwns is applied again as a defense-in-depth filter.
func cleanupObject(ctx context.Context, conn provider.ObjectConn, namespace string) error {
	cli, err := newObjectClient(conn)
	if err != nil {
		return fmt.Errorf("seed cleanup object: %w", err)
	}
	cx, cancel := context.WithTimeout(ctx, objectCleanupTimeout)
	defer cancel()

	prefix := namespace + "/"
	var toDelete []string
	for obj := range cli.ListObjects(cx, conn.Bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if obj.Err != nil {
			return fmt.Errorf("seed cleanup object: list: %w", obj.Err)
		}
		if ObjectOwns(namespace, obj.Key) {
			toDelete = append(toDelete, obj.Key)
		}
	}
	for _, key := range toDelete {
		if err := cli.RemoveObject(cx, conn.Bucket, key, minio.RemoveObjectOptions{}); err != nil {
			return fmt.Errorf("seed cleanup object: remove %s: %w", key, err)
		}
	}
	return nil
}
