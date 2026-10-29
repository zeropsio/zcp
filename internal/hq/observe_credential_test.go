package hq

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadObserveCredential_FileStates_TypedSafeErrors(t *testing.T) {
	t.Parallel()
	token := "zobs.org." + strings.Repeat("A", 43)
	for _, tt := range []struct {
		name, body string
		missing    bool
		want       error
	}{
		{name: "missing", missing: true, want: ErrNoObserveCredential},
		{name: "empty", want: ErrInvalidObserveCredential},
		{name: "corrupt", body: "private-reflected-secret", want: ErrInvalidObserveCredential},
		{name: "project key", body: "Project private-reflected-secret", want: ErrInvalidObserveCredential},
		{name: "oversize", body: strings.Repeat("x", 257), want: ErrInvalidObserveCredential},
		{name: "header injection", body: token + "\r\nInjected: private-reflected-secret", want: ErrInvalidObserveCredential},
		{name: "embedded newline", body: "zobs.org." + strings.Repeat("A", 20) + "\n" + strings.Repeat("A", 23), want: ErrInvalidObserveCredential},
		{name: "short secret", body: "zobs.org.AAAA", want: ErrInvalidObserveCredential},
		{name: "padded secret", body: token + "=", want: ErrInvalidObserveCredential},
		{name: "bad org", body: "zobs.org/id." + strings.Repeat("A", 43), want: ErrInvalidObserveCredential},
		{name: "valid", body: token}, {name: "writer newline", body: token + "\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "credential")
			if !tt.missing {
				if err := os.WriteFile(path, []byte(tt.body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := ReadObserveCredential(path)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error class = %v, want %v", err, tt.want)
			}
			if err != nil {
				if got != "" || strings.Contains(err.Error(), "private-reflected-secret") {
					t.Fatal("credential leaked")
				}
			} else if got != token {
				t.Fatal("credential changed")
			}
		})
	}
}

func TestReadObserveCredential_Rotation_ReadsReplacement(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "credential")
	for _, suffix := range []string{strings.Repeat("A", 43), strings.Repeat("B", 42) + "A"} {
		token := "zobs.org." + suffix
		if err := os.WriteFile(path+".next", []byte(token), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(path+".next", path); err != nil {
			t.Fatal(err)
		}
		if got, err := ReadObserveCredential(path); err != nil || got != token {
			t.Fatalf("rotation did not read replacement: %v", err)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadObserveCredential(path); !errors.Is(err, ErrNoObserveCredential) {
		t.Fatalf("removed credential: %v", err)
	}
}
