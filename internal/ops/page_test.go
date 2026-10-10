package ops

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// A 1×1 PNG: the smallest real image a page can carry.
var onePixelPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0xf8, 0xcf, 0xc0, 0xf0,
	0x1f, 0x00, 0x05, 0x00, 0x01, 0xff, 0x89, 0x99, 0x3d, 0x1d, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45,
	0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

const smallSVG = `<?xml version="1.0"?><!-- a dot --><svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"/>`

func pageErrorCode(t *testing.T, err error) string {
	t.Helper()
	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("error %v is not a platform error", err)
	}
	return pe.Code
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPublishPage_RealImages_InlinedAsDataURIs(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	stateDir := filepath.Join(cwd, ".zcp", "state")
	writeFile(t, filepath.Join(cwd, "shots", "home.png"), onePixelPNG)
	abs := filepath.Join(t.TempDir(), "dot.svg")
	writeFile(t, abs, []byte(smallSVG))

	tests := []struct {
		name string
		html string
		want []string
	}{
		{
			name: "an img by a path relative to the project",
			html: `<img alt="home" src="shots/home.png">`,
			want: []string{`src="data:image/png;base64,` + base64.StdEncoding.EncodeToString(onePixelPNG) + `"`},
		},
		{
			name: "a single-quoted absolute path and a CSS url()",
			html: `<img src='` + abs + `'><div style="background:url(` + abs + `)"></div>`,
			want: []string{
				`src='data:image/svg+xml;base64,` + base64.StdEncoding.EncodeToString([]byte(smallSVG)) + `'`,
				`url(data:image/svg+xml;base64,` + base64.StdEncoding.EncodeToString([]byte(smallSVG)) + `)`,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			page, err := PublishPage(stateDir, cwd, PageInput{Title: "Shots", HTML: tt.html})
			if err != nil {
				t.Fatalf("publish: %v", err)
			}
			written, err := os.ReadFile(page.File)
			if err != nil {
				t.Fatalf("read the written page: %v", err)
			}
			for _, want := range tt.want {
				if !strings.Contains(string(written), want) {
					t.Errorf("page lacks %q:\n%s", want, written)
				}
			}
			if page.Images != len(tt.want) {
				t.Errorf("images = %d, want %d", page.Images, len(tt.want))
			}
			if page.Bytes != len(written) {
				t.Errorf("bytes = %d, the file holds %d", page.Bytes, len(written))
			}
		})
	}
}

func TestPublishPage_Refusals(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	stateDir := filepath.Join(cwd, ".zcp", "state")
	writeFile(t, filepath.Join(cwd, "secret.png"), []byte("only words, no picture\n"))
	writeFile(t, filepath.Join(cwd, "big.png"), append(append([]byte{}, onePixelPNG...), make([]byte, 2048)...))

	tests := []struct {
		name  string
		in    PageInput
		code  string
		words string
	}{
		{name: "a file whose bytes are not an image", in: PageInput{Title: "x", HTML: `<img src="secret.png">`}, code: platform.ErrInvalidParameter, words: "secret.png is not an image"},
		{name: "an image that is not there", in: PageInput{Title: "x", HTML: `<img src="gone.png">`}, code: platform.ErrFileNotFound, words: "gone.png"},
		{name: "a page over the cap", in: PageInput{Title: "x", HTML: strings.Repeat("a", 1025)}, code: platform.ErrInvalidParameter, words: "over"},
		{name: "a page over the cap once its images are inlined", in: PageInput{Title: "x", HTML: `<img src="big.png">`}, code: platform.ErrInvalidParameter, words: "over"},
		{name: "no title", in: PageInput{HTML: "<p>hi</p>"}, code: platform.ErrInvalidParameter, words: "title"},
		{name: "neither html nor a path", in: PageInput{Title: "x"}, code: platform.ErrInvalidParameter, words: "html or path"},
		{name: "both html and a path", in: PageInput{Title: "x", HTML: "<p>hi</p>", Path: "page.html"}, code: platform.ErrInvalidParameter, words: "html or path"},
		{name: "a path that is not there", in: PageInput{Title: "x", Path: "nope.html"}, code: platform.ErrFileNotFound, words: "nope.html"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := publishPage(stateDir, cwd, tt.in, pageLimits{maxBytes: 1024}, time.Now())
			if err == nil {
				t.Fatal("published, want a refusal")
			}
			if code := pageErrorCode(t, err); code != tt.code {
				t.Errorf("code = %s, want %s (%v)", code, tt.code, err)
			}
			if !strings.Contains(err.Error(), tt.words) {
				t.Errorf("error %q does not say %q", err, tt.words)
			}
		})
	}
	if entries, _ := os.ReadDir(filepath.Join(stateDir, "pages")); len(entries) != 0 {
		t.Errorf("a refused page left %d files behind", len(entries))
	}
}

