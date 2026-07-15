package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/zeropsio/zcp/internal/capture"
	"github.com/zeropsio/zcp/internal/captureinspector/internal/projection"
)

func TestViewCacheDetectsSameSizeTamperWithRestoredMtime(t *testing.T) {
	server, client, sessionDir := newTestServer(t)
	authorizeTestClient(t, server, client)

	first := testGET(t, client, server.URL()+"/api/v1/captures/capture-ui-fixture/view")
	firstBody, err := io.ReadAll(first.Body)
	_ = first.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	var initial projection.View
	if err := json.Unmarshal(firstBody, &initial); err != nil || !initial.Integrity.Valid {
		t.Fatalf("initial view invalid: %v, %s", err, firstBody)
	}

	path := filepath.Join(sessionDir, "provider.jsonl")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	changed := false
	for index := range data {
		if data[index] == 'A' {
			data[index] = 'B'
			changed = true
			break
		}
	}
	if !changed {
		t.Fatal("fixture has no byte suitable for same-size tamper")
	}
	if err := os.WriteFile(path, data, info.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if _, err := capture.InspectSession(sessionDir); err == nil {
		t.Fatal("fresh inspection unexpectedly accepted tampered evidence")
	}

	second := testGET(t, client, server.URL()+"/api/v1/captures/capture-ui-fixture/view")
	defer second.Body.Close()
	var after projection.View
	if err := json.NewDecoder(second.Body).Decode(&after); err != nil {
		t.Fatal(err)
	}
	if second.StatusCode == http.StatusOK && after.Integrity.Valid {
		t.Fatal("cached view still reports integrity valid after same-size evidence tamper with restored mtime")
	}
}

func TestPinnedSessionDuplicateIDFailsInsteadOfMixingIndexAndView(t *testing.T) {
	rootCopy := completeFixture(t)
	pinnedCopy := completeFixture(t)
	setManifestLabel(t, rootCopy, "root-copy")
	setManifestLabel(t, pinnedCopy, "pinned-copy")

	root := t.TempDir()
	rootSession := filepath.Join(root, "root-session")
	if err := os.Rename(rootCopy, rootSession); err != nil {
		t.Fatal(err)
	}
	server := &Server{config: Config{CaptureRoot: root, SessionDir: pinnedCopy}, cache: make(map[string]*projection.View)}
	if _, err := server.captureEntries(context.Background()); err == nil {
		t.Fatal("capture index accepted a pinned/root duplicate capture ID")
	}
	if _, err := server.sessionPath(context.Background(), "capture-ui-fixture"); err == nil {
		t.Fatal("by-ID lookup accepted a pinned/root duplicate capture ID")
	}
}

func setManifestLabel(t *testing.T, sessionDir, label string) {
	t.Helper()
	path := filepath.Join(sessionDir, "manifest.json")
	manifest, err := capture.ReadSessionManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Label = label
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}
