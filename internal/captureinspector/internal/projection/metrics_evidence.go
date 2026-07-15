package projection

import (
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/capture"
)

const maxMetricEvidenceRefs = 64

type metricEvidenceCatalog struct {
	raw        []EvidenceRef
	graph      []EvidenceRef
	lifecycle  []EvidenceRef
	provider   []EvidenceRef
	context    []EvidenceRef
	mcp        []EvidenceRef
	tools      []EvidenceRef
	client     []EvidenceRef
	provenance []EvidenceRef
}

func attachMetricEvidence(view *View) {
	catalog := buildMetricEvidenceCatalog(view)
	for index := range view.Metrics {
		metric := &view.Metrics[index]
		var evidence []EvidenceRef
		switch {
		case metric.ID == "graph.edges":
			evidence = catalog.graph
		case metric.ID == "scope.client_sessions":
			evidence = catalog.provider
		case strings.HasPrefix(metric.ID, "scope."):
			evidence = catalog.lifecycle
		case strings.HasPrefix(metric.ID, "provider."), strings.HasPrefix(metric.ID, "context."):
			evidence = collapseMetricEvidence(catalog.provider, catalog.context)
		case strings.HasPrefix(metric.ID, "mcp."):
			evidence = catalog.mcp
		case strings.HasPrefix(metric.ID, "tools."):
			evidence = catalog.tools
		case strings.HasPrefix(metric.ID, "client."):
			evidence = catalog.client
		case strings.HasPrefix(metric.ID, "provenance."):
			evidence = catalog.provenance
		default:
			evidence = catalog.raw
		}
		if len(evidence) == 0 {
			evidence = catalog.raw
		}
		metric.Evidence = append([]EvidenceRef(nil), evidence...)
	}
}

func buildMetricEvidenceCatalog(view *View) metricEvidenceCatalog {
	manifest := EvidenceRef{ID: "manifest:manifest.json", File: "manifest.json"}
	catalog := metricEvidenceCatalog{
		raw: []EvidenceRef{manifest}, graph: []EvidenceRef{manifest}, lifecycle: []EvidenceRef{manifest},
		provider: []EvidenceRef{manifest}, context: []EvidenceRef{manifest}, mcp: []EvidenceRef{manifest},
		tools: []EvidenceRef{manifest}, client: []EvidenceRef{manifest}, provenance: []EvidenceRef{manifest},
	}
	for _, file := range view.RawFiles {
		ref := EvidenceRef{ID: "file:" + file.Path, File: file.Path}
		catalog.raw = append(catalog.raw, ref)
		switch file.Kind {
		case capture.ManifestFileProvider:
			catalog.provider = append(catalog.provider, ref)
		case capture.ManifestFileLifecycle:
			catalog.lifecycle = append(catalog.lifecycle, ref)
		case capture.ManifestFileMCP:
			catalog.mcp = append(catalog.mcp, ref)
		case capture.ManifestFileProvenance:
			catalog.provenance = append(catalog.provenance, ref)
		}
	}
	for _, edge := range view.Edges {
		catalog.graph = append(catalog.graph, edge.Evidence...)
	}
	for _, event := range view.Timeline {
		if event.EvalRunID != "" || event.ScenarioRunID != "" || event.InvocationID != "" {
			catalog.lifecycle = append(catalog.lifecycle, event.Evidence...)
		}
	}
	for _, exchange := range view.Exchanges {
		catalog.provider = append(catalog.provider, exchange.Evidence...)
	}
	for _, event := range view.ProviderEvents {
		catalog.provider = append(catalog.provider, event.Evidence)
	}
	for _, block := range view.ProviderBlocks {
		catalog.provider = append(catalog.provider, block.Evidence)
	}
	for _, snapshot := range view.Contexts {
		catalog.context = append(catalog.context, snapshot.Evidence)
	}
	for _, process := range view.MCPProcesses {
		catalog.mcp = append(catalog.mcp, EvidenceRef{ID: "file:" + process.File, File: process.File})
	}
	for _, call := range view.MCPCalls {
		catalog.mcp = append(catalog.mcp, call.Evidence...)
	}
	for _, tool := range view.Tools {
		catalog.tools = append(catalog.tools, tool.Evidence...)
		if tool.Category == toolCategoryMCP {
			catalog.mcp = append(catalog.mcp, tool.Evidence...)
		}
	}
	for _, run := range view.ClientRuns {
		catalog.client = append(catalog.client, run.Evidence...)
	}
	for _, event := range view.Conversation {
		catalog.client = append(catalog.client, event.Evidence...)
	}
	for _, source := range view.Sources {
		if source.File != "" {
			catalog.provenance = append(catalog.provenance, EvidenceRef{ID: "file:" + source.File, File: source.File})
		}
	}
	catalog.raw = collapseMetricEvidence(catalog.raw)
	catalog.graph = collapseMetricEvidence(catalog.graph)
	catalog.lifecycle = collapseMetricEvidence(catalog.lifecycle)
	catalog.provider = collapseMetricEvidence(catalog.provider)
	catalog.context = collapseMetricEvidence(catalog.context)
	catalog.mcp = collapseMetricEvidence(catalog.mcp)
	catalog.tools = collapseMetricEvidence(catalog.tools)
	catalog.client = collapseMetricEvidence(catalog.client)
	catalog.provenance = collapseMetricEvidence(catalog.provenance)
	return catalog
}

type metricEvidenceRange struct {
	file      string
	minSeq    uint64
	maxSeq    uint64
	hasSeq    bool
	wholeFile bool
}

// collapseMetricEvidence turns an arbitrary aggregate's observations into one
// deterministic record range per canonical file. This keeps metric payloads
// bounded without pretending that a sampled record alone supports the whole
// aggregate; manifest.json remains the inventory coordinate when more than the
// bounded number of files participate.
func collapseMetricEvidence(groups ...[]EvidenceRef) []EvidenceRef {
	ranges := make(map[string]metricEvidenceRange)
	for _, group := range groups {
		for _, ref := range group {
			if ref.File == "" {
				continue
			}
			item := ranges[ref.File]
			item.file = ref.File
			start, end := ref.SeqStart, ref.SeqEnd
			if start == 0 && end > 0 {
				start = end
			}
			if end == 0 && start > 0 {
				end = start
			}
			if start == 0 {
				item.wholeFile = true
			} else {
				if !item.hasSeq || start < item.minSeq {
					item.minSeq = start
				}
				if !item.hasSeq || end > item.maxSeq {
					item.maxSeq = end
				}
				item.hasSeq = true
			}
			ranges[ref.File] = item
		}
	}
	files := make([]string, 0, len(ranges))
	for file := range ranges {
		if file != "manifest.json" {
			files = append(files, file)
		}
	}
	sort.Strings(files)
	if _, ok := ranges["manifest.json"]; ok {
		files = append([]string{"manifest.json"}, files...)
	}
	if len(files) > maxMetricEvidenceRefs {
		files = files[:maxMetricEvidenceRefs]
	}
	result := make([]EvidenceRef, 0, len(files))
	for _, file := range files {
		item := ranges[file]
		ref := EvidenceRef{ID: "file:" + file, File: file}
		if item.hasSeq && !item.wholeFile {
			ref.ID = rawRangeID(file, item.minSeq, item.maxSeq)
			ref.SeqStart = item.minSeq
			ref.SeqEnd = item.maxSeq
		}
		result = append(result, ref)
	}
	return result
}
