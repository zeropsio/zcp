package ops

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// pngOf is a real 2×2 picture.
func pngOf(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 230, G: 100, B: 50, A: 255})
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

const smallSVG = `<?xml version="1.0"?><!-- a dot --><svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"><circle r="1"/></svg>`

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

func publish(t *testing.T, cwd string, in PageInput) (*Page, string) {
	t.Helper()
	page, err := PublishPage(context.Background(), filepath.Join(cwd, ".zcp", "state"), cwd, in, nil)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	written, err := os.ReadFile(page.File)
	if err != nil {
		t.Fatalf("read the written page: %v", err)
	}
	return page, string(written)
}

var dataURI = regexp.MustCompile(`data:(image/[a-z+]+);base64,([A-Za-z0-9+/=]+)`)

func TestPublishPage_RealImages_InlinedAsDataURIs(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	writeFile(t, filepath.Join(cwd, "shots", "home.png"), pngOf(t))
	writeFile(t, filepath.Join(cwd, "art", "dot.svg"), []byte(smallSVG))

	tests := []struct {
		name  string
		html  string
		mimes []string
	}{
		{name: "an img by a path relative to the project", html: `<img alt="home" src="shots/home.png">`, mimes: []string{"image/png"}},
		{
			name:  "an absolute path in an img and a CSS url() in a style element and attribute",
			html:  `<img src='` + filepath.Join(cwd, "art", "dot.svg") + `'><style>.a{background:url("art/dot.svg")}</style><div style="background:url(shots/home.png)"></div>`,
			mimes: []string{"image/svg+xml", "image/svg+xml", "image/png"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			page, written := publish(t, cwd, PageInput{Title: "Shots", HTML: tt.html})
			found := dataURI.FindAllStringSubmatch(written, -1)
			if len(found) != len(tt.mimes) || page.Images != len(tt.mimes) {
				t.Fatalf("inlined %d (images=%d), want %d:\n%s", len(found), page.Images, len(tt.mimes), written)
			}
			for i, match := range found {
				if match[1] != tt.mimes[i] {
					t.Errorf("picture %d is %s, want %s", i, match[1], tt.mimes[i])
				}
				if match[1] == "image/png" {
					data, _ := base64.StdEncoding.DecodeString(match[2])
					if _, err := png.Decode(bytes.NewReader(data)); err != nil {
						t.Errorf("the inlined picture does not decode: %v", err)
					}
				}
			}
			if page.Bytes != len(written) {
				t.Errorf("bytes = %d, the file holds %d", page.Bytes, len(written))
			}
		})
	}
}

// Only a picture's pixels ride along: bytes appended to a real picture never
// reach the page.
func TestPublishPage_BytesBehindAPicture_NeverRideAlong(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	writeFile(t, filepath.Join(cwd, "smuggle.png"), append(pngOf(t), []byte("TRAILING-WORDS-NOT-A-PICTURE")...))
	_, written := publish(t, cwd, PageInput{Title: "x", HTML: `<img src="smuggle.png">`})
	for _, match := range dataURI.FindAllStringSubmatch(written, -1) {
		data, _ := base64.StdEncoding.DecodeString(match[2])
		if bytes.Contains(data, []byte("TRAILING-WORDS")) {
			t.Fatal("the bytes behind the picture reached the page")
		}
	}
}

