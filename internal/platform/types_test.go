package platform

import "testing"

func TestServiceStack_IsSystem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		category string
		want     bool
	}{
		{name: "CORE is system", category: "CORE", want: true},
		{name: "BUILD is system", category: "BUILD", want: true},
		{name: "INTERNAL is system", category: "INTERNAL", want: true},
		{name: "PREPARE_RUNTIME is system", category: "PREPARE_RUNTIME", want: true},
		{name: "HTTP_L7_BALANCER is system", category: "HTTP_L7_BALANCER", want: true},
		{name: "USER is not system", category: "USER", want: false},
		{name: "STANDARD is not system", category: "STANDARD", want: false},
		{name: "SHARED_STORAGE is not system", category: "SHARED_STORAGE", want: false},
		{name: "OBJECT_STORAGE is not system", category: "OBJECT_STORAGE", want: false},
		{name: "empty category is not system", category: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := ServiceStack{
				ServiceStackTypeInfo: ServiceTypeInfo{
					ServiceStackTypeCategoryName: tt.category,
				},
			}
			if got := svc.IsSystem(); got != tt.want {
				t.Errorf("IsSystem() = %v, want %v for category %q", got, tt.want, tt.category)
			}
		})
	}
}

// TestServiceStack_HasDeployedCode pins the one question every reader of
// "is this runtime empty?" asks the platform: a `startWithoutCode: true`
// import leaves the service ACTIVE with an ACTIVE app version of source NONE
// and no build (live-verified 2026-09-29 on a Mate added from its group's
// recipe), so neither the status nor the presence of an app version says
// code was ever deployed. Only the version's source does — and only the
// full-DTO reads carry it; the Elasticsearch list's light digest names the
// id alone, which reads as deployed, as it always has.
func TestServiceStack_HasDeployedCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		active *ActiveAppVersionDigest
		want   bool
	}{
		{name: "no active version (a plain import, READY_TO_DEPLOY)", active: nil, want: false},
		{name: "an empty digest", active: &ActiveAppVersionDigest{}, want: false},
		{name: "startWithoutCode placeholder", active: &ActiveAppVersionDigest{ID: "av-1", Source: AppVersionSourceNone}, want: false},
		{name: "a zcli push", active: &ActiveAppVersionDigest{ID: "av-2", Source: "CLI", Built: true}, want: true},
		{name: "a buildFromGit import", active: &ActiveAppVersionDigest{ID: "av-3", Source: "GIT", Built: true}, want: true},
		{name: "source NONE carrying a build is a real deploy", active: &ActiveAppVersionDigest{ID: "av-4", Source: AppVersionSourceNone, Built: true}, want: true},
		{name: "the ES list's id-only digest reads as deployed", active: &ActiveAppVersionDigest{ID: "av-5"}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := ServiceStack{Name: "appdev", Status: ServiceStatusActive, ActiveAppVersion: tt.active}
			if got := svc.HasDeployedCode(); got != tt.want {
				t.Errorf("HasDeployedCode() = %v, want %v", got, tt.want)
			}
		})
	}
}
