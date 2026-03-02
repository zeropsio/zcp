// Tests for: plans/analysis/ops.md § process
package ops

import (
	"context"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestGetProcessStatus(t *testing.T) {
	t.Parallel()

	failReason := "out of memory"

	tests := []struct {
		name       string
		processID  string
		mock       *platform.Mock
		wantStatus string
		wantFail   *string
		wantErr    string
	}{
		{
			name:      "Success",
			processID: "proc-1",
			mock: platform.NewMock().
				WithProcess(&platform.Process{
					ID:         "proc-1",
					ActionName: "restart",
					Status:     "RUNNING",
					Created:    "2024-01-01T00:00:00Z",
				}),
			wantStatus: "RUNNING",
		},
		{
			name:      "Failed_WithReason",
			processID: "proc-2",
			mock: platform.NewMock().
				WithProcess(&platform.Process{
					ID:         "proc-2",
					ActionName: "start",
					Status:     statusFailed,
					Created:    "2024-01-01T00:00:00Z",
					FailReason: &failReason,
				}),
			wantStatus: statusFailed,
			wantFail:   &failReason,
		},
		{
			name:      "NotFound",
			processID: "proc-missing",
			mock:      platform.NewMock(),
			wantErr:   platform.ErrProcessNotFound,
		},
		{
			name:      "EmptyID",
			processID: "",
			mock:      platform.NewMock(),
			wantErr:   platform.ErrInvalidParameter,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := GetProcessStatus(context.Background(), tt.mock, tt.processID)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				pe, ok := err.(*platform.PlatformError)
				if !ok {
					t.Fatalf("expected *PlatformError, got %T: %v", err, err)
				}
				if pe.Code != tt.wantErr {
					t.Fatalf("expected code %s, got %s", tt.wantErr, pe.Code)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.ProcessID != tt.processID {
				t.Errorf("expected processId=%s, got %s", tt.processID, result.ProcessID)
			}
			if result.Status != tt.wantStatus {
				t.Errorf("expected status=%s, got %s", tt.wantStatus, result.Status)
			}
			if tt.wantFail != nil {
				if result.FailReason == nil {
					t.Fatal("expected FailReason, got nil")
				}
				if *result.FailReason != *tt.wantFail {
					t.Errorf("expected failReason=%s, got %s", *tt.wantFail, *result.FailReason)
				}
			}
		})
	}
}

func TestGetProcessStatus_ErrorPropagation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mock    *platform.Mock
		wantErr string
	}{
		{
			name: "AuthExpired_Propagated",
			mock: platform.NewMock().
				WithError("GetProcess", platform.NewPlatformError(platform.ErrAuthTokenExpired, "token expired", "Refresh token")),
			wantErr: platform.ErrAuthTokenExpired,
		},
		{
			name: "NetworkError_Propagated",
			mock: platform.NewMock().
				WithError("GetProcess", platform.NewPlatformError(platform.ErrNetworkError, "connection refused", "Check network")),
			wantErr: platform.ErrNetworkError,
		},
		{
			name:    "PlainError_FallbackToProcessNotFound",
			mock:    platform.NewMock(), // no process configured → returns plain fmt.Errorf
			wantErr: platform.ErrProcessNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := GetProcessStatus(context.Background(), tt.mock, "proc-test")
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			pe, ok := err.(*platform.PlatformError)
			if !ok {
				t.Fatalf("expected *PlatformError, got %T: %v", err, err)
			}
			if pe.Code != tt.wantErr {
				t.Errorf("Code = %q, want %q", pe.Code, tt.wantErr)
			}
		})
	}
}

func TestCancelProcess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		processID string
		mock      *platform.Mock
		wantErr   string
	}{
		{
			name:      "Success",
			processID: "proc-1",
			mock: platform.NewMock().
				WithProcess(&platform.Process{
					ID:     "proc-1",
					Status: "RUNNING",
				}),
		},
		{
			name:      "AlreadyTerminal_Finished",
			processID: "proc-2",
			mock: platform.NewMock().
				WithProcess(&platform.Process{
					ID:     "proc-2",
					Status: statusFinished,
				}),
			wantErr: platform.ErrProcessAlreadyTerminal,
		},
		{
			name:      "AlreadyTerminal_Failed",
			processID: "proc-3",
			mock: platform.NewMock().
				WithProcess(&platform.Process{
					ID:     "proc-3",
					Status: statusFailed,
				}),
			wantErr: platform.ErrProcessAlreadyTerminal,
		},
		{
			name:      "AlreadyTerminal_Canceled",
			processID: "proc-4",
			mock: platform.NewMock().
				WithProcess(&platform.Process{
					ID:     "proc-4",
					Status: "CANCELED",
				}),
			wantErr: platform.ErrProcessAlreadyTerminal,
		},
		{
			name:      "NotFound",
			processID: "proc-missing",
			mock:      platform.NewMock(),
			wantErr:   platform.ErrProcessNotFound,
		},
		{
			name:      "EmptyID",
			processID: "",
			mock:      platform.NewMock(),
			wantErr:   platform.ErrInvalidParameter,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := CancelProcess(context.Background(), tt.mock, tt.processID)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				pe, ok := err.(*platform.PlatformError)
				if !ok {
					t.Fatalf("expected *PlatformError, got %T: %v", err, err)
				}
				if pe.Code != tt.wantErr {
					t.Fatalf("expected code %s, got %s", tt.wantErr, pe.Code)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.ProcessID != tt.processID {
				t.Errorf("expected processId=%s, got %s", tt.processID, result.ProcessID)
			}
			if result.Status != "CANCELED" {
				t.Errorf("expected status=CANCELED, got %s", result.Status)
			}
		})
	}
}

func TestCancelProcess_ErrorPropagation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mock    *platform.Mock
		wantErr string
	}{
		{
			name: "GetProcess_AuthExpired_Propagated",
			mock: platform.NewMock().
				WithError("GetProcess", platform.NewPlatformError(platform.ErrAuthTokenExpired, "token expired", "")),
			wantErr: platform.ErrAuthTokenExpired,
		},
		{
			name: "CancelProcess_APIError_Propagated",
			mock: platform.NewMock().
				WithProcess(&platform.Process{ID: "proc-1", Status: "RUNNING"}).
				WithError("CancelProcess", platform.NewPlatformError(platform.ErrPermissionDenied, "no permission", "")),
			wantErr: platform.ErrPermissionDenied,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := CancelProcess(context.Background(), tt.mock, "proc-1")
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			pe, ok := err.(*platform.PlatformError)
			if !ok {
				t.Fatalf("expected *PlatformError, got %T: %v", err, err)
			}
			if pe.Code != tt.wantErr {
				t.Errorf("Code = %q, want %q", pe.Code, tt.wantErr)
			}
		})
	}
}
