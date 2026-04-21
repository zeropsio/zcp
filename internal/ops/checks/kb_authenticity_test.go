package checks

import (
	"context"
	"strings"
	"testing"
)

// gotchaKB wraps authentic + synthetic stems into the knowledge-base
// fragment shape ExtractGotchaEntries expects: a `## Gotchas` section
// header followed by `- **stem** — body` bullet lines.
func gotchaKB(entries []struct{ stem, body string }) string {
	var sb strings.Builder
	sb.WriteString("## Gotchas\n\n")
	for _, e := range entries {
		sb.WriteString("- **")
		sb.WriteString(e.stem)
		sb.WriteString("** — ")
		sb.WriteString(e.body)
		sb.WriteString("\n")
	}
	return sb.String()
}

func TestCheckKnowledgeBaseAuthenticity_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		kb         string
		hostname   string
		wantStatus string
		wantDetail []string
	}{
		{
			name:       "empty kb returns nil",
			kb:         "",
			wantStatus: "",
		},
		{
			name: "four authentic gotchas passes",
			kb: gotchaKB([]struct{ stem, body string }{
				{"execOnce advisory lock", "wraps migrations so multi-container deploys don't race on the same DDL"},
				{"L7 balancer terminates SSL", "app must set trust proxy 1 otherwise ${REMOTE_ADDR} is the balancer"},
				{"NATS fails with DNS errors", "when containers resolve the managed hostname before it becomes routable"},
				{"execOnce idempotent seed", "prevents duplicate seed on fresh container warm-up"},
			}),
			wantStatus: "pass",
			wantDetail: []string{"authentic"},
		},
		{
			name: "all synthetic fails",
			kb: gotchaKB([]struct{ stem, body string }{
				{"Shared database with the API", "both services read the same postgres"},
				{"NATS authentication", "we configure NATS auth"},
				{"Storage buckets named after services", "follow the hostname"},
				{"Meilisearch seeded at deploy", "search index populated"},
			}),
			hostname:   "apidev",
			wantStatus: "fail",
			wantDetail: []string{"Synthetic"},
		},
		{
			name: "mix below threshold fails",
			kb: gotchaKB([]struct{ stem, body string }{
				{"Shared database with the API", "both services read the same postgres"},
				{"execOnce prevents duplicate seed", "on multi-container deploys"},
				{"NATS authentication", "we configure NATS auth"},
			}),
			wantStatus: "fail",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := CheckKnowledgeBaseAuthenticity(context.Background(), tt.kb, tt.hostname)
			shim := make([]stepCheckShim, 0, len(got))
			for _, c := range got {
				shim = append(shim, stepCheckShim{Name: c.Name, Status: c.Status, Detail: c.Detail})
			}
			if tt.wantStatus == "" {
				if len(shim) != 0 {
					t.Errorf("expected nil, got %+v", shim)
				}
				return
			}
			check := findCheck(shim, "knowledge_base_authenticity")
			if check == nil {
				t.Fatalf("expected knowledge_base_authenticity, got %+v", shim)
			}
			if check.Status != tt.wantStatus {
				t.Errorf("status: got %q, want %q (detail: %s)", check.Status, tt.wantStatus, check.Detail)
			}
			for _, w := range tt.wantDetail {
				if !strings.Contains(check.Detail, w) {
					t.Errorf("detail missing %q; full: %s", w, check.Detail)
				}
			}
		})
	}
}
