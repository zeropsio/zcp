package eval

import (
	"path/filepath"
	"testing"
)

// TestScenarios_LiveFilesParse ensures every shipped scenario file under
// internal/eval/scenarios/*.md parses and passes validation. Guards against a
// scenario being committed with bad frontmatter or a missing fixture.
func TestScenarios_LiveFilesParse(t *testing.T) {
	t.Parallel()

	matches, err := filepath.Glob("scenarios/*.md")
	if err != nil {
		t.Fatalf("glob scenarios: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no scenarios found under scenarios/*.md")
	}

	for _, path := range matches {
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()
			sc, err := ParseScenario(path)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if sc.ID == "" {
				t.Error("id empty")
			}
			if len(sc.Expect.MustCallTools) == 0 {
				t.Error("mustCallTools should not be empty — every scenario should assert some tool usage")
			}
			// Fixture files must actually exist.
			if sc.Fixture != "" {
				fixturePath := resolveFixturePath(sc)
				if _, err := filepath.Abs(fixturePath); err != nil {
					t.Errorf("resolve fixture: %v", err)
				}
				matches, err := filepath.Glob(fixturePath)
				if err != nil || len(matches) == 0 {
					t.Errorf("fixture %q does not exist", fixturePath)
				}
			}
		})
	}
}
