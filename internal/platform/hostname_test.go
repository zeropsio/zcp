package platform

import (
	"errors"
	"testing"
)

func TestValidateHostname(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		hostname string
		wantErr  bool
		wantCode string
	}{
		{name: "valid_short", hostname: "app", wantErr: false},
		{name: "valid_with_digits", hostname: "db1", wantErr: false},
		{name: "valid_single_char", hostname: "a", wantErr: false},
		{name: "valid_max_25", hostname: "a234567890123456789012345", wantErr: false},
		{name: "empty", hostname: "", wantErr: true, wantCode: ErrInvalidHostname},
		{name: "starts_with_digit", hostname: "1app", wantErr: true, wantCode: ErrInvalidHostname},
		{name: "uppercase_and_hyphen", hostname: "My-App", wantErr: true, wantCode: ErrInvalidHostname},
		{name: "underscore", hostname: "my_app", wantErr: true, wantCode: ErrInvalidHostname},
		{name: "too_long_26", hostname: "a2345678901234567890123456", wantErr: true, wantCode: ErrInvalidHostname},
		{name: "special_char", hostname: "app!", wantErr: true, wantCode: ErrInvalidHostname},
		{name: "all_uppercase", hostname: "APP", wantErr: true, wantCode: ErrInvalidHostname},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateHostname(tt.hostname)
			if !tt.wantErr {
				if err != nil {
					t.Errorf("expected no error, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error with code %s, got nil", tt.wantCode)
			}
			var pe *PlatformError
			if !errors.As(err, &pe) {
				t.Fatalf("expected *PlatformError, got %T: %v", err, err)
			}
			if pe.Code != tt.wantCode {
				t.Errorf("code = %s, want %s", pe.Code, tt.wantCode)
			}
		})
	}
}
