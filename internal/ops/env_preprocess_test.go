package ops

import (
	"strings"
	"testing"
)

func TestExpandPreprocessor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		input       string
		wantLen     int
		wantPrefix  string
		wantSuffix  string
		wantLiteral string
		wantErr     string
	}{
		{
			name:    "bare_generate_random_string_32",
			input:   "<@generateRandomString(<32>)>",
			wantLen: 32,
		},
		{
			name:    "bare_generate_random_string_64",
			input:   "<@generateRandomString(<64>)>",
			wantLen: 64,
		},
		{
			name:       "prefix_preserved",
			input:      "base64:<@generateRandomString(<32>)>",
			wantLen:    len("base64:") + 32,
			wantPrefix: "base64:",
		},
		{
			name:       "suffix_preserved",
			input:      "<@generateRandomString(<32>)>=",
			wantLen:    33,
			wantSuffix: "=",
		},
		{
			name:        "plain_value_passthrough",
			input:       "literal-secret",
			wantLiteral: "literal-secret",
		},
		{
			name:        "empty_value_passthrough",
			input:       "",
			wantLiteral: "",
		},
		{
			name:    "unsupported_function_errors",
			input:   "<@generateRSA2048Key(<key1>)>",
			wantErr: "not supported",
		},
		{
			name:    "invalid_length_arg_errors",
			input:   "<@generateRandomString(<abc>)>",
			wantErr: "invalid length",
		},
		{
			name:    "zero_length_errors",
			input:   "<@generateRandomString(<0>)>",
			wantErr: "length must be between",
		},
		{
			name:    "oversize_length_errors",
			input:   "<@generateRandomString(<99999>)>",
			wantErr: "length must be between",
		},
		{
			name:    "malformed_arg_wrapper_errors",
			input:   "<@generateRandomString(32)>",
			wantErr: "invalid length",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := expandPreprocessor(tt.input)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got value %q", tt.wantErr, got)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %q, want to contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantLiteral != "" || tt.input == "" {
				if got != tt.wantLiteral {
					t.Errorf("got %q, want literal %q", got, tt.wantLiteral)
				}
				return
			}
			if tt.wantLen > 0 && len(got) != tt.wantLen {
				t.Errorf("length = %d, want %d (value: %q)", len(got), tt.wantLen, got)
			}
			if tt.wantPrefix != "" && !strings.HasPrefix(got, tt.wantPrefix) {
				t.Errorf("value %q missing prefix %q", got, tt.wantPrefix)
			}
			if tt.wantSuffix != "" && !strings.HasSuffix(got, tt.wantSuffix) {
				t.Errorf("value %q missing suffix %q", got, tt.wantSuffix)
			}
			// Alphanumeric-only assertion on the generated portion.
			gen := strings.TrimPrefix(got, tt.wantPrefix)
			gen = strings.TrimSuffix(gen, tt.wantSuffix)
			for _, r := range gen {
				ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
				if !ok {
					t.Errorf("non-alphanumeric char %q in generated string %q", r, gen)
					break
				}
			}
		})
	}
}

func TestExpandPreprocessor_RandomnessDiffers(t *testing.T) {
	t.Parallel()
	a, err := expandPreprocessor("<@generateRandomString(<32>)>")
	if err != nil {
		t.Fatal(err)
	}
	b, err := expandPreprocessor("<@generateRandomString(<32>)>")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Errorf("two invocations produced identical strings %q — rng not seeded", a)
	}
}