func TestPublishPage_Refusals(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	stateDir := filepath.Join(cwd, ".zcp", "state")
	picture := pngOf(t)
	writeFile(t, filepath.Join(cwd, "words.png"), []byte("only words, no picture\n"))
	writeFile(t, filepath.Join(cwd, "half.png"), append(append([]byte{}, picture[:40]...), bytes.Repeat([]byte{0}, 600)...))
	writeFile(t, filepath.Join(cwd, "big.png"), append(append([]byte{}, picture...), make([]byte, 2048)...))
	writeFile(t, filepath.Join(cwd, "a.png"), picture)
	writeFile(t, filepath.Join(cwd, "b.png"), picture)
	outside := filepath.Join(t.TempDir(), "elsewhere.png")
	writeFile(t, outside, picture)
	if err := os.Symlink(outside, filepath.Join(cwd, "link.png")); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		in    PageInput
		cap   int
		code  string
		words string
	}{
		{name: "a file whose bytes are not a picture", in: PageInput{Title: "x", HTML: `<img src="words.png">`}, code: platform.ErrInvalidParameter, words: "words.png is not a picture"},
		{name: "a file that only opens like a picture", in: PageInput{Title: "x", HTML: `<img src="half.png">`}, code: platform.ErrInvalidParameter, words: "half.png is not a picture"},
		{name: "a picture that is not there", in: PageInput{Title: "x", HTML: `<img src="gone.png">`}, code: platform.ErrFileNotFound, words: "gone.png"},
		{name: "a picture outside the project", in: PageInput{Title: "x", HTML: `<img src="` + outside + `">`}, code: platform.ErrInvalidParameter, words: "outside the project"},
		{name: "a picture that links elsewhere", in: PageInput{Title: "x", HTML: `<img src="link.png">`}, code: platform.ErrInvalidParameter, words: "link.png is not a file"},
		{name: "a page over the cap", in: PageInput{Title: "x", HTML: strings.Repeat("a", 1025)}, code: platform.ErrInvalidParameter, words: "over"},
		{name: "a picture over the cap", in: PageInput{Title: "x", HTML: `<img src="big.png">`}, code: platform.ErrInvalidParameter, words: "over"},
		{name: "pictures each under the cap, over it together", in: PageInput{Title: "x", HTML: `<img src="a.png"><img src="b.png">`}, cap: len(picture) * 3 / 2, code: platform.ErrInvalidParameter, words: "b.png takes the page over"},
		{name: "no title", in: PageInput{HTML: "<p>hi</p>"}, code: platform.ErrInvalidParameter, words: "title"},
		{name: "neither html nor a path", in: PageInput{Title: "x"}, code: platform.ErrInvalidParameter, words: "html or path"},
		{name: "both html and a path", in: PageInput{Title: "x", HTML: "<p>hi</p>", Path: "page.html"}, code: platform.ErrInvalidParameter, words: "html or path"},
		{name: "a path that is not there", in: PageInput{Title: "x", Path: "nope.html"}, code: platform.ErrFileNotFound, words: "nope.html"},
		{name: "a path outside the project", in: PageInput{Title: "x", Path: outside}, code: platform.ErrInvalidParameter, words: "outside the project"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			limit := 1024
			if tt.cap != 0 {
				limit = tt.cap
			}
			_, err := publishPage(context.Background(), stateDir, cwd, tt.in, nil, pageLimits{maxBytes: limit}, time.Now())
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

// A FIFO named as a picture never blocks the publish.
func TestPublishPage_AFifo_RefusedWithoutWaiting(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("no FIFOs")
	}
	cwd := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(cwd, "pipe.png"), 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := PublishPage(context.Background(), filepath.Join(cwd, ".zcp", "state"), cwd, PageInput{Title: "x", HTML: `<img src="pipe.png">`}, nil)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "pipe.png is not a file") {
			t.Errorf("err = %v, want the FIFO refused", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the publish blocked on a FIFO")
	}
}

// What a script or a template says is never read as a picture: ordinary
// pages carry bundles and templates.
func TestPublishPage_ScriptsAndTemplates_Publish(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	bundle := `<script>/* a bundle */var a=function(u){return new URL(u,location.href)};` +
		`var css="background:url(" + x + ")";var b=c.toDataURL("image/png");` +
		"var t=`<img src=\"${logo}\">`;el.innerHTML='<img src=\"chart.png\">';var d=url;</script>"
	html := `<!doctype html><html><head>` + bundle + `<style>.k{color:red}</style></head>` +
		`<body><p>Write url(notes.png) in prose</p><img src="{{ logo }}"><!-- <img src="old.png"> --></body></html>`
	page, written := publish(t, cwd, PageInput{Title: "App", HTML: html})
	if written != html {
		t.Errorf("the page changed:\n%s", written)
	}
	if page.Images != 0 {
		t.Errorf("images = %d, want none", page.Images)
	}
	if strings.Join(page.WontLoad, " ") != "{{ logo }}" {
		t.Errorf("wontLoad = %v, want the template reference named", page.WontLoad)
	}
}

func TestPublishPage_FromPath_ImagesResolveBesideTheFile(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	writeFile(t, filepath.Join(cwd, "report", "chart.png"), pngOf(t))
	writeFile(t, filepath.Join(cwd, "report", "index.html"), []byte(`<h1>Plan</h1><img src="./chart.png">`))
	page, written := publish(t, cwd, PageInput{Title: "Plan", Path: "report/index.html"})
	if !strings.Contains(written, "data:image/png;base64,") || page.Images != 1 {
		t.Errorf("the chart beside the file was not inlined:\n%s", written)
	}
}

func TestPublishPage_RemoteAndDataReferences_LeftAndNamed(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	html := `<img src="https://example.com/a.png"><img src="data:image/png;base64,AAAA">` +
		`<script src="//cdn.example.com/x.js"></script><link rel="stylesheet" href="https://example.com/s.css">` +
		`<a href="https://example.com/doc">doc</a><img src="#frag">`
	page, written := publish(t, cwd, PageInput{Title: "Links", HTML: html})
	if written != html {
		t.Errorf("the page changed:\n%s", written)
	}
	want := []string{"https://example.com/a.png", "//cdn.example.com/x.js", "https://example.com/s.css"}
	if strings.Join(page.WontLoad, " ") != strings.Join(want, " ") {
		t.Errorf("wontLoad = %v, want %v (a link is a click, not a load)", page.WontLoad, want)
	}
}

func TestPublishPage_Height_MeasuredAsTheConversationDrawsIt(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	stateDir := filepath.Join(cwd, ".zcp", "state")
	var measured string
	measure := func(_ context.Context, file string) int {
		data, _ := os.ReadFile(file)
		measured = string(data)
		return 612
	}
	page, err := PublishPage(context.Background(), stateDir, cwd, PageInput{Title: "Plan", HTML: "<p>plan</p>"}, measure)
	if err != nil {
		t.Fatal(err)
	}
	if page.Height != 612 {
		t.Errorf("height = %d, want the measured 612", page.Height)
	}
	if !strings.Contains(measured, "default-src 'none'") || !strings.HasSuffix(measured, "<p>plan</p>") {
		t.Errorf("measured %q, want the page behind the conversation's policy", measured)
	}
	entries, _ := os.ReadDir(filepath.Join(stateDir, "pages"))
	if len(entries) != 1 {
		t.Errorf("pages holds %d files, want the page alone", len(entries))
	}
	unmeasured, _ := PublishPage(context.Background(), stateDir, cwd, PageInput{Title: "Plan", HTML: "<p>other</p>"}, nil)
	if unmeasured.Height != 0 {
		t.Errorf("height without a browser = %d, want none", unmeasured.Height)
	}
}

func TestPublishPage_WritesUnderPages_ByContent(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	stateDir := filepath.Join(cwd, ".zcp", "state")
	first, _ := publish(t, cwd, PageInput{Title: "  Plan  ", HTML: "<p>one</p>"})
	if filepath.Dir(first.File) != filepath.Join(stateDir, "pages") || !strings.HasPrefix(first.ID, "page-") ||
		filepath.Base(first.File) != first.ID+".html" {
		t.Errorf("page %+v is not <stateDir>/pages/<id>.html", first)
	}
	if first.Title != "Plan" {
		t.Errorf("title = %q, want it trimmed", first.Title)
	}
	again, _ := publish(t, cwd, PageInput{Title: "Plan", HTML: "<p>one</p>"})
	other, _ := publish(t, cwd, PageInput{Title: "Plan", HTML: "<p>two</p>"})
	if again.ID != first.ID || other.ID == first.ID {
		t.Errorf("ids %s, %s, %s: the same page is one file, another is another", first.ID, again.ID, other.ID)
	}
}

func TestPublishPage_OldPages_Pruned(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	old, _ := publish(t, cwd, PageInput{Title: "Old", HTML: "<p>old</p>"})
	long := time.Now().Add(-PageKeepFor - time.Hour)
	if err := os.Chtimes(old.File, long, long); err != nil {
		t.Fatal(err)
	}
	publish(t, cwd, PageInput{Title: "New", HTML: "<p>new</p>"})
	if _, err := os.Stat(old.File); !os.IsNotExist(err) {
		t.Errorf("a page older than %s is still kept", PageKeepFor)
	}
}
