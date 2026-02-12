// Tests for: ops/mount.go — MountService, UnmountService, MountStatus.
package ops

import (
	"context"
	"errors"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// mockMounter tracks calls and returns configurable results.
type mockMounter struct {
	mounted   map[string]bool
	writable  map[string]bool
	mountErr  error
	umountErr error
	checkErr  error
}

func newMockMounter() *mockMounter {
	return &mockMounter{
		mounted:  make(map[string]bool),
		writable: make(map[string]bool),
	}
}

func (m *mockMounter) IsMounted(_ context.Context, path string) (bool, error) {
	if m.checkErr != nil {
		return false, m.checkErr
	}
	return m.mounted[path], nil
}

func (m *mockMounter) Mount(_ context.Context, _, localPath string) error {
	if m.mountErr != nil {
		return m.mountErr
	}
	m.mounted[localPath] = true
	return nil
}

func (m *mockMounter) Unmount(_ context.Context, _, path string) error {
	if m.umountErr != nil {
		return m.umountErr
	}
	delete(m.mounted, path)
	return nil
}

func (m *mockMounter) IsWritable(_ context.Context, path string) (bool, error) {
	return m.writable[path], nil
}

func testServices() []platform.ServiceStack {
	return []platform.ServiceStack{
		{ID: "svc-1", Name: "app"},
		{ID: "svc-2", Name: "worker"},
		{ID: "svc-3", Name: "db"},
	}
}

func TestMountService_Success(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		hostname   string
		wantStatus string
	}{
		{
			name:       "new mount",
			hostname:   "app",
			wantStatus: "MOUNTED",
		},
		{
			name:       "different service",
			hostname:   "worker",
			wantStatus: "MOUNTED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock := platform.NewMock().WithServices(testServices())
			mounter := newMockMounter()

			result, err := MountService(context.Background(), mock, "proj-1", mounter, tt.hostname)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Status != tt.wantStatus {
				t.Errorf("status = %s, want %s", result.Status, tt.wantStatus)
			}
			if result.Hostname != tt.hostname {
				t.Errorf("hostname = %s, want %s", result.Hostname, tt.hostname)
			}
			if result.MountPath != "/var/www/"+tt.hostname {
				t.Errorf("mountPath = %s, want /var/www/%s", result.MountPath, tt.hostname)
			}
		})
	}
}

func TestMountService_AlreadyMounted(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices(testServices())
	mounter := newMockMounter()
	mounter.mounted["/var/www/app"] = true
	mounter.writable["/var/www/app"] = true

	result, err := MountService(context.Background(), mock, "proj-1", mounter, "app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "ALREADY_MOUNTED" {
		t.Errorf("status = %s, want ALREADY_MOUNTED", result.Status)
	}
	if !result.Writable {
		t.Error("expected writable=true for already mounted service")
	}
}

func TestMountService_ServiceNotFound(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices(testServices())
	mounter := newMockMounter()

	_, err := MountService(context.Background(), mock, "proj-1", mounter, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent service")
	}

	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrServiceNotFound {
		t.Errorf("code = %s, want %s", pe.Code, platform.ErrServiceNotFound)
	}
}

func TestMountService_InvalidHostname(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		hostname string
		wantCode string
	}{
		{name: "empty", hostname: "", wantCode: platform.ErrServiceRequired},
		{name: "starts with digit", hostname: "1app", wantCode: platform.ErrInvalidHostname},
		{name: "special chars", hostname: "app;rm -rf", wantCode: platform.ErrInvalidHostname},
		{name: "spaces", hostname: "my app", wantCode: platform.ErrInvalidHostname},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock := platform.NewMock().WithServices(testServices())
			mounter := newMockMounter()

			_, err := MountService(context.Background(), mock, "proj-1", mounter, tt.hostname)
			if err == nil {
				t.Fatal("expected error")
			}

			var pe *platform.PlatformError
			if !errors.As(err, &pe) {
				t.Fatalf("expected PlatformError, got %T: %v", err, err)
			}
			if pe.Code != tt.wantCode {
				t.Errorf("code = %s, want %s", pe.Code, tt.wantCode)
			}
		})
	}
}

