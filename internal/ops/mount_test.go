// Tests for: ops/mount.go — MountService, UnmountService, MountStatus.
package ops

import (
	"context"
	"errors"
	"maps"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// mockMounter tracks calls and returns configurable results.
type mockMounter struct {
	states         map[string]platform.MountState
	writable       map[string]bool
	mountErr       error
	umountErr      error
	forceUmountErr error
	checkErr       error
	mountDirs      []string
	mountDirsErr   error
}

func newMockMounter() *mockMounter {
	return &mockMounter{
		states:   make(map[string]platform.MountState),
		writable: make(map[string]bool),
	}
}

func (m *mockMounter) CheckMount(_ context.Context, path string) (platform.MountState, error) {
	if m.checkErr != nil {
		return platform.MountStateNotMounted, m.checkErr
	}
	state, ok := m.states[path]
	if !ok {
		return platform.MountStateNotMounted, nil
	}
	return state, nil
}

func (m *mockMounter) Mount(_ context.Context, _, localPath string) error {
	if m.mountErr != nil {
		return m.mountErr
	}
	m.states[localPath] = platform.MountStateActive
	return nil
}

func (m *mockMounter) Unmount(_ context.Context, _, path string) error {
	if m.umountErr != nil {
		return m.umountErr
	}
	delete(m.states, path)
	return nil
}

func (m *mockMounter) ForceUnmount(_ context.Context, _, path string) error {
	if m.forceUmountErr != nil {
		return m.forceUmountErr
	}
	delete(m.states, path)
	return nil
}

func (m *mockMounter) IsWritable(_ context.Context, path string) (bool, error) {
	return m.writable[path], nil
}

func (m *mockMounter) ListMountDirs(_ context.Context, _ string) ([]string, error) {
	return m.mountDirs, m.mountDirsErr
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
	mounter.states["/var/www/app"] = platform.MountStateActive
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

func TestMountService_StaleRemounted(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices(testServices())
	mounter := newMockMounter()
	mounter.states["/var/www/app"] = platform.MountStateStale

	result, err := MountService(context.Background(), mock, "proj-1", mounter, "app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "MOUNTED" {
		t.Errorf("status = %s, want MOUNTED", result.Status)
	}
	if result.MountPath != "/var/www/app" {
		t.Errorf("mountPath = %s, want /var/www/app", result.MountPath)
	}
}

func TestUnmountService_Success(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices(testServices())
	mounter := newMockMounter()
	mounter.states["/var/www/app"] = platform.MountStateActive

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
	mounter.states["/var/www/app"] = platform.MountStateActive
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

func TestUnmountService_StaleMountSuccess(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices(testServices())
	mounter := newMockMounter()
	mounter.states["/var/www/app"] = platform.MountStateStale

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

func TestUnmountService_StaleMountForceUnmountFails(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices(testServices())
	mounter := newMockMounter()
	mounter.states["/var/www/app"] = platform.MountStateStale
	mounter.forceUmountErr = errors.New("permission denied")

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

func TestUnmountService_ActiveServiceDeleted(t *testing.T) {
	t.Parallel()

	// Service exists in mount but not in API (deleted).
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-2", Name: "worker"},
	})
	mounter := newMockMounter()
	mounter.states["/var/www/app"] = platform.MountStateActive

	result, err := UnmountService(context.Background(), mock, "proj-1", mounter, "app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "UNMOUNTED" {
		t.Errorf("status = %s, want UNMOUNTED", result.Status)
	}
}

func TestMountStatus_SingleService(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices(testServices())
	mounter := newMockMounter()
	mounter.states["/var/www/app"] = platform.MountStateActive
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
	mounter.states["/var/www/app"] = platform.MountStateActive

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

func TestMountStatus_StaleMount(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices(testServices())
	mounter := newMockMounter()
	mounter.states["/var/www/app"] = platform.MountStateStale

	result, err := MountStatus(context.Background(), mock, "proj-1", mounter, "app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Mounts) != 1 {
		t.Fatalf("mounts count = %d, want 1", len(result.Mounts))
	}
	m := result.Mounts[0]
	if m.Mounted {
		t.Error("stale mount should report mounted=false")
	}
	if !m.Stale {
		t.Error("stale mount should report stale=true")
	}
}

func TestMountStatus_Messages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		services    []platform.ServiceStack
		mountDirs   []string
		states      map[string]platform.MountState
		hostname    string
		wantMessage string
	}{
		{
			name: "stale mount message",
			services: []platform.ServiceStack{
				{ID: "svc-1", Name: "app"},
			},
			states: map[string]platform.MountState{
				"/var/www/app": platform.MountStateStale,
			},
			hostname:    "app",
			wantMessage: "Mount is stale (transport disconnected). Unmount and remount after build/deploy completes.",
		},
		{
			name: "orphan stale mount message",
			services: []platform.ServiceStack{
				{ID: "svc-1", Name: "app"},
			},
			mountDirs: []string{"app", "deleted"},
			states: map[string]platform.MountState{
				"/var/www/deleted": platform.MountStateStale,
			},
			hostname:    "deleted",
			wantMessage: "Service was deleted but mount is stale. Use unmount to clean up.",
		},
		{
			name: "orphan not-mounted message",
			services: []platform.ServiceStack{
				{ID: "svc-1", Name: "app"},
			},
			mountDirs:   []string{"app", "oldservice"},
			states:      map[string]platform.MountState{},
			hostname:    "oldservice",
			wantMessage: "Leftover mount directory from deleted service.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock := platform.NewMock().WithServices(tt.services)
			mounter := newMockMounter()
			mounter.mountDirs = tt.mountDirs
			maps.Copy(mounter.states, tt.states)

			// Use empty hostname to get all services + orphans.
			result, err := MountStatus(context.Background(), mock, "proj-1", mounter, "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			var found bool
			for _, m := range result.Mounts {
				if m.Hostname == tt.hostname {
					found = true
					if m.Message != tt.wantMessage {
						t.Errorf("message = %q, want %q", m.Message, tt.wantMessage)
					}
				}
			}
			if !found {
				t.Errorf("hostname %s not found in results", tt.hostname)
			}
		})
	}
}

func TestMountStatus_OrphanDetection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		services   []platform.ServiceStack
		mountDirs  []string
		states     map[string]platform.MountState
		wantCount  int
		wantOrphan string
	}{
		{
			name: "orphan stale mount from deleted service",
			services: []platform.ServiceStack{
				{ID: "svc-1", Name: "app"},
			},
			mountDirs: []string{"app", "deleted"},
			states: map[string]platform.MountState{
				"/var/www/deleted": platform.MountStateStale,
			},
			wantCount:  2,
			wantOrphan: "deleted",
		},
		{
			name: "orphan not-mounted directory from deleted service",
			services: []platform.ServiceStack{
				{ID: "svc-1", Name: "app"},
			},
			mountDirs:  []string{"app", "oldservice"},
			states:     map[string]platform.MountState{},
			wantCount:  2,
			wantOrphan: "oldservice",
		},
		{
			name: "no orphans when all dirs match services",
			services: []platform.ServiceStack{
				{ID: "svc-1", Name: "app"},
				{ID: "svc-2", Name: "worker"},
			},
			mountDirs:  []string{"app", "worker"},
			states:     map[string]platform.MountState{},
			wantCount:  2,
			wantOrphan: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock := platform.NewMock().WithServices(tt.services)
			mounter := newMockMounter()
			mounter.mountDirs = tt.mountDirs
			maps.Copy(mounter.states, tt.states)

			result, err := MountStatus(context.Background(), mock, "proj-1", mounter, "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(result.Mounts) != tt.wantCount {
				t.Fatalf("mounts count = %d, want %d", len(result.Mounts), tt.wantCount)
			}

			if tt.wantOrphan != "" {
				var found bool
				for _, m := range result.Mounts {
					if m.Hostname == tt.wantOrphan {
						found = true
						if !m.Orphan {
							t.Errorf("expected orphan=true for %s", tt.wantOrphan)
						}
					}
				}
				if !found {
					t.Errorf("orphan %s not found in results", tt.wantOrphan)
				}
			}
		})
	}
}
