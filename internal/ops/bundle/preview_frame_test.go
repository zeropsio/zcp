package bundle

import "testing"

// TestWithoutMateFraming_ExactContainerOrigin pins that Mate's origin set is
// read with the container's exact origin as well as the zerops.app wildcard
// older setups name: a Mate now widens a page's framing by its hosted client
// and its own subdomain only (agents_preview.md), and production keeps none of
// either. A Zerops subdomain beside Mate's hosted origin is a Mate's — its
// project is never production's — and a list without the hosted origin stays
// the app's own.
func TestWithoutMateFraming_ExactContainerOrigin(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, in, want string
		framed         bool
	}{
		{
			name:   "a directive keeps the app's own sources",
			in:     `"default-src 'self'; frame-ancestors 'self' https://mate.zerops.io https://zcp-1df2-8080.prg1.zerops.app" always`,
			want:   `"default-src 'self'; frame-ancestors 'self'" always`,
			framed: true,
		},
		{
			name:   "a bare source list of a devel container",
			in:     "'self' https://mate.zerops.io https://zcp-1df2-8080.app-tatami.zerops.dev",
			want:   "'self'",
			framed: true,
		},
		{
			name:   "only Mate's origins leave none",
			in:     "https://mate.zerops.io https://zcp-1df2-8080.prg1.zerops.app/",
			want:   "'none'",
			framed: true,
		},
		{
			name: "a setting naming Mate's address beside another site is the app's own",
			in:   "https://mate.zerops.io https://app.zerops.io",
			want: "https://mate.zerops.io https://app.zerops.io",
		},
		{
			name: "a zerops.app source without Mate's hosted origin is the app's own",
			in:   `"frame-ancestors https://partner.example.com https://appstage-1df2.prg1.zerops.app" always`,
			want: `"frame-ancestors https://partner.example.com https://appstage-1df2.prg1.zerops.app" always`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, framed := WithoutMateFraming(tt.in)
			if got != tt.want || framed != tt.framed {
				t.Errorf("WithoutMateFraming(%q) = %q, %v; want %q, %v", tt.in, got, framed, tt.want, tt.framed)
			}
		})
	}
}

// A setup names an address of the project it was written in when a value
// holds a host on a Zerops subdomain — written out, or with the project's own
// subdomain variables among its labels, as a Mate writes its stage's address.
// A value that is only a reference resolves in each environment for itself.
func TestNamesZeropsSubdomainAddress_ReferencesInLabels_Count(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "an address written out", value: "https://apistage-1f2a-3000.prg1.zerops.app", want: true},
		{name: "a cookie domain without a scheme", value: "hqstage-1f2a.prg1.zerops.app", want: true},
		{name: "a devel address", value: "https://apistage-1f2a-3000.dev1.zerops.dev/login", want: true},
		{name: "the zerops.app wildcard", value: "https://*.zerops.app", want: true},
		{name: "the location as a reference", value: "https://hqstage-${zeropsSubdomainHost}.${zeropsLocation}.zerops.app", want: true},
		{name: "the subdomain host and port inside a label", value: "https://webstage-${zeropsSubdomainHost}-3000.${zeropsLocation}.zerops.app", want: true},
		{name: "a path after a referenced host", value: "https://hqstage-${zeropsSubdomainHost}.${zeropsLocation}.zerops.app/github-auth", want: true},
		{name: "the subdomain host in a written-out location", value: "https://appstage-${zeropsSubdomainHost}-3000.prg1.zerops.app", want: true},
		{name: "only the service's own subdomain reference", value: "${zeropsSubdomain}", want: false},
		{name: "only another service's subdomain reference", value: "${appstage_zeropsSubdomain}", want: false},
		{name: "Mate's hosted address", value: "https://mate.zerops.io", want: false},
		{name: "a production cookie domain", value: ".zerops.io", want: false},
		{name: "the shared domain alone", value: "zerops.app", want: false},
		{name: "another domain ending alike", value: "https://example.zerops.apps", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := namesZeropsSubdomainAddress(tt.value); got != tt.want {
				t.Errorf("namesZeropsSubdomainAddress(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}
