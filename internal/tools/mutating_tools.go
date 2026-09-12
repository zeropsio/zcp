package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// nopMounterForRegistry is a Mounter stub used only to satisfy Register*
// signatures while building the in-memory registry MutatingToolNames reads
// annotations from — no handler is ever invoked, so every method is unused
// dead weight beyond the interface contract.
type nopMounterForRegistry struct{}

func (nopMounterForRegistry) CheckMount(context.Context, string) (platform.MountState, error) {
	return platform.MountStateNotMounted, nil
}
func (nopMounterForRegistry) Mount(context.Context, string, string) error        { return nil }
func (nopMounterForRegistry) Unmount(context.Context, string, string) error      { return nil }
func (nopMounterForRegistry) ForceUnmount(context.Context, string, string) error { return nil }
func (nopMounterForRegistry) IsWritable(context.Context, string) (bool, error)   { return false, nil }
func (nopMounterForRegistry) ListMountDirs(context.Context, string) ([]string, error) {
	return nil, nil
}
func (nopMounterForRegistry) HasUnit(context.Context, string) (bool, error) { return false, nil }
func (nopMounterForRegistry) CleanupUnit(context.Context, string) error     { return nil }

// mutatingToolRegistrySnapshot builds a throwaway *mcp.Server carrying every
// tool this package registers whose annotations do not vary by runtime
// wiring (the fixed table docs/spec-eval-farm.md §4.1 FM-31's askWhen
// correlation and internal/tools/annotations_test.go's "Mutating tools"
// group both describe), using inert stand-ins for every dependency — no
// handler is ever invoked. Read-only-gated variants (SSH vs local deploy)
// register under the same tool name either way, so registering the local
// variant alone is sufficient to capture "zerops_deploy"'s annotations.
func mutatingToolRegistrySnapshot() *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "mutating-tool-registry", Version: "0.0.0"}, nil)

	client := platform.NewMock()
	const projectID = "p1"
	mounter := nopMounterForRegistry{}
	rt := runtime.Info{}
	authInfo := &auth.Info{ProjectID: projectID}
	logFetcher := platform.NewMockLogFetcher()

	RegisterProcess(srv, client, projectID)
	RegisterManage(srv, client, projectID)
	RegisterScale(srv, client, projectID)
	RegisterDelete(srv, client, projectID, "", mounter, rt)
	RegisterSubdomain(srv, client, nil, projectID, "")
	RegisterEnv(srv, client, projectID, "")
	RegisterImport(srv, client, projectID, nil, "", nil, rt)
	RegisterMount(srv, client, projectID, mounter, rt, "", nil, nil)
	RegisterDevServer(srv, client, projectID, nil)
	RegisterDeployLocal(srv, client, nil, projectID, authInfo, logFetcher, "", nil, nil)
	RegisterDeployBatch(srv, client, nil, projectID, nil, authInfo, logFetcher, rt, "", nil, nil)

	return srv
}

// MutatingToolNames returns the set of registered MCP tool names whose
// annotations declare ReadOnlyHint == false — the vocabulary
// docs/spec-eval-farm.md §4.1 FM-31's askWhen correlation uses to decide
// whether an MCP call between an error and a user-sim reply counts as "the
// next mutating call". Derived from the live registry (connect over an
// in-memory transport and list tools, mirroring
// internal/tools/annotations_test.go's listAllTools), never a hand-list, so
// a newly registered mutating tool is picked up automatically. Returns an
// empty set (never panics) if the registry cannot be listed — a caller
// treating every call as non-mutating on failure is safer than a wrong
// classification from a partial list.
func MutatingToolNames() map[string]bool {
	names := make(map[string]bool)
	srv := mutatingToolRegistrySnapshot()

	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, serverTransport, nil); err != nil {
		return names
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "mutating-tool-registry-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		return names
	}
	defer session.Close()

	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		return names
	}
	for _, tool := range result.Tools {
		if tool.Annotations != nil && !tool.Annotations.ReadOnlyHint {
			names[tool.Name] = true
		}
	}
	return names
}
