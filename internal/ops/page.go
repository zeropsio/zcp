package ops

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
	_ "golang.org/x/image/bmp"  // registers BMP with image.Decode
	_ "golang.org/x/image/webp" // registers WebP with image.Decode
)

// A Mate's pages: a self-contained HTML document the agent publishes for the
// person to see in its conversation, above its reply — a chart, a
// comparison, a plan, a gallery. The page is kept as one file under the state
// dir, named by its content (`pages/page-<digest>.html`), and the tool's
// result names that file; the Mate server reads it once, when it records the
// call, into its own asset store (docs/spec-mate.md §5.9), so a page here is
// kept only for PageKeepFor.
//
// The conversation draws the page in a sandboxed frame whose policy lets it
// request nothing — no fetch, no script, style, picture or font from
// anywhere — so everything it shows must be inside it: every local picture an
// <img> or a CSS url() names is inlined as a data: URI, re-encoded from its
// decoded pixels so nothing but a picture rides along (a secret renamed .png
// is refused), as is every local font an @font-face names; every remote
// resource it would load, and every picture in a format nothing here decodes,
// is named back to the agent as one that will not load. Scripts and comments
// are never read for pictures: a bundle's `url(` or a template's
// `<img src="${x}">` is code.

// PageMaxBytes caps a page, its pictures inlined.
const PageMaxBytes = 8 << 20

// PageKeepFor is how long a page file is kept: the Mate server takes its own
// copy as it records the call.
const PageKeepFor = 7 * 24 * time.Hour

// PageMaxPixels caps a picture's size: a larger one is refused before it is
// decoded, so a small file that decodes to gigabytes never does.
const PageMaxPixels = 40_000_000

// pageTitleMax caps a page's title, in characters.
const pageTitleMax = 120

const pagesDir = "pages"

// PageInput is what the agent publishes: a title and the page, given whole
// (HTML) or as a file (Path, in the project).
type PageInput struct {
	Title string
	HTML  string
	Path  string
}

// Page is a published page.
type Page struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// File is where the page is kept until the Mate server takes it.
	File  string `json:"file"`
	Bytes int    `json:"bytes"`
	// Images is how many local pictures were inlined, Fonts how many fonts.
	Images int `json:"images"`
	Fonts  int `json:"fonts,omitempty"`
	// WontLoad is every resource the page would load that the conversation
	// will not: a remote one, a reference that names no file, a picture in a
	// format nothing here decodes.
	WontLoad []string `json:"wontLoad,omitempty"`
}

type pageLimits struct {
	maxBytes int
}

// PublishPage validates in, inlines its local pictures and fonts, and keeps
// it as a page under stateDir. Every file it reads is in the project, cwd.
func PublishPage(stateDir, cwd string, in PageInput) (*Page, error) {
	return publishPage(stateDir, cwd, in, pageLimits{maxBytes: PageMaxBytes}, time.Now())
}

func pageRefusal(format string, args ...any) error {
	return platform.NewPlatformError(platform.ErrInvalidParameter, fmt.Sprintf(format, args...),
		"Publish a self-contained page: its pictures (PNG, JPEG, GIF, WebP, BMP, SVG) and fonts as files in the project, its scripts and styles inline.")
}

