package catalog

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestSync_WritesSnapshot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		types          []platform.ServiceStackType
		wantContains   []string
		wantNotContain []string
	}{
		{
			name: "active versions collected and specials added",
			types: []platform.ServiceStackType{
				{
					Name:     "Node.js",
					Category: "USER",
					Versions: []platform.ServiceStackTypeVersion{
						{Name: "nodejs@22", Status: "ACTIVE"},
						{Name: "nodejs@20", Status: "ACTIVE"},
						{Name: "nodejs@18", Status: "DEPRECATED"},
					},
				},
				{
					Name:     "Go",
					Category: "USER",
					Versions: []platform.ServiceStackTypeVersion{
						{Name: "go@1", Status: "ACTIVE"},
					},
				},
			},
			wantContains:   []string{"nodejs@22", "nodejs@20", "go@1", "object-storage", "shared-storage", "static"},
			wantNotContain: []string{"nodejs@18"},
		},
		{
			name:         "empty catalog still has specials",
			types:        nil,
			wantContains: []string{"object-storage", "shared-storage", "static"},
		},
		{
			name: "deduplicates versions",
			types: []platform.ServiceStackType{
				{
					Name:     "PHP",
					Category: "USER",
					Versions: []platform.ServiceStackTypeVersion{
						{Name: "php@8.4", Status: "ACTIVE"},
					},
				},
				{
					Name:     "PHP Apache",
					Category: "USER",
					Versions: []platform.ServiceStackTypeVersion{
						{Name: "php@8.4", Status: "ACTIVE"},
					},
				},
			},
			wantContains: []string{"php@8.4"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock := platform.NewMock().WithServiceStackTypes(tt.types)

			outPath := filepath.Join(t.TempDir(), "active_versions.json")
			snap, err := Sync(context.Background(), mock, outPath)
			if err != nil {
				t.Fatalf("Sync: %v", err)
			}

			// Verify file was written and is valid JSON.
			data, err := os.ReadFile(outPath)
			if err != nil {
				t.Fatalf("read snapshot: %v", err)
			}

			var fromFile Snapshot
			if err := json.Unmarshal(data, &fromFile); err != nil {
				t.Fatalf("parse snapshot: %v", err)
			}

			// Verify timestamp is recent.
			gen, err := time.Parse(time.RFC3339, snap.Generated)
			if err != nil {
				t.Fatalf("parse generated time: %v", err)
			}
			if time.Since(gen) > time.Minute {
				t.Errorf("generated timestamp too old: %s", snap.Generated)
			}

			// Verify expected versions present.
			versionSet := make(map[string]bool, len(snap.Versions))
			for _, v := range snap.Versions {
				versionSet[v] = true
			}

			for _, want := range tt.wantContains {
				if !versionSet[want] {
					t.Errorf("missing expected version %q in %v", want, snap.Versions)
				}
			}
			for _, notWant := range tt.wantNotContain {
				if versionSet[notWant] {
					t.Errorf("unexpected version %q in %v", notWant, snap.Versions)
				}
			}

			// Verify sorted.
			for i := 1; i < len(snap.Versions); i++ {
				if snap.Versions[i] < snap.Versions[i-1] {
					t.Errorf("versions not sorted: %q before %q", snap.Versions[i-1], snap.Versions[i])
					break
				}
			}
		})
	}
}
