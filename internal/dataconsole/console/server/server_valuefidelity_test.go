package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// This file pins DD-9's value-fidelity contract (S28) at the HTTP layer:
// OBJ-AUD-01 (content-type carried on write + upload), KV-AUD-05 (truncated
// blob's true size), and the decode()-side half of T-AUD-02 (a bare JSON
// integer in a mutating body must reach the provider as json.Number, never a
// rounded float64). Evidence: plans/dataconsole-audit/{object-stream,kv,
// tabular}.md.

// pngMagic is the PNG file signature — http.DetectContentType recognizes it
// deterministically as "image/png", making it a reliable sniff-fallback probe
// (unlike plain ASCII text, whose sniffed type can be a matter of taste).
var pngMagic = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

// ---- OBJ-AUD-01: content-type carried on write ----

func TestHandleBlob_Write_ContentType_ExplicitFieldWins(t *testing.T) {
	t.Parallel()
	ts, tok, fake := newTestServerWithFake(t, true)
	defer ts.Close()

	body := fmt.Sprintf(`{"path":{"service":"store","segments":["pic.png"]},"data":%q,"contentType":"image/png"}`,
		base64.StdEncoding.EncodeToString(pngMagic))
	if r := do(t, ts, "PUT", "/api/blob", tok, body, true); r.code != http.StatusOK {
		t.Fatalf("PUT = %d, want 200", r.code)
	}
	if got := fake.contentTypes["pic.png"]; got != "image/png" {
		t.Fatalf("provider received contentType = %q, want image/png (explicit field must win)", got)
	}
}

func TestHandleBlob_Write_ContentType_DefaultsToSniffWhenAbsent(t *testing.T) {
	t.Parallel()
	ts, tok, fake := newTestServerWithFake(t, true)
	defer ts.Close()

	// No "contentType" field at all — the fix must sniff from the bytes
	// rather than hand the provider an empty string (which OBJ-AUD-01 proved
	// degrades to "application/octet-stream" and breaks the next read's
	// preview/edit classification).
	body := fmt.Sprintf(`{"path":{"service":"store","segments":["sniffed.png"]},"data":%q}`,
		base64.StdEncoding.EncodeToString(pngMagic))
	if r := do(t, ts, "PUT", "/api/blob", tok, body, true); r.code != http.StatusOK {
		t.Fatalf("PUT = %d, want 200", r.code)
	}
	if got := fake.contentTypes["sniffed.png"]; got != "image/png" {
		t.Fatalf("provider received contentType = %q, want image/png sniffed from the PNG magic bytes", got)
	}
}

// TestHandleUpload_PreservesDeclaredContentType is the multipart half of
// OBJ-AUD-01: the browser-declared MIME on the file part must reach the
// provider, not be silently discarded (the old code read the FileHeader via
// `file, _, err := r.FormFile("file")`, throwing away the one place the
// declared type lived).
func TestHandleUpload_PreservesDeclaredContentType(t *testing.T) {
	t.Parallel()
	ts, tok, fake := newTestServerWithFake(t, true)
	defer ts.Close()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("service", "store")
	_ = mw.WriteField("segs", `["uploaded.png"]`)
	part, err := mw.CreatePart(map[string][]string{
		"Content-Disposition": {`form-data; name="file"; filename="uploaded.png"`},
		"Content-Type":        {"image/png"},
	})
	if err != nil {
		t.Fatalf("CreatePart: %v", err)
	}
	_, _ = part.Write(pngMagic)
	_ = mw.Close()

	req := newRequest(t, http.MethodPost, "/api/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp := doReq(t, ts, req, tok, true)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload = %d, want 200", resp.StatusCode)
	}
	if got := fake.contentTypes["uploaded.png"]; got != "image/png" {
		t.Fatalf("provider received contentType = %q, want image/png (declared MIME on the multipart part)", got)
	}
}

// TestHandleUpload_NoDeclaredType_Sniffs covers the upload path's own
// fallback: a part with no Content-Type at all must still resolve to a real
// type via sniffing, not an empty string.
func TestHandleUpload_NoDeclaredType_Sniffs(t *testing.T) {
	t.Parallel()
	ts, tok, fake := newTestServerWithFake(t, true)
	defer ts.Close()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("service", "store")
	_ = mw.WriteField("segs", `["bare.png"]`)
	// CreateFormFile always sets "application/octet-stream" itself, so build
	// the part by hand with NO Content-Type header to exercise the true
	// absent-header path.
	part, err := mw.CreatePart(map[string][]string{
		"Content-Disposition": {`form-data; name="file"; filename="bare.png"`},
	})
	if err != nil {
		t.Fatalf("CreatePart: %v", err)
	}
	_, _ = part.Write(pngMagic)
	_ = mw.Close()

	req := newRequest(t, http.MethodPost, "/api/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp := doReq(t, ts, req, tok, true)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload = %d, want 200", resp.StatusCode)
	}
	if got := fake.contentTypes["bare.png"]; got != "image/png" {
		t.Fatalf("provider received contentType = %q, want image/png sniffed from bytes", got)
	}
}