func publishPage(stateDir, cwd string, in PageInput, limits pageLimits, now time.Time) (*Page, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, pageRefusal("A page needs a title")
	}
	if n := len([]rune(title)); n > pageTitleMax {
		return nil, pageRefusal("The title is %d characters, over %d", n, pageTitleMax)
	}
	if (in.HTML == "") == (in.Path == "") {
		return nil, pageRefusal("Give the page as html or path, one of them")
	}
	reader := &projectReader{root: cwd, maxBytes: limits.maxBytes}
	html, base := in.HTML, cwd
	if in.Path != "" {
		file := in.Path
		if !filepath.IsAbs(file) {
			file = filepath.Join(cwd, file)
		}
		read, err := reader.read(in.Path, file)
		if err != nil {
			return nil, err
		}
		html, base = string(read), filepath.Dir(file)
	}
	if len(html) > limits.maxBytes {
		return nil, pageRefusal("The page is %d bytes, over the %d byte cap", len(html), limits.maxBytes)
	}

	inlined, counts, unnamed, err := inlinePictures(html, base, reader)
	if err != nil {
		return nil, err
	}
	if len(inlined) > limits.maxBytes {
		return nil, pageRefusal("With its pictures inlined the page is %d bytes, over the %d byte cap", len(inlined), limits.maxBytes)
	}

	digest := sha256.Sum256([]byte(inlined))
	id := "page-" + hex.EncodeToString(digest[:8])
	dir := filepath.Join(stateDir, pagesDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("publish page: %w", err)
	}
	prunePages(dir, now)
	file := filepath.Join(dir, id+".html")
	if err := writeAtomically(file, []byte(inlined)); err != nil {
		return nil, fmt.Errorf("publish page: %w", err)
	}
	page := &Page{
		ID:       id,
		Title:    title,
		File:     file,
		Bytes:    len(inlined),
		Images:   counts.images,
		Fonts:    counts.fonts,
		WontLoad: limitRefs(append(unnamed, remoteLoads(inlined)...)),
	}
	return page, nil
}

// The places a page names a picture: an <img>'s src (any quoting) in its
// markup, and a CSS url() in a <style> element or a style attribute.
var (
	imgSrc    = regexp.MustCompile(`(?i)(<img\b[^>]*?\bsrc\s*=\s*)("[^"]*"|'[^']*'|[^\s"'>]+)`)
	cssURL    = regexp.MustCompile(`(?i)(url\(\s*)("[^"]*"|'[^']*'|[^)"'\s]+)(\s*\))`)
	styleAttr = regexp.MustCompile(`(?i)(\sstyle\s*=\s*)("[^"]*"|'[^']*')`)
	// opaque is what is never read for pictures — a script's or a comment's
	// text — and a style element's text, read as CSS.
	opaque = regexp.MustCompile(`(?is)(<script\b[^>]*>)(.*?)(</script\s*>)|(<style\b[^>]*>)(.*?)(</style\s*>)|<!--.*?-->`)
)

// remoteRef is a reference the conversation's frame never loads.
var remoteRef = regexp.MustCompile(`(?i)^(https?:)?//`)

// notLocal is a reference that names no file: a URL, inline data, a fragment.
var notLocal = regexp.MustCompile(`(?i)^([a-z][a-z0-9+.-]*:|//|#)`)

// filePath is a reference that reads as a file's path; anything else — a
// template's `{{ logo }}`, a `${x}` — names no file.
var filePath = regexp.MustCompile(`^[\w./~@%+-][\w ./~@%+,=-]*$`)

func unquote(value string) (string, string) {
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
		return value[1 : len(value)-1], value[:1]
	}
	return value, ""
}

// eachSegment rewrites html's markup with markup and its style elements'
// text with css, leaving scripts and comments as they are.
func eachSegment(html string, markup, css func(string) string) string {
	var out strings.Builder
	at := 0
	for _, m := range opaque.FindAllStringSubmatchIndex(html, -1) {
		out.WriteString(markup(html[at:m[0]]))
		switch {
		case m[2] >= 0: // a script: its start tag is markup, its text is code
			out.WriteString(markup(html[m[2]:m[3]]))
			out.WriteString(html[m[4]:m[1]])
		case m[8] >= 0: // a style element: its text is CSS
			out.WriteString(markup(html[m[8]:m[9]]))
			out.WriteString(css(html[m[10]:m[11]]))
			out.WriteString(html[m[12]:m[13]])
		default: // a comment
			out.WriteString(html[m[0]:m[1]])
		}
		at = m[1]
	}
	out.WriteString(markup(html[at:]))
	return out.String()
}

// inlined counts what a page carries inline.
type inlined struct {
	images, fonts int
}

// errNoDecoder is a picture in a format nothing here decodes (AVIF, ICO):
// named back as one that will not load, never refused.
var errNoDecoder = errors.New("no decoder for its format")

