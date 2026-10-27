package hq

import (
	"net/url"
	"strings"
)

// RepoOfURL is the application and repository remote names on the HQ at
// address — `<address>/git/<orgId>/<appId>/<repo>`, `.git` or not, https, its
// host whatever its case and its port as address's: the address RepoURLAt
// builds — and false for a remote of any other host, or of no repository
// there, an address naming no organisation among them (HQ answers it 404): a
// lease's record keeps the repository it cloned as such an address, and only
// one on this Mate's HQ has a change here (ops.IsHQRemote names the host
// alone). The organisation is not compared: the application names the
// repository's place in HQ.
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
	if len(parts) != 3 || parts[0] == "" {
		return "", "", false
	}
	appID, errApp := url.PathUnescape(parts[1])
	repo, errRepo := url.PathUnescape(parts[2])
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
