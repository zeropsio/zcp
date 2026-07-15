package capture

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/content"
)

const inspectionAtomPathPrefix = "internal/content/atoms/"

// ContentSourceMatch is an exact byte-span match against the inspector
// binary's current embedded atom corpus. It is a candidate source owner, not
// proof that an older capture used the same corpus revision.
type ContentSourceMatch struct {
	AtomID       string
	File         string
	MatchedBytes int
}

type inspectionSourceDocument struct {
	AtomID string
	File   string
	Body   string
}

func loadInspectionSourceDocuments() ([]inspectionSourceDocument, error) {
	atoms, err := content.ReadAllAtoms()
	if err != nil {
		return nil, err
	}
	documents := make([]inspectionSourceDocument, 0, len(atoms))
	for _, atom := range atoms {
		atomID, body, err := splitInspectionAtom(atom.Content)
		if err != nil {
			return nil, fmt.Errorf("parse source atom %s: %w", atom.Name, err)
		}
		if body == "" {
			continue
		}
		documents = append(documents, inspectionSourceDocument{
			AtomID: atomID,
			File:   inspectionAtomPathPrefix + atom.Name,
			Body:   body,
		})
	}
	return documents, nil
}

func splitInspectionAtom(raw string) (string, string, error) {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "---" {
		return "", "", fmt.Errorf("missing opening frontmatter delimiter")
	}
	closing := -1
	atomID := ""
	for index := 1; index < len(lines); index++ {
		line := strings.TrimSpace(lines[index])
		if line == "---" {
			closing = index
			break
		}
		if value, ok := strings.CutPrefix(line, "id:"); ok {
			atomID = strings.Trim(strings.TrimSpace(value), `"'`)
		}
	}
	if closing == -1 {
		return "", "", fmt.Errorf("missing closing frontmatter delimiter")
	}
	if atomID == "" {
		return "", "", fmt.Errorf("missing atom id")
	}
	return atomID, strings.TrimSpace(strings.Join(lines[closing+1:], "\n")), nil
}

func matchInspectionSources(resultText string, documents []inspectionSourceDocument) ([]ContentSourceMatch, error) {
	segments, err := inspectionResultTextSegments(resultText)
	if err != nil {
		return nil, err
	}
	var matches []ContentSourceMatch
	for _, document := range documents {
		body := strings.TrimSpace(document.Body)
		if body == "" {
			continue
		}
		matched := false
		for _, segment := range segments {
			if strings.Contains(segment, body) {
				matched = true
				break
			}
		}
		if matched {
			matches = append(matches, ContentSourceMatch{
				AtomID:       document.AtomID,
				File:         document.File,
				MatchedBytes: len([]byte(body)),
			})
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].File < matches[j].File })
	return matches, nil
}

func inspectionResultTextSegments(resultText string) ([]string, error) {
	trimmed := strings.TrimSpace(resultText)
	if trimmed == "" {
		return nil, nil
	}
	if trimmed[0] != '{' && trimmed[0] != '[' && trimmed[0] != '"' {
		return []string{resultText}, nil
	}
	segments := make([]string, 0, 1)
	if err := collectInspectionJSONStrings(json.RawMessage(trimmed), &segments); err != nil {
		return nil, err
	}
	segments = append(segments, resultText)
	return segments, nil
}

func collectInspectionJSONStrings(raw json.RawMessage, segments *[]string) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	switch raw[0] {
	case '"':
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		*segments = append(*segments, value)
	case '{':
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			return err
		}
		keys := make([]string, 0, len(object))
		for key := range object {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if err := collectInspectionJSONStrings(object[key], segments); err != nil {
				return err
			}
		}
	case '[':
		var array []json.RawMessage
		if err := json.Unmarshal(raw, &array); err != nil {
			return err
		}
		for _, item := range array {
			if err := collectInspectionJSONStrings(item, segments); err != nil {
				return err
			}
		}
	}
	return nil
}
