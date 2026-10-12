package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestChiefContainer_RunsNoCodeServer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		store string
		want  bool
	}{
		{name: "a Chief's container", store: `{"ZCP_CHIEF":"1","ZCP_MATE_ENABLED":"1"}`, want: true},
		{name: "a Chief switched on by true", store: `{"ZCP_CHIEF":"true"}`, want: true},
		{name: "a Mate's container", store: `{"ZCP_MATE_ENABLED":"1"}`, want: false},
		{name: "a Chief switched off", store: `{"ZCP_CHIEF":"0"}`, want: false},
		{name: "no live env store", store: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			storePath := filepath.Join(t.TempDir(), "env.json")
			if tt.store != "" {
				if err := os.WriteFile(storePath, []byte(tt.store), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if got := chiefContainer(storePath); got != tt.want {
				t.Fatalf("chiefContainer() = %v, want %v", got, tt.want)
			}
		})
	}
}
