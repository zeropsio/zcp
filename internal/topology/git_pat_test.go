package topology

import "testing"

// A self-hosted git remote is not GitHub. The PAT guidance named github.com
// unconditionally, so a Gitea or GitLab user was sent to a settings page that
// does not exist for them — measured while wiring a Mate to a self-hosted
// Gitea, where the whole credential story runs through
// `<host>/user/settings/applications`, not github.com.
func TestClassifyGitHost(t *testing.T) {
	cases := []struct {
		name   string
		remote string
		want   GitHostKind
	}{
		{"github https", "https://github.com/acme/app.git", GitHostGitHub},
		{"github ssh", "git@github.com:acme/app.git", GitHostGitHub},
		{"gitlab saas", "https://gitlab.com/acme/app.git", GitHostGitLab},
		{"gitlab self-hosted", "https://gitlab.acme.io/acme/app.git", GitHostGitLab},
		{"gitea by path", "https://web-2ff4-3000.prg1.zerops.app/mate/shortlink.git", GitHostUnknown},
		{"empty", "", GitHostUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyGitHost(tc.remote); got != tc.want {
				t.Errorf("ClassifyGitHost(%q) = %q, want %q", tc.remote, got, tc.want)
			}
		})
	}
}

func TestGitTokenSettingsURL(t *testing.T) {
	if got := GitTokenSettingsURL("https://github.com/acme/app.git"); got != GHPATSettingsURL {
		t.Errorf("github settings URL = %q, want %q", got, GHPATSettingsURL)
	}
	// A host we cannot identify must not be told to go to github.com; it gets
	// its own host back, with the path most self-hosted forges use.
	got := GitTokenSettingsURL("https://web-2ff4-3000.prg1.zerops.app/mate/shortlink.git")
	if got == GHPATSettingsURL {
		t.Fatalf("an unknown host must not be pointed at github.com, got %q", got)
	}
	if want := "https://web-2ff4-3000.prg1.zerops.app"; got[:len(want)] != want {
		t.Errorf("settings URL = %q, want it rooted at %q", got, want)
	}
}

func TestGitTokenPushScope(t *testing.T) {
	if got := GitTokenPushScope("https://github.com/acme/app.git"); got != GHPATPushMinScope {
		t.Errorf("github push scope = %q, want %q", got, GHPATPushMinScope)
	}
	if got := GitTokenPushScope("https://gitlab.com/acme/app.git"); got != GitLabPushMinScope {
		t.Errorf("gitlab push scope = %q, want %q", got, GitLabPushMinScope)
	}
}
