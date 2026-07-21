package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"testing/fstest"

	"github.com/zeropsio/zcp/internal/dataconsole/console"
	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
	"github.com/zeropsio/zcp/internal/dataconsole/console/safety"
)

type tableWireProvider struct {
	page      provider.Page
	readPath  provider.Path
	countPath provider.Path
}

func (*tableWireProvider) Kind() string { return "table-wire" }
func (*tableWireProvider) Caps() provider.Capabilities {
	return provider.Capabilities{Family: provider.FamilyTabular, Support: provider.SupportFull}
}
func (*tableWireProvider) Health(context.Context) error { return nil }
func (*tableWireProvider) Close() error                 { return nil }
func (p *tableWireProvider) ReadTable(_ context.Context, path provider.Path, page provider.Page) (provider.TablePage, error) {
	p.readPath, p.page = path, page
	return provider.TablePage{
		Columns:    []provider.Column{{Name: "name", DataType: "text", Sortable: true}},
		Rows:       [][]any{{"zulu"}},
		NextCursor: "300",
		BestEffort: true,
		Numbered:   true,
	}, nil
}
func (p *tableWireProvider) CountTable(_ context.Context, path provider.Path) (int64, error) {
	p.countPath = path
	return 50000, nil
}

func TestHandleTableAndCount_RoundTripIndependentWireContracts(t *testing.T) {
	fake := &tableWireProvider{}
	host := &recHost{svcType: "postgresql:single@18"}
	eng := console.NewEngine(host, writePolicy(false), map[provider.Family]console.Factory{
		provider.FamilyTabular: func(console.ConnectionInfo, *safety.Policy) (provider.Provider, error) { return fake, nil },
	})
	if err := eng.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	srv := New(eng, "secret", fstest.MapFS{"index.html": {Data: []byte("ok")}})
	ts := &testServer{handler: srv.Handler()}
	pathQuery := `service=svc&segs=%5B%22public%22%2C%22events%22%5D`

	pageResp := doReq(t, ts, newRequest(t, http.MethodGet,
		`/api/table?`+pathQuery+`&cursor=200&limit=100&sort=name&direction=desc`, nil), "secret", false)
	defer func() { _ = pageResp.Body.Close() }()
	if pageResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/table = %d, want 200", pageResp.StatusCode)
	}
	var page provider.TablePage
	if err := json.NewDecoder(pageResp.Body).Decode(&page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	if fake.page.Cursor != "200" || fake.page.Limit != 100 || fake.page.Sort == nil ||
		fake.page.Sort.Column != "name" || fake.page.Sort.Direction != provider.SortDesc {
		t.Fatalf("provider page = %+v, want cursor/limit/name desc", fake.page)
	}
	if len(page.Rows) != 1 || page.NextCursor != "300" || !page.BestEffort || !page.Numbered {
		t.Fatalf("page wire = %+v, want rows/cursor/bestEffort/numbered and no count coupling", page)
	}

	countResp := doReq(t, ts, newRequest(t, http.MethodGet, `/api/table/count?`+pathQuery, nil), "secret", false)
	defer func() { _ = countResp.Body.Close() }()
	if countResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/table/count = %d, want 200", countResp.StatusCode)
	}
	var count struct {
		Count int64 `json:"count"`
	}
	if err := json.NewDecoder(countResp.Body).Decode(&count); err != nil {
		t.Fatalf("decode count: %v", err)
	}
	if count.Count != 50000 {
		t.Fatalf("count wire = %d, want 50000", count.Count)
	}
	if fake.readPath.Service != "svc" || fake.countPath.Service != "svc" || len(fake.countPath.Segments) != 2 {
		t.Fatalf("paths: read=%+v count=%+v, want same service/relation", fake.readPath, fake.countPath)
	}
}
