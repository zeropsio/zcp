package ops

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

// envIsolationKey is the project's own variable-sharing rule list, an entry
// the platform creates with every project (default `service`).
const envIsolationKey = "envIsolation"

// EnvIsolationAdmission is what AdmitContainerToProjectEnv found and did.
type EnvIsolationAdmission struct {
	// Before is the project's envIsolation as read; After is the value now
	// asked of the platform (equal to Before when nothing was written).
	Before, After string
	// Changed is whether a write was asked.
	Changed bool
}

// AdmitContainerToProjectEnv makes the project's envIsolation admit host —
// a Mate's container — to every service's variables that inherits the
// project value, so the platform writes them into the container's live env
// store (docs/spec-mate.md C-14). It reads the project's entry once and,
// when host is not yet admitted, updates that entry in place adding
// `service@<host>` and keeping every other rule; an entry that already
// admits host is left unwritten. A service's own value is never read or
// touched, and nothing is retried: the write's process is the platform's.
//
// A rule that blocks host (`-service@<host>`) was set on purpose and is kept:
// that is a refusal, as is a project without the entry or with one the
// platform marks not editable.
func AdmitContainerToProjectEnv(ctx context.Context, client platform.Client, projectID, host string) (EnvIsolationAdmission, error) {
	envs, err := client.GetProjectEnv(ctx, projectID)
	if err != nil {
		return EnvIsolationAdmission{}, fmt.Errorf("read the project's variables: %w", err)
	}
	var entry *platform.ProjectEnvVar
	for i := range envs {
		if envs[i].Key == envIsolationKey {
			entry = &envs[i]
			break
		}
	}
	if entry == nil {
		return EnvIsolationAdmission{}, fmt.Errorf("the project has no %s entry", envIsolationKey)
	}
	after, admitted, blocked := admitEnvIsolation(entry.Content, host)
	result := EnvIsolationAdmission{Before: entry.Content, After: entry.Content}
	switch {
	case blocked:
		return result, fmt.Errorf("the project's %s %q blocks %s (-service@%s); left as set", envIsolationKey, entry.Content, host, host)
	case admitted:
		return result, nil
	case !entry.Editable:
		return result, fmt.Errorf("the project's %s %q does not admit %s and is not editable", envIsolationKey, entry.Content, host)
	}
	if _, err := client.UpdateProjectEnv(ctx, entry.ID, envIsolationKey, after, entry.Sensitive); err != nil {
		return result, fmt.Errorf("set the project's %s to %q: %w", envIsolationKey, after, err)
	}
	result.After, result.Changed = after, true
	return result, nil
}

// admitEnvIsolation reads an envIsolation rule list for reader host by the
// platform's order (a block wins, then `service@<host>`, then `none`) and
// returns the list that admits host: value itself when it already does,
// else value with `service@<host>` appended. blocked reports a
// `-service@<host>` rule, which no added rule can override.
func admitEnvIsolation(value, host string) (after string, admitted, blocked bool) {
	rules := strings.Fields(value)
	grant := "service@" + host
	open := false
	for _, r := range rules {
		switch r {
		case "-" + grant:
			return value, false, true
		case grant:
			admitted = true
		case "none":
			open = true
		}
	}
	if admitted || open {
		return value, true, false
	}
	return strings.Join(append(rules, grant), " "), false, false
}
