package tools

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/knowledge"
)

func TestKnowledgeTool_PlaybookURI_FetchesEmbedded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		uri         string
		wantContent string
	}{
		{
			name:        "onboarding playbook",
			uri:         "zerops://playbooks/onboarding",
			wantContent: "# Onboard me to Zerops",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store, err := knowledge.GetEmbeddedStore()
			if err != nil {
				t.Fatalf("GetEmbeddedStore: %v", err)
			}
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			RegisterKnowledge(srv, store, nil, nil, nil, nil)

			result := callTool(t, srv, "zerops_knowledge", map[string]any{
				"uri": tt.uri,
			})

			if result.IsError {
				t.Fatalf("unexpected error: %s", getTextContent(t, result))
			}
			body := getTextContent(t, result)
			if !strings.Contains(body, tt.wantContent) {
				t.Errorf("fetch body should contain %q; got: %s", tt.wantContent, body)
			}
		})
	}
}