func TestMountService_MountError(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices(testServices())
	mounter := newMockMounter()
	mounter.mountErr = errors.New("sshfs: connection refused")

	_, err := MountService(context.Background(), mock, "proj-1", mounter, "app")
	if err == nil {
		t.Fatal("expected error")
	}

	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrMountFailed {
		t.Errorf("code = %s, want %s", pe.Code, platform.ErrMountFailed)
	}
}

func TestMountService_CheckError(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices(testServices())
	mounter := newMockMounter()
	mounter.checkErr = errors.New("mountpoint command not found")

	_, err := MountService(context.Background(), mock, "proj-1", mounter, "app")
	if err == nil {
		t.Fatal("expected error")
	}

	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrMountFailed {
		t.Errorf("code = %s, want %s", pe.Code, platform.ErrMountFailed)
	}
}

func TestUnmountService_Success(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices(testServices())
	mounter := newMockMounter()
	mounter.mounted["/var/www/app"] = true

	result, err := UnmountService(context.Background(), mock, "proj-1", mounter, "app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "UNMOUNTED" {
		t.Errorf("status = %s, want UNMOUNTED", result.Status)
	}
	if result.MountPath != "/var/www/app" {
		t.Errorf("mountPath = %s, want /var/www/app", result.MountPath)
	}
}

func TestUnmountService_NotMounted(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices(testServices())
	mounter := newMockMounter()

	result, err := UnmountService(context.Background(), mock, "proj-1", mounter, "app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "NOT_MOUNTED" {
		t.Errorf("status = %s, want NOT_MOUNTED", result.Status)
	}
}

func TestUnmountService_UnmountError(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices(testServices())
	mounter := newMockMounter()
	mounter.mounted["/var/www/app"] = true
	mounter.umountErr = errors.New("device busy")

	_, err := UnmountService(context.Background(), mock, "proj-1", mounter, "app")
	if err == nil {
		t.Fatal("expected error")
	}

	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrUnmountFailed {
		t.Errorf("code = %s, want %s", pe.Code, platform.ErrUnmountFailed)
	}
}

func TestMountStatus_SingleService(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices(testServices())
	mounter := newMockMounter()
	mounter.mounted["/var/www/app"] = true
	mounter.writable["/var/www/app"] = true

	result, err := MountStatus(context.Background(), mock, "proj-1", mounter, "app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Mounts) != 1 {
		t.Fatalf("mounts count = %d, want 1", len(result.Mounts))
	}
	m := result.Mounts[0]
	if m.Hostname != "app" {
		t.Errorf("hostname = %s, want app", m.Hostname)
	}
	if !m.Mounted {
		t.Error("expected mounted=true")
	}
	if !m.Writable {
		t.Error("expected writable=true")
	}
}

func TestMountStatus_AllServices(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices(testServices())
	mounter := newMockMounter()
	mounter.mounted["/var/www/app"] = true

	result, err := MountStatus(context.Background(), mock, "proj-1", mounter, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Mounts) != 3 {
		t.Fatalf("mounts count = %d, want 3", len(result.Mounts))
	}

	// Verify app is mounted, others are not.
	for _, m := range result.Mounts {
		if m.Hostname == "app" {
			if !m.Mounted {
				t.Error("app should be mounted")
			}
		} else {
			if m.Mounted {
				t.Errorf("%s should not be mounted", m.Hostname)
			}
		}
	}
}

func TestMountStatus_ServiceNotFound(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices(testServices())
	mounter := newMockMounter()

	_, err := MountStatus(context.Background(), mock, "proj-1", mounter, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent service")
	}

	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrServiceNotFound {
		t.Errorf("code = %s, want %s", pe.Code, platform.ErrServiceNotFound)
	}
}
