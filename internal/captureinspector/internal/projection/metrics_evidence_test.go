package projection

import (
	"context"
	"testing"
)

func TestEveryKnownMetricCarriesBoundedEvidenceCoordinates(t *testing.T) {
	view, err := Build(context.Background(), completeFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, metric := range view.Metrics {
		if metric.Value == nil {
			continue
		}
		if len(metric.Evidence) == 0 {
			t.Errorf("known metric %s has no evidence coordinates", metric.ID)
		}
		if len(metric.Evidence) > maxMetricEvidenceRefs {
			t.Errorf("metric %s has %d evidence refs, limit %d", metric.ID, len(metric.Evidence), maxMetricEvidenceRefs)
		}
		for _, ref := range metric.Evidence {
			if ref.File == "" || ref.ID == "" {
				t.Errorf("metric %s has incomplete evidence ref %+v", metric.ID, ref)
			}
		}
	}
	assertMetricEvidenceFile(t, view, "provider.exchanges", "provider.jsonl")
	assertMetricEvidenceFile(t, view, "context.snapshots", "provider.jsonl")
	assertMetricEvidenceFile(t, view, "client.stream_events", "eval/suite/scenario/transcript.jsonl")
}

func TestCollapseMetricEvidenceUsesDeterministicFileRanges(t *testing.T) {
	input := []EvidenceRef{
		{ID: "later", File: "provider.jsonl", SeqStart: 9, SeqEnd: 12},
		{ID: "other", File: "mcp/zcp.jsonl", SeqStart: 3, SeqEnd: 3},
		{ID: "earlier", File: "provider.jsonl", SeqStart: 2, SeqEnd: 4},
		{ID: "duplicate", File: "provider.jsonl", SeqStart: 9, SeqEnd: 12},
	}
	got := collapseMetricEvidence(input)
	if len(got) != 2 {
		t.Fatalf("collapsed evidence = %+v", got)
	}
	if got[0].File != "mcp/zcp.jsonl" || got[0].SeqStart != 3 || got[0].SeqEnd != 3 {
		t.Fatalf("first range = %+v", got[0])
	}
	if got[1].File != "provider.jsonl" || got[1].SeqStart != 2 || got[1].SeqEnd != 12 {
		t.Fatalf("provider range = %+v", got[1])
	}
}

func assertMetricEvidenceFile(t *testing.T, view *View, metricID, file string) {
	t.Helper()
	for _, metric := range view.Metrics {
		if metric.ID != metricID {
			continue
		}
		for _, ref := range metric.Evidence {
			if ref.File == file {
				return
			}
		}
		t.Fatalf("metric %s evidence does not include %s: %+v", metricID, file, metric.Evidence)
	}
	t.Fatalf("metric %s not found", metricID)
}
