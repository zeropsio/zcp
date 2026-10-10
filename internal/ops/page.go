package ops

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
// The conversation draws the page in a sandboxed frame that reaches no
// network, so everything it shows must be inside it: every local picture an
// <img> or a CSS url() names is inlined as a data: URI — refused if its bytes
// are not a picture, so a secret renamed .png never rides along — and every
// remote resource it would load is named back to the agent as one that will
// not load.

// PageMaxBytes caps a page, its pictures inlined.
const PageMaxBytes = 8 << 20

// PageKeepFor is how long a page file is kept: the Mate server takes its own
// copy as it records the call.
const PageKeepFor = 7 * 24 * time.Hour

// pageTitleMax caps a page's title, in characters.
const pageTitleMax = 120

const pagesDir = "pages"

// PageInput is what the agent publishes: a title and the page, given whole
// (HTML) or as a file (Path, relative to the project or absolute).
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
	// Images is how many local pictures were inlined.
	Images int `json:"images"`
	// WontLoad is every remote resource the page would load, which the
	// conversation will not.
	WontLoad []string `json:"wontLoad,omitempty"`
}

type pageLimits struct {
	maxBytes int
}

// PublishPage validates in, inlines its local pictures, and keeps it as a
// page under stateDir; a relative path resolves in cwd.
func PublishPage(stateDir, cwd string, in PageInput) (*Page, error) {
	return publishPage(stateDir, cwd, in, pageLimits{maxBytes: PageMaxBytes}, time.Now())
}

func pageRefusal(format string, args ...any) error {
	return platform.NewPlatformError(platform.ErrInvalidParameter, fmt.Sprintf(format, args...),
		"Publish a self-contained page: its pictures as files in the project or data: URIs, its scripts and styles inline.")
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
	html, base := in.HTML, cwd
	if in.Path != "" {
		file := in.Path
		if !filepath.IsAbs(file) {
			file = filepath.Join(cwd, file)
		}
		read, err := readCapped(file, limits.maxBytes)
		if err != nil {
			return nil, pageFileError(in.Path, err, limits.maxBytes)
		}
		html, base = string(read), filepath.Dir(file)
	}
	if len(html) > limits.maxBytes {
		return nil, pageRefusal("The page is %d bytes, over the %d byte cap", len(html), limits.maxBytes)
	}

	inlined, images, err := inlinePictures(html, base, limits.maxBytes)
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
	return &Page{
		ID:       id,
		Title:    title,
		File:     file,
		Bytes:    len(inlined),
		Images:   images,
		WontLoad: remoteLoads(inlined),
	}, nil
}

// pictureRefs are the places a page names a picture: an <img>'s src (any
// quoting) and a CSS url().
var (
	imgSrc = regexp.MustCompile(`(?i)(<img\b[^>]*?\bsrc\s*=\s*)("[^"]*"|'[^']*'|[^\s"'>]+)`)
	cssURL = regexp.MustCompile(`(?i)(url\(\s*)("[^"]*"|'[^']*'|[^)"'\s]+)(\s*\))`)
)

// remoteRef is a reference the conversation's frame never loads.
var remoteRef = regexp.MustCompile(`(?i)^(https?:)?//`)

// notLocal is a reference that names no file: a URL, inline data, a fragment.
var notLocal = regexp.MustCompile(`(?i)^([a-z][a-z0-9+.-]*:|//|#)`)

func unquote(value string) (string, string) {
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
		return value[1 : len(value)-1], value[:1]
	}
	return value, ""
}

