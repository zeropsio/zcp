package knowledge

import (
	"embed"
	"io/fs"
	"strings"
)

//go:embed embed/**/*.md core/*.md core/**/*.md
var contentFS embed.FS

// Document represents a parsed knowledge document.
type Document struct {
	Path        string   // embed/services/postgresql.md OR core/core-principles.md
	URI         string   // zerops://docs/services/postgresql OR zerops://docs/core/core-principles
	Title       string   // PostgreSQL on Zerops
	Keywords    []string // [postgresql, postgres, sql, ...]
	TLDR        string   // One-sentence summary
	Content     string   // Full markdown content
	Description string   // TL;DR or first paragraph
}

// loadFromEmbedded walks the embedded filesystem and parses all markdown documents.
func loadFromEmbedded() map[string]*Document {
	docs := make(map[string]*Document)

	// Walk embed/ directory (existing knowledge base)
	_ = fs.WalkDir(contentFS, "embed", func(path string, d fs.DirEntry, err error) error {
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

	// Walk core/ directory (new contextual knowledge)
	_ = fs.WalkDir(contentFS, "core", func(path string, d fs.DirEntry, err error) error {
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
	// Strip embed/ or core/ prefix
	rel := strings.TrimPrefix(fsPath, "embed/")
	rel = strings.TrimPrefix(rel, "core/")
	// Strip .md suffix
	rel = strings.TrimSuffix(rel, ".md")

	// Reconstruct URI
	if strings.HasPrefix(fsPath, "core/") {
		return "zerops://docs/core/" + rel
	}
	return "zerops://docs/" + rel
}

func uriToPath(uri string) string {
	rel := strings.TrimPrefix(uri, "zerops://docs/")
	// Check if it's a core document
	if strings.HasPrefix(rel, "core/") {
		return rel + ".md" // Already has core/ prefix
	}
	return "embed/" + rel + ".md"
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
