package bundle

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

// The one pool template's import half: a part's dev pool as zcp imports it
// (knowledge theme pools-and-box, "A part's dev pool"; docs/spec-workflows.md
// §13.1). Pool-migrate's provision stanza renders it, the AI Agent tier
// (groupPoolEntry) writes the same facts in its own commented form, and the
// repository half — the setup check — is ops.CheckPoolSetups
// (docs/spec-mate.md §2.8b). The template spans two
// packages because of the import direction: the AI Agent tier here needs the
// stanza, and ops imports bundle, while the repository half needs ops' own
// zerops.yaml reader (extends resolved) and zcp-devsup facts. Moving that half
// here would be an import cycle or a second copy of both.

const (
	// PoolType is the type zcp imports a part's dev pool as.
	PoolType = "swarm@1"
	// PoolMinContainers is a pool's minimum: no container until a lease
	// asks for one (a container the platform creates for the minimum cannot
	// be reset, DP-15).
	PoolMinContainers = 0
	// DefaultPoolMaxContainers is the most containers a part's dev pool is
	// written with when its live scale was not read.
	DefaultPoolMaxContainers = 3
	// PoolSSHIsolationKey is a pool's SSH admission: which service its
	// containers answer SSH from. It reaches the containers only as a
	// service variable.
	PoolSSHIsolationKey = "sshIsolation"
)

// PoolSSHIsolation is the admission that lets the zcp service zcpHostname
// reach a pool's containers over SSH.
func PoolSSHIsolation(zcpHostname string) string {
	return "vpn service@" + zcpHostname
}

// PoolStanza is a part's dev pool as one import: its hostname, PoolType,
// PoolMinContainers, MaxContainers (DefaultPoolMaxContainers when 0), its
// subdomain, its vertical scale (Vertical's container counts are not read)
// and the sshIsolation admitting ZcpHostname among its service variables. It
// never carries a credential: GIT_TOKEN reaches a pool another way.
type PoolStanza struct {
	Hostname              string
	MaxContainers         int
	EnableSubdomainAccess bool
	Vertical              *Scaling
	ZcpHostname           string
}

// poolStanzaDoc is the import of a pool alone.
type poolStanzaDoc struct {
	Services []poolStanzaService `yaml:"services"`
}

type poolStanzaService struct {
	Hostname      string `yaml:"hostname"`
	Type          string `yaml:"type"`
	MinContainers int    `yaml:"minContainers"`
	MaxContainers int    `yaml:"maxContainers"`
	// EnableSubdomainAccess switches the pool's subdomain on: each lease's
	// preview is the pool's subdomain on the lease's preview port.
	EnableSubdomainAccess bool                `yaml:"enableSubdomainAccess,omitempty"`
	VerticalAutoscaling   *poolStanzaVertical `yaml:"verticalAutoscaling,omitempty"`
	// Vault holds the pool's own service env. A service-level envVariables
	// block is deprecated in the import schema, and sshIsolation reaches the
	// pool's containers only as a service env variable.
	Vault map[string]string `yaml:"vault"`
}

type poolStanzaVertical struct {
	CPUMode string  `yaml:"cpuMode,omitempty"`
	MinCPU  int     `yaml:"minCpu,omitempty"`
	MaxCPU  int     `yaml:"maxCpu,omitempty"`
	MinRAM  float64 `yaml:"minRam,omitempty"`
	MaxRAM  float64 `yaml:"maxRam,omitempty"`
	MinDisk float64 `yaml:"minDisk,omitempty"`
	MaxDisk float64 `yaml:"maxDisk,omitempty"`
}

// Render is the stanza as the import YAML zcp names for it.
func (p PoolStanza) Render() (string, error) {
	maxContainers := p.MaxContainers
	if maxContainers <= 0 {
		maxContainers = DefaultPoolMaxContainers
	}
	svc := poolStanzaService{
		Hostname:              p.Hostname,
		Type:                  PoolType,
		MinContainers:         PoolMinContainers,
		MaxContainers:         maxContainers,
		EnableSubdomainAccess: p.EnableSubdomainAccess,
		Vault:                 map[string]string{PoolSSHIsolationKey: PoolSSHIsolation(p.ZcpHostname)},
	}
	if s := p.Vertical; s != nil {
		v := poolStanzaVertical{
			CPUMode: s.CPUMode,
			MinCPU:  s.MinCPU, MaxCPU: s.MaxCPU,
			MinRAM: s.MinRAM, MaxRAM: s.MaxRAM,
			MinDisk: s.MinDisk, MaxDisk: s.MaxDisk,
		}
		if v != (poolStanzaVertical{}) {
			svc.VerticalAutoscaling = &v
		}
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(poolStanzaDoc{Services: []poolStanzaService{svc}}); err != nil {
		return "", fmt.Errorf("encode the pool stanza: %w", err)
	}
	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("encode the pool stanza: %w", err)
	}
	return buf.String(), nil
}
