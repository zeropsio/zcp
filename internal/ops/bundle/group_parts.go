package bundle

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// HeldRuntimes reads what a tier file holds of the app's parts: each runtime
// built from a repository with a named setup, under the hostname the tier
// gives it, its repository as a recipe names it (publicRepoURL). A file that
// does not parse holds nothing it can be read back by.
func HeldRuntimes(body string) []HeldRuntime {
	var doc struct {
		Services []struct {
			Hostname     string `yaml:"hostname"`
			BuildFromGit string `yaml:"buildFromGit"`
			ZeropsSetup  string `yaml:"zeropsSetup"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
		return nil
	}
	var held []HeldRuntime
	for _, svc := range doc.Services {
		host, repo, setup := strings.TrimSpace(svc.Hostname), strings.TrimSpace(svc.BuildFromGit), strings.TrimSpace(svc.ZeropsSetup)
		if host == "" || repo == "" || setup == "" {
			continue
		}
		repo, private := publicRepoURL(repo)
		if private {
			continue
		}
		held = append(held, HeldRuntime{Hostname: host, RepoURL: repo, SetupName: setup})
	}
	return held
}

// A change to an app's set of parts reaches a tier on main as one targeted
// edit (spec-mate §2.8a.5): a part's service added to it or removed from it,
// every other line as main has it, so a person's edits stay and the diff a
// person reviews is that service alone. Like SpliceHostScaling these read
// the tier as text, by the composer's layout: `- hostname:` items under
// services, separated by a blank line.

// AddTierService returns mainBody with composedBody's entry for host — its
// comment lines included — added to its services: after the entry named
// after when main names it, last otherwise. A main that already names host
// is returned as it is; one whose services are no list of `- hostname:`
// items is refused, never written.
func AddTierService(mainBody, composedBody, host, after string) (string, error) {
	lines := strings.Split(mainBody, "\n")
	if start, _ := hostEntryRange(lines, host); start >= 0 {
		return mainBody, nil
	}
	composed := strings.Split(composedBody, "\n")
	from, to := hostEntryRange(composed, host)
	if from < 0 {
		return mainBody, fmt.Errorf("the composed tier names no %s", host)
	}
	entryIndent := indentOf(composed[from])
	entry := composed[leadingComments(composed, from):to]

	dash, at := -1, -1
	if after != "" {
		if start, end := hostEntryRange(lines, after); start >= 0 {
			dash, at = indentOf(lines[start]), end
		}
	}
	if at < 0 {
		for _, line := range lines {
			m := hostnameLine.FindStringSubmatch(line)
			if m == nil || dash >= 0 && len(m[1]) != dash {
				continue
			}
			dash = len(m[1])
			_, at = hostEntryRange(lines, strings.Trim(m[2], `"'`))
		}
	}
	if at < 0 {
		return mainBody, fmt.Errorf("its services are no list of `- hostname:` items the edit reads — add %s by hand", host)
	}
	out := make([]string, 0, len(lines)+len(entry)+1)
	out = append(out, lines[:at]...)
	out = append(out, "")
	out = append(out, reindent(entry, dash-entryIndent)...)
	out = append(out, lines[at:]...)
	return strings.Join(out, "\n"), nil
}

// AddTierServiceInRank returns mainBody with composedBody's entry for host
// added where the platform creates it: before the first of main's services
// whose priority ranks below the entry's — a managed service among the
// managed ones, ahead of every runtime — last when none does. A service
// without a priority ranks as the platform's default, 0. A main that already
// names host is returned as it is; one whose services are no list of
// `- hostname:` items is refused, never written.
func AddTierServiceInRank(mainBody, composedBody, host string) (string, error) {
	lines := strings.Split(mainBody, "\n")
	if start, _ := hostEntryRange(lines, host); start >= 0 {
		return mainBody, nil
	}
	composed := strings.Split(composedBody, "\n")
	from, to := hostEntryRange(composed, host)
	if from < 0 {
		return mainBody, fmt.Errorf("the composed tier names no %s", host)
	}
	rank := entryPriority(composed[from:to])
	dash, before := -1, -1
	for i, line := range lines {
		m := hostnameLine.FindStringSubmatch(line)
		if m == nil || dash >= 0 && len(m[1]) != dash {
			continue
		}
		dash = len(m[1])
		start, end := hostEntryRange(lines, strings.Trim(m[2], `"'`))
		if start == i && entryPriority(lines[start:end]) < rank {
			before = leadingComments(lines, start)
			break
		}
	}
	if dash < 0 {
		return mainBody, fmt.Errorf("its services are no list of `- hostname:` items the edit reads — add %s by hand", host)
	}
	if before < 0 {
		return AddTierService(mainBody, composedBody, host, "")
	}
	entry := reindent(composed[leadingComments(composed, from):to], dash-indentOf(composed[from]))
	out := make([]string, 0, len(lines)+len(entry)+1)
	out = append(out, lines[:before]...)
	out = append(out, entry...)
	out = append(out, "")
	out = append(out, lines[before:]...)
	return strings.Join(out, "\n"), nil
}

// entryPriority is the priority a `- hostname:` item's lines write, 0 when
// they write none or do not parse.
func entryPriority(entry []string) int {
	if len(entry) == 0 {
		return 0
	}
	var items []struct {
		Priority int `yaml:"priority"`
	}
	if err := yaml.Unmarshal([]byte(strings.Join(reindent(entry, -indentOf(entry[0])), "\n")), &items); err != nil || len(items) == 0 {
		return 0
	}
	return items[0].Priority
}

// RemoveTierService returns mainBody without host's entry, its comment lines
// and the blank line that set it apart. A host main does not name as a
// `- hostname:` item is refused, never reported removed.
func RemoveTierService(mainBody, host string) (string, error) {
	lines := strings.Split(mainBody, "\n")
	start, end := hostEntryRange(lines, host)
	if start < 0 {
		return mainBody, fmt.Errorf("the tier names no %s as a `- hostname:` item the edit reads — remove it by hand", host)
	}
	start = leadingComments(lines, start)
	switch {
	case start > 0 && strings.TrimSpace(lines[start-1]) == "":
		start--
	case end < len(lines) && strings.TrimSpace(lines[end]) == "":
		end++
	}
	return strings.Join(append(lines[:start:start], lines[end:]...), "\n"), nil
}

// leadingComments is the first of the comment lines right above an item at
// its own column — the composer's comment on an entry — or start itself.
func leadingComments(lines []string, start int) int {
	for start > 0 && isComment(lines[start-1]) && indentOf(lines[start-1]) == indentOf(lines[start]) {
		start--
	}
	return start
}
