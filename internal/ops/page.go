package ops

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
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
// is refused), and every remote resource it would load is named back to the
// agent as one that will not load. Scripts and comments are never read for
// pictures: a bundle's `url(` or a template's `<img src="${x}">` is code.

// PageMaxBytes caps a page, its pictures inlined.
const PageMaxBytes = 8 << 20

// PageKeepFor is how long a page file is kept: the Mate server takes its own
// copy as it records the call.
const PageKeepFor = 7 * 24 * time.Hour

// PageMeasureWidth is the width a page is measured at: the Mate's
// conversation column, about.
const PageMeasureWidth = 760

// pageTitleMax caps a page's title, in characters.
const pageTitleMax = 120

const pagesDir = "pages"

// pagePolicy is the conversation's policy for a page (the client's
// PAGE_POLICY): a page is measured as it will be drawn, loading nothing.
const pagePolicy = "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; " +
	"img-src data: blob:; font-src data:; media-src data: blob:; form-action 'none'; base-uri 'none'"

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
	// Height is the page's height in CSS pixels at PageMeasureWidth, as a
	// browser laid it out; absent when no browser measured it.
	Height int `json:"height,omitempty"`
	// Images is how many local pictures were inlined.
	Images int `json:"images"`
	// WontLoad is every resource the page would load that the conversation
	// will not: a remote one, or a reference that names no file.
	WontLoad []string `json:"wontLoad,omitempty"`
}

// PageMeasurer lays out the page in file and answers its height; 0 when it
// cannot.
type PageMeasurer func(ctx context.Context, file string) int

type pageLimits struct {
	maxBytes int
}

// PublishPage validates in, inlines its local pictures, keeps it as a page
// under stateDir, and measures it with measure when one is given. Every file
// it reads is in the project, cwd.
func PublishPage(ctx context.Context, stateDir, cwd string, in PageInput, measure PageMeasurer) (*Page, error) {
	return publishPage(ctx, stateDir, cwd, in, measure, pageLimits{maxBytes: PageMaxBytes}, time.Now())
}

func pageRefusal(format string, args ...any) error {
	return platform.NewPlatformError(platform.ErrInvalidParameter, fmt.Sprintf(format, args...),
		"Publish a self-contained page: its pictures as PNG, JPEG, GIF or SVG files in the project, its scripts and styles inline.")
}

func publishPage(ctx context.Context, stateDir, cwd string, in PageInput, measure PageMeasurer, limits pageLimits, now time.Time) (*Page, error) {
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

	inlined, images, unnamed, err := inlinePictures(html, base, reader)
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
		Images:   images,
		WontLoad: limitRefs(append(unnamed, remoteLoads(inlined)...)),
	}
	if measure != nil {
		page.Height = measurePage(ctx, dir, id, inlined, measure)
	}
	return page, nil
}

// measurePage lays the page out behind the conversation's policy, from a
// file of its own that it removes after.
func measurePage(ctx context.Context, dir, id, html string, measure PageMeasurer) int {
	file := filepath.Join(dir, ".measure-"+id+".html")
	doc := `<!doctype html><html><head><meta http-equiv="Content-Security-Policy" content="` + pagePolicy + `">` +
		`<meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">` +
		`<style>body{margin:0;font-family:system-ui,sans-serif}</style>` + html
	if err := writeAtomically(file, []byte(doc)); err != nil {
		return 0
	}
	defer os.Remove(file)
	if height := measure(ctx, file); height > 0 {
		return height
	}
	return 0
}