func TestPublishPage_FromPath_ImagesResolveBesideTheFile(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	stateDir := filepath.Join(cwd, ".zcp", "state")
	writeFile(t, filepath.Join(cwd, "report", "chart.png"), onePixelPNG)
	writeFile(t, filepath.Join(cwd, "report", "index.html"), []byte(`<h1>Plan</h1><img src="./chart.png">`))

	page, err := PublishPage(stateDir, cwd, PageInput{Title: "Plan", Path: "report/index.html"})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	written, _ := os.ReadFile(page.File)
	if !strings.Contains(string(written), "data:image/png;base64,") || page.Images != 1 {
		t.Errorf("the chart beside the file was not inlined:\n%s", written)
	}
}

func TestPublishPage_RemoteAndDataReferences_LeftAndNamed(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	html := `<img src="https://example.com/a.png"><img src="data:image/png;base64,AAAA">` +
		`<script src="//cdn.example.com/x.js"></script><link rel="stylesheet" href="https://example.com/s.css">` +
		`<a href="https://example.com/doc">doc</a><img src="#frag">`
	page, err := PublishPage(filepath.Join(cwd, ".zcp", "state"), cwd, PageInput{Title: "Links", HTML: html})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	written, _ := os.ReadFile(page.File)
	if string(written) != html {
		t.Errorf("the page changed:\n%s", written)
	}
	want := []string{"https://example.com/a.png", "//cdn.example.com/x.js", "https://example.com/s.css"}
	if strings.Join(page.WontLoad, " ") != strings.Join(want, " ") {
		t.Errorf("wontLoad = %v, want %v (a link is a click, not a load)", page.WontLoad, want)
	}
}

func TestPublishPage_WritesUnderPages_ByContent(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	stateDir := filepath.Join(cwd, ".zcp", "state")
	first, err := PublishPage(stateDir, cwd, PageInput{Title: "  Plan  ", HTML: "<p>one</p>"})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if filepath.Dir(first.File) != filepath.Join(stateDir, "pages") || !strings.HasPrefix(first.ID, "page-") ||
		filepath.Base(first.File) != first.ID+".html" {
		t.Errorf("page %+v is not <stateDir>/pages/<id>.html", first)
	}
	if first.Title != "Plan" {
		t.Errorf("title = %q, want it trimmed", first.Title)
	}
	again, _ := PublishPage(stateDir, cwd, PageInput{Title: "Plan", HTML: "<p>one</p>"})
	other, _ := PublishPage(stateDir, cwd, PageInput{Title: "Plan", HTML: "<p>two</p>"})
	if again.ID != first.ID || other.ID == first.ID {
		t.Errorf("ids %s, %s, %s: the same page is one file, another is another", first.ID, again.ID, other.ID)
	}
}

func TestPublishPage_OldPages_Pruned(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	stateDir := filepath.Join(cwd, ".zcp", "state")
	old, err := PublishPage(stateDir, cwd, PageInput{Title: "Old", HTML: "<p>old</p>"})
	if err != nil {
		t.Fatal(err)
	}
	long := time.Now().Add(-PageKeepFor - time.Hour)
	if err := os.Chtimes(old.File, long, long); err != nil {
		t.Fatal(err)
	}
	if _, err := PublishPage(stateDir, cwd, PageInput{Title: "New", HTML: "<p>new</p>"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old.File); !os.IsNotExist(err) {
		t.Errorf("a page older than %s is still kept", PageKeepFor)
	}
}
