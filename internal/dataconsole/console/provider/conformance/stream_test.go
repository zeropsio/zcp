//go:build e2e

package conformance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// TestStream_Conversions supersedes the 4 skips left in
// internal/dataconsole/console/provider/stream/stream_test.go pointing at
// "see e2e" (now this file). The stream provider's kafkaTopics/natsStreams
// helpers are unexported (white-box only from inside the stream package), so
// this suite — which only ever sees the public provider.ObjectProvider shape,
// exactly like the real server — proves the SAME success paths black-box,
// through the methods that call them:
//
//   - TestKafkaTopics_NeedsLiveBroker / TestNatsStreams_NeedsLiveBroker  →
//     List({}) succeeding (topics/streams enumerated).
//   - TestList_Health_RootPath_SuccessPath_NeedsLiveBroker              →
//     setupService's Health() gate (Health delegates to List internally,
//     see stream.go) + the List({}) call below.
//   - TestReadBlob_SuccessPath_NeedsLiveBroker                          →
//     ReadBlob on the first topic/stream found.
//
// Seeds its own namespaced fixture (console/seed — S10b) before listing, so
// a topic/stream is guaranteed present by this test itself rather than
// depending on cmd/dcseed having run out-of-band; torn down (deferred)
// after the assertions.
func TestStream_Conversions(t *testing.T) {
	requireHarness(t)
	entries := activeConfig.ByFamily(provider.FamilyStream)
	if len(entries) == 0 {
		t.Skip("no stream services in DC_LIVE_CONFIG")
	}
	for _, entry := range entries {
		t.Run(entry.Hostname, func(t *testing.T) {
			prov := setupService(t, entry) // Health() already proves List({}) once — see below
			if prov == nil {
				return
			}
			defer func() { _ = prov.Close() }()
			op, ok := prov.(provider.ObjectProvider)
			if !ok {
				t.Fatalf("%s: provider %T does not implement ObjectProvider", entry.Hostname, prov)
			}

			desc, err := entry.Descriptor()
			if err != nil {
				t.Fatalf("%s: descriptor: %v", entry.Hostname, err) // already validated at config load
			}
			cleanupFixture := seedNamespacedFixture(t, entry, desc)
			if cleanupFixture == nil {
				return // seed failed — already skipped/failed by seedNamespacedFixture
			}
			defer cleanupFixture()

			ctx, cancel := context.WithTimeout(context.Background(), assertTimeout)
			defer cancel()

			// supersedes TestKafkaTopics_NeedsLiveBroker / TestNatsStreams_NeedsLiveBroker
			nodes, _, err := op.List(ctx, provider.Path{Service: entry.Hostname}, provider.Page{})
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(nodes) == 0 {
				skipOrFail(t, entry, "no topics/streams present even after seeding")
				return
			}
			t.Logf("%s: found %d topics/streams", entry.Hostname, len(nodes))

			// supersedes TestReadBlob_SuccessPath_NeedsLiveBroker
			preview, meta, err := op.ReadBlob(ctx, nodes[0].Path)
			if err != nil {
				t.Fatalf("ReadBlob(%v): %v", nodes[0].Path.Segments, err)
			}
			if meta.ContentType != "application/json" {
				t.Errorf("ReadBlob(%v).ContentType = %q, want application/json", nodes[0].Path.Segments, meta.ContentType)
			}
			// ProofStreamMetadata: a topic/stream ReadBlob must be flagged as a
			// generated metadata summary, never indistinguishable from a real
			// document (U-04, DD-3) — the discriminator the SPA keys on to render
			// a labelled "metadata, not messages" card.
			if !meta.StreamMetadata {
				t.Errorf("ReadBlob(%v).StreamMetadata = false, want true (a stream summary must be flagged, U-04/DD-3)", nodes[0].Path.Segments)
			}

			// ProofDownloadContent: download is the generated summary itself,
			// byte-for-byte, never a separate message-consumption path.
			dl, ok := prov.(provider.BlobDownloader)
			if !ok {
				t.Fatalf("%s: provider %T does not implement BlobDownloader", entry.Hostname, prov)
			}
			download, downloadMeta, err := dl.DownloadBlob(ctx, nodes[0].Path)
			if err != nil {
				t.Fatalf("DownloadBlob(%v): %v", nodes[0].Path.Segments, err)
			}
			downloaded, readErr := io.ReadAll(download)
			closeErr := download.Close()
			if readErr != nil {
				t.Fatalf("read DownloadBlob(%v): %v", nodes[0].Path.Segments, readErr)
			}
			if closeErr != nil {
				t.Fatalf("close DownloadBlob(%v): %v", nodes[0].Path.Segments, closeErr)
			}
			if !bytes.Equal(downloaded, preview) || !json.Valid(downloaded) {
				t.Errorf("DownloadBlob(%v) = %s, want the same valid generated metadata as ReadBlob", nodes[0].Path.Segments, downloaded)
			}
			wantFilename := nodes[0].Name + ".json"
			if downloadMeta.Size != int64(len(downloaded)) || downloadMeta.ContentType != "application/json" || downloadMeta.Filename != wantFilename {
				t.Errorf("DownloadBlob(%v) metadata = %+v, want size=%d contentType=application/json filename=%q", nodes[0].Path.Segments, downloadMeta, len(downloaded), wantFilename)
			}

			recordSummary(t, entry.Hostname, string(provider.FamilyStream))
		})
	}
}

// TestStream_MutationRefusal proves ProofMutationRefusal for the stream
// family's view-only engines (kafka, nats): every mutation shape refuses
// with the one ErrReadOnly signal even under armed writes (stream.go
// hardcodes every mutating method to ErrReadOnly unconditionally —
// STR-AUD-02 resolution 7) — the provider's own posture, not the caller's
// requested write flag.
func TestStream_MutationRefusal(t *testing.T) {
	requireHarness(t)
	entries := activeConfig.ByFamily(provider.FamilyStream)
	if len(entries) == 0 {
		t.Skip("no stream services in DC_LIVE_CONFIG")
	}
	for _, entry := range entries {
		t.Run(entry.Hostname, func(t *testing.T) {
			prov := setupService(t, entry) // armed writes requested — the provider's own posture must still win
			if prov == nil {
				return
			}
			defer func() { _ = prov.Close() }()
			op, ok := prov.(provider.ObjectProvider)
			if !ok {
				t.Fatalf("%s: provider %T does not implement ObjectProvider", entry.Hostname, prov)
			}
			ctx, cancel := context.WithTimeout(context.Background(), assertTimeout)
			defer cancel()

			probe := provider.Path{Service: entry.Hostname, Segments: []string{"_conformance_probe_topic"}}
			if err := op.WriteBlob(ctx, probe, []byte("x"), ""); !errors.Is(err, provider.ErrReadOnly) {
				t.Errorf("WriteBlob on a stream (view-only) engine = %v, want ErrReadOnly", err)
			}
			if err := op.Delete(ctx, probe); !errors.Is(err, provider.ErrReadOnly) {
				t.Errorf("Delete on a stream (view-only) engine = %v, want ErrReadOnly", err)
			}

			recordSummary(t, entry.Hostname, string(provider.FamilyStream))
		})
	}
}