// cssComment is a CSS comment: never read for a url().
var cssComment = regexp.MustCompile(`(?s)/\*.*?\*/`)

// fontFace is an @font-face rule: the url()s in it are fonts.
var fontFace = regexp.MustCompile(`(?is)@font-face\s*\{[^}]*\}`)

// inlinePictures replaces every local picture and font html names with a
// data: URI; relative names resolve in base. It refuses a file that is not
// what it is named as or is outside the project, lists every one it cannot
// find, and answers the references that will not load.
func inlinePictures(html, base string, reader *projectReader) (string, inlined, []string, error) {
	var missing, unnamed []string
	var refused error
	var counts inlined
	cache := map[string]string{}
	replace := func(ref string, font bool) (string, bool) {
		if ref == "" || notLocal.MatchString(ref) {
			return "", false
		}
		if !filePath.MatchString(ref) {
			unnamed = append(unnamed, ref)
			return "", false
		}
		file := ref
		if !filepath.IsAbs(file) {
			file = filepath.Join(base, file)
		}
		count := func() {
			if font {
				counts.fonts++
			} else {
				counts.images++
			}
		}
		if uri, ok := cache[file]; ok {
			count()
			return uri, true
		}
		data, err := reader.read(ref, file)
		if err != nil {
			var pe *platform.PlatformError
			if errors.As(err, &pe) && pe.Code == platform.ErrFileNotFound {
				missing = append(missing, ref)
			} else if refused == nil {
				refused = err
			}
			return "", false
		}
		var mime string
		var carried []byte
		if font {
			if mime = fontMime(data); mime == "" {
				if refused == nil {
					refused = pageRefusal("%s is not a font: its bytes are no WOFF2, WOFF, TTF or OTF", ref)
				}
				return "", false
			}
			carried = data
		} else {
			mime, carried, err = pictureOf(data)
			if errors.Is(err, errNoDecoder) {
				unnamed = append(unnamed, ref)
				return "", false
			}
			if err != nil {
				if refused == nil {
					refused = pageRefusal("%s is not a picture: %v", ref, err)
				}
				return "", false
			}
		}
		uri := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(carried)
		cache[file] = uri
		count()
		return uri, true
	}
	urls := func(css string, font bool) string {
		return cssURL.ReplaceAllStringFunc(css, func(match string) string {
			parts := cssURL.FindStringSubmatch(match)
			ref, quote := unquote(parts[2])
			if uri, ok := replace(strings.TrimSpace(ref), font); ok {
				return parts[1] + quote + uri + quote + parts[3]
			}
			return match
		})
	}
	inCSS := func(css string) string {
		css = cssComment.ReplaceAllString(css, "")
		var out strings.Builder
		at := 0
		for _, m := range fontFace.FindAllStringIndex(css, -1) {
			out.WriteString(urls(css[at:m[0]], false))
			out.WriteString(urls(css[m[0]:m[1]], true))
			at = m[1]
		}
		out.WriteString(urls(css[at:], false))
		return out.String()
	}
	inMarkup := func(markup string) string {
		markup = imgSrc.ReplaceAllStringFunc(markup, func(match string) string {
			parts := imgSrc.FindStringSubmatch(match)
			ref, quote := unquote(parts[2])
			if uri, ok := replace(strings.TrimSpace(ref), false); ok {
				return parts[1] + quote + uri + quote
			}
			return match
		})
		return styleAttr.ReplaceAllStringFunc(markup, func(match string) string {
			parts := styleAttr.FindStringSubmatch(match)
			value, quote := unquote(parts[2])
			return parts[1] + quote + inCSS(value) + quote
		})
	}
	out := eachSegment(html, inMarkup, inCSS)
	if refused != nil {
		return "", inlined{}, nil, refused
	}
	if len(missing) > 0 {
		return "", inlined{}, nil, platform.NewPlatformError(platform.ErrFileNotFound,
			"These files are not there: "+strings.Join(missing, ", "),
			"Name each picture or font by its path in the project, or drop it.")
	}
	return out, counts, unnamed, nil
}

