package mate_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/mate"
)

// TestSeedSignIns: a Mate migrated from main knows who signed each login in
// only from its project's `mate:signer:{key}:{userId}` tags; the server keeps
// that record in its own sign-in store. Before the server starts, zcp writes
// the store from the tags once — when there is none and it never did — and
// never again, whatever the store or the tags hold later.
func TestSeedSignIns(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 2, 18, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		store     string // "" for none
		seeded    bool
		tags      []string
		readErr   error
		wantWrote bool
		wantStore map[string]mate.SignIn // nil: no store
		wantErr   bool
		wantMark  bool
	}{
		{
			name:      "tags present: the store is written",
			tags:      []string{"mate", "mate:signer:claude-code:u-ada", "mate:signer:codex:u-bo", "mate:signer:claudeAgent-work:u-cy", "mate:closed-off"},
			wantWrote: true,
			wantStore: map[string]mate.SignIn{
				"claude-code":      {By: "u-ada", At: now.UnixMilli()},
				"codex":            {By: "u-bo", At: now.UnixMilli()},
				"claudeAgent-work": {By: "u-cy", At: now.UnixMilli()},
			},
			wantMark: true,
		},
		{
			name:     "a store present: untouched",
			store:    `{"codex":{"by":"u-own","at":1}}`,
			tags:     []string{"mate:signer:codex:u-bo"},
			wantMark: true,
		},
		{
			name:     "no signer tags: nothing",
			tags:     []string{"mate", "mate:closed-off"},
			wantMark: true,
		},
		{
			name:     "tags the server would not read: nothing",
			tags:     []string{"mate:signer:gemini:u-ada", "mate:signer:codex:", "mate:signer:codex"},
			wantMark: true,
		},
		{
			name:   "seeded once already, the store gone since: nothing",
			seeded: true,
			tags:   []string{"mate:signer:codex:u-bo"},
		},
		{
			name:    "the tags unread: nothing, and the next launch asks again",
			readErr: errors.New("api down"),
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			storePath := filepath.Join(dir, ".mate", "signed-in.json")
			markPath := filepath.Join(dir, ".zcp", "state", "mate-sign-ins-seeded")
			if tt.store != "" {
				writeFile(t, storePath, tt.store)
			}
			if tt.seeded {
				writeFile(t, markPath, "")
			}
			read := func() ([]string, error) { return tt.tags, tt.readErr }

			wrote, err := mate.SeedSignIns(storePath, markPath, read, now)

			if (err != nil) != tt.wantErr || wrote != tt.wantWrote {
				t.Fatalf("SeedSignIns = %v, %v; want wrote %v, error %v", wrote, err, tt.wantWrote, tt.wantErr)
			}
			raw, readErr := os.ReadFile(storePath)
			switch {
			case tt.store != "":
				if string(raw) != tt.store {
					t.Errorf("the store present was rewritten: %s", raw)
				}
			case tt.wantStore == nil:
				if readErr == nil {
					t.Errorf("a store was written: %s", raw)
				}
			default:
				var got map[string]mate.SignIn
				if err := json.Unmarshal(raw, &got); err != nil {
					t.Fatalf("the store does not read: %v (%s)", err, raw)
				}
				if !reflect.DeepEqual(got, tt.wantStore) {
					t.Errorf("store = %+v, want %+v", got, tt.wantStore)
				}
			}
			if _, err := os.Stat(markPath); (err == nil) != (tt.wantMark || tt.seeded) {
				t.Errorf("the seeded mark is there: %v, want %v", err == nil, tt.wantMark || tt.seeded)
			}
		})
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
