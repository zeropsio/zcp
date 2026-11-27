package platform

import (
	"fmt"
	"slices"
	"strconv"
	"testing"

	"github.com/zeropsio/zerops-go/dto/output"
)

// AV-PAGES — The app-version page reader keeps the rows of a recorded page whose decoded total is lower than its row count, and continues after a full page until a short or empty page.
// witness: internal/platform/zerops_appversion.go | if len(page) < directListLimit { → if len(page) <= directListLimit {
func TestAppVersionPages_AVPages_ReturnsEveryRow(t *testing.T) {
	t.Parallel()

	_, raw := loadRecordings(t)
	var recorded output.ServiceStackList
	decodeRaw(t, raw, "GET /api/rest/public/project/{id}/service-stack", &recorded)
	if recorded.TotalCount.Native() >= len(recorded.List) {
		t.Fatalf("recorded total = %d, rows = %d: need a lower total", recorded.TotalCount.Native(), len(recorded.List))
	}
	recordedIDs := make([]string, len(recorded.List))
	for i, item := range recorded.List {
		recordedIDs[i] = item.Id.Native()
	}
	full := make([]string, directListLimit)
	for i := range full {
		full[i] = strconv.Itoa(i)
	}
	short := []string{"last-1", "last-2"}
	fullThenShort := append(slices.Clone(full), short...)
	tests := []struct {
		name  string
		pages [][]string
		want  []string
	}{
		{"recorded lower total", [][]string{recordedIDs}, recordedIDs},
		{"full then short", [][]string{full, short}, fullThenShort},
		{"full then empty", [][]string{full, nil}, full},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := readAppVersionPages(func(offset int) ([]string, error) {
				start := 0
				for _, page := range tt.pages {
					if offset == start {
						return page, nil
					}
					start += len(page)
				}
				return nil, fmt.Errorf("no page at offset %d", offset)
			})
			if err != nil {
				t.Fatalf("read pages: %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("got %d rows, want %d matching rows", len(got), len(tt.want))
			}
		})
	}
}
