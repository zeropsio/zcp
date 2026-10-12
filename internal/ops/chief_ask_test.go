// Tests for: internal/ops/chief_ask.go — a project's Mate proposes asking
// the person's Chief, and sends nothing (docs/spec-mate.md §2.11).
package ops

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// TestProposeAskChief_Shape pins the chief.message proposal the client
// builds its card from, field by field.
func TestProposeAskChief_Shape(t *testing.T) {
	t.Parallel()
	long := "Production's medusa throws 502s since this morning's release; can you look at its logs and tell me what broke?"
	tests := []struct {
		name string
		in   AskChiefInput
		want string
	}{
		{
			name: "words alone",
			in:   AskChiefInput{Words: "Is stage up?", Reason: "the tests hit stage"},
			want: `{"proposal":{"id":"11111111-2222-3333-4444-555555555555","kind":"chief.message","summary":"Ask your Chief: Is stage up?","reason":"the tests hit stage","projectId":"pRossy","projectName":"Northwind Shop - Rossy","params":{"words":"Is stage up?"}},"note":"Nothing has been sent. The person approves or declines it on the card."}`,
		},
		{
			name: "with an attachment, words trimmed",
			in:   AskChiefInput{Words: "  Why does prod fail?\n", AttachmentTitle: "my failing test", AttachmentText: "GET /cart 502", Reason: "prod is outside my project"},
			want: `{"proposal":{"id":"11111111-2222-3333-4444-555555555555","kind":"chief.message","summary":"Ask your Chief: Why does prod fail?","reason":"prod is outside my project","projectId":"pRossy","projectName":"Northwind Shop - Rossy","params":{"words":"Why does prod fail?","attachment":{"title":"my failing test","text":"GET /cart 502"}}},"note":"Nothing has been sent. The person approves or declines it on the card."}`,
		},
		{
			name: "an attachment without a title",
			in:   AskChiefInput{Words: "Look at this.", AttachmentText: "trace", Reason: "r"},
			want: `{"proposal":{"id":"11111111-2222-3333-4444-555555555555","kind":"chief.message","summary":"Ask your Chief: Look at this.","reason":"r","projectId":"pRossy","projectName":"Northwind Shop - Rossy","params":{"words":"Look at this.","attachment":{"title":"","text":"trace"}}},"note":"Nothing has been sent. The person approves or declines it on the card."}`,
		},
		{
			name: "long words are cut in the summary, on one line",
			in:   AskChiefInput{Words: "Production's medusa\nthrows 502s since this morning's release; can you look at its logs and tell me what broke?", Reason: "r"},
			want: `{"proposal":{"id":"11111111-2222-3333-4444-555555555555","kind":"chief.message","summary":"Ask your Chief: ` + long[:60] + `…","reason":"r","projectId":"pRossy","projectName":"Northwind Shop - Rossy","params":{"words":"Production's medusa\nthrows 502s since this morning's release; can you look at its logs and tell me what broke?"}},"note":"Nothing has been sent. The person approves or declines it on the card."}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ProposeAskChief("pRossy", "Northwind Shop - Rossy", fixedID, tt.in)
			if err != nil {
				t.Fatalf("ProposeAskChief: %v", err)
			}
			raw, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			if string(raw) != tt.want {
				t.Errorf("proposal =\n%s\nwant\n%s", raw, tt.want)
			}
		})
	}
}

// TestProposeAskChief_Validates refuses what the card could not carry.
func TestProposeAskChief_Validates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		in      AskChiefInput
		wantMsg string
	}{
		{name: "no words", in: AskChiefInput{Words: "  ", Reason: "r"}, wantMsg: "needs words"},
		{name: "no reason", in: AskChiefInput{Words: "w", Reason: " "}, wantMsg: "needs a reason"},
		{name: "a title without text", in: AskChiefInput{Words: "w", Reason: "r", AttachmentTitle: "logs"}, wantMsg: "no text"},
		{name: "an attachment over 64 KiB", in: AskChiefInput{Words: "w", Reason: "r", AttachmentText: strings.Repeat("x", 64<<10+1)}, wantMsg: "over"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ProposeAskChief("pRossy", "Northwind Shop - Rossy", fixedID, tt.in)
			var pe *platform.PlatformError
			if !errors.As(err, &pe) || pe.Code != platform.ErrInvalidParameter || !strings.Contains(pe.Message, tt.wantMsg) {
				t.Fatalf("err = %v, want %s containing %q", err, platform.ErrInvalidParameter, tt.wantMsg)
			}
		})
	}
	if _, err := ProposeAskChief("pRossy", "Rossy", fixedID, AskChiefInput{Words: "w", Reason: "r", AttachmentText: strings.Repeat("x", 64<<10)}); err != nil {
		t.Errorf("an attachment of exactly 64 KiB is refused: %v", err)
	}
}
