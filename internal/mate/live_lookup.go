package mate

import (
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
