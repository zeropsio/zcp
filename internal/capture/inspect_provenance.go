package capture

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// CompositionSourceMatch is capture-time proof that one exact model-visible
// string was assembled from the listed static/dynamic owners.
type CompositionSourceMatch struct {
	File        string
	Record      int
	Surface     string
	OutputBytes int
	Components  []CompositionComponent
}

type inspectedCompositionRecord struct {
	file   string
	record int
	value  CompositionRecord
}

func inspectCompositionFile(path, relative, captureID string) ([]inspectedCompositionRecord, error) {
	records, err := ReadCompositionRecords(path)
	if err != nil {
		return nil, err
	}
	inspected := make([]inspectedCompositionRecord, 0, len(records))
	for index, record := range records {
		if record.CaptureID != captureID {
			return nil, fmt.Errorf("record %d capture ID %q differs from %q", index+1, record.CaptureID, captureID)
		}
		if record.Surface == "" || record.ProcessID <= 0 || record.Time.IsZero() || record.OutputBytes < 0 {
			return nil, fmt.Errorf("record %d has incomplete composition identity", index+1)
		}
		if len(record.OutputSHA256) != sha256.Size*2 {
			return nil, fmt.Errorf("record %d output hash has invalid length", index+1)
		}
		if _, err := hex.DecodeString(record.OutputSHA256); err != nil {
			return nil, fmt.Errorf("record %d output hash: %w", index+1, err)
		}
		previousEnd := 0
		for componentIndex, component := range record.Components {
			if component.Start < previousEnd || component.End < component.Start || component.End > record.OutputBytes || len(component.SHA256) != sha256.Size*2 {
				return nil, fmt.Errorf("record %d component %d has invalid span/hash", index+1, componentIndex)
			}
			previousEnd = component.End
		}
		inspected = append(inspected, inspectedCompositionRecord{file: relative, record: index + 1, value: record})
	}
	return inspected, nil
}

func matchCompositionSources(resultText string, records []inspectedCompositionRecord) ([]CompositionSourceMatch, error) {
	segments, err := inspectionResultTextSegments(resultText)
	if err != nil {
		return nil, err
	}
	var matches []CompositionSourceMatch
	for _, record := range records {
		for _, segment := range segments {
			bytesValue := []byte(segment)
			if len(bytesValue) != record.value.OutputBytes {
				continue
			}
			sum := sha256.Sum256(bytesValue)
			if hex.EncodeToString(sum[:]) != record.value.OutputSHA256 {
				continue
			}
			for componentIndex, component := range record.value.Components {
				componentSum := sha256.Sum256(bytesValue[component.Start:component.End])
				if hex.EncodeToString(componentSum[:]) != component.SHA256 {
					return nil, fmt.Errorf("%s record %d component %d hash mismatch", record.file, record.record, componentIndex)
				}
			}
			matches = append(matches, CompositionSourceMatch{
				File: record.file, Record: record.record, Surface: record.value.Surface, OutputBytes: record.value.OutputBytes,
				Components: append([]CompositionComponent(nil), record.value.Components...),
			})
			break
		}
	}
	return matches, nil
}
