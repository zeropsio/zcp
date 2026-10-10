package ops

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
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
	"golang.org/x/image/bmp"
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
	page, err := PublishPage(filepath.Join(cwd, ".zcp", "state"), cwd, in)
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
			_, err := publishPage(stateDir, cwd, tt.in, pageLimits{maxBytes: limit}, time.Now())
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
		_, err := PublishPage(filepath.Join(cwd, ".zcp", "state"), cwd, PageInput{Title: "x", HTML: `<img src="pipe.png">`})
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

// gopherWebP is a real lossless WebP (x/image's testdata gopher-doc.1bpp).
const gopherWebP = "UklGRrIBAABXRUJQVlA4TKUBAAAvSsAYAA8w//M///MfeJAkbXvaSG7m8Q3GfYSBJekwQztm/IcZlgwnmWImn2BK7aFmBtnVir6q//8VOkFE/xm4baTIu8c48ArEo6+B3zFKYln3pqClSCKX0begFTAXFOLXHSyF8cCNcZEG4OywuA4KVVfJCiArU7GAgJI8+lJP/OKMT/fBAjevg1cYB7YVkFuWga2lyPi5I0HFy5YTpWIHg0RZpkniRVW9odHAKOwosWuOGdxIyn2OvaCDvhg/we6TwadPBPbqBV58MsLmMJ8yZnOWk8SRz4N+QoyPL+MnamzMvcE1rHNEr91F9GKZPVUcS9w7PhhH36suB9qPeYb/oLk6cuTiJ0wOK3m5h1cKjW6EVZCYMK7dxcKCBdgP9HkKr9gkAO2P8GKZGWVdIAatQa+1IDpt6qyorVwdy01xdW8Jkfk6xjEXmVQQ+HQdFr6OKhIN34dXWq0+0qr6EJSCeeVLH9+gvGTLyqM65PQ44ihzlTXxQKjKbAvshXgir7Lil9w4L2bvMycmjQcqXaMCO6BlY28i+FOLzbfI1vEqxAhotocAAA=="

// hugePNG is a real PNG header claiming 10000×5000 pixels: refused before a
// byte of it is decoded.
func hugePNG(t *testing.T) []byte {
	t.Helper()
	data := pngOf(t)
	// IHDR's width and height follow the signature (8), the length (4) and the type (4).
	binary.BigEndian.PutUint32(data[16:], 10000)
	binary.BigEndian.PutUint32(data[20:], 5000)
	binary.BigEndian.PutUint32(data[29:], crc32.ChecksumIEEE(data[12:29]))
	return data
}

func TestPublishPage_PicturesAndFonts_ByFormat(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	webp, _ := base64.StdEncoding.DecodeString(gopherWebP)
	writeFile(t, filepath.Join(cwd, "gopher.webp"), webp)
	var bmpBytes bytes.Buffer
	if err := bmp.Encode(&bmpBytes, image.NewRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(cwd, "old.bmp"), bmpBytes.Bytes())
	writeFile(t, filepath.Join(cwd, "icon.ico"), append([]byte{0, 0, 1, 0, 1, 0}, make([]byte, 40)...))
	writeFile(t, filepath.Join(cwd, "fonts", "brand.woff2"), append([]byte("wOF2"), make([]byte, 60)...))
	writeFile(t, filepath.Join(cwd, "entities.svg"), []byte(`<?xml version="1.0"?><!DOCTYPE svg [<!ENTITY a "aaaaaaaaaa"><!ENTITY b "&a;&a;&a;&a;&a;">]><svg xmlns="http://www.w3.org/2000/svg"><text>&b;</text></svg>`))

	html := `<style>/* url(gone-in-a-comment.png) */@font-face{font-family:Brand;src:url("fonts/brand.woff2") format("woff2")}` +
		`.a{background:url(gopher.webp)}</style><img src="old.bmp"><img src="icon.ico"><img src="entities.svg">`
	page, written := publish(t, cwd, PageInput{Title: "Formats", HTML: html})
	found := regexp.MustCompile(`data:([a-z]+/[a-z0-9+]+);base64,`).FindAllStringSubmatch(written, -1)
	mimes := make([]string, 0, len(found))
	for _, match := range found {
		mimes = append(mimes, match[1])
	}
	if want := []string{"font/woff2", "image/png", "image/png", "image/svg+xml"}; strings.Join(mimes, ",") != strings.Join(want, ",") {
		t.Errorf("inlined %v, want the font, the WebP and BMP as PNG, the SVG", mimes)
	}
	if page.Images != 3 || page.Fonts != 1 {
		t.Errorf("images = %d, fonts = %d, want 3 and 1", page.Images, page.Fonts)
	}
	if strings.Join(page.WontLoad, " ") != "icon.ico" {
		t.Errorf("wontLoad = %v, want the ICO nothing here decodes", page.WontLoad)
	}
	if strings.Contains(written, "gone-in-a-comment") {
		t.Error("a CSS comment was kept and read")
	}
}

