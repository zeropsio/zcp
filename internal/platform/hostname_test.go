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
		{name: "valid_30_chars", hostname: "a23456789012345678901234567890", wantErr: false},
		{name: "valid_max_40", hostname: "a234567890123456789012345678901234567890", wantErr: false},
		{name: "empty", hostname: "", wantErr: true, wantCode: ErrInvalidHostname},
		{name: "starts_with_digit", hostname: "1app", wantErr: true, wantCode: ErrInvalidHostname},
		{name: "uppercase_and_hyphen", hostname: "My-App", wantErr: true, wantCode: ErrInvalidHostname},
		{name: "underscore", hostname: "my_app", wantErr: true, wantCode: ErrInvalidHostname},
		{name: "too_long_41", hostname: "a2345678901234567890123456789012345678901", wantErr: true, wantCode: ErrInvalidHostname},
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

func TestValidateLeaseAlias(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		alias   string
		wantErr bool
	}{
		{name: "pool_and_number", alias: "apppool-3"},
		{name: "two_digit_number", alias: "web-12"},
		{name: "max_40_char_base", alias: "a234567890123456789012345678901234567890-7"},
		{name: "no_number", alias: "apppool", wantErr: true},
		{name: "uppercase", alias: "App-3", wantErr: true},
		{name: "two_hyphens", alias: "a-b-3", wantErr: true},
		{name: "empty_number", alias: "apppool-", wantErr: true},
		{name: "empty_base", alias: "-3", wantErr: true},
		{name: "shell_suffix", alias: "apppool-3; rm", wantErr: true},
		{name: "trailing_newline", alias: "apppool-3\n", wantErr: true},
		{name: "ssh_config_injection", alias: "apppool-3\nProxyCommand x", wantErr: true},
		{name: "41_char_base", alias: "a2345678901234567890123456789012345678901-7", wantErr: true},
		{name: "path_traversal", alias: "../etc-3", wantErr: true},
		{name: "empty", alias: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateLeaseAlias(tt.alias)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateLeaseAlias(%q) = %v, wantErr %v", tt.alias, err, tt.wantErr)
			}
			if err != nil && err.Code != ErrInvalidHostname {
				t.Errorf("code = %s, want %s", err.Code, ErrInvalidHostname)
			}
		})
	}
}

func TestValidateContainerHostname(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		hostname string
		wantErr  bool
	}{
		{name: "pool_container", hostname: "node-id-3.runtime.apppool.zerops"},
		{name: "multi_digit_number", hostname: "node-id-1234.runtime.pool2.zerops"},
		{name: "no_node_prefix", hostname: "runtime.apppool.zerops", wantErr: true},
		{name: "non_numeric_number", hostname: "node-id-x.runtime.apppool.zerops", wantErr: true},
		{name: "empty_number", hostname: "node-id-.runtime.apppool.zerops", wantErr: true},
		{name: "wrong_suffix", hostname: "node-id-3.runtime.apppool.example.com", wantErr: true},
		{name: "uppercase_pool", hostname: "node-id-3.runtime.AppPool.zerops", wantErr: true},
		{name: "hyphenated_pool", hostname: "node-id-3.runtime.app-pool.zerops", wantErr: true},
		{name: "trailing_newline", hostname: "node-id-3.runtime.apppool.zerops\n", wantErr: true},
		{name: "newline_proxy_command", hostname: "node-id-3.runtime.apppool.zerops\n    ProxyCommand curl evil", wantErr: true},
		{name: "space_option", hostname: "node-id-3.runtime.apppool.zerops -oProxyCommand=x", wantErr: true},
		{name: "plain_service_hostname", hostname: "appdev", wantErr: true},
		{name: "empty", hostname: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateContainerHostname(tt.hostname)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateContainerHostname(%q) = %v, wantErr %v", tt.hostname, err, tt.wantErr)
			}
			if err != nil && err.Code != ErrInvalidHostname {
				t.Errorf("code = %s, want %s", err.Code, ErrInvalidHostname)
			}
		})
	}
}

func TestValidateMountName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{name: "service_hostname", in: "appdev"},
		{name: "lease_alias", in: "apppool-3"},
		{name: "neither", in: "app-dev-3", wantErr: true},
		{name: "shell", in: "app;rm -rf", wantErr: true},
		{name: "empty", in: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if err := ValidateMountName(tt.in); (err != nil) != tt.wantErr {
				t.Fatalf("ValidateMountName(%q) = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
		})
	}
}
