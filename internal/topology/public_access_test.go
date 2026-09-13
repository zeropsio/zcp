package topology

import "testing"

// TestReconcilePublicAccess_Transitions_Table pins the PA-2/PA-3 reconcile
// rules from docs/spec-workflows.md §8 O3: domains always win into `domain`;
// a stamped auto/subdomain intent observed `off` becomes `none` (the user
// switched it off); a `domain` intent with no domains left reverts to
// `auto`; everything else passes through unchanged.
func TestReconcilePublicAccess_Transitions_Table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		rec        PublicAccessRecord
		obs        PublicAccessObserved
		wantIntent PublicAccessIntent
		wantChange bool
	}{
		{
			name:       "auto+stamp+off => none, changed",
			rec:        PublicAccessRecord{Intent: PublicAccessAuto, SubdomainEnabledByZcpAt: "2026-01-01T00:00:00Z"},
			obs:        PublicAccessObserved{Subdomain: SubdomainOff},
			wantIntent: PublicAccessNone,
			wantChange: true,
		},
		{
			name:       "subdomain+stamp+off => none, changed",
			rec:        PublicAccessRecord{Intent: PublicAccessSubdomain, SubdomainEnabledByZcpAt: "2026-01-01T00:00:00Z"},
			obs:        PublicAccessObserved{Subdomain: SubdomainOff},
			wantIntent: PublicAccessNone,
			wantChange: true,
		},
		{
			name:       "auto+nostamp+off => auto, unchanged",
			rec:        PublicAccessRecord{Intent: PublicAccessAuto},
			obs:        PublicAccessObserved{Subdomain: SubdomainOff},
			wantIntent: PublicAccessAuto,
			wantChange: false,
		},
		{
			name:       "auto+domains => domain",
			rec:        PublicAccessRecord{Intent: PublicAccessAuto},
			obs:        PublicAccessObserved{Domains: []string{"example.com"}},
			wantIntent: PublicAccessDomain,
			wantChange: true,
		},
		{
			name:       "domain+no domains => auto",
			rec:        PublicAccessRecord{Intent: PublicAccessDomain},
			obs:        PublicAccessObserved{},
			wantIntent: PublicAccessAuto,
			wantChange: true,
		},
		{
			name:       "none+off => none, unchanged",
			rec:        PublicAccessRecord{Intent: PublicAccessNone},
			obs:        PublicAccessObserved{Subdomain: SubdomainOff},
			wantIntent: PublicAccessNone,
			wantChange: false,
		},
		{
			name:       "none+domains => domain",
			rec:        PublicAccessRecord{Intent: PublicAccessNone},
			obs:        PublicAccessObserved{Domains: []string{"example.com"}},
			wantIntent: PublicAccessDomain,
			wantChange: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out, changed := ReconcilePublicAccess(c.rec, c.obs)
			if out.Intent != c.wantIntent {
				t.Errorf("Intent = %q, want %q", out.Intent, c.wantIntent)
			}
			if changed != c.wantChange {
				t.Errorf("changed = %v, want %v", changed, c.wantChange)
			}
		})
	}
}

// TestShouldAutoEnableSubdomain_Table pins PA-1's meta half: auto-enable
// fires only for an unstamped `auto` intent with no domains, subdomain off,
// and a live listener. The mode allow-list stays the caller's job.
func TestShouldAutoEnableSubdomain_Table(t *testing.T) {
	t.Parallel()
	base := PublicAccessObserved{Subdomain: SubdomainOff, Listener: true}
	cases := []struct {
		name string
		rec  PublicAccessRecord
		obs  PublicAccessObserved
		want bool
	}{
		{
			name: "auto/nostamp/off/listener => true",
			rec:  PublicAccessRecord{Intent: PublicAccessAuto},
			obs:  base,
			want: true,
		},
		{
			name: "stamped => false",
			rec:  PublicAccessRecord{Intent: PublicAccessAuto, SubdomainEnabledByZcpAt: "2026-01-01T00:00:00Z"},
			obs:  base,
			want: false,
		},
		{
			name: "domains => false",
			rec:  PublicAccessRecord{Intent: PublicAccessAuto},
			obs:  PublicAccessObserved{Subdomain: SubdomainOff, Listener: true, Domains: []string{"example.com"}},
			want: false,
		},
		{
			name: "no listener => false",
			rec:  PublicAccessRecord{Intent: PublicAccessAuto},
			obs:  PublicAccessObserved{Subdomain: SubdomainOff, Listener: false},
			want: false,
		},
		{
			name: "intent subdomain => false",
			rec:  PublicAccessRecord{Intent: PublicAccessSubdomain},
			obs:  base,
			want: false,
		},
		{
			name: "intent none => false",
			rec:  PublicAccessRecord{Intent: PublicAccessNone},
			obs:  base,
			want: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := ShouldAutoEnableSubdomain(c.rec, c.obs)
			if got != c.want {
				t.Errorf("ShouldAutoEnableSubdomain() = %v, want %v", got, c.want)
			}
		})
	}
}

// TestDeriveAdoptedIntent_Table pins PA-6's adopt rule: domains win, then
// an observed-on subdomain, else auto.
func TestDeriveAdoptedIntent_Table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		obs  PublicAccessObserved
		want PublicAccessIntent
	}{
		{"domains => domain", PublicAccessObserved{Domains: []string{"example.com"}}, PublicAccessDomain},
		{"on => subdomain", PublicAccessObserved{Subdomain: SubdomainOn}, PublicAccessSubdomain},
		{"off => auto", PublicAccessObserved{Subdomain: SubdomainOff}, PublicAccessAuto},
		{"enabling => auto", PublicAccessObserved{Subdomain: SubdomainEnabling}, PublicAccessAuto},
		{"domains win over on", PublicAccessObserved{Subdomain: SubdomainOn, Domains: []string{"example.com"}}, PublicAccessDomain},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := DeriveAdoptedIntent(c.obs)
			if got != c.want {
				t.Errorf("DeriveAdoptedIntent() = %q, want %q", got, c.want)
			}
		})
	}
}

// TestPublicAccessIntent_IsValid pins that "" is NOT a valid intent — callers
// default "" → auto at the boundary rather than treating empty as a fifth value.
func TestPublicAccessIntent_IsValid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		intent PublicAccessIntent
		want   bool
	}{
		{PublicAccessAuto, true},
		{PublicAccessSubdomain, true},
		{PublicAccessDomain, true},
		{PublicAccessNone, true},
		{"", false},
		{"bogus", false},
	}
	for _, c := range cases {
		if got := c.intent.IsValid(); got != c.want {
			t.Errorf("IsValid(%q) = %v, want %v", c.intent, got, c.want)
		}
	}
}

// TestParsePublicAccessIntent pins the string-to-intent parse helper used at
// I/O boundaries (plan JSON, tool input).
func TestParsePublicAccessIntent(t *testing.T) {
	t.Parallel()
	cases := []struct {
		s      string
		want   PublicAccessIntent
		wantOK bool
	}{
		{"auto", PublicAccessAuto, true},
		{"subdomain", PublicAccessSubdomain, true},
		{"domain", PublicAccessDomain, true},
		{"none", PublicAccessNone, true},
		{"", "", false},
		{"bogus", "", false},
	}
	for _, c := range cases {
		got, ok := ParsePublicAccessIntent(c.s)
		if got != c.want || ok != c.wantOK {
			t.Errorf("ParsePublicAccessIntent(%q) = (%q, %v), want (%q, %v)", c.s, got, ok, c.want, c.wantOK)
		}
	}
}