// MeasurePageInBrowser measures a page file with the container's browser at
// PageMeasureWidth: the height of its document. 0 when the browser is not
// there, busy or failing — a page is published unmeasured rather than not.
func MeasurePageInBrowser(ctx context.Context, file string) int {
	result, err := BrowserBatch(ctx, BrowserBatchInput{
		URL: "file://" + file,
		Commands: [][]string{
			{"set", "viewport", fmt.Sprint(PageMeasureWidth), "200"},
			{"get", "box", "html"},
		},
		TimeoutSeconds: 30,
	})
	if err != nil || result == nil {
		return 0
	}
	for _, step := range result.Steps {
		if len(step.Command) >= 2 && step.Command[0] == "get" && step.Command[1] == "box" && step.Success {
			var box struct {
				Height float64 `json:"height"`
			}
			if json.Unmarshal(step.Result, &box) == nil && box.Height > 0 {
				return int(box.Height + 0.999)
			}
		}
	}
	return 0
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

// inlinePictures replaces every local picture html names with a data: URI;
// relative names resolve in base. It refuses a file that is not a picture or
// is outside the project, lists every one it cannot find, and answers the
// references that name no file.
func inlinePictures(html, base string, reader *projectReader) (string, int, []string, error) {
	var missing, unnamed []string
	var refused error
	count := 0
	cache := map[string]string{}
	replace := func(ref string) (string, bool) {
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
		if uri, ok := cache[file]; ok {
			count++
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
		mime, picture, err := pictureOf(data)
		if err != nil {
			if refused == nil {
				refused = pageRefusal("%s is not a picture: %v", ref, err)
			}
			return "", false
		}
		uri := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(picture)
		cache[file] = uri
		count++
		return uri, true
	}
	inCSS := func(css string) string {
		return cssURL.ReplaceAllStringFunc(css, func(match string) string {
			parts := cssURL.FindStringSubmatch(match)
			ref, quote := unquote(parts[2])
			if uri, ok := replace(strings.TrimSpace(ref)); ok {
				return parts[1] + quote + uri + quote + parts[3]
			}
			return match
		})
	}
	inMarkup := func(markup string) string {
		markup = imgSrc.ReplaceAllStringFunc(markup, func(match string) string {
			parts := imgSrc.FindStringSubmatch(match)
			ref, quote := unquote(parts[2])
			if uri, ok := replace(strings.TrimSpace(ref)); ok {
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
		return "", 0, nil, refused
	}
	if len(missing) > 0 {
		return "", 0, nil, platform.NewPlatformError(platform.ErrFileNotFound,
			"These pictures are not there: "+strings.Join(missing, ", "),
			"Name each picture by its path in the project, or drop it.")
	}
	return out, count, unnamed, nil
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

// pictureOf is the picture data holds, as the page carries it: a raster
// picture decoded whole and encoded again from its pixels, so nothing but
// the picture rides along; an SVG parsed whole as XML with an <svg> root.
func pictureOf(data []byte) (string, []byte, error) {
	if isSVG(data) {
		if err := wholeSVG(data); err != nil {
			return "", nil, err
		}
		return "image/svg+xml", data, nil
	}
	_, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", nil, errors.New("its bytes are no PNG, JPEG, GIF or SVG")
	}
	var out bytes.Buffer
	switch format {
	case "gif":
		all, err := gif.DecodeAll(bytes.NewReader(data))
		if err != nil {
			return "", nil, fmt.Errorf("it does not decode whole: %w", err)
		}
		if err := gif.EncodeAll(&out, all); err != nil {
			return "", nil, fmt.Errorf("encode: %w", err)
		}
		return "image/gif", out.Bytes(), nil
	case "png", "jpeg":
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return "", nil, fmt.Errorf("it does not decode whole: %w", err)
		}
		if format == "png" {
			err = png.Encode(&out, img)
		} else {
			err = jpeg.Encode(&out, img, &jpeg.Options{Quality: 90})
		}
		if err != nil {
			return "", nil, fmt.Errorf("encode: %w", err)
		}
		return "image/" + format, out.Bytes(), nil
	}
	return "", nil, errors.New("its bytes are no PNG, JPEG, GIF or SVG")
}

// svgPrologue is what may stand ahead of an SVG's root: an XML declaration,
// comments, a doctype, whitespace.
var svgPrologue = regexp.MustCompile(`^(?s)\s*(<\?xml[^>]*\?>\s*)?((<!--.*?-->|<!DOCTYPE[^>\[]*(\[[^\]]*\])?>)\s*)*<svg[\s>]`)

func isSVG(data []byte) bool {
	return svgPrologue.Match(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")))
}

// wholeSVG parses data to its end as one XML document.
func wholeSVG(data []byte) error {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = true
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

// projectReader reads the files a page names: regular files in the project,
// never a link, a FIFO or a device, within one running byte budget.
type projectReader struct {
	root      string
	maxBytes  int
	readSoFar int
}

func (r *projectReader) read(name, file string) ([]byte, error) {
	root, err := filepath.Abs(r.root)
	if err != nil {
		return nil, fmt.Errorf("project root: %w", err)
	}
	abs, err := filepath.Abs(file)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", name, err)
	}
	if rel, err := filepath.Rel(root, abs); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, pageRefusal("%s is outside the project", name)
	}
	info, err := os.Lstat(abs)
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
	f, err := os.OpenFile(abs, os.O_RDONLY|syscall.O_NONBLOCK, 0)
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
