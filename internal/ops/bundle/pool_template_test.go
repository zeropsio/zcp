package bundle

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// PT-STANZA-SSH — A part's dev pool is imported with its sshIsolation
// admitting the zcp service among its service variables.
// witness: internal/ops/bundle/pool_template.go | Vault:                 map[string]string{PoolSSHIsolationKey: PoolSSHIsolation(p.ZcpHostname)}, → Vault:                 map[string]string{},
func TestPoolStanza_PTStanzaSSH_AdmitsZcpOverSSH(t *testing.T) {
	t.Parallel()
	out, err := PoolStanza{Hostname: "apppool", ZcpHostname: "zcp"}.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	var doc struct {
		Services []struct {
			Hostname string            `yaml:"hostname"`
			Vault    map[string]string `yaml:"vault"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("stanza does not decode: %v\n%s", err, out)
	}
	if len(doc.Services) != 1 || doc.Services[0].Hostname != "apppool" {
		t.Fatalf("stanza services = %+v, want the one pool apppool", doc.Services)
	}
	if got := doc.Services[0].Vault["sshIsolation"]; got != "vpn service@zcp" {
		t.Errorf("pool sshIsolation = %q, want %q", got, "vpn service@zcp")
	}
}