// pageLoads are the places a page's markup would load something from: a
// src, a stylesheet's href; and a CSS url().
var (
	srcAttr        = regexp.MustCompile(`(?i)<[a-z][^>]*?\bsrc\s*=\s*("[^"]*"|'[^']*'|[^\s"'>]+)`)
	stylesheetHref = regexp.MustCompile(`(?i)<link\b[^>]*?\bhref\s*=\s*("[^"]*"|'[^']*'|[^\s"'>]+)`)
)

const wontLoadMax = 10

// remoteLoads is every remote resource html's markup and styles would load,
// in order.
func remoteLoads(html string) []string {
	var found []string
	add := func(ref string) {
		if ref = strings.TrimSpace(ref); remoteRef.MatchString(ref) {
			found = append(found, ref)
		}
	}
	inCSS := func(css string) string {
		for _, match := range cssURL.FindAllStringSubmatch(css, -1) {
			ref, _ := unquote(match[2])
			add(ref)
		}
		return css
	}
	eachSegment(html, func(markup string) string {
		for _, re := range []*regexp.Regexp{srcAttr, stylesheetHref} {
			for _, match := range re.FindAllStringSubmatch(markup, -1) {
				ref, _ := unquote(match[1])
				add(ref)
			}
		}
		for _, match := range styleAttr.FindAllStringSubmatch(markup, -1) {
			value, _ := unquote(match[2])
			inCSS(value)
		}
		return markup
	}, inCSS)
	return found
}

// limitRefs is refs each once, in order, at most wontLoadMax.
func limitRefs(refs []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, ref := range refs {
		if !seen[ref] && len(out) < wontLoadMax {
			seen[ref] = true
			out = append(out, ref)
		}
	}
	return out
}

// fontMime is the font format data's bytes are in, or "" when they are none.
func fontMime(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte("wOF2")):
		return "font/woff2"
	case bytes.HasPrefix(data, []byte("wOFF")):
		return "font/woff"
	case bytes.HasPrefix(data, []byte("OTTO")):
		return "font/otf"
	case bytes.HasPrefix(data, []byte{0, 1, 0, 0}), bytes.HasPrefix(data, []byte("true")):
		return "font/ttf"
	}
	return ""
}

// pictureOf is the picture data holds, as the page carries it: a raster
// picture of at most PageMaxPixels decoded whole and encoded again from its
// pixels, so nothing but the picture rides along (WebP and BMP as PNG); an
// SVG parsed whole as XML. A format nothing here decodes is errNoDecoder.
func pictureOf(data []byte) (string, []byte, error) {
	if isSVG(data) {
		if err := wholeSVG(data); err != nil {
			return "", nil, err
		}
		return "image/svg+xml", data, nil
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		if noDecoder(data) {
			return "", nil, errNoDecoder
		}
		return "", nil, errors.New("its bytes are no PNG, JPEG, GIF, WebP, BMP or SVG")
	}
	if pixels := int64(config.Width) * int64(config.Height); pixels > PageMaxPixels {
		return "", nil, fmt.Errorf("it is %d×%d, over %d million pixels", config.Width, config.Height, PageMaxPixels/1_000_000)
	}
	var out bytes.Buffer
	if format == "gif" {
		all, err := gif.DecodeAll(bytes.NewReader(data))
		if err != nil {
			return "", nil, fmt.Errorf("it does not decode whole: %w", err)
		}
		if err := gif.EncodeAll(&out, all); err != nil {
			return "", nil, fmt.Errorf("encode: %w", err)
		}
		return "image/gif", out.Bytes(), nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", nil, fmt.Errorf("it does not decode whole: %w", err)
	}
	switch format {
	case "jpeg":
		err = jpeg.Encode(&out, img, &jpeg.Options{Quality: 90})
		format = "image/jpeg"
	case "png", "webp", "bmp":
		err = png.Encode(&out, img)
		format = "image/png"
	default:
		return "", nil, errNoDecoder
	}
	if err != nil {
		return "", nil, fmt.Errorf("encode: %w", err)
	}
	return format, out.Bytes(), nil
}

