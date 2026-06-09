package content

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/schema"
)

// yamlFence matches a fenced ```yaml block.
var yamlFence = regexp.MustCompile("(?s)```yaml\\n(.*?)\\n```")

// snippetPlaceholder matches the `<word…>` / `{word…}` placeholders that mark a
// SCAFFOLD TEMPLATE (e.g. develop-first-deploy-scaffold-yaml's `<hostname>`),
// which is not a concrete document to validate.
var snippetPlaceholder = regexp.MustCompile(`<[a-zA-Z]|\{[a-z]`)

// TestContentYAMLSnippetsNoMisplacedFields pins the B2 class on the content
// surface: a `zerops.yaml` snippet in an atom / recipe-authoring / example file
// must not place a field in the wrong section (the `run.verticalAutoscaling` /
// `run.deployFiles` family). Owner of "valid placement" is the live schema
// (schema.ValidateZeropsYAMLStructure — same one export/launch use).
//
// Scope is deliberately MISPLACEMENT only ("additionalProperties … not
// allowed"), not omission ("missing properties: …"): many content snippets are
// intentionally partial illustrations (e.g. dual-runtime-consumption shows only
// the env-var section), so a missing required field is not a defect. Skips:
// placeholder templates, `>`-quoted fail-narration examples, and non-`zerops:`
// roots (import.yaml snippets validate against a different schema).
func TestContentYAMLSnippetsNoMisplacedFields(t *testing.T) {
	t.Parallel()
	var violations []string
	err := filepath.Walk(".", func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(p, ".md") {
			return nil
		}
		b, readErr := os.ReadFile(p)
		if readErr != nil {
			return readErr
		}
		for i, m := range yamlFence.FindAllStringSubmatch(string(b), -1) {
			y := m[1]
			trimmed := strings.TrimSpace(y)
			if snippetPlaceholder.MatchString(y) || strings.HasPrefix(trimmed, ">") || !strings.HasPrefix(trimmed, "zerops:") {
				continue
			}
			for _, e := range schema.ValidateZeropsYAMLStructure(y, "") {
				msg := e.Error()
				if strings.Contains(msg, "additionalProperties") && strings.Contains(msg, "not allowed") {
					violations = append(violations, p+" block#"+strconv.Itoa(i)+": "+msg)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk content: %v", err)
	}
	if len(violations) > 0 {
		t.Errorf("misplaced field(s) in content zerops.yaml snippet(s) — move the field to its correct section (the schema is the owner of valid placement):\n%s",
			strings.Join(violations, "\n"))
	}
}
