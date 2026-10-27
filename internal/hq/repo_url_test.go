package hq

import "testing"

// TestRepoOfURL: a remote names a repository of this Mate's HQ when it is on
// HQ's address — https, its host whatever its case, its port — at
// /git/<orgId>/<appId>/<repo>, with or without .git: the address RepoURLAt
// builds. Anything else is no repository of it, an address without the
// organisation among them (HQ answers it 404).
func TestRepoOfURL(t *testing.T) {
	t.Parallel()
	const address = "https://hq.example.com"
	built, err := RepoURLAt(address, "org 1", "app 1", "apps dev")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, address, remote string
		wantApp, wantRepo     string
		wantOK                bool
	}{
		{name: "the clone address", address: address, remote: "https://hq.example.com/git/org-1/app-1/appsdev.git", wantApp: "app-1", wantRepo: "appsdev", wantOK: true},
		{name: "the address RepoURLAt builds", address: address, remote: built, wantApp: "app 1", wantRepo: "apps dev", wantOK: true},
		{name: "canonical, no .git", address: address, remote: "https://hq.example.com/git/org-1/app-1/appsdev", wantApp: "app-1", wantRepo: "appsdev", wantOK: true},
		{name: "a trailing slash and the host's case", address: address + "/", remote: "https://HQ.example.com/git/org-1/app-1/appsdev/", wantApp: "app-1", wantRepo: "appsdev", wantOK: true},
		{name: "the default port named", address: address, remote: "https://hq.example.com:443/git/org-1/app-1/appsdev.git", wantApp: "app-1", wantRepo: "appsdev", wantOK: true},
		{name: "HQ on a port of its own", address: "https://127.0.0.1:8443", remote: "https://127.0.0.1:8443/git/org-1/app-1/appsdev.git", wantApp: "app-1", wantRepo: "appsdev", wantOK: true},
		{name: "another port", address: "https://127.0.0.1:8443", remote: "https://127.0.0.1:9443/git/org-1/app-1/appsdev.git"},
		{name: "another host", address: address, remote: "https://github.com/git/org-1/app-1/appsdev.git"},
		{name: "plain http", address: address, remote: "http://hq.example.com/git/org-1/app-1/appsdev.git"},
		{name: "not under /git", address: address, remote: "https://hq.example.com/org-1/app-1/appsdev.git"},
		{name: "no organisation", address: address, remote: "https://hq.example.com/git/app-1/appsdev.git"},
		{name: "no repository", address: address, remote: "https://hq.example.com/git/org-1/app-1"},
		{name: "a deeper path", address: address, remote: "https://hq.example.com/git/org-1/app-1/appsdev/more.git"},
		{name: "no HQ address", remote: "https://hq.example.com/git/org-1/app-1/appsdev.git"},
		{name: "not a URL", address: address, remote: "::"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			app, repo, ok := RepoOfURL(tt.address, tt.remote)
			if ok != tt.wantOK || app != tt.wantApp || repo != tt.wantRepo {
				t.Errorf("RepoOfURL(%q, %q) = (%q, %q, %v), want (%q, %q, %v)", tt.address, tt.remote, app, repo, ok, tt.wantApp, tt.wantRepo, tt.wantOK)
			}
		})
	}
}
