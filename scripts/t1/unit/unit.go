// Package unit maps sentence IDs to the untagged test cases run by T1.
package unit

import (
	"path"
	"regexp"
	"slices"
	"strings"
)

var (
	idLine   = regexp.MustCompile(`^(?:- )?([A-Z][A-Z0-9]*(?:-[A-Z0-9]+)*) — `)
	testFunc = regexp.MustCompile(`^func (Test[A-Za-z0-9_]+)\(\w+ \*testing\.T\)`)
)

// ID identifies a sentence and its list.
type ID struct{ ID, List string }

// Case identifies a top-level test and its package directory.
type Case struct{ Name, Dir string }

// IDKey is the name segment a sentence ID maps to.
func IDKey(id string) string { return strings.ToLower(strings.ReplaceAll(id, "-", "")) }

// CaseKey is the second underscore-separated segment of a test name.
func CaseKey(name string) string {
	segments := strings.Split(name, "_")
	if len(segments) < 2 {
		return ""
	}
	return strings.ToLower(segments[1])
}

// Tagged reports the same go:build line that excludes a file from T1.
func Tagged(data []byte) bool {
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.HasPrefix(line, "//go:build ") {
			return true
		}
	}
	return false
}

func sortedFiles(files map[string][]byte) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// ReadIDs reads every unit ID from the lists, in list order.
func ReadIDs(files map[string][]byte) []ID {
	var ids []ID
	for _, file := range sortedFiles(files) {
		for line := range strings.SplitSeq(string(files[file]), "\n") {
			if match := idLine.FindStringSubmatch(line); match != nil {
				ids = append(ids, ID{ID: match[1], List: file})
			}
		}
	}
	return ids
}

// ReadCases indexes the untagged test files by their cases' second name segment.
func ReadCases(files map[string][]byte) map[string][]Case {
	byKey := map[string][]Case{}
	for _, file := range sortedFiles(files) {
		data := files[file]
		if Tagged(data) {
			continue
		}
		for line := range strings.SplitSeq(string(data), "\n") {
			if match := testFunc.FindStringSubmatch(line); match != nil {
				if key := CaseKey(match[1]); key != "" {
					byKey[key] = append(byKey[key], Case{Name: match[1], Dir: path.Dir(file)})
				}
			}
		}
	}
	return byKey
}
