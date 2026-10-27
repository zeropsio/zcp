// Tests for: tools/origin.go action="withdraw" — the agent takes back an ask
// it no longer needs (re-planned, repository retired, asked by mistake), so
// Mate closes its card instead of the agent saying so in chat
// (docs/spec-mate.md §5.10).
package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestOriginWithdraw_AnswersWhatItWithdrew: a withdraw writes nothing and asks
// HQ nothing; it answers {"withdrawn":{"origin":{"url","repo"}},"nextActions"}
// — the address canonical, as a request answers it, and repo the repository
// the withdrawn ask named — and without repo it withdraws every ask naming
// that address, so the answer names none.
func TestOriginWithdraw_AnswersWhatItWithdrew(t *testing.T) {
	tests := []struct {
		name     string
		args     map[string]any
		wantJSON string
		wantNext string
	}{
		{
			name:     "the ask of one repository",
			args:     map[string]any{"action": "withdraw", "url": "https://github.com/zeropsio/mate.git", "repo": "appdev"},
			wantJSON: `{"withdrawn":{"origin":{"url":"https://github.com/zeropsio/mate","repo":"appdev"}}`,
			wantNext: "appdev",
		},
		{
			name:     "every ask naming the address",
			args:     map[string]any{"action": "withdraw", "url": "https://github.com/zeropsio/mate"},
			wantJSON: `{"withdrawn":{"origin":{"url":"https://github.com/zeropsio/mate"}}`,
			wantNext: "every ask",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, others := originHQ(t, `[]`)

			result := callTool(t, srv, "zerops_origin", tt.args)

			text := getTextContent(t, result)
			if result.IsError {
				t.Fatalf("want a withdraw, got a refusal: %s", text)
			}
			if !strings.HasPrefix(text, tt.wantJSON+`,"nextActions":"`) {
				t.Errorf("answer = %s, want it to start %s and carry nextActions", text, tt.wantJSON)
			}
			var answer struct {
				NextActions string `json:"nextActions"`
			}
			if err := json.Unmarshal([]byte(text), &answer); err != nil {
				t.Fatalf("answer is not JSON: %v\n%s", err, text)
			}
			for _, want := range []string{tt.wantNext, "withdrawn", "nothing more to say"} {
				if !strings.Contains(answer.NextActions, want) {
					t.Errorf("nextActions lacks %q: %s", want, answer.NextActions)
				}
			}
			if got := others(); len(got) != 0 {
				t.Errorf("a withdraw sent HQ %v", got)
			}
		})
	}
}

// TestOriginWithdraw_BadInput_Refused: a withdraw names the address as it was
// asked — https, no credential — and a repository that is a hostname; the
// refusal never echoes the address.
func TestOriginWithdraw_BadInput_Refused(t *testing.T) {
	srv, _ := originHQ(t, `[]`)
	for name, args := range map[string]map[string]any{
		"no url":           {"action": "withdraw", "repo": "appdev"},
		"url with a token": {"action": "withdraw", "url": "https://ghp_secret@github.com/zeropsio/mate"},
		"repo no hostname": {"action": "withdraw", "url": "https://github.com/zeropsio/mate", "repo": "App Dev"},
	} {
		result := callTool(t, srv, "zerops_origin", args)
		text := getTextContent(t, result)
		if !result.IsError {
			t.Errorf("%s: want a refusal, got %s", name, text)
		}
		if strings.Contains(text, "ghp_secret") {
			t.Errorf("%s: the refusal echoes the address: %s", name, text)
		}
	}
}

// TestOriginRequest_NamesTheWithdraw: an ask's answer says how to take it back
// once the agent no longer needs the pick — never by telling the person in
// chat that its card is outdated.
func TestOriginRequest_NamesTheWithdraw(t *testing.T) {
	for _, url := range []string{"https://github.com/zeropsio/mate", "https://gitlab.com/zeropsio/mate"} {
		srv, _ := originHQ(t, `[]`)
		answer, text := originAnswerTo(t, srv, url)
		if answer.Requested == nil {
			t.Fatalf("%s: want an ask, got %s", url, text)
		}
		for _, want := range []string{`action="withdraw"`, "never say in chat"} {
			if !strings.Contains(answer.NextActions, want) {
				t.Errorf("%s: nextActions lacks %q: %s", url, want, answer.NextActions)
			}
		}
	}
}
