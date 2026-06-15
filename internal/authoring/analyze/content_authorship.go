package analyze

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ContentMeta is the sidecar metadata the harness writes alongside each
// copied recipe-output file. Captures authorship (writer / engine /
// main) so the harness can detect main-agent-rewrite-of-writer-path —
// a class v39 shipped silently for all 3 per-codebase READMEs.
type ContentMeta struct {
	Author          string `json:"author"`
	FirstWriteTime  string `json:"firstWriteTime,omitempty"`
	EditCount       int    `json:"editCount"`
	MarkerFormValid bool   `json:"markerFormValid"`
	SizeBytes       int    `json:"sizeBytes"`
	Path            string `json:"path"`
}

// WriteContentAuthorship copies every recipe-output file under
// deliverableRoot into <outputDir>/content/<path> and writes a sibling
// .meta.json with authorship. Authorship is inferred from the RawTree
// — an Edit/Write by agent X attributes the file to X. If multiple
// authors touched a file, the main-agent-is-last heuristic flags it.
func WriteContentAuthorship(deliverableRoot, outputDir string, tree *RawTree) ([]ContentMeta, error) {
	contentDir := filepath.Join(outputDir, "content")
	if err := os.MkdirAll(contentDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir content: %w", err)
	}

	// Build path → authors-sequence from the raw tree (main + subs).
	authors := authorsFromTree(tree)

	var metas []ContentMeta
	err := filepath.WalkDir(deliverableRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // walk-tolerant — skip unreadable entries
		}
		rel, relErr := filepath.Rel(deliverableRoot, path)
		if relErr != nil {
			return nil //nolint:nilerr // walk-tolerant — skip paths outside root
		}
		if strings.HasPrefix(rel, "analysis/") || strings.HasPrefix(rel, "SESSIONS_LOGS/") {
			return nil // skip nested harness outputs and session logs
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return nil //nolint:nilerr // walk-tolerant — skip unreadable files
		}
		dst := filepath.Join(contentDir, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, src, 0o600); err != nil {
			return err
		}
		meta := ContentMeta{
			Path:            rel,
			SizeBytes:       len(src),
			MarkerFormValid: true,
			Author:          authorFor(authors[rel]),
			EditCount:       len(authors[rel]),
		}
		if err := writeJSON(dst+".meta.json", meta); err != nil {
			return err
		}
		metas = append(metas, meta)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return metas, writeJSON(filepath.Join(outputDir, "content-authorship.json"), metas)
}

// authorsFromTree walks every AgentTrace event and collects, per
// target path, the ordered list of authors who wrote/edited it. Used
// to infer final author and detect main-agent-rewrite.
func authorsFromTree(tree *RawTree) map[string][]string {
	out := map[string][]string{}
	if tree == nil {
		return out
	}
	if tree.Main != nil {
		mergeAuthors(out, tree.Main, "main")
	}
	for _, sub := range tree.Subs {
		mergeAuthors(out, sub, sub.Role)
	}
	return out
}

func mergeAuthors(out map[string][]string, trace *AgentTrace, author string) {
	for _, ev := range trace.Events {
		if ev.Type != eventTypeToolUse {
			continue
		}
		if ev.Name != "Edit" && ev.Name != "Write" {
			continue
		}
		var obj struct {
			Input struct {
				FilePath string `json:"file_path"`
			} `json:"input"`
		}
		if err := json.Unmarshal(ev.ToolUse, &obj); err != nil {
			continue
		}
		if obj.Input.FilePath == "" {
			continue
		}
		out[obj.Input.FilePath] = append(out[obj.Input.FilePath], author)
	}
}

// authorFor picks the "effective" author per file: the last writer wins,
// unless "main" rewrote a writer-owned path — then return "main"
// explicitly so the surfaces gate detects the regression class.
func authorFor(chain []string) string {
	if len(chain) == 0 {
		return "unknown"
	}
	return chain[len(chain)-1]
}
