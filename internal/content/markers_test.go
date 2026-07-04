package content

import "testing"

func TestIndexMarkerLine(t *testing.T) {
	t.Parallel()
	const m = "<!-- ZCP:BEGIN -->"
	tests := []struct {
		name string
		text string
		from int
		want int
	}{
		{"whole file is the marker", m, 0, 0},
		{"marker at start of file with newline", m + "\nbody\n", 0, 0},
		{"marker after newline", "head\n" + m + "\nbody\n", 0, 5},
		{"marker at end of text without newline", "head\n" + m, 0, 5},
		{"marker line with CRLF terminator", "head\r\n" + m + "\r\nbody", 0, 6},
		{"mid-line mention is not a marker", "prose about " + m + " inline\n", 0, -1},
		{"trailing text on the line disqualifies", m + " trailing\n", 0, -1},
		{"leading text on the line disqualifies", "lead " + m + "\n", 0, -1},
		{"indented marker is not anchored", "  " + m + "\n", 0, -1},
		{"first mention mid-line, second anchored", "see " + m + " here\n" + m + "\n", 0, 28},
		{"from skips an earlier anchored match", m + "\nx\n" + m + "\n", 1, 21},
		{"from may point mid-line", "see " + m + " here\n" + m + "\n", 6, 28},
		{"absent marker", "no markers here\n", 0, -1},
		{"empty text", "", 0, -1},
		{"negative from clamps to zero", m + "\n", -5, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IndexMarkerLine(tt.text, m, tt.from); got != tt.want {
				t.Errorf("IndexMarkerLine(%q, marker, %d) = %d, want %d", tt.text, tt.from, got, tt.want)
			}
		})
	}
}
