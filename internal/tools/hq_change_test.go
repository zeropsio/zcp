// Tests for: tools/hq_change.go — the title a change opens with.
package tools

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestChangeTitleOfIntent: a change opens with the task in the person's
// words, its first line, never cut mid-word: a task longer than a title is cut
// at the last word that fits, and says so with "…".
func TestChangeTitleOfIntent(t *testing.T) {
	long := "Build a todo app with a list, a form and filters, wired to Postgres and a cache, and a status page that shows the number of todos still open"
	tests := []struct {
		name   string
		intent string
		want   string
	}{
		{name: "a short task stays whole", intent: "  Add a login page  ", want: "Add a login page"},
		{name: "only the first line", intent: "Add a login page\nwith a form", want: "Add a login page"},
		{name: "a NUL no title keeps", intent: "Add a\x00 login page", want: "Add a login page"},
		{name: "a long task cut at the last word that fits", intent: long,
			want: "Build a todo app with a list, a form and filters, wired to Postgres and a cache, and a status page that shows the…"},
		{name: "a cut that ends on punctuation drops it", intent: strings.Repeat("word ", 22) + "end, continuing",
			want: strings.TrimSpace(strings.Repeat("word ", 22)) + " end…"},
		{name: "one word longer than a title is cut where it must be", intent: strings.Repeat("a", 200),
			want: strings.Repeat("a", changeTitleRunes-1) + "…"},
		{name: "a task exactly a title long stays whole", intent: strings.Repeat("b", changeTitleRunes),
			want: strings.Repeat("b", changeTitleRunes)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := changeTitleOfIntent(tt.intent)
			if got != tt.want {
				t.Errorf("changeTitleOfIntent = %q, want %q", got, tt.want)
			}
			if n := utf8.RuneCountInString(got); n > changeTitleRunes {
				t.Errorf("title is %d runes, more than %d", n, changeTitleRunes)
			}
		})
	}
}