// inlinePictures replaces every local picture html names with a data: URI;
// relative names resolve in base. It refuses a file that is not a picture and
// lists every one it cannot find.
func inlinePictures(html, base string, maxBytes int) (string, int, error) {
	var missing []string
	var refused error
	count := 0
	cache := map[string]string{}
	replace := func(ref string) (string, bool) {
		if ref == "" || notLocal.MatchString(ref) {
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
		data, err := readCapped(file, maxBytes)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				missing = append(missing, ref)
			} else if refused == nil {
				refused = pageFileError(ref, err, maxBytes)
			}
			return "", false
		}
		mime := pictureMime(data)
		if mime == "" {
			if refused == nil {
				refused = pageRefusal("%s is not an image: its bytes are no picture format", ref)
			}
			return "", false
		}
		uri := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
		cache[file] = uri
		count++
		return uri, true
	}
	out := imgSrc.ReplaceAllStringFunc(html, func(match string) string {
		parts := imgSrc.FindStringSubmatch(match)
		ref, quote := unquote(parts[2])
		if uri, ok := replace(strings.TrimSpace(ref)); ok {
			return parts[1] + quote + uri + quote
		}
		return match
	})
	out = cssURL.ReplaceAllStringFunc(out, func(match string) string {
		parts := cssURL.FindStringSubmatch(match)
		ref, quote := unquote(parts[2])
		if uri, ok := replace(strings.TrimSpace(ref)); ok {
			return parts[1] + quote + uri + quote + parts[3]
		}
		return match
	})
	if refused != nil {
		return "", 0, refused
	}
	if len(missing) > 0 {
		return "", 0, platform.NewPlatformError(platform.ErrFileNotFound,
			"These pictures are not there: "+strings.Join(missing, ", "),
			"Name each picture by its path in the project, or drop it.")
	}
	return out, count, nil
}

// pageLoads are the places a page would load something from: a src, a
// stylesheet's href, a CSS url().
var (
	srcAttr        = regexp.MustCompile(`(?i)<[a-z][^>]*?\bsrc\s*=\s*("[^"]*"|'[^']*'|[^\s"'>]+)`)
	stylesheetHref = regexp.MustCompile(`(?i)<link\b[^>]*?\bhref\s*=\s*("[^"]*"|'[^']*'|[^\s"'>]+)`)
)

const wontLoadMax = 10

// remoteLoads is every remote resource html would load, each once, in order.
func remoteLoads(html string) []string {
	var found []string
	seen := map[string]bool{}
	add := func(ref string) {
		ref = strings.TrimSpace(ref)
		if remoteRef.MatchString(ref) && !seen[ref] && len(found) < wontLoadMax {
			seen[ref] = true
			found = append(found, ref)
		}
	}
	for _, re := range []*regexp.Regexp{srcAttr, stylesheetHref, cssURL} {
		for _, match := range re.FindAllStringSubmatch(html, -1) {
			ref, _ := unquote(match[1])
			if re == cssURL {
				ref, _ = unquote(match[2])
			}
			add(ref)
		}
	}
	return found
}

// pictureMime is the picture format data's bytes are in, or "" when they are
// none: sniffed from the bytes, never taken from a name.
func pictureMime(data []byte) string {
	switch sniffed := http.DetectContentType(data); sniffed {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/bmp", "image/x-icon":
		return sniffed
	}
	if len(data) >= 12 && string(data[4:8]) == "ftyp" && (string(data[8:12]) == "avif" || string(data[8:12]) == "avis") {
		return "image/avif"
	}
	if isSVG(data) {
		return "image/svg+xml"
	}
	return ""
}

// svgPrologue is what may stand ahead of an SVG's root: an XML declaration,
// comments, a doctype, whitespace.
var svgPrologue = regexp.MustCompile(`^(?s)\s*(<\?xml[^>]*\?>\s*)?((<!--.*?-->|<!DOCTYPE[^>\[]*(\[[^\]]*\])?>)\s*)*<svg[\s>]`)

func isSVG(data []byte) bool {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	return svgPrologue.Match(data)
}

// readCapped reads file, refusing one over maxBytes without reading it whole.
func readCapped(file string, maxBytes int) ([]byte, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errNotAFile
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(maxBytes)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxBytes {
		return nil, errOverCap
	}
	return data, nil
}

var (
	errNotAFile = errors.New("not a file")
	errOverCap  = errors.New("over the cap")
)

func pageFileError(name string, err error, maxBytes int) error {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return platform.NewPlatformError(platform.ErrFileNotFound, name+" is not there", "Name a file in the project.")
	case errors.Is(err, errOverCap):
		return pageRefusal("%s is over the %d byte cap", name, maxBytes)
	case errors.Is(err, errNotAFile):
		return pageRefusal("%s is not a file", name)
	}
	return fmt.Errorf("read %s: %w", name, err)
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