// noDecoder is a picture in a format nothing here decodes: AVIF or ICO.
func noDecoder(data []byte) bool {
	avif := len(data) >= 12 && string(data[4:8]) == "ftyp" &&
		(string(data[8:12]) == "avif" || string(data[8:12]) == "avis")
	return avif || bytes.HasPrefix(data, []byte{0, 0, 1, 0})
}

// svgPrologue is what may stand ahead of an SVG's root: an XML declaration,
// comments, a doctype, whitespace.
var svgPrologue = regexp.MustCompile(`^(?s)\s*(<\?xml[^>]*\?>\s*)?((<!--.*?-->|<!DOCTYPE[^>\[]*(\[[^\]]*\])?>)\s*)*<svg[\s>]`)

func isSVG(data []byte) bool {
	return svgPrologue.Match(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")))
}

// wholeSVG parses data to its end as one XML document with an <svg> root.
// Entities a DOCTYPE declares are passed over, never expanded.
func wholeSVG(data []byte) error {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = false
	depth, roots := 0, 0
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("it is no whole SVG: %w", err)
		}
		switch t := token.(type) {
		case xml.StartElement:
			if depth == 0 {
				roots++
				if t.Name.Local != "svg" {
					return errors.New("its root is no <svg>")
				}
			}
			depth++
		case xml.EndElement:
			depth--
		case xml.CharData:
			if depth == 0 && len(bytes.TrimSpace(t)) > 0 {
				return errors.New("it carries text outside its <svg>")
			}
		}
	}
	if roots != 1 || depth != 0 {
		return errors.New("it is no whole SVG")
	}
	return nil
}

// projectReader reads the files a page names: regular files in the project
// (its real path, links in its directories resolved), never a link, a FIFO
// or a device, within one running byte budget.
type projectReader struct {
	root      string
	maxBytes  int
	readSoFar int
}

func (r *projectReader) read(name, file string) ([]byte, error) {
	root, err := filepath.EvalSymlinks(r.root)
	if err != nil {
		return nil, fmt.Errorf("project root: %w", err)
	}
	abs, err := filepath.Abs(file)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", name, err)
	}
	dir, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, platform.NewPlatformError(platform.ErrFileNotFound, name+" is not there", "Name a file in the project.")
		}
		return nil, fmt.Errorf("resolve %s: %w", name, err)
	}
	resolved := filepath.Join(dir, filepath.Base(abs))
	if rel, err := filepath.Rel(root, resolved); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, pageRefusal("%s is outside the project", name)
	}
	info, err := os.Lstat(resolved)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, platform.NewPlatformError(platform.ErrFileNotFound, name+" is not there", "Name a file in the project.")
		}
		return nil, fmt.Errorf("stat %s: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return nil, pageRefusal("%s is not a file", name)
	}
	// Non-blocking, so a FIFO swapped in after the Lstat never holds the read.
	f, err := os.OpenFile(resolved, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", name, err)
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", name, err)
	}
	if !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, pageRefusal("%s is not a file", name)
	}
	left := r.maxBytes - r.readSoFar
	data, err := io.ReadAll(io.LimitReader(f, int64(left)+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	if len(data) > left {
		return nil, pageRefusal("%s takes the page over the %d byte cap", name, r.maxBytes)
	}
	r.readSoFar += len(data)
	return data, nil
}

// writeAtomically writes data to file in one rename, so a reader never sees
// half a page.
func writeAtomically(file string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(file), ".page-*")
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write: %w", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	if err := os.Rename(tmp.Name(), file); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// prunePages forgets every page kept longer than PageKeepFor. Best-effort: a
// page that cannot be removed now is removed by a later publish.
func prunePages(dir string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "page-") || !strings.HasSuffix(entry.Name(), ".html") {
			continue
		}
		info, err := entry.Info()
		if err == nil && now.Sub(info.ModTime()) > PageKeepFor {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
}
