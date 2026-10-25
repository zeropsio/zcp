package hq

import (
	"net/url"
	"strings"
)

// RepoOfURL is the application and repository remote names on the HQ at
// address — `<address>/git/<appId>/<repo>`, `.git` or not, https, its host
// whatever its case and its port as address's — and false for a remote of
// any other host, or of no repository there: a lease's record keeps the
// repository it cloned as such an address, and only one on this Mate's HQ
// has a change here (ops.IsHQRemote names the host alone).
func RepoOfURL(address, remote string) (appID, repo string, ok bool) {
	hqURL, err := url.Parse(strings.TrimRight(address, "/"))
	if err != nil || !strings.EqualFold(hqURL.Scheme, "https") || hqURL.Hostname() == "" {
		return "", "", false
	}
	u, err := url.Parse(strings.TrimSpace(remote))
	if err != nil || !strings.EqualFold(u.Scheme, "https") ||
		!strings.EqualFold(u.Hostname(), hqURL.Hostname()) || httpsPortOf(u) != httpsPortOf(hqURL) {
		return "", "", false
	}
	rest, found := strings.CutPrefix(strings.TrimSuffix(u.EscapedPath(), "/"), strings.TrimSuffix(hqURL.EscapedPath(), "/")+"/git/")
	if !found {
		return "", "", false
	}
	parts := strings.Split(strings.TrimSuffix(rest, ".git"), "/")
	if len(parts) != 2 {
		return "", "", false
	}
	appID, errApp := url.PathUnescape(parts[0])
	repo, errRepo := url.PathUnescape(parts[1])
	if errApp != nil || errRepo != nil || appID == "" || repo == "" {
		return "", "", false
	}
	return appID, repo, true
}

// httpsPortOf is u's port, 443 when it names none.
func httpsPortOf(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	return "443"
}
