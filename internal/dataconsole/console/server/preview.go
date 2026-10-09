package server

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// Keep the positional rows and cursor intact while bounding each cell and the
// aggregate cell payload. Exact short numbers retain their original wire type.
func boundTableCells(page *provider.TablePage) {
	cells := 0
	for _, row := range page.Rows {
		cells += len(row)
	}
	if cells == 0 {
		return
	}
	limit := min(4096, (512*1024)/cells)
	for _, row := range page.Rows {
		for i, value := range row {
			data, err := json.Marshal(value)
			if err != nil || len(data) <= limit {
				continue
			}
			text, ok := value.(string)
			if !ok {
				text = string(data)
			}
			text = text[:min(len(text), limit)]
			for !utf8.ValidString(text) {
				text = strings.ToValidUTF8(text, "")
			}
			row[i] = text + "… [truncated]"
		}
	}
}
