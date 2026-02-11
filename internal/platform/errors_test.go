package platform

import (
	"context"
	"errors"
	"net"
	"testing"
)

// Tests for: design/zcp-prd.md section 4.4 (Error Codes)

func TestNewPlatformError_Fields(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		code       string
		message    string
		suggestion string
	}{
		{
			name:       "all fields populated",
			code:       ErrAuthRequired,
			message:    "authentication required",
			suggestion: "Set ZCP_API_KEY",
		},
		{
			name:       "empty suggestion",
			code:       ErrAPIError,
			message:    "api error",
			suggestion: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := NewPlatformError(tt.code, tt.message, tt.suggestion)
			if err.Code != tt.code {
				t.Errorf("Code = %q, want %q", err.Code, tt.code)
			}
			if err.Message != tt.message {
				t.Errorf("Message = %q, want %q", err.Message, tt.message)
			}
			if err.Suggestion != tt.suggestion {
				t.Errorf("Suggestion = %q, want %q", err.Suggestion, tt.suggestion)
			}
			if err.Error() != tt.message {
				t.Errorf("Error() = %q, want %q", err.Error(), tt.message)
			}
		})
	}
}

func TestMapNetworkError_Detection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		err       error
		wantCode  string
		wantIsNet bool
	}{
		{
			name:      "nil error",
			err:       nil,
			wantCode:  "",
			wantIsNet: false,
		},
		{
			name:      "net.OpError",
			err:       &net.OpError{Op: "dial", Err: errors.New("connection refused")},
			wantCode:  ErrNetworkError,
			wantIsNet: true,
		},
		{
			name:      "net.DNSError",
			err:       &net.DNSError{Err: "no such host", Name: "example.com"},
			wantCode:  ErrNetworkError,
			wantIsNet: true,
		},
		{
			name:      "connection refused string",
			err:       errors.New("connection refused"),
			wantCode:  ErrNetworkError,
			wantIsNet: true,
		},
		{
			name:      "no such host string",
			err:       errors.New("no such host"),
			wantCode:  ErrNetworkError,
			wantIsNet: true,
		},
		{
			name:      "context deadline exceeded",
			err:       errors.New("context deadline exceeded"),
			wantCode:  ErrAPITimeout,
			wantIsNet: true,
		},
		{
			name:      "random error",
			err:       errors.New("something went wrong"),
			wantCode:  "",
			wantIsNet: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			code, isNet := MapNetworkError(tt.err)
			if code != tt.wantCode {
				t.Errorf("code = %q, want %q", code, tt.wantCode)
			}
			if isNet != tt.wantIsNet {
				t.Errorf("isNetwork = %v, want %v", isNet, tt.wantIsNet)
			}
		})
	}
}

func TestMapNetworkError_ContextDeadlineExceeded(t *testing.T) {
	t.Parallel()
	code, isNet := MapNetworkError(context.DeadlineExceeded)
	if code != ErrAPITimeout {
		t.Errorf("code = %q, want %q", code, ErrAPITimeout)
	}
	if !isNet {
		t.Error("isNetwork = false, want true")
	}
}
