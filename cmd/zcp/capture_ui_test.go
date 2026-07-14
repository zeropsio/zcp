package main

import (
	"strings"
	"testing"
)

func TestParseCaptureUIArgs_ActiveHasOneAuthoritativeRoot(t *testing.T) {
	t.Parallel()

	_, err := parseCaptureUIArgs([]string{"--active", "--root", t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "cannot be combined with --root") {
		t.Fatalf("parseCaptureUIArgs() error = %v", err)
	}

	options, err := parseCaptureUIArgs([]string{"--active", "--no-open"})
	if err != nil {
		t.Fatalf("parseCaptureUIArgs(active) error = %v", err)
	}
	if !options.Active || !options.NoOpen || options.CaptureRoot != "" {
		t.Fatalf("parseCaptureUIArgs(active) = %+v", options)
	}
}
