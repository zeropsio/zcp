// Tests for: tools/hq_change.go — the title a change opens with.
package tools

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/zeropsio/zcp/internal/ops"
)

// TestCutChangeTitle: a line made a change's title is its first line, never
// cut mid-word: a line longer than a title is cut at the last word that fits,
// and says so with "…".
func TestCutChangeTitle(t *testing.T) {
	long := "Build a todo app with a list, a form and filters, wired to Postgres and a cache, and a status page that shows the number of todos still open"
	tests := []struct {
		name string
		line string
		want string
	}{
		{name: "a short line stays whole", line: "  Add a login page  ", want: "Add a login page"},
		{name: "only the first line", line: "Add a login page\nwith a form", want: "Add a login page"},
		{name: "a NUL no title keeps", line: "Add a\x00 login page", want: "Add a login page"},
		{name: "a long line cut at the last word that fits", line: long,
			want: "Build a todo app with a list, a form and filters, wired to Postgres and a cache, and a status page that shows the…"},
		{name: "a cut that ends on punctuation drops it", line: strings.Repeat("word ", 22) + "end, continuing",
			want: strings.TrimSpace(strings.Repeat("word ", 22)) + " end…"},
		{name: "one word longer than a title is cut where it must be", line: strings.Repeat("a", 200),
			want: strings.Repeat("a", changeTitleRunes-1) + "…"},
		{name: "a line exactly a title long stays whole", line: strings.Repeat("b", changeTitleRunes),
			want: strings.Repeat("b", changeTitleRunes)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cutChangeTitle(tt.line)
			if got != tt.want {
				t.Errorf("cutChangeTitle = %q, want %q", got, tt.want)
			}
			if n := utf8.RuneCountInString(got); n > changeTitleRunes {
				t.Errorf("title is %d runes, more than %d", n, changeTitleRunes)
			}
		})
	}
}

// TestChangeTitleOfWork: a bare change — pushed, not described yet — is
// titled by what it holds in its own repository, never by the work session's
// task, which is the same for every repository it touched: the newest commit
// the Mate wrote, else the files that differ from main.
func TestChangeTitleOfWork(t *testing.T) {
	long := strings.Repeat("Rework the checkout ", 10)
	tests := []struct {
		name string
		work ops.ChangeWork
		want string
	}{
		{name: "the Mate's newest commit", work: ops.ChangeWork{Subject: "Add the product API"}, want: "Add the product API"},
		{name: "a long subject cut at a word", work: ops.ChangeWork{Subject: long},
			want: cutChangeTitle(long)},
		{name: "one file added", work: ops.ChangeWork{Count: 1, Files: []string{"index.js"}, Kinds: "A"}, want: "Add index.js"},
		{name: "two files added", work: ops.ChangeWork{Count: 2, Files: []string{".gitignore", "index.js"}, Kinds: "A"},
			want: "Add .gitignore and index.js"},
		{name: "one more file", work: ops.ChangeWork{Count: 3, Files: []string{"a.js", "b.js"}, Kinds: "A"},
			want: "Add a.js, b.js and 1 more file"},
		{name: "files modified", work: ops.ChangeWork{Count: 5, Files: []string{"a.js", "b.js"}, Kinds: "MT"},
			want: "Update a.js, b.js and 3 more files"},
		{name: "files removed", work: ops.ChangeWork{Count: 1, Files: []string{"old.js"}, Kinds: "D"}, want: "Remove old.js"},
		{name: "files added and updated", work: ops.ChangeWork{Count: 2, Files: []string{"a.js", "b.js"}, Kinds: "AM"},
			want: "Change a.js and b.js"},
		{name: "kinds mixed", work: ops.ChangeWork{Count: 3, Files: []string{"a.js", "b.js"}, Kinds: "DAM"},
			want: "Change a.js, b.js and 1 more file"},
		{name: "nothing read: zcp's own words for the repository", work: ops.ChangeWork{}, want: "Mate: appdev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := changeTitleOfWork(tt.work, "appdev")
			if got != tt.want {
				t.Errorf("changeTitleOfWork = %q, want %q", got, tt.want)
			}
			if n := utf8.RuneCountInString(got); n > changeTitleRunes {
				t.Errorf("title is %d runes, more than %d", n, changeTitleRunes)
			}
		})
	}
}
