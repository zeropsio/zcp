package init

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWarnMissingClaudeMD_FileMissing_EmitsWarning(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var buf bytes.Buffer

	emitted := WarnMissingClaudeMD(dir, &buf)

	if !emitted {
		t.Fatalf("WarnMissingClaudeMD returned false; expected true for missing CLAUDE.md")
	}
	out := buf.String()
	if !strings.Contains(out, "WARNING") {
		t.Errorf("warning missing WARNING prefix: %q", out)
	}
	if !strings.Contains(out, "zcp init") {
		t.Errorf("warning missing remediation hint `zcp init`: %q", out)
	}
}

func TestWarnMissingClaudeMD_FilePresent_NoOp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), nil, 0o644); err != nil {
		t.Fatalf("seed CLAUDE.md: %v", err)
	}
	var buf bytes.Buffer

	emitted := WarnMissingClaudeMD(dir, &buf)

	if emitted {
		t.Fatalf("WarnMissingClaudeMD returned true; expected false when file present")
	}
	if buf.Len() != 0 {
		t.Errorf("writer should be empty when CLAUDE.md present, got %q", buf.String())
	}
}

func TestWarnMissingClaudeMD_StatError_NoEmit(t *testing.T) {
	t.Parallel()
	// Pass a path that contains a NUL byte — os.Stat returns a non-IsNotExist
	// error (invalid argument) rather than ENOENT. This lets us exercise the
	// "stat error other than not-exist → don't warn" branch without needing
	// platform-specific permission setup.
	var buf bytes.Buffer

	emitted := WarnMissingClaudeMD("/nonexistent\x00path", &buf)

	if emitted {
		t.Errorf("WarnMissingClaudeMD returned true on stat error; expected false")
	}
	if buf.Len() != 0 {
		t.Errorf("writer should be empty on stat error, got %q", buf.String())
	}
}
