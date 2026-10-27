package bundle

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

// Mate's preview pane draws a service's page in a frame on Mate's own origins
// (docs/spec-mate.md §10.10): the hosted client, and the container's own
// `/mate/` on its zerops.app subdomain. A Mate lets the pane frame its dev and
// stage by widening the app's `frame-ancestors` with them — an app's own
// sources plus Mate's, the container named by its exact origin — and
// production keeps the app's own sources. Setups written before the exact
// origin name the container by the zerops.app wildcard; both read as Mate's.
const (
	mateHostedOrigin         = "https://mate.zerops.io"
	mateContainerWildcard    = "https://*.zerops.app"
	zeropsSubdomainSuffix    = ".zerops.app"
	zeropsDevSubdomainSuffix = ".zerops.dev"
)

// isMateContainerSource reports whether a lowercased frame source names a
// Mate container: the zerops.app wildcard, or an https origin on a Zerops
// subdomain (zerops.app, zerops.dev on devel). It is read only beside Mate's
// hosted origin, where a Zerops subdomain is a Mate's: a Mate's project is
// never the production its setups are carried into.
func isMateContainerSource(source string) bool {
	source = strings.TrimSuffix(source, "/")
	if source == mateContainerWildcard {
		return true
	}
	host, ok := strings.CutPrefix(source, "https://")
	if !ok || host == "" || strings.ContainsAny(host, "/*") {
		return false
	}
	if i := strings.LastIndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	return strings.HasSuffix(host, zeropsSubdomainSuffix) || strings.HasSuffix(host, zeropsDevSubdomainSuffix)
}

// frameAncestorsDirective is a CSP `frame-ancestors` directive and its sources,
// which end at the next directive or at the quote a static header's value is
// wrapped in.
var frameAncestorsDirective = regexp.MustCompile(`(?i)\bframe-ancestors\b([^;"]*)`)

// cspSourceToken is one source expression of a bare source list, lowercased:
// a quoted keyword (`'self'`, `'none'`), a scheme (`https:`) or a host with an
// optional scheme, `*.` wildcard, port and path.
var cspSourceToken = regexp.MustCompile(`^('[a-z-]+'|[a-z][a-z0-9+.-]*:|([a-z][a-z0-9+.-]*://)?(\*|(\*\.)?[a-z0-9-]+(\.[a-z0-9-]+)*)(:([0-9]+|\*))?(/[^\s;,]*)?)$`)

// WithoutMateFraming returns v — a string, or a mapping or list parsed from
// YAML — with Mate's frame sources taken out of every value that lets Mate's
// preview pane frame the app, and whether any did. A value lets it when a
// `frame-ancestors` directive in it names Mate's hosted origin, or when the
// whole value is a bare source list naming Mate's origin set — the hosted
// origin beside a Mate container's origin (isMateContainerSource), since a
// setting that only names Mate's address (an app that talks to Mate) frames
// nothing. Its own sources stay and Mate's go — `'none'` when nothing stays.
// v is never changed in place.
func WithoutMateFraming(v any) (any, bool) {
	switch v := v.(type) {
	case string:
		return withoutMateFrameSources(v)
	case map[string]any:
		out := make(map[string]any, len(v))
		framed := false
		for key, item := range v {
			got, ok := WithoutMateFraming(item)
			out[key] = got
			framed = framed || ok
		}
		return out, framed
	case []any:
		out := make([]any, len(v))
		framed := false
		for i, item := range v {
			got, ok := WithoutMateFraming(item)
			out[i] = got
			framed = framed || ok
		}
		return out, framed
	default:
		return v, false
	}
}

// withoutMateFrameSources is WithoutMateFraming for one value: every
// `frame-ancestors` directive naming Mate's hosted origin, else the whole
// value when it is a bare source list naming Mate's origin set.
func withoutMateFrameSources(value string) (string, bool) {
	if !strings.Contains(strings.ToLower(value), mateHostedOrigin) {
		return value, false
	}
	if matches := frameAncestorsDirective.FindAllStringSubmatchIndex(value, -1); len(matches) > 0 {
		var b strings.Builder
		last, framed := 0, false
		for _, m := range matches {
			sources := value[m[2]:m[3]]
			kept, named := withoutMateSources(strings.Fields(sources))
			if !named {
				continue
			}
			framed = true
			b.WriteString(value[last:m[2]])
			b.WriteString(" " + strings.Join(kept, " "))
			b.WriteString(sources[len(strings.TrimRightFunc(sources, unicode.IsSpace)):])
			last = m[3]
		}
		if !framed {
			return value, false
		}
		b.WriteString(value[last:])
		return b.String(), true
	}
	fields := strings.Fields(value)
	containerOrigins := false
	for _, field := range fields {
		source := strings.ToLower(field)
		if !cspSourceToken.MatchString(source) {
			return value, false
		}
		containerOrigins = containerOrigins || isMateContainerSource(source)
	}
	if !containerOrigins {
		return value, false
	}
	kept, named := withoutMateSources(fields)
	if !named {
		return value, false
	}
	return strings.Join(kept, " "), true
}

