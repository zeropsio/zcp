package checks

import (
	"context"
	"strings"
	"testing"
)

func readmeWithKB(stems []string) string {
	var sb strings.Builder
	sb.WriteString("# X\n\n<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n## Gotchas\n")
	for _, s := range stems {
		sb.WriteString("- **")
		sb.WriteString(s)
		sb.WriteString("** — body\n")
	}
	sb.WriteString("<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->\n")
	return sb.String()
}

func TestCheckCrossReadmeGotchaUniqueness_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		readmes    map[string]string
		wantStatus string
		wantDetail []string
	}{
		{
			name:       "no readmes passes",
			readmes:    map[string]string{},
			wantStatus: "pass",
		},
		{
			name: "single readme passes",
			readmes: map[string]string{
				"api": readmeWithKB([]string{"NATS queue group", "SSHFS uid fix"}),
			},
			wantStatus: "pass",
		},
		{
			name: "two readmes with distinct stems pass",
			readmes: map[string]string{
				"api":    readmeWithKB([]string{"NATS queue group needed for replicas"}),
				"worker": readmeWithKB([]string{"SIGTERM drain before exit on deploy"}),
			},
			wantStatus: "pass",
		},
		{
			name: "duplicate stem across readmes fails",
			readmes: map[string]string{
				"api":    readmeWithKB([]string{"NATS queue group needed for replicas"}),
				"worker": readmeWithKB([]string{"NATS queue group needed for replicas"}),
			},
			wantStatus: "fail",
			wantDetail: []string{"api", "worker"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := CheckCrossReadmeGotchaUniqueness(context.Background(), tt.readmes)
			shim := make([]stepCheckShim, 0, len(got))
			for _, c := range got {
				shim = append(shim, stepCheckShim{Name: c.Name, Status: c.Status, Detail: c.Detail})
			}
			check := findCheck(shim, "cross_readme_gotcha_uniqueness")
			if check == nil {
				t.Fatalf("expected check, got %+v", shim)
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
