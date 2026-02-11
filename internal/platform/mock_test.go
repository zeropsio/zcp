package platform

import (
	"context"
	"fmt"
	"testing"
)

// Tests for: design/zcp-prd.md section 4.2 (Mock Client)

func TestMock_WithUserInfo_Success(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		userInfo *UserInfo
	}{
		{
			name:     "returns configured user info",
			userInfo: &UserInfo{ID: "u1", FullName: "Test User", Email: "test@example.com"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := NewMock().WithUserInfo(tt.userInfo)
			got, err := m.GetUserInfo(context.Background())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.ID != tt.userInfo.ID {
				t.Errorf("ID = %q, want %q", got.ID, tt.userInfo.ID)
			}
			if got.FullName != tt.userInfo.FullName {
				t.Errorf("FullName = %q, want %q", got.FullName, tt.userInfo.FullName)
			}
			if got.Email != tt.userInfo.Email {
				t.Errorf("Email = %q, want %q", got.Email, tt.userInfo.Email)
			}
		})
	}
}

func TestMock_WithError_Override(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		method string
		err    error
	}{
		{
			name:   "GetUserInfo returns configured error",
			method: "GetUserInfo",
			err:    fmt.Errorf("auth failed"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := NewMock().
				WithUserInfo(&UserInfo{ID: "u1"}).
				WithError(tt.method, tt.err)
			_, err := m.GetUserInfo(context.Background())
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if err.Error() != tt.err.Error() {
				t.Errorf("error = %q, want %q", err.Error(), tt.err.Error())
			}
		})
	}
}

func TestMock_GetService_FallbackToList(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		services  []ServiceStack
		serviceID string
		wantName  string
	}{
		{
			name: "finds service in list by ID",
			services: []ServiceStack{
				{ID: "svc-1", Name: "api"},
				{ID: "svc-2", Name: "db"},
			},
			serviceID: "svc-2",
			wantName:  "db",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := NewMock().WithServices(tt.services)
			got, err := m.GetService(context.Background(), tt.serviceID)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", got.Name, tt.wantName)
			}
		})
	}
}

func TestMock_GetService_NotFound(t *testing.T) {
	t.Parallel()
	m := NewMock()
	_, err := m.GetService(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestMock_SetAutoscaling_NilProcess(t *testing.T) {
	t.Parallel()
	m := NewMock()
	proc, err := m.SetAutoscaling(context.Background(), "svc-1", AutoscalingParams{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proc != nil {
		t.Errorf("process = %v, want nil", proc)
	}
}

func TestMock_CancelProcess_StatusChange(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		processID  string
		wantStatus string
	}{
		{
			name:       "cancels process and updates status",
			processID:  "proc-1",
			wantStatus: statusCancelled,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := NewMock().WithProcess(&Process{ID: tt.processID, Status: "RUNNING"})
			got, err := m.CancelProcess(context.Background(), tt.processID)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", got.Status, tt.wantStatus)
			}
		})
	}
}

func TestMock_MockLogFetcher_Success(t *testing.T) {
	t.Parallel()
	entries := []LogEntry{
		{ID: "1", Timestamp: "2024-01-01T00:00:00Z", Severity: "info", Message: "hello"},
	}
	f := NewMockLogFetcher().WithEntries(entries)
	got, err := f.FetchLogs(context.Background(), nil, LogFetchParams{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Message != "hello" {
		t.Errorf("Message = %q, want %q", got[0].Message, "hello")
	}
}

func TestMock_MockLogFetcher_Error(t *testing.T) {
	t.Parallel()
	f := NewMockLogFetcher().WithError(fmt.Errorf("log fetch failed"))
	_, err := f.FetchLogs(context.Background(), nil, LogFetchParams{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