// ---- KV-AUD-05: truncated blob's true size ----

// TestHandleBlob_Read_TruncatedBlob_ReportsTrueSize pins KV-AUD-05:
// BlobMeta.Size (the true, pre-truncation size every provider computes) used
// to be dropped in transport — handleBlob's GET case only turned
// ContentType/Truncated into headers. Without it, a client has no way to
// show "showing 16 MiB of N".
func TestHandleBlob_Read_TruncatedBlob_ReportsTrueSize(t *testing.T) {
	t.Parallel()
	ts, tok, fake := newTestServerWithFake(t, true)
	defer ts.Close()
	fake.blobs["big.bin"] = []byte("only-a-head-slice")
	fake.truncatedSize = map[string]int64{"big.bin": 17 << 20} // 17 MiB true size

	r := do(t, ts, "GET", `/api/blob?service=store&segs=%5B%22big.bin%22%5D`, tok, "", false)
	if r.code != http.StatusOK {
		t.Fatalf("GET = %d, want 200", r.code)
	}
	if got := r.header.Get("X-DataConsole-Truncated"); got != "true" {
		t.Fatalf("X-DataConsole-Truncated = %q, want true", got)
	}
	if got := r.header.Get("X-DataConsole-Size"); got != "17825792" { // 17<<20
		t.Fatalf("X-DataConsole-Size = %q, want 17825792 (the true pre-truncation size, not len(body))", got)
	}
}

// ---- T-AUD-02 (decode-side): a bare JSON integer must reach the provider
// as json.Number, never a rounded float64 ----

// TestHandleRow_Insert_BigintJSONNumber_ReachesProviderExact proves decode()'s
// UseNumber() end-to-end: POSTing a bare (unquoted) 2^53+1 JSON integer in
// InsertRow's row must arrive at the provider as json.Number holding the
// exact source text — plain json.Decode (float64 target) would already have
// rounded it during parsing itself, before the provider is ever called.
func TestHandleRow_Insert_BigintJSONNumber_ReachesProviderExact(t *testing.T) {
	t.Parallel()
	ts, tok, rec, _ := newRouteServer(t, "postgresql:single@18", provider.FamilyTabular, true)
	defer ts.Close()

	body := `{"path":{"service":"svc","segments":["public","t"]},"row":{"size_bytes":9007199254740993}}`
	if r := do(t, ts, "POST", "/api/row", tok, body, true); r.code != http.StatusOK {
		t.Fatalf("insert = %d, want 200", r.code)
	}
	got, ok := rec.insertRow()["size_bytes"].(json.Number)
	if !ok {
		t.Fatalf("row[size_bytes] = %#v (%T), want json.Number", rec.insertRow()["size_bytes"], rec.insertRow()["size_bytes"])
	}
	if got.String() != "9007199254740993" {
		t.Fatalf("row[size_bytes] = %q, want the exact source text 9007199254740993 (T-AUD-02)", got.String())
	}
}

// TestHandleCell_Edit_BigintJSONNumber_ReachesProviderExact mirrors the above
// for CellEdit.NewValue/ExpectedOld — the other site T-AUD-02/DD-9 names
// explicitly.
func TestHandleCell_Edit_BigintJSONNumber_ReachesProviderExact(t *testing.T) {
	t.Parallel()
	ts, tok, rec, _ := newRouteServer(t, "postgresql:single@18", provider.FamilyTabular, true)
	defer ts.Close()

	body := `{"path":{"service":"svc","segments":["public","t"]},"rowKey":{"id":42},"column":"size_bytes",` +
		`"newValue":9007199254740993,"expectedOld":9007199254740992}`
	if r := do(t, ts, "PUT", "/api/cell", tok, body, true); r.code != http.StatusOK {
		t.Fatalf("edit = %d, want 200", r.code)
	}
	newVal, ok := rec.cellEdit().NewValue.(json.Number)
	if !ok || newVal.String() != "9007199254740993" {
		t.Fatalf("NewValue = %#v, want json.Number(9007199254740993) (T-AUD-02)", rec.cellEdit().NewValue)
	}
	oldVal, ok := rec.cellEdit().ExpectedOld.(json.Number)
	if !ok || oldVal.String() != "9007199254740992" {
		t.Fatalf("ExpectedOld = %#v, want json.Number(9007199254740992) (T-AUD-02)", rec.cellEdit().ExpectedOld)
	}
	rowID, ok := rec.cellEdit().RowKey["id"].(json.Number)
	if !ok || rowID.String() != "42" {
		t.Fatalf("RowKey[id] = %#v, want json.Number(42)", rec.cellEdit().RowKey["id"])
	}
}
