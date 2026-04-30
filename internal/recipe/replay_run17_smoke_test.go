package recipe

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReplay_Run17_BriefComposes is a smoke test that proves the
// run-17 on-disk artifacts (plan.json + facts.jsonl) are sufficient to
// reconstruct the codebase-content brief offline — no session log
// digging, no engine session needed.
//
// This is the load-bearing pre-condition for cmd/zcp-recipe-sim:
// if this fails, the replay loop can't function. If it passes, the
// remaining replay work is plumbing.
func TestReplay_Run17_BriefComposes(t *testing.T) {
	t.Parallel()
	root := "../../docs/zcprecipator3/runs/17/environments"

	if _, err := os.Stat(filepath.Join(root, "plan.json")); err != nil {
		t.Skipf("run-17 corpus not present (%v) — skipping", err)
	}

	plan, err := ReadPlan(root)
	if err != nil {
		t.Fatalf("ReadPlan: %v", err)
	}
	if plan.Slug != "nestjs-showcase" {
		t.Fatalf("expected slug=nestjs-showcase, got %q", plan.Slug)
	}
	if len(plan.Codebases) != 3 {
		t.Fatalf("expected 3 codebases, got %d", len(plan.Codebases))
	}
	for _, cb := range plan.Codebases {
		if cb.SourceRoot == "" {
			t.Fatalf("codebase %s has empty SourceRoot — backfill incomplete", cb.Hostname)
		}
		if !strings.HasSuffix(cb.SourceRoot, "dev") {
			t.Fatalf("codebase %s SourceRoot %q must end in 'dev' (M-1 contract)", cb.Hostname, cb.SourceRoot)
		}
	}

	facts, err := loadFactsJSONL(filepath.Join(root, "facts.jsonl"))
	if err != nil {
		t.Fatalf("load facts: %v", err)
	}
	if len(facts) < 40 {
		t.Fatalf("expected ~51 facts, got %d", len(facts))
	}

	for _, cb := range plan.Codebases {
		t.Run("brief/"+cb.Hostname, func(t *testing.T) {
			t.Parallel()
			brief, err := BuildCodebaseContentBrief(plan, cb, nil, facts)
			if err != nil {
				t.Fatalf("BuildCodebaseContentBrief: %v", err)
			}
			if brief.Bytes == 0 {
				t.Fatalf("brief body empty")
			}
			body := brief.Body
			for _, want := range []string{
				"## Codebase",
				cb.Hostname,
				cb.SourceRoot,
			} {
				if !strings.Contains(body, want) {
					t.Errorf("brief missing %q (first 400 chars: %q)", want, body[:min(400, len(body))])
				}
			}
		})
	}
}

func loadFactsJSONL(path string) ([]FactRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []FactRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<20)
	for sc.Scan() {
		ln := strings.TrimSpace(sc.Text())
		if ln == "" {
			continue
		}
		var r FactRecord
		if err := json.Unmarshal([]byte(ln), &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, sc.Err()
}
