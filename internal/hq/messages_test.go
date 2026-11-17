package hq

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// witness: internal/hq/client.go | resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated → resp.StatusCode != http.StatusOK
func TestClient_SendMessage_HQsCreatedAnswerIsTheSentMessage(t *testing.T) {
	t.Parallel()
	var call hqCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		call = hqCall{r.Method, r.URL.EscapedPath(), r.Header.Get("Authorization"), r.Header.Get("Content-Type"), string(raw)}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"m-2","chainId":"m-1","hop":2,"to":{"projectId":"p-shop","projectName":"shop - Rhea","mateName":"Rhea"},"delivery":"on-wake"}`)
	}))
	t.Cleanup(srv.Close)

	sent, err := keyedClient(t, srv).SendMessage(context.Background(), MessageSend{
		To: "p-shop", Words: "Which port does the API listen on?",
		Attachment: &MessageAttachment{Title: "the failing request", Text: "GET /api 502"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := SentMessage{ID: "m-2", ChainID: "m-1", Hop: 2,
		To: MessageTo{ProjectID: "p-shop", ProjectName: "shop - Rhea", MateName: "Rhea"}, Delivery: DeliveryOnWake}
	if sent != want {
		t.Errorf("sent = %+v, want %+v", sent, want)
	}
	wantCall := hqCall{http.MethodPost, "/api/mate/messages", "Project good", "application/json",
		`{"to":"p-shop","words":"Which port does the API listen on?","attachment":{"title":"the failing request","text":"GET /api 502"}}`}
	if call != wantCall {
		t.Errorf("call = %+v, want %+v", call, wantCall)
	}
}
