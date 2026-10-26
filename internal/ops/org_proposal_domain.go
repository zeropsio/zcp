package ops

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

// domainPattern is a fully qualified domain name as a person puts a project
// on it: lowercase labels of letters, digits and inner hyphens, at least two
// of them, the last a top-level domain, and a leading "*." for every subdomain
// of one (the platform takes wildcards). No scheme, port or path.
var domainPattern = regexp.MustCompile(`^(\*\.)?([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+([a-z]{2,63}|xn--[a-z0-9-]{1,59})$`)

// domainMaxLength is the longest name DNS holds.
const domainMaxLength = 253

// webPorts are the service's ports a domain may point at: the ones the
// platform routes HTTP to, or that declare an HTTP scheme (set at deploy,
// before any routing exists). Mate's addresses sheet offers the same ports.
func webPorts(s *platform.ServiceStack) []int {
	var ports []int
	for _, p := range s.Ports {
		if (p.HTTPSupport || isHTTPScheme(p)) && !slices.Contains(ports, p.Port) {
			ports = append(ports, p.Port)
		}
	}
	return ports
}

// domainPortFits refuses a port no domain can point at on s, and any port of
// a Mate's own container: it serves the Mate, never the person's site.
func domainPortFits(s *platform.ServiceStack, project string, port int) error {
	if strings.HasPrefix(s.ServiceStackTypeInfo.ServiceStackTypeVersionName, zcpTypePrefix) {
		return invalid(fmt.Sprintf("%s in %s is a Mate's own container, which no domain points at", s.Name, project),
			"Point the domain at the service that serves the web (zerops_org_services lists their ports)")
	}
	ports := webPorts(s)
	if len(ports) == 0 {
		return invalid(fmt.Sprintf("%s in %s has no web port a domain can point at", s.Name, project),
			"Point the domain at the service that serves the web (zerops_org_services lists their ports)")
	}
	named := make([]string, len(ports))
	for i, p := range ports {
		named[i] = strconv.Itoa(p)
	}
	serves := strings.Join(named, ", ")
	if port == 0 {
		return invalid(fmt.Sprintf("domain.add needs the port the domain points at: %s serves the web on %s", s.Name, serves), "")
	}
	if !slices.Contains(ports, port) {
		return invalid(fmt.Sprintf("%s in %s serves the web on %s, not on port %d", s.Name, project, serves, port), "")
	}
	return nil
}

// proposedDomains are the domains as the platform takes them: trimmed,
// lowercase, without a trailing dot, each once and each a domain name.
func proposedDomains(raw []string) ([]string, error) {
	var domains []string
	for _, r := range raw {
		d := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(r)), ".")
		if d == "" {
			continue
		}
		if len(d) > domainMaxLength || !domainPattern.MatchString(d) {
			return nil, invalid(fmt.Sprintf("%q is not a domain name", strings.TrimSpace(r)),
				"Pass the bare name, such as shop.example.com: no https://, port or path")
		}
		if slices.Contains(domains, d) {
			return nil, invalid(fmt.Sprintf("%s is named twice", d), "Name each domain once")
		}
		domains = append(domains, d)
	}
	if len(domains) == 0 {
		return nil, invalid("domain.add needs a domain", "Pass the person's domain, and its www. name if they want both")
	}
	return domains, nil
}

// domainList names domains in a sentence: "a", "a and b", "a, b and c".
func domainList(domains []string) string {
	if len(domains) == 1 {
		return domains[0]
	}
	return strings.Join(domains[:len(domains)-1], ", ") + " and " + domains[len(domains)-1]
}
