package knowledge

import (
	"embed"
	"io/fs"
	"strings"
)

//go:embed themes/*.md recipes/*.md
var contentFS embed.FS

// knowledgeDirs lists the top-level directories in the embedded knowledge filesystem.
var knowledgeDirs = []string{"themes", "recipes"}

// Document represents a parsed knowledge document.
type Document struct {
	Path        string            // themes/core.md, recipes/laravel-jetstream.md
	URI         string            // zerops://themes/core, zerops://recipes/laravel-jetstream
	Title       string            // Zerops Core Reference
	Keywords    []string          // [zerops, core, principles, ...]
	TLDR        string            // One-sentence summary
	Content     string            // Full markdown content
	Description string            // TL;DR or first paragraph
	sections    map[string]string // cached H2 sections (lazily populated)
}

// H2Sections returns the parsed H2 sections, caching the result.
func (d *Document) H2Sections() map[string]string {
	if d.sections == nil {
		d.sections = parseH2Sections(d.Content)
	}
	return d.sections
}

// loadFromEmbedded walks the embedded filesystem and parses all markdown documents.
func loadFromEmbedded() map[string]*Document {
	docs := make(map[string]*Document)

	for _, dir := range knowledgeDirs {
		_ = fs.WalkDir(contentFS, dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
				return nil //nolint:nilerr // intentional: continue walking on individual file errors
			}
			data, err := contentFS.ReadFile(path)
			if err != nil {
				return nil //nolint:nilerr // intentional: continue walking on individual file errors
			}
			doc := parseDocument(path, string(data))
			docs[doc.URI] = doc
			return nil
		})
	}

	return docs
}

func parseDocument(path, content string) *Document {
	uri := pathToURI(path)
	title := extractTitle(content)
	keywords := extractKeywords(content)
	tldr := extractTLDR(content)

	desc := tldr
	if desc == "" {
		desc = extractFirstParagraph(content)
	}

	return &Document{
		Path:        path,
		URI:         uri,
		Title:       title,
		Keywords:    keywords,
		TLDR:        tldr,
		Content:     content,
		Description: desc,
	}
}

func pathToURI(fsPath string) string {
	// Strip .md suffix, keep directory prefix as URI path
	rel := strings.TrimSuffix(fsPath, ".md")
	return "zerops://" + rel
}

func uriToPath(uri string) string {
	rel := strings.TrimPrefix(uri, "zerops://")
	return rel + ".md"
}

func extractTitle(content string) string {
	for line := range strings.SplitSeq(content, "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "# "); ok {
			return rest
		}
	}
	return ""
}

func extractKeywords(content string) []string {
	lines := strings.Split(content, "\n")
	inKeywords := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "## Keywords" {
			inKeywords = true
			continue
		}
		if inKeywords {
			if trimmed == "" || strings.HasPrefix(trimmed, "##") {
				break
			}
			parts := strings.Split(trimmed, ",")
			var keywords []string
			for _, p := range parts {
				kw := strings.TrimSpace(p)
				if kw != "" {
					keywords = append(keywords, strings.ToLower(kw))
				}
			}
			return keywords
		}
	}
	return nil
}

func extractTLDR(content string) string {
	lines := strings.Split(content, "\n")
	inTLDR := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "## TL;DR" {
			inTLDR = true
			continue
		}
		if inTLDR {
			if trimmed == "" {
				continue
			}
			if strings.HasPrefix(trimmed, "##") {
				break
			}
			return trimmed
		}
	}
	return ""
}

func extractFirstParagraph(content string) string {
	lines := strings.Split(content, "\n")
	var para []string
	pastTitle := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			pastTitle = true
			continue
		}
		if !pastTitle {
			continue
		}
		if trimmed == "" && len(para) > 0 {
			break
		}
		if trimmed != "" && !strings.HasPrefix(trimmed, "##") {
			para = append(para, trimmed)
		}
	}
	result := strings.Join(para, " ")
	if len(result) > 200 {
		return result[:200] + "..."
	}
	return result
}
