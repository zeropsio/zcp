//go:build e2e

package conformance

import (
	"context"
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
func TestStream_Conversions(t *testing.T) {
	requireHarness(t)
	entries := activeConfig.ByFamily(provider.FamilyStream)
	if len(entries) == 0 {
		t.Skip("no stream services in DC_LIVE_CONFIG")
	}
	for _, entry := range entries {
		t.Run(entry.Hostname, func(t *testing.T) {
			prov := setupService(t, entry, true) // Health() already proves List({}) once — see below
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

			// supersedes TestKafkaTopics_NeedsLiveBroker / TestNatsStreams_NeedsLiveBroker
			nodes, _, err := op.List(ctx, provider.Path{Service: entry.Hostname}, provider.Page{})
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(nodes) == 0 {
				skipOrFail(t, entry, "no topics/streams present")
				return
			}
			t.Logf("%s: found %d topics/streams", entry.Hostname, len(nodes))

			// supersedes TestReadBlob_SuccessPath_NeedsLiveBroker
			_, meta, err := op.ReadBlob(ctx, nodes[0].Path)
			if err != nil {
				t.Fatalf("ReadBlob(%v): %v", nodes[0].Path.Segments, err)
			}
			if meta.ContentType != "application/json" {
				t.Errorf("ReadBlob(%v).ContentType = %q, want application/json", nodes[0].Path.Segments, meta.ContentType)
			}

			globalSummary.Record(entry.Hostname, string(provider.FamilyStream), outcomePass, "")
		})
	}
}
