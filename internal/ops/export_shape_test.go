package ops

import (
	"context"
	"errors"
	"testing"

	"github.com/zeropsio/zcp/internal/ops/bundle"
	"github.com/zeropsio/zcp/internal/platform"
)

// One read of a live service gives the group recipe its scale, its profile,
// and — for a runtime built from a public repository — that repository.
func TestFetchServiceShape(t *testing.T) {
	t.Parallel()
	explicit := true
	tests := []struct {
		name string
		svc  platform.ServiceStack
		want ServiceShape
	}{
		{
			name: "a managed service: its profile and its scale",
			svc: platform.ServiceStack{ID: "db", Profile: "oltp-hobby", CurrentAutoscaling: &platform.CustomAutoscaling{
				CPUMode: "SHARED", MinCPU: 1, MaxCPU: 3, MinRAM: 0.25, MaxRAM: 4, HorizontalMinCount: 1, HorizontalMaxCount: 1,
			}},
			want: ServiceShape{Profile: "oltp-hobby", Scaling: &bundle.Scaling{CPUMode: "SHARED", MinCPU: 1, MaxCPU: 3, MinRAM: 0.25, MaxRAM: 4, MinContainers: 1, MaxContainers: 1}},
		},
		{
			name: "a runtime built from a public repository",
			svc: platform.ServiceStack{ID: "mailpit", ActiveAppVersion: &platform.ActiveAppVersionDigest{
				ID: "av1", PublicGitSource: &platform.AppVersionGitSource{GitURL: "https://github.com/zerops-recipe-apps/mailpit-app", BranchName: "main"},
				PublicGitSourceExplicitSet: &explicit,
			}},
			want: ServiceShape{PublicGitURL: "https://github.com/zerops-recipe-apps/mailpit-app", ExplicitSetup: true},
		},
		{
			name: "no active version, no scale read",
			svc:  platform.ServiceStack{ID: "empty"},
			want: ServiceShape{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := platform.NewMock().WithServices([]platform.ServiceStack{tt.svc})
			got, err := FetchServiceShape(context.Background(), client, tt.svc.ID)
			if err != nil {
				t.Fatalf("FetchServiceShape: %v", err)
			}
			if got.Profile != tt.want.Profile || got.PublicGitURL != tt.want.PublicGitURL || got.ExplicitSetup != tt.want.ExplicitSetup {
				t.Errorf("shape = %+v, want %+v", got, tt.want)
			}
			if (got.Scaling == nil) != (tt.want.Scaling == nil) || (got.Scaling != nil && *got.Scaling != *tt.want.Scaling) {
				t.Errorf("scaling = %+v, want %+v", got.Scaling, tt.want.Scaling)
			}
		})
	}
}

// An object storage's size is its quota variable; its access policy is only
// in the platform's own export of the service. A policy that cannot be read
// is said, never guessed; a size that cannot be read is an error, since a
// tier that lands with the wrong one stays wrong.
func TestFetchObjectStorageShape(t *testing.T) {
	t.Parallel()
	const exported = `services:
  - hostname: storage
    type: object-storage
    objectStorageSize: 5
    objectStoragePolicy: public-read
`
	tests := []struct {
		name       string
		envs       []platform.ServiceEnvVar
		exportYAML string
		exportErr  error
		envErr     error
		want       ObjectStorageShape
		wantUnread bool
		wantErr    bool
	}{
		{
			name:       "its quota and its exported policy",
			envs:       []platform.ServiceEnvVar{{Key: "quotaGBytes", Content: "5", Type: platform.ServiceEnvSystem}},
			exportYAML: exported,
			want:       ObjectStorageShape{SizeGB: 5, Policy: "public-read"},
		},
		{
			name:       "a custom policy brings its document",
			envs:       []platform.ServiceEnvVar{{Key: "quotaGBytes", Content: "2"}},
			exportYAML: "services:\n  - hostname: storage\n    objectStoragePolicy: custom\n    objectStorageRawPolicy: '{\"Statement\":[]}'\n",
			want:       ObjectStorageShape{SizeGB: 2, Policy: "custom", RawPolicy: `{"Statement":[]}`},
		},
		{
			name: "a custom policy names the bucket each environment gets, not this one",
			envs: []platform.ServiceEnvVar{{Key: "quotaGBytes", Content: "2"}, {Key: "bucketName", Content: "abc123-storage"}},
			exportYAML: "services:\n  - hostname: storage\n    objectStoragePolicy: custom\n" +
				"    objectStorageRawPolicy: '{\"Resource\":[\"arn:aws:s3:::abc123-storage/*\"]}'\n",
			want: ObjectStorageShape{SizeGB: 2, Policy: "custom", RawPolicy: `{"Resource":["arn:aws:s3:::{{ .BucketName }}/*"]}`},
		},
		{
			name:       "no quota variable — the export's size",
			exportYAML: exported,
			want:       ObjectStorageShape{SizeGB: 5, Policy: "public-read"},
		},
		{
			name:       "the export unreadable — the policy is said to be unread",
			envs:       []platform.ServiceEnvVar{{Key: "quotaGBytes", Content: "3"}},
			exportErr:  errors.New("forbidden"),
			want:       ObjectStorageShape{SizeGB: 3},
			wantUnread: true,
		},
		{
			name:       "the export names no policy — said too",
			envs:       []platform.ServiceEnvVar{{Key: "quotaGBytes", Content: "3"}},
			exportYAML: "services:\n  - hostname: storage\n    type: object-storage\n",
			want:       ObjectStorageShape{SizeGB: 3},
			wantUnread: true,
		},
		{
			name:       "a policy the import does not know is not carried",
			envs:       []platform.ServiceEnvVar{{Key: "quotaGBytes", Content: "3"}},
			exportYAML: "services:\n  - hostname: storage\n    objectStoragePolicy: world-writable\n",
			want:       ObjectStorageShape{SizeGB: 3},
			wantUnread: true,
		},
		{
			name:    "the variables unreadable — an error, retried",
			envErr:  errors.New("timeout"),
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := platform.NewMock().WithServiceEnv("svc-storage", tt.envs).WithServiceExportYAML(tt.exportYAML)
			if tt.exportErr != nil {
				client = client.WithError("GetServiceStackExport", tt.exportErr)
			}
			if tt.envErr != nil {
				client = client.WithError("GetServiceEnv", tt.envErr)
			}
			got, err := FetchObjectStorageShape(context.Background(), client, "svc-storage", "storage")
			if tt.wantErr {
				if err == nil {
					t.Fatal("want an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("FetchObjectStorageShape: %v", err)
			}
			unread := got.PolicyUnread != ""
			got.PolicyUnread = ""
			if got != tt.want || unread != tt.wantUnread {
				t.Errorf("shape = %+v (unread %v), want %+v (unread %v)", got, unread, tt.want, tt.wantUnread)
			}
		})
	}
}