// withoutMateSources takes Mate's origins out of a source list that names its
// hosted origin — that origin and every Mate container's beside it; a list
// that does not is the app's own and stays whole, even with a zerops.app
// source in it.
func withoutMateSources(sources []string) ([]string, bool) {
	named := false
	kept := make([]string, 0, len(sources))
	for _, source := range sources {
		lower := strings.ToLower(source)
		switch {
		case strings.TrimSuffix(lower, "/") == mateHostedOrigin:
			named = true
			continue
		case isMateContainerSource(lower):
			continue
		}
		kept = append(kept, source)
	}
	if !named {
		return sources, false
	}
	if len(kept) == 0 {
		kept = []string{"'none'"}
	}
	return kept, true
}

// groupEnvironmentSetup names the setup a group environment builds for a part
// whose stage builds setup. A stage builds the setup production builds, so a
// Mate gives its stage Mate's preview variant of it: a setup that `extends`
// one other setup of the zerops.yaml and carries what only that Mate's project
// has — its own `run` lets Mate's preview pane frame the app, or it names an
// address on a Zerops subdomain (the Mate's stage as the app's address, a
// cookie domain), which no group environment has. A page that forbids framing
// gets a variant that frames nothing and only points its addresses (Zerops
// Apps' hq and box). A group environment builds the setup the variant extends
// — one level, to a setup the zerops.yaml declares and that does not let the
// pane frame the app — so the application's stage and production keep the
// app's own framing and addresses; any other setup it builds as it is, one
// that extends another and carries neither (production over a shared base)
// too. warning is non-empty for a framed setup a group environment builds
// framed.
func groupEnvironmentSetup(r GroupRuntime, setup string) (environmentSetup, warning string) {
	entries := zeropsYAMLSetupEntries(r.ZeropsYAMLBody)
	entry, ok := entries[setup]
	if !ok {
		return setup, ""
	}
	_, framed := WithoutMateFraming(entry["run"])
	if !framed && !namesZeropsSubdomainAddress(entry) {
		return setup, ""
	}
	if bases := yamlExtendsNames(entry["extends"]); len(bases) == 1 && bases[0] != setup {
		if base, declared := entries[bases[0]]; declared {
			if _, baseFramed := WithoutMateFraming(base["run"]); !baseFramed {
				return bases[0], ""
			}
		}
	}
	if !framed {
		return setup, ""
	}
	return setup, fmt.Sprintf(
		"runtime %q: its stage's setup %q lets Mate's preview pane frame the app, and the application's stage and production build it framed — give the stage a setup that extends the production setup and changes only the framing, and deploy the stage with it",
		r.DevHostname, setup)
}

// zeropsSubdomainAddress is a host on a Zerops subdomain (zeropsSubdomainSuffix,
// zeropsDevSubdomainSuffix) anywhere in a value: a service's own public
// address, with or without its scheme, written out or with `${name}`
// references among its labels — the project's own subdomain variables, as in
// `webstage-${zeropsSubdomainHost}-3000.${zeropsLocation}.zerops.app`. A value
// that is only a reference (`${zeropsSubdomain}`) holds no such host.
var zeropsSubdomainAddress = regexp.MustCompile(`(?i)(?:\*|(?:[a-z0-9-]|\$\{[a-z0-9_]+\})+)(?:\.(?:[a-z0-9-]|\$\{[a-z0-9_]+\})+)*\.zerops\.(?:app|dev)\b`)

// namesZeropsSubdomainAddress reports whether v — a string, or a mapping or
// list parsed from YAML — names an address on a Zerops subdomain: an address
// of the project the setup was written in, which a reference such as
// `${zeropsSubdomain}` is not.
func namesZeropsSubdomainAddress(v any) bool {
	switch v := v.(type) {
	case string:
		return zeropsSubdomainAddress.MatchString(v)
	case map[string]any:
		for _, item := range v {
			if namesZeropsSubdomainAddress(item) {
				return true
			}
		}
	case []any:
		return slices.ContainsFunc(v, namesZeropsSubdomainAddress)
	}
	return false
}

// GroupEnvironmentSetup is the setup a group environment builds for a part of
// r whose stage builds setup — the setup Mate's preview variant extends, else
// setup itself (groupEnvironmentSetup, without its warning). A reader matching
// a tier's services to the Mate's parts names a part by it, so the AI Agent
// tier's stage building the variant and a group environment's runtime
// building the setup it extends read as one part.
func GroupEnvironmentSetup(r GroupRuntime, setup string) string {
	environmentSetup, _ := groupEnvironmentSetup(r, setup)
	return environmentSetup
}

// zeropsYAMLSetupEntries indexes a zerops.yaml's setups by name, as written —
// extends unresolved. Empty for a body that does not parse.
func zeropsYAMLSetupEntries(body string) map[string]map[string]any {
	var doc struct {
		Zerops []map[string]any `yaml:"zerops"`
	}
	if yaml.Unmarshal([]byte(body), &doc) != nil {
		return nil
	}
	entries := make(map[string]map[string]any, len(doc.Zerops))
	for _, entry := range doc.Zerops {
		if name, ok := entry["setup"].(string); ok && name != "" {
			entries[name] = entry
		}
	}
	return entries
}

// yamlExtendsNames names the setups an `extends` value names: a scalar or a
// list.
func yamlExtendsNames(v any) []string {
	switch v := v.(type) {
	case string:
		return []string{v}
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if name, ok := item.(string); ok {
				out = append(out, name)
			}
		}
		return out
	default:
		return nil
	}
}
