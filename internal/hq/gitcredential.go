package hq

import (
	"bufio"
	"io"
	"net/url"
	"strings"
)

// GitUser is the user git presents to HQ: `/git` reads Basic auth as
// `mate:<credential>`.
const GitUser = "mate"

// GitCredential answers git's credential request (the attributes git writes
// to a helper's stdin) with the Mate credential the enrollment at path holds —
// only when the request names the HQ this Mate enrolled with, by its scheme,
// host and port; any other remote gets nothing (`zcp hq git-credential`).
// The enrollment is read at each request, so a credential a re-enrollment
// rotated is the one git gets.
func GitCredential(request io.Reader, path string) (string, bool) {
	kept, found, err := LoadEnrollment(path)
	if err != nil || !found || kept.Credential == "" {
		return "", false
	}
	hq, err := url.Parse(kept.HQ)
	if err != nil || hq.Host == "" {
		return "", false
	}
	protocol, host := "", ""
	scanner := bufio.NewScanner(request)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			break
		}
		key, value, _ := strings.Cut(line, "=")
		switch key {
		case "protocol":
			protocol = value
		case "host":
			host = value
		}
	}
	if host == "" || !strings.EqualFold(protocol, hq.Scheme) || hostPort(host, protocol) != hostPort(hq.Host, hq.Scheme) {
		return "", false
	}
	return kept.Credential, true
}

// hostPort is host lower-cased with the scheme's own port dropped, so
// "hq.example:443" and "hq.example" are one https host — git names the port
// when the remote URL does.
func hostPort(host, scheme string) string {
	host = strings.ToLower(host)
	if strings.EqualFold(scheme, "http") {
		return strings.TrimSuffix(host, ":80")
	}
	return strings.TrimSuffix(host, ":443")
}
