// Tests for: tools/lease.go — zerops_lease action=acquire carries, per lease it
// took, the dev servers zcp left there and the sibling values they were
// handed, and nextActions names the status call for a server still starting
// (docs/spec-workflows.md §8 "Dev pools (DP)").
package tools

import (
	"encoding/json"
	"maps"
	"strings"
	"testing"
)

// TestLeaseTool_Acquire_CarriesEachLeasesDevServers: a group acquire's result
// names, per lease, each dev server with its status as the acquire left it
// and the sibling values its servers run with — never a lease string — and
// nextActions names zerops_dev_server action=status for the server whose port
// did not answer yet.
func TestLeaseTool_Acquire_CarriesEachLeasesDevServers(t *testing.T) {
	t.Parallel()
	ssh, _, web := newRewireToolSSH()
	// webpool-7 is a reused container: zcp starts its supervisor, and web is
	// still compiling its first answer when the acquire ends.
	web.Running, web.Servers = false, nil
	web.Settle = map[string]int{"web": 100}
	srv, orchs, _ := groupLeaseServerSSH(t, ssh)

	res := callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pools": []string{"hqpool", "webpool"}, "repo": leaseTestRepo, "branch": "feat-x"})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("acquire: %s", text)
	}
	for _, o := range orchs {
		if strings.Contains(text, o.lease()) || strings.Contains(text, o.token) {
			t.Fatalf("result carries a secret: %s", text)
		}
	}
	var got struct {
		Leases []struct {
			Alias      string `json:"alias"`
			DevServers []struct {
				Name   string `json:"name"`
				Status string `json:"status"`
			} `json:"devServers"`
			SiblingEnv *struct {
				Set map[string]string `json:"set"`
			} `json:"siblingEnv"`
		} `json:"leases"`
		NextActions string `json:"nextActions"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
	want := map[string]string{"hqpool-3": "api running", "webpool-7": "web starting"}
	siblings := map[string]string{"HQ_HOST": "node-id-3.runtime.hqpool.zerops", "WEB_HOST": "node-id-7.runtime.webpool.zerops"}
	if len(got.Leases) != 2 {
		t.Fatalf("leases = %+v, want hqpool-3 and webpool-7", got.Leases)
	}
	for _, l := range got.Leases {
		if len(l.DevServers) != 1 || l.DevServers[0].Name+" "+l.DevServers[0].Status != want[l.Alias] {
			t.Errorf("%s devServers = %+v, want %s", l.Alias, l.DevServers, want[l.Alias])
		}
		if l.SiblingEnv == nil || !maps.Equal(l.SiblingEnv.Set, siblings) {
			t.Errorf("%s siblingEnv = %+v, want %v", l.Alias, l.SiblingEnv, siblings)
		}
	}
	if !strings.Contains(got.NextActions, `zerops_dev_server action=status hostname="webpool-7" name="web"`) {
		t.Errorf("nextActions %q should name the status call for web, still starting", got.NextActions)
	}
	if strings.Contains(got.NextActions, `hostname="hqpool-3" name="api"`) {
		t.Errorf("nextActions %q names api, which runs", got.NextActions)
	}
}
