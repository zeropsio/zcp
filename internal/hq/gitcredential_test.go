package hq

import (
	"path/filepath"
	"strings"
	"testing"
)

// git asks the helper with the remote's protocol and host; only the HQ this
// Mate enrolled with gets its credential, under the user HQ's git reads.
func TestGitCredential_AnswersOnlyTheEnrolledHQ(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		hq       string
		enrolled bool
		request  string
		want     string
		wantOK   bool
	}{
		{"the enrolled HQ", "https://hq.example", true, "protocol=https\nhost=hq.example\npath=git/app/appdev.git\n\n", "secret", true},
		{"https's own port named", "https://hq.example", true, "protocol=https\nhost=hq.example:443\n\n", "secret", true},
		{"an HQ on a port of its own", "http://127.0.0.1:8080/", true, "protocol=http\nhost=127.0.0.1:8080\n\n", "secret", true},
		{"the host in another case", "https://HQ.example", true, "protocol=https\nhost=hq.EXAMPLE\n\n", "secret", true},
		{"another port on the HQ's host", "https://hq.example", true, "protocol=https\nhost=hq.example:8443\n\n", "", false},
		{"the HQ's host over plain http", "https://hq.example", true, "protocol=http\nhost=hq.example\n\n", "", false},
		{"another host", "https://hq.example", true, "protocol=https\nhost=github.com\n\n", "", false},
		{"no host at all", "https://hq.example", true, "protocol=https\n\n", "", false},
		{"not enrolled", "https://hq.example", false, "protocol=https\nhost=hq.example\n\n", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "enrollment.json")
			if tt.enrolled {
				if err := SaveEnrollment(path, Enrollment{HQ: tt.hq, HQProjectID: "hq1", ProjectID: "p1", Credential: "secret"}); err != nil {
					t.Fatal(err)
				}
			}
			got, ok := GitCredential(strings.NewReader(tt.request), path)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("GitCredential = %q, %v; want %q, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
