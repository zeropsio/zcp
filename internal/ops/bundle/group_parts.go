package bundle

import (
	"fmt"
	"reflect"
	"slices"
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
// after when main names it, last otherwise. A main that already lists host
// is returned as it is; one whose services are no list of `- hostname:`
// items, or whose edit would read back as anything but main with that one
// service added, is refused, never written.
func AddTierService(mainBody, composedBody, host, after string) (string, error) {
	return addVerified(mainBody, host, func(body string) (string, error) {
		return addTierService(body, composedBody, host, after)
	})
}

// addTierService is AddTierService on a body with LF line endings.
func addTierService(mainBody, composedBody, host, after string) (string, error) {
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
// without a priority ranks as the platform's default, 0. It is returned and
// refused as AddTierService's is.
func AddTierServiceInRank(mainBody, composedBody, host string) (string, error) {
	return addVerified(mainBody, host, func(body string) (string, error) {
		return addTierServiceInRank(body, composedBody, host)
	})
}

// addTierServiceInRank is AddTierServiceInRank on a body with LF line
// endings.
func addTierServiceInRank(mainBody, composedBody, host string) (string, error) {
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
		return addTierService(mainBody, composedBody, host, "")
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
	return inLineEnding(mainBody, func(body string) (string, error) { return removeTierService(body, host) })
}

func removeTierService(mainBody, host string) (string, error) {
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

// TierListsService reports whether a tier file, read as YAML, lists a
// service named host — however its entry is written.
func TierListsService(body, host string) bool {
	services, ok := tierServices(body)
	return ok && slices.ContainsFunc(services, func(svc map[string]any) bool { return svc["hostname"] == host })
}

// tierServices is a tier file's services as YAML reads them; ok is false
// when it does not parse.
func tierServices(body string) ([]map[string]any, bool) {
	var doc struct {
		Services []map[string]any `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
		return nil, false
	}
	return doc.Services, true
}

// addVerified runs a text edit that adds host to mainBody in the file's own
// line ending, and keeps it only when it reads back as main with that one
// service added: every other service, in its order, and every other key as
// main has them. A main that already lists host is returned as it is.
func addVerified(mainBody, host string, edit func(string) (string, error)) (string, error) {
	if TierListsService(mainBody, host) {
		return mainBody, nil
	}
	edited, err := inLineEnding(mainBody, edit)
	if err != nil || edited == mainBody {
		return edited, err
	}
	if !addsOnly(mainBody, edited, host) {
		return mainBody, fmt.Errorf("the edit would change more of it than %s — add %s by hand", host, host)
	}
	return edited, nil
}

// addsOnly reports whether edited reads as mainBody with one service named
// host added to its services and nothing else changed.
func addsOnly(mainBody, edited, host string) bool {
	var before, after map[string]any
	if yaml.Unmarshal([]byte(mainBody), &before) != nil || yaml.Unmarshal([]byte(edited), &after) != nil {
		return false
	}
	was, _ := before["services"].([]any)
	now, _ := after["services"].([]any)
	if len(now) != len(was)+1 {
		return false
	}
	kept := make([]any, 0, len(was))
	added := 0
	for _, svc := range now {
		if entry, ok := svc.(map[string]any); ok && entry["hostname"] == host {
			added++
			continue
		}
		kept = append(kept, svc)
	}
	delete(before, "services")
	delete(after, "services")
	return added == 1 && reflect.DeepEqual(kept, was) && reflect.DeepEqual(before, after)
}

// inLineEnding runs edit on body with LF line endings and gives its result
// back in body's own: a CRLF file stays CRLF.
func inLineEnding(body string, edit func(string) (string, error)) (string, error) {
	if !strings.Contains(body, "\r\n") {
		return edit(body)
	}
	edited, err := edit(strings.ReplaceAll(body, "\r\n", "\n"))
	if err != nil {
		return body, err
	}
	return strings.ReplaceAll(edited, "\n", "\r\n"), nil
}
