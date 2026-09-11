package farm

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// newEnvResolveMock seeds a platform.Mock with the two services a fresh
// session's ZCP_FARM_* configuration resolves from (docs/spec-eval-farm.md
// §3.1 FM-17): "farm" carries the ZCP_FARM_* keys, some as literal
// "${os_<key>}" references (read via the API, never substituted by the
// platform since these tests never run inside a container), and "os"
// carries the referenced object-storage values.
func newEnvResolveMock(t *testing.T) *platform.Mock {
	t.Helper()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-farm", Name: "farm", ProjectID: "proj-1"},
		{ID: "svc-os", Name: "os", ProjectID: "proj-1"},
	})
	mock.WithServiceEnv("svc-farm", []platform.ServiceEnvVar{
		{Key: "ZCP_FARM_S3_URL", Content: "${os_apiUrl}"},
		{Key: "ZCP_FARM_S3_BUCKET", Content: "${os_bucketName}"},
		{Key: "ZCP_FARM_S3_KEY", Content: "${os_accessKeyId}"},
		{Key: "ZCP_FARM_S3_SECRET", Content: "${os_secretAccessKey}"},
		{Key: "ZCP_FARM_CLIENT_ID", Content: "client-abc"},
	})
	mock.WithServiceEnv("svc-os", []platform.ServiceEnvVar{
		{Key: "apiUrl", Content: "https://s3.example/api"},
		{Key: "bucketName", Content: "zcp-farm-bucket"},
		{Key: "accessKeyId", Content: "AKIA-fake"},
		{Key: "secretAccessKey", Content: "secret-fake-value"},
	})
	return mock
}

// TestResolveFarmEnv_FillsMissingKeysFromFarmService pins docs/spec-eval-farm.md
// §3.1 FM-17: every ZCP_FARM_S3_* key missing from the environment resolves
// from the farm service's env, following its "${os_<key>}" reference to the
// os service; ZCP_FARM_CLIENT_ID (no reference) is taken as is.
func TestResolveFarmEnv_FillsMissingKeysFromFarmService(t *testing.T) {
	mock := newEnvResolveMock(t)
	base := func(string) string { return "" }
	r := NewEnvResolver(context.Background(), mock, "proj-1", base)

	want := map[string]string{
		"ZCP_FARM_S3_URL":    "https://s3.example/api",
		"ZCP_FARM_S3_BUCKET": "zcp-farm-bucket",
		"ZCP_FARM_S3_KEY":    "AKIA-fake",
		"ZCP_FARM_S3_SECRET": "secret-fake-value",
		"ZCP_FARM_CLIENT_ID": "client-abc",
	}
	for key, wantVal := range want {
		if got := r.Lookup(key); got != wantVal {
			t.Errorf("Lookup(%s) = %q, want %q", key, got, wantVal)
		}
	}
	if err := r.Err(); err != nil {
		t.Fatalf("Err() = %v, want nil", err)
	}
}

// TestResolveFarmEnv_EnvironmentWins pins that a key already present in the
// base lookup is never overridden by — or even looked up against — the farm
// service.
func TestResolveFarmEnv_EnvironmentWins(t *testing.T) {
	mock := newEnvResolveMock(t)
	base := func(key string) string {
		if key == "ZCP_FARM_S3_URL" {
			return "https://from-environment"
		}
		return ""
	}
	r := NewEnvResolver(context.Background(), mock, "proj-1", base)

	if got := r.Lookup("ZCP_FARM_S3_URL"); got != "https://from-environment" {
		t.Errorf("Lookup(ZCP_FARM_S3_URL) = %q, want the environment value", got)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("Err() = %v, want nil", err)
	}
	if n := mock.CallCounts["ListServices"]; n != 0 {
		t.Errorf("ListServices called %d times, want 0 — the environment already answered the key", n)
	}
}

// TestResolveFarmEnv_UnknownReferenceIsAnErrorNamingTheKey pins that a
// ZCP_FARM_* value carrying a reference other than "${os_*}" is refused,
// naming the ZCP_FARM_* key.
func TestResolveFarmEnv_UnknownReferenceIsAnErrorNamingTheKey(t *testing.T) {
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-farm", Name: "farm", ProjectID: "proj-1"},
	})
	mock.WithServiceEnv("svc-farm", []platform.ServiceEnvVar{
		{Key: "ZCP_FARM_S3_URL", Content: "${vault_url}"},
	})
	base := func(string) string { return "" }
	r := NewEnvResolver(context.Background(), mock, "proj-1", base)

	if got := r.Lookup("ZCP_FARM_S3_URL"); got != "" {
		t.Errorf("Lookup(ZCP_FARM_S3_URL) = %q, want empty on an unsupported reference", got)
	}
	err := r.Err()
	if err == nil {
		t.Fatal("Err() = nil, want an error naming ZCP_FARM_S3_URL")
	}
	if !strings.Contains(err.Error(), "ZCP_FARM_S3_URL") {
		t.Errorf("error %q does not name the key ZCP_FARM_S3_URL", err.Error())
	}
}

// TestResolveFarmEnv_NoProjectIDKeepsTodaysError pins that with no
// ZCP_FARM_PROJECT_ID, resolution never runs — a missing key stays missing
// and farm.ConfigFromLookup produces today's plain "missing env var(s)"
// message, unchanged.
func TestResolveFarmEnv_NoProjectIDKeepsTodaysError(t *testing.T) {
	base := func(string) string { return "" }
	r := NewEnvResolver(context.Background(), nil, "", base)

	if got := r.Lookup("ZCP_FARM_S3_URL"); got != "" {
		t.Errorf("Lookup(ZCP_FARM_S3_URL) = %q, want empty — no project id to resolve from", got)
	}
	if err := r.Err(); err != nil {
		t.Errorf("Err() = %v, want nil", err)
	}

	_, err := ConfigFromLookup(r.Lookup)
	if err == nil || !strings.Contains(err.Error(), "ZCP_FARM_S3_URL") {
		t.Errorf("ConfigFromLookup error = %v, want the missing-env-var(s) message naming ZCP_FARM_S3_URL", err)
	}
}

// TestResolveFarmEnv_ErrorsNeverCarryValues pins that a resolution failure's
// error message never embeds the raw (possibly secret-shaped) value it
// failed to parse — only key names (CLAUDE.md: never log/print a secret
// value).
func TestResolveFarmEnv_ErrorsNeverCarryValues(t *testing.T) {
	const secretLookingValue = "AKIA-super-secret-token-should-never-appear"
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-farm", Name: "farm", ProjectID: "proj-1"},
	})
	// Malformed: starts like a reference but does not close with "}" —
	// refused as unsupported, never echoed.
	mock.WithServiceEnv("svc-farm", []platform.ServiceEnvVar{
		{Key: "ZCP_FARM_S3_SECRET", Content: "${os_key}" + secretLookingValue},
	})
	base := func(string) string { return "" }
	r := NewEnvResolver(context.Background(), mock, "proj-1", base)

	r.Lookup("ZCP_FARM_S3_SECRET")
	err := r.Err()
	if err == nil {
		t.Fatal("Err() = nil, want an error (malformed reference)")
	}
	if strings.Contains(err.Error(), secretLookingValue) {
		t.Errorf("error %q carries the raw value", err.Error())
	}
}
