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