func TestPublishPage_FontsAndHugePictures_Refused(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	writeFile(t, filepath.Join(cwd, "fake.woff2"), []byte("only words, no font\n"))
	writeFile(t, filepath.Join(cwd, "huge.png"), hugePNG(t))
	tests := []struct {
		name, html, words string
	}{
		{name: "a font whose bytes are no font", html: `<style>@font-face{src:url(fake.woff2)}</style>`, words: "fake.woff2 is not a font"},
		{name: "a picture over 40 million pixels", html: `<img src="huge.png">`, words: "over 40 million pixels"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := PublishPage(filepath.Join(cwd, ".zcp", "state"), cwd, PageInput{Title: "x", HTML: tt.html})
			if err == nil || !strings.Contains(err.Error(), tt.words) {
				t.Errorf("err = %v, want it to say %q", err, tt.words)
			}
		})
	}
}

// A project reached through a link is still the project; a directory in it
// that links out of it is not.
func TestPublishPage_LinkedDirectories_ResolvedBeforeTheProjectCheck(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	project := filepath.Join(base, "real")
	writeFile(t, filepath.Join(project, "shots", "home.png"), pngOf(t))
	if err := os.Symlink(project, filepath.Join(base, "via")); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(base, "outside")
	writeFile(t, filepath.Join(outside, "secret.png"), pngOf(t))
	if err := os.Symlink(outside, filepath.Join(project, "elsewhere")); err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(base, "via")
	if page, _ := publish(t, cwd, PageInput{Title: "x", HTML: `<img src="shots/home.png">`}); page.Images != 1 {
		t.Errorf("a picture in a project reached through a link was not inlined")
	}
	_, err := PublishPage(filepath.Join(cwd, ".zcp", "state"), cwd, PageInput{Title: "x", HTML: `<img src="elsewhere/secret.png">`})
	if err == nil || !strings.Contains(err.Error(), "outside the project") {
		t.Errorf("err = %v, want a directory that links out refused", err)
	}
}

// manyFrameGIF is a GIF of frames frames, each width×height, whose image data
// is a stub: its frames' pixels are counted from their descriptors, before a
// byte of it is decoded.
func manyFrameGIF(frames, width, height int) []byte {
	var out bytes.Buffer
	out.WriteString("GIF89a")
	_ = binary.Write(&out, binary.LittleEndian, [2]uint16{uint16(width), uint16(height)})
	out.Write([]byte{0, 0, 0})
	for range frames {
		out.WriteByte(0x2C)
		_ = binary.Write(&out, binary.LittleEndian, [4]uint16{0, 0, uint16(width), uint16(height)})
		out.Write([]byte{0x80, 0, 0, 0, 255, 255, 255}) // a two-colour local table
		out.Write([]byte{2, 1, 0x44, 0})                // LZW minimum code size, one sub-block, end
	}
	out.WriteByte(0x3B)
	return out.Bytes()
}

func TestPublishPage_AnimatedGIF_PixelsCountedAcrossFrames(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	frame := image.NewPaletted(image.Rect(0, 0, 4, 4), color.Palette{color.Black, color.White})
	var animated bytes.Buffer
	if err := gif.EncodeAll(&animated, &gif.GIF{Image: []*image.Paletted{frame, frame}, Delay: []int{10, 10}}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(cwd, "spinner.gif"), animated.Bytes())
	writeFile(t, filepath.Join(cwd, "bomb.gif"), manyFrameGIF(41, 1000, 1000))

	if _, written := publish(t, cwd, PageInput{Title: "x", HTML: `<img src="spinner.gif">`}); !strings.Contains(written, "data:image/gif;base64,") {
		t.Error("an ordinary animated GIF was not inlined")
	}
	_, err := PublishPage(filepath.Join(cwd, ".zcp", "state"), cwd, PageInput{Title: "x", HTML: `<img src="bomb.gif">`})
	if err == nil || !strings.Contains(err.Error(), "over 40 million pixels across its frames") {
		t.Errorf("err = %v, want 41 frames of a million pixels refused before they are decoded", err)
	}
}
