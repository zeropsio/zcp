// Tests for: internal/devsup/state.go — reading a server's log since an
// earlier read (`ctl logs --since`).
package devsup

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func logSince(t *testing.T, content, cursor string) LogRead {
	t.Helper()
	read, err := readLogSince(bytes.NewReader([]byte(content)), int64(len(content)), cursor, 500)
	if err != nil {
		t.Fatalf("readLogSince: %v", err)
	}
	return read
}

// LG-SINCE — A log read since a cursor answers only the complete lines written
// after the earlier read, so a status never repeats a line nor cuts one still
// being written.
// witness: internal/devsup/state.go | err == nil && off >= 0 && off <= size { → err == nil && off >= 0 && off > size {
func TestLogSince_LGSince_OnlyCompleteLinesAfterTheCursor(t *testing.T) {
	t.Parallel()
	first := "--- zcp-devsup: start #1 at 2026-10-14T21:00:00Z: vite\nready in 2 s\n"
	read := logSince(t, first, "")
	if want := []string{"--- zcp-devsup: start #1 at 2026-10-14T21:00:00Z: vite", "ready in 2 s"}; !reflect.DeepEqual(read.Lines, want) {
		t.Fatalf("first read = %q, want %q", read.Lines, want)
	}
	second := first + "hmr update /src/App.tsx\n[vite] Internal server er"
	read = logSince(t, second, read.Cursor)
	if want := []string{"hmr update /src/App.tsx"}; !reflect.DeepEqual(read.Lines, want) {
		t.Fatalf("read after the cursor = %q, want %q", read.Lines, want)
	}
	read = logSince(t, second+"ror: Failed to resolve import\n", read.Cursor)
	if want := []string{"[vite] Internal server error: Failed to resolve import"}; !reflect.DeepEqual(read.Lines, want) {
		t.Fatalf("read once the line ended = %q, want %q", read.Lines, want)
	}
}

// LG-OTHER-FILE — A cursor of another log file (one rotated away, a new
// container behind the same alias) reads the log from its start, never from
// that file's offset.
// witness: internal/devsup/state.go | ok && id != "" && cid == id { → ok && id != "" && cid != "" {
func TestLogSince_LGOtherFile_ReadFromItsStart(t *testing.T) {
	t.Parallel()
	old := "--- zcp-devsup: start #3 at 2026-10-14T20:00:00Z: vite\nready in 2 s\n"
	cursor := logSince(t, old, "").Cursor
	other := "--- zcp-devsup: start #1 at 2026-10-14T21:00:00Z: vite\nError: Cannot find module 'vite'\nexit 1\n"
	read := logSince(t, other, cursor)
	want := []string{"--- zcp-devsup: start #1 at 2026-10-14T21:00:00Z: vite", "Error: Cannot find module 'vite'", "exit 1"}
	if !reflect.DeepEqual(read.Lines, want) {
		t.Errorf("read of another log = %q, want it whole %q", read.Lines, want)
	}
}

// LG-LONG-HEAD — A log whose first line is longer than the part that names the file is still named by that part, so a status after it reads only the lines written since, never the whole log again.
// witness: internal/devsup/state.go | ok || len(head) == logHeadSize { → ok {
func TestLogSince_LGLongHead_NamedByItsHead(t *testing.T) {
	t.Parallel()
	first := strings.Repeat("x", logHeadSize+100) + "\nready in 2 s\n"
	cursor := logSince(t, first, "").Cursor

	read := logSince(t, first+"hmr update /src/App.tsx\n", cursor)

	if want := []string{"hmr update /src/App.tsx"}; !reflect.DeepEqual(read.Lines, want) {
		t.Errorf("read after the cursor = %q, want %q", read.Lines, want)
	}
}
