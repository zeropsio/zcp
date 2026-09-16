package topology

import "testing"

// A self-hosted git remote is not GitHub. The PAT guidance named github.com
// unconditionally, so a Gitea or GitLab user was sent to a settings page that
// does not exist for them — measured while wiring a Mate to a self-hosted
// Gitea, where the whole credential story runs through
// `<host>/user/settings/applications`, not github.com.
//
// Gitea is told apart by ONE signal: the remote's host is the host of the
// account's own GITEA_URL, threaded in by the caller. There is no path or URL
// shape a Gitea can be recognised by — it answers on an arbitrary host and
// serves `/{owner}/{repo}` exactly as every other forge does — so without the
// environment's own answer a Gitea remote is honestly Unknown.
func TestClassifyGitHost(t *testing.T) {
	const giteaURL = "https://web-2ff4-3000.prg1.zerops.app"

	cases := []struct {
		name     string
		remote   string
		giteaURL string
		want     GitHostKind
	}{
		{"github https", "https://github.com/acme/app.git", "", GitHostGitHub},
		{"github ssh", "git@github.com:acme/app.git", "", GitHostGitHub},
		{"gitlab saas", "https://gitlab.com/acme/app.git", "", GitHostGitLab},
		{"gitlab self-hosted", "https://gitlab.acme.io/acme/app.git", "", GitHostGitLab},
		{"gitea by env host", "https://web-2ff4-3000.prg1.zerops.app/acme/api", giteaURL, GitHostGitea},
		{"gitea by env host, port and case ignored", "HTTPS://WEB-2FF4-3000.PRG1.ZEROPS.APP/acme/api.git", giteaURL, GitHostGitea},
		{"gitea by env host, scp form", "git@web-2ff4-3000.prg1.zerops.app:acme/api", giteaURL, GitHostGitea},
		{"the same host without the env is unknown", "https://web-2ff4-3000.prg1.zerops.app/acme/api", "", GitHostUnknown},
		{"another self-hosted host with the env set stays unknown", "https://git.acme.io/acme/app.git", giteaURL, GitHostUnknown},
		{"github wins over a nonsense gitea env", "https://github.com/acme/app.git", "://", GitHostGitHub},
		{"empty", "", giteaURL, GitHostUnknown},
		{"empty remote, empty env", "", "", GitHostUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyGitHost(tc.remote, tc.giteaURL); got != tc.want {
				t.Errorf("ClassifyGitHost(%q, %q) = %q, want %q", tc.remote, tc.giteaURL, got, tc.want)
			}
		})
	}
}

func TestGitTokenSettingsURL(t *testing.T) {
	if got := GitTokenSettingsURL("https://github.com/acme/app.git", ""); got != GHPATSettingsURL {
		t.Errorf("github settings URL = %q, want %q", got, GHPATSettingsURL)
	}
	// A host we cannot identify must not be told to go to github.com; it gets
	// its own host back, with the path most self-hosted forges use.
	got := GitTokenSettingsURL("https://web-2ff4-3000.prg1.zerops.app/mate/shortlink.git", "")
	if got == GHPATSettingsURL {
		t.Fatalf("an unknown host must not be pointed at github.com, got %q", got)
	}
	if want := "https://web-2ff4-3000.prg1.zerops.app"; got[:len(want)] != want {
		t.Errorf("settings URL = %q, want it rooted at %q", got, want)
	}
	// The account's own Gitea has no settings page to send anyone to: the
	// Mate's bot token is already in its environment.
	gitea := GitTokenSettingsURL(
		"https://web-2ff4-3000.prg1.zerops.app/acme/api",
		"https://web-2ff4-3000.prg1.zerops.app",
	)
	if gitea != GiteaBotTokenSource {
		t.Errorf("gitea settings URL = %q, want %q", gitea, GiteaBotTokenSource)
	}
}

func TestGitTokenPushScope(t *testing.T) {
	cases := []struct {
		name     string
		remote   string
		giteaURL string
		want     string
	}{
		{"github", "https://github.com/acme/app.git", "", GHPATPushMinScope},
		{"gitlab", "https://gitlab.com/acme/app.git", "", GitLabPushMinScope},
		{"gitea", "https://web-2ff4-3000.prg1.zerops.app/acme/api", "https://web-2ff4-3000.prg1.zerops.app", GiteaPushMinScope},
		{"unknown", "https://git.acme.io/acme/app.git", "", GiteaPushMinScope},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := GitTokenPushScope(tc.remote, tc.giteaURL); got != tc.want {
				t.Errorf("GitTokenPushScope(%q, %q) = %q, want %q", tc.remote, tc.giteaURL, got, tc.want)
			}
		})
	}
}

// TestGiteaCommitIdentity pins the Mate's commit identity on a Gitea remote
// (guide 2.3): the bot's login, and an address in a domain that provably
// resolves nowhere — the bot has no mailbox and `.invalid` is reserved for
// exactly this (RFC 2606), so a notification can never be sent to a real
// person by accident.
func TestGiteaCommitIdentity(t *testing.T) {
	cases := []struct {
		login string
		name  string
		email string
	}{
		{"mate-p1", "mate-p1", "mate-p1@mate.invalid"},
		{"mate-abc123", "mate-abc123", "mate-abc123@mate.invalid"},
		{"", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.login, func(t *testing.T) {
			name, email := GiteaCommitIdentity(tc.login)
			if name != tc.name || email != tc.email {
				t.Errorf("GiteaCommitIdentity(%q) = (%q, %q), want (%q, %q)", tc.login, name, email, tc.name, tc.email)
			}
		})
	}
}
