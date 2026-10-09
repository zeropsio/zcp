package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/runtime"
)

// The console is inside the project it serves only in that project's own
// container; a person's machine, or a container of another project, is not.
func TestConsoleInsideProject(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		rt      runtime.Info
		project string
		want    bool
	}{
		{"the project's own container", runtime.Info{InContainer: true, ProjectID: "p1"}, "p1", true},
		{"a person's machine", runtime.Info{}, "p1", false},
		{"a container of another project", runtime.Info{InContainer: true, ProjectID: "p2"}, "p1", false},
		{"a container that knows no project", runtime.Info{InContainer: true}, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := consoleInsideProject(tc.rt, tc.project); got != tc.want {
				t.Fatalf("inside = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEmitReady_TokenNeverInStderr(t *testing.T) {
	tests := []struct {
		name        string
		allowWrites bool
	}{
		{name: "read only", allowWrites: false},
		{name: "write enabled", allowWrites: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const token = "sentinel-token-never-log"
			const writeToken = "sentinel-write-token-never-log"
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			if err := emitReady(&stdout, &stderr, "http://127.0.0.1:1234", token, writeToken, 4242, tt.allowWrites); err != nil {
				t.Fatalf("emit ready: %v", err)
			}

			// Neither the read bearer nor the write token may reach stderr (which is
			// logged); both ride the stdout ready-line — a private pipe the host reads.
			if strings.Contains(stderr.String(), token) || strings.Contains(stderr.String(), writeToken) {
				t.Fatalf("stderr leaked a token: %q", stderr.String())
			}
			if !strings.Contains(stdout.String(), token) {
				t.Fatalf("stdout ready-line did not contain session token: %q", stdout.String())
			}

			var ready struct {
				URL          string `json:"url"`
				SessionToken string `json:"sessionToken"`
				WriteToken   string `json:"writeToken"`
				PID          int    `json:"pid"`
				AllowWrites  bool   `json:"allowWrites"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &ready); err != nil {
				t.Fatalf("decode ready-line: %v", err)
			}
			if ready.SessionToken != token {
				t.Fatalf("sessionToken = %q, want %q", ready.SessionToken, token)
			}
			if ready.WriteToken != writeToken {
				t.Fatalf("writeToken = %q, want %q", ready.WriteToken, writeToken)
			}
			// The write token is a SEPARATE secret from the read bearer.
			if ready.WriteToken == ready.SessionToken {
				t.Fatal("writeToken must be independent of the session bearer")
			}
			if ready.AllowWrites != tt.allowWrites {
				t.Fatalf("allowWrites = %v, want %v", ready.AllowWrites, tt.allowWrites)
			}
		})
	}
}
