package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

func TestPreviewLimits(t *testing.T) {
	t.Parallel()
	for _, query := range []string{"", "?limit=1000", "?limit=-2"} {
		if got := parsePage(httptest.NewRequest(http.MethodGet, "/api/table"+query, nil)); got.Limit != 100 {
			t.Fatalf("page cap = %d", got.Limit)
		}
	}
	page := provider.TablePage{Rows: [][]any{{json.Number("9223372036854775807"), strings.Repeat("\u5b57", 10000)}}}
	boundTableCells(&page)
	if page.Rows[0][0] != json.Number("9223372036854775807") {
		t.Fatal("large exact integer changed")
	}
	value, ok := page.Rows[0][1].(string)
	if !ok || !strings.HasSuffix(value, "… [truncated]") || len(value) > 4130 {
		t.Fatal("cell not bounded and marked")
	}
}
