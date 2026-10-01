package mate

import (
	"bufio"
	"io"
	"net/url"
	"os"
	"strings"
)

// LiveLookup reads a key from the live env store at storePath (see
// LiveEnvStorePath) as it is now, falling back to this process's environment
// for a key the store does not hold or holds empty — the dev loop, or a
// store that cannot be read. The store is read once, when LiveLookup is
// called.
func LiveLookup(storePath string) func(string) string {
	live := map[string]string{}
	if lines, err := LoadLiveEnv(storePath); err == nil {
		for _, line := range lines {
			if key, value, ok := strings.Cut(line, "="); ok {
				live[key] = value
			}
		}
	}
	return func(key string) string {
		if v := live[key]; v != "" {
			return v
		}
		return os.Getenv(key)
	}
}

// GiteaToken answers git's credential request (the attributes git writes to
// a helper's stdin) with the Mate's Gitea bot token as lookup holds it now —
// only when the request names the Gitea's own host, with its port when
// GITEA_URL names one; any other host gets nothing. The broker rotates
// GITEA_TOKEN on the zcp service, and a service env change reaches a running
// process's environment only with a restart, while the live env store has it
// within seconds: the helper `zcp mate git-token` serves reads it here, so a
// rotation needs no restart (ops.giteaCredentialHelperShell).
func GiteaToken(stdin io.Reader, lookup func(string) string) string {
	gitea, err := url.Parse(strings.TrimSpace(lookup("GITEA_URL")))
	if err != nil || gitea.Host == "" {
		return ""
	}
	want := strings.ToLower(gitea.Host)
	if gitea.Port() == "443" {
		want = strings.ToLower(gitea.Hostname())
	}
	host := ""
	scanner := bufio.NewScanner(stdin)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			break
		}
		if v, ok := strings.CutPrefix(line, "host="); ok {
			host = strings.ToLower(v)
		}
	}
	if host == "" || host != want {
		return ""
	}
	return lookup("GITEA_TOKEN")
}
