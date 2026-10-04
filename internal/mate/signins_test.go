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

// TestSeedSignIns pins the one-time seed from HQ, preserving a store already
// present and ending an unavailable seed visibly without launch-time retries.
func TestSeedSignIns(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 2, 18, 0, 0, 0, time.UTC)
	tests := []struct {
		name              string
		brokenStoreParent bool
		store             string // "" for none
		seeded            bool
		signers           map[string]string
		readErr           error
		wantWrote         bool
		wantStore         map[string]mate.SignIn // nil: no store
		wantErr           bool
		wantMark          bool
	}{
		{
			name:      "HQ signers present: the store is written",
			signers:   map[string]string{"claude-code": "u-ada", "codex": "u-bo", "claudeAgent-work": "u-cy"},
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
			signers:  map[string]string{"codex": "u-bo"},
			wantMark: true,
		},
		{
			name:     "HQ has no signers: nothing",
			wantMark: true,
		},
		{
			name:     "signers the server would not read: nothing",
			signers:  map[string]string{"gemini": "u-ada", "codex": ""},
			wantMark: true,
		},
		{
			name:    "seeded once already, the store gone since: nothing",
			seeded:  true,
			signers: map[string]string{"codex": "u-bo"},
		},
		{
			name:              "a failed store write is visible and never automatically retried",
			brokenStoreParent: true,
			signers:           map[string]string{"codex": "u-bo"},
			wantErr:           true,
			wantMark:          true,
		},
		{
			name:     "HQ unavailable: nothing, and no automatic launch retry",
			readErr:  errors.New("HQ down"),
			wantErr:  true,
			wantMark: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			storePath := filepath.Join(dir, ".mate", "signed-in.json")
			markPath := filepath.Join(dir, ".zcp", "state", "mate-sign-ins-seeded")
			if tt.brokenStoreParent {
				if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Dir(storePath)); err != nil {
					t.Fatal(err)
				}
			}
			if tt.store != "" {
				writeFile(t, storePath, tt.store)
			}
			if tt.seeded {
				writeFile(t, markPath, "")
			}
			calls := 0
			read := func() (map[string]string, error) { calls++; return tt.signers, tt.readErr }

			wrote, err := mate.SeedSignIns(storePath, markPath, read, now)

			if (err != nil) != tt.wantErr || wrote != tt.wantWrote {
				t.Fatalf("SeedSignIns = %v, %v; want wrote %v, error %v", wrote, err, tt.wantWrote, tt.wantErr)
			}
			firstCalls := calls
			_, againErr := mate.SeedSignIns(storePath, markPath, read, now)
			if calls != firstCalls {
				t.Errorf("another launch read HQ again")
			}
			if tt.wantErr && againErr == nil {
				t.Errorf("failed attempt lost its visible reason")
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
