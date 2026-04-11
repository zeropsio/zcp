package knowledge

import (
	"embed"
	"io/fs"
	"strings"
	"sync"
)

//go:embed themes/*.md bases/*.md all:recipes all:guides all:decisions
var contentFS embed.FS

// knowledgeDirs lists the top-level directories in the embedded knowledge filesystem.
var knowledgeDirs = []string{"themes", "bases", "recipes", "guides", "decisions"}

// Document represents a parsed knowledge document.
type Document struct {
	Path        string // themes/core.md, recipes/laravel.md
	URI         string // zerops://themes/core, zerops://recipes/laravel
	Title       string // Zerops Core Reference
	Content     string // Full markdown content (what gets injected into agent context)
	Description string // Frontmatter description or first paragraph

	sectionsOnce sync.Once
	sections     map[string]string // cached H2 sections (lazily populated, thread-safe)
}

// H2Sections returns the parsed H2 sections, caching the result.
// Safe for concurrent use.
func (d *Document) H2Sections() map[string]string {
	d.sectionsOnce.Do(func() {
		d.sections = parseH2Sections(d.Content)
	})
	return d.sections
}

// H3Section returns the body of a specific H3 subsection inside a specific H2.
// The H3 heading is matched by prefix (trailing modifiers like " — notes"
// after the target name are tolerated). Returns "" if either heading is not
// found. Used for surgical injection when the full H2 is too broad — for
// example, pulling just `verticalAutoscaling` from `import.yaml Schema`.
func (d *Document) H3Section(h2, h3 string) string {
	body, ok := d.H2Sections()[h2]
	if !ok {
		return ""
	}
	return extractH3(body, h3)
}

// extractH3 walks an H2-scoped body and returns the contents of the first
// H3 whose heading text begins with `target`. The walk stops at the next
// sibling H3, the next H2, or EOF.
func extractH3(h2Body, target string) string {
	lines := strings.Split(h2Body, "\n")
	var out []string
	inside := false
	for _, l := range lines {
		if rest, ok := strings.CutPrefix(l, "### "); ok {
			if inside {
				break
			}
			if strings.HasPrefix(rest, target) {
				inside = true
			}
			continue
		}
		if strings.HasPrefix(l, "## ") {
			if inside {
				break
			}
			continue
		}
		if inside {
			out = append(out, l)
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
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

	// Parse optional YAML frontmatter (--- block at start of file).
	var frontmatter map[string]string
	frontmatter, body := extractFrontmatter(content)

	title := extractTitle(body)

	// Description: frontmatter description > first paragraph
	desc := frontmatter["description"]
	if desc == "" {
		desc = extractFirstParagraph(body)
	}

	return &Document{
		Path:        path,
		URI:         uri,
		Title:       title,
		Content:     body,
		Description: desc,
	}
}

// extractFrontmatter parses YAML-style frontmatter from the start of content.
// Returns key-value pairs and the remaining body. If no frontmatter, returns empty map and original content.
func extractFrontmatter(content string) (map[string]string, string) {
	lines := strings.Split(content, "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "---" {
		return nil, content
	}

	fm := make(map[string]string)
	endIdx := -1
	for i := 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "---" {
			endIdx = i
			break
		}
		if key, val, ok := strings.Cut(trimmed, ":"); ok {
			k := strings.TrimSpace(key)
			v := strings.TrimSpace(val)
			// Strip surrounding quotes
			if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
				v = v[1 : len(v)-1]
			}
			fm[k] = v
		}
	}

	if endIdx < 0 {
		return nil, content
	}

	// Skip blank lines after frontmatter
	bodyStart := endIdx + 1
	for bodyStart < len(lines) && strings.TrimSpace(lines[bodyStart]) == "" {
		bodyStart++
	}

	return fm, strings.Join(lines[bodyStart:], "\n")
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
