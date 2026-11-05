package hq

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadMateChanges_FileStates: the file holds the link's last `state`
// frame's changes as the Mate's server encodes them; no file is no finished
// change, and a file that is no such frame is an error, never a state.
func TestReadMateChanges_FileStates(t *testing.T) {
	t.Parallel()
	const frame = `{"appId":"app-1","appName":"Shop","changes":[` +
		`{"repo":"appdev","number":3,"title":"Fix the login","state":"merged","head":null,"mergedSha":null,"landedHead":null,"branch":"fix-login"},` +
		`{"repo":"appdev","number":2,"title":null,"state":"open","head":null,"mergedSha":null,"landedHead":null,"branch":"mate/p-1/2","ready":false,"updatedAt":"2026-11-04T10:00:00.000Z"}` +
		`],"openPerBranch":true}`
	for _, tt := range []struct {
		name, body string
		missing    bool
		wantErr    bool
		wantApp    string
		wantStates []string
	}{
		{name: "no file", missing: true},
		{name: "a frame", body: frame, wantApp: "app-1", wantStates: []string{ChangeMerged, ChangeOpen}},
		{name: "a frame of a Mate in no application", body: `{"appId":null,"appName":null,"changes":[]}`},
		{name: "not a frame", body: `{"appId":`, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "mate-changes.json")
			if !tt.missing {
				if err := os.WriteFile(path, []byte(tt.body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := ReadMateChanges(path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, want error %v", err, tt.wantErr)
			}
			app := ""
			if got.AppID != nil {
				app = *got.AppID
			}
			if app != tt.wantApp {
				t.Errorf("appId = %q, want %q", app, tt.wantApp)
			}
			states := make([]string, 0, len(got.Changes))
			for _, c := range got.Changes {
				states = append(states, c.State)
			}
			if strings.Join(states, ",") != strings.Join(tt.wantStates, ",") {
				t.Errorf("states = %v, want %v", states, tt.wantStates)
			}
			if tt.name == "a frame" {
				if c := got.Changes[0]; c.Repo != "appdev" || c.Number != 3 || c.Branch != "fix-login" {
					t.Errorf("merged change = %+v, want appdev #3 on fix-login", c)
				}
				if c := got.Changes[1]; c.UpdatedAt != "2026-11-04T10:00:00.000Z" || !got.OpenPerBranch {
					t.Errorf("open change = %+v, openPerBranch %v; want its updatedAt and openPerBranch", c, got.OpenPerBranch)
				}
			}
		})
	}
}
