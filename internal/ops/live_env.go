package ops

import (
	"strings"

	"github.com/zeropsio/zcp/internal/mate"
)

// LiveEnvRefusalKind is why LiveEnvValues handed out no value.
type LiveEnvRefusalKind int

const (
	// LiveEnvNotAName: what stands where a key's name belongs is not one.
	// It is never echoed: it may be a KEY=value.
	LiveEnvNotAName LiveEnvRefusalKind = iota + 1
	// LiveEnvHidden: Key is a name the Mate's own vault hides
	// (IsOwnVaultHidden) — one Mate and zcp keep for themselves.
	LiveEnvHidden
	// LiveEnvNotHeld: the store does not hold Key — it is not saved yet,
	// was saved moments ago, or was never asked for.
	LiveEnvNotHeld
	// LiveEnvStoreUnread: the store could not be read; Err says why.
	LiveEnvStoreUnread
)

// LiveEnvRefusal is a refusal of LiveEnvValues. Key is set for a hidden and
// a missing key — a key's name, never a value. Err is the read's own error
// for LiveEnvStoreUnread: one from the store's content may quote it, so a
// caller prints only an error on the file itself.
type LiveEnvRefusal struct {
	Kind LiveEnvRefusalKind
	Key  string
	Err  error
}

// LiveEnvValues answers the values the container's live env store at
// storePath (mate.LiveEnvStorePath) holds under keys, read now: the one way
// zcp hands a stored value to something it starts — a command (`zcp with`)
// or a page (`{{env:KEY}}` in a zerops_browser command; docs/spec-mate.md
// §5.8a). A value the person saved is there a few seconds after the write,
// while a process started earlier keeps the env it started with. Every key
// is checked before the store is read, and all of them are held or none is
// answered: refused is nil exactly when values holds each key.
func LiveEnvValues(storePath string, keys []string) (values map[string]string, refused *LiveEnvRefusal) {
	for _, key := range keys {
		if !IsEnvKeyName(key) {
			return nil, &LiveEnvRefusal{Kind: LiveEnvNotAName}
		}
		if IsOwnVaultHidden(key) {
			return nil, &LiveEnvRefusal{Kind: LiveEnvHidden, Key: key}
		}
	}
	lines, err := mate.LoadLiveEnv(storePath)
	if err != nil {
		return nil, &LiveEnvRefusal{Kind: LiveEnvStoreUnread, Err: err}
	}
	held := make(map[string]string, len(lines))
	for _, line := range lines {
		if key, value, ok := strings.Cut(line, "="); ok {
			held[key] = value
		}
	}
	values = make(map[string]string, len(keys))
	for _, key := range keys {
		value, ok := held[key]
		if !ok {
			return nil, &LiveEnvRefusal{Kind: LiveEnvNotHeld, Key: key}
		}
		values[key] = value
	}
	return values, nil
}
