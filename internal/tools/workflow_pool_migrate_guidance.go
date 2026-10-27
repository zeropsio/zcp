package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/workflow"
)

// A pool-migrate answer's guidance is the pool-migrate-active atoms its step
// reaches — the plan and the move step's on every answer, the env step's once
// the pool is settled, the image step's once it holds the dev's env, the
// retire's once the image step is done — and it grows with the flow. A flow's
// answers carry each atom once (docs/spec-workflows.md §13 PM-15): the first
// answer that reaches it carries it in full, every later one names it in one
// line beside the call that repeats it all, action="status", the recovery
// after a context compaction. The three sessions of 2026-10-27 received the
// whole set, 6–19 KB, on every answer — ~110–210 KB a session.

// poolMigrateGuidancePart is one atom of an answer's guidance.
type poolMigrateGuidancePart struct {
	id, title, body string
}

// poolMigrateGuidanceSent remembers which guidance atoms each flow — keyed by
// its dev service's hostname — has sent in this zcp process. One is made per
// registered zerops_workflow tool: an MCP server serves one conversation, so
// what it remembers is what that conversation was sent. Nothing is persisted:
// the read side writes nothing (PM-1).
type poolMigrateGuidanceSent struct {
	mu   sync.Mutex
	sent map[string]map[string]bool
}

func newPoolMigrateGuidanceSent() *poolMigrateGuidanceSent {
	return &poolMigrateGuidanceSent{sent: map[string]map[string]bool{}}
}

// addGuidance appends atom id to resp's guidance: its body to the text, and
// the part render reads.
func (resp *poolMigrateResponse) addGuidance(corpus []workflow.KnowledgeAtom, id string) {
	body := atomBody(corpus, id)
	if body == "" {
		return
	}
	if resp.Guidance != "" {
		resp.Guidance += "\n\n"
	}
	resp.Guidance += body
	title := id
	for _, a := range corpus {
		if a.ID == id && a.Title != "" {
			title = a.Title
		}
	}
	resp.guidance = append(resp.guidance, poolMigrateGuidancePart{id: id, title: title, body: body})
}

// render is resp's guidance as this call sends it: in full when full is set
// (action="status") or nothing remembers (g nil); else the atoms the flow has
// not been sent yet in full, and one line naming those it has, with the call
// that repeats them. Each atom sent is remembered.
func (g *poolMigrateGuidanceSent) render(resp poolMigrateResponse, full bool) string {
	if g == nil || len(resp.guidance) == 0 || resp.Service == "" {
		return resp.Guidance
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	sent := g.sent[resp.Service]
	if sent == nil {
		sent = map[string]bool{}
		g.sent[resp.Service] = sent
	}
	bodies := make([]string, 0, len(resp.guidance)+1)
	var earlier []string
	for _, p := range resp.guidance {
		if !full && sent[p.id] {
			earlier = append(earlier, fmt.Sprintf("%q", p.title))
			continue
		}
		bodies = append(bodies, p.body)
		sent[p.id] = true
	}
	if len(earlier) > 0 {
		bodies = append(bodies, fmt.Sprintf("Unchanged, sent in full on an earlier answer of this flow: %s. If you no longer have it (a context compaction), zerops_workflow workflow=%q action=\"status\" service=%q answers it all again.",
			strings.Join(earlier, ", "), workflowPoolMigrate, resp.Service))
	}
	return strings.Join(bodies, "\n\n")
}

// respond answers resp with its guidance rendered for this call.
func (d poolMigrateDeps) respond(resp poolMigrateResponse) *mcp.CallToolResult {
	resp.Guidance = d.guidance.render(resp, d.fullGuidance)
	return jsonResult(resp)
}

// poolMigrateStatusRun is zerops_workflow workflow="pool-migrate"
// action="status": the read side's answer with the flow's whole guidance, the
// recovery of an agent that lost what the earlier answers sent.
func poolMigrateStatusRun(ctx context.Context, d poolMigrateDeps, input WorkflowInput) (*mcp.CallToolResult, any, error) {
	d.fullGuidance = true
	return poolMigrateRun(ctx, d, input)
}
