package ops

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// A Mate proposes connecting an MCP server for its agents
// (docs/spec-mate.md §2.11, mcp.add). The proposal is the server's
// definition — name, transport, how it signs in — apart from the fields the
// person types on the card (a key, a token): it never carries a value, so
// a secret never passes through the agent. Nothing runs here.

// ProposalMcpAdd is the kind of a proposal to connect an MCP server.
const ProposalMcpAdd = "mcp.add"

// McpProposeNote follows every mcp.add proposal.
const McpProposeNote = "Nothing is set up yet. The person adds it on the card and types any key there: never ask for a key in chat. Their next message tells you which of this Mate's agents have it, and whether it waits for them to sign in."

// McpScopeNote follows a proposal for more than this Mate: only the Mate is
// set up for now.
const McpScopeNote = " Only this Mate is set up for now; the card tells the person so."

// McpFieldInput is one value the person types on the card: its name and
// label, and where it goes. Never its value.
type McpFieldInput struct {
	Key        string `json:"key"                  jsonschema:"The field's name: an environment variable's name, or a name for a header's value."`
	Label      string `json:"label"                jsonschema:"What the person types, in a few words: \"Linear API key\"."`
	Placement  string `json:"placement"            jsonschema:"env (a command's environment variable named key) or header (a remote server's request header)."`
	HeaderName string `json:"headerName,omitempty" jsonschema:"The header for placement header; key when absent."`
	Prefix     string `json:"prefix,omitempty"     jsonschema:"Words put before the typed value, such as \"Bearer \"."`
}

// McpProposalInput is zerops_propose_mcp's input.
type McpProposalInput struct {
	Name      string          `json:"name"              jsonschema:"The server's key in every agent's config: letters, digits, - and _."`
	Title     string          `json:"title,omitempty"   jsonschema:"Its name for the person: \"Linear\"."`
	Why       string          `json:"why"               jsonschema:"What it is and why you want it, in one sentence, for the person deciding."`
	Transport string          `json:"transport"         jsonschema:"http, sse or stdio."`
	URL       string          `json:"url,omitempty"     jsonschema:"http and sse: the server's address, with no key in it."`
	Command   string          `json:"command,omitempty" jsonschema:"stdio: the command, such as npx."`
	Args      []string        `json:"args,omitempty"    jsonschema:"stdio: its arguments, with no key among them."`
	Auth      string          `json:"auth"              jsonschema:"How it signs in: none, header (a key the person types, sent in a header), oauth (the person signs in on the provider's page) or oauth-app (OAuth with an app the person registered)."`
	Fields    []McpFieldInput `json:"fields,omitempty"  jsonschema:"The values the person types on the card, by name and label only."`
	Scopes    []string        `json:"scopes,omitempty"  jsonschema:"oauth and oauth-app: the scopes to ask for."`
	DocsURL   string          `json:"docsUrl,omitempty" jsonschema:"Where the server's own setup is documented."`
	Scope     string          `json:"scope,omitempty"   jsonschema:"Who it is for: mate (this Mate, the default), project or org."`
}

// McpTransport is how an agent reaches the server.
type McpTransport struct {
	Type    string   `json:"type"`
	URL     string   `json:"url,omitempty"`
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
}

// McpAuth is how the server signs in.
type McpAuth struct {
	Kind   string   `json:"kind"`
	Scopes []string `json:"scopes,omitempty"`
}

// McpServerDefinition is what the server is, whoever signs in to it.
type McpServerDefinition struct {
	Name      string       `json:"name"`
	Title     string       `json:"title"`
	Transport McpTransport `json:"transport"`
	Auth      McpAuth      `json:"auth"`
	DocsURL   string       `json:"docsUrl,omitempty"`
}

// McpCredentialField is one value the person types on the card.
type McpCredentialField struct {
	Key        string `json:"key"`
	Label      string `json:"label"`
	Placement  string `json:"placement"`
	HeaderName string `json:"headerName,omitempty"`
	Prefix     string `json:"prefix,omitempty"`
}

// McpAddParams are mcp.add's params: the definition and the credential
// fields apart, since they may later live at different scopes (a server
// the organization defines, each Mate signing in itself).
type McpAddParams struct {
	Scope       string               `json:"scope"`
	Server      McpServerDefinition  `json:"server"`
	Credentials []McpCredentialField `json:"credentials"`
}

var (
	// Every agent's config takes such a key (TOML table, JSON key, CLI flag).
	mcpNamePattern   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	mcpFieldPattern  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,63}$`)
	mcpHeaderPattern = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")
	// An environment reference the agent's own config resolves, never a value.
	mcpEnvReference = regexp.MustCompile(`^\$\{?[A-Za-z_][A-Za-z0-9_]*\}?$`)
	mcpUUID         = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	mcpLongRun      = regexp.MustCompile(`[A-Za-z0-9_+=.\-]{32,}`)
	// Flags and query keys that carry a credential.
	mcpSecretName = regexp.MustCompile(`(?i)(token|secret|password|passwd|api[-_]?key|apikey|access[-_]?key|auth|signature|^sig$|^key$|private)`)
)

// Prefixes providers give their keys and tokens.
var mcpTokenPrefixes = []string{
	"ghp_", "gho_", "ghu_", "ghs_", "ghr_", "github_pat_", "glpat-", "sk-", "sk_live_", "sk_test_",
	"rk_live_", "xoxb-", "xoxp-", "xoxa-", "xapp-", "lin_api_", "lin_oauth_", "AKIA", "ASIA", "AIza",
	"ya29.", "sntrys_", "sntryu_", "npm_", "dop_v1_", "shpat_", "eyJ",
}

const (
	mcpScopeMate = "mate"
	mcpStdio     = "stdio"
	mcpHeader    = "header"
	mcpEnv       = "env"
	mcpOAuth     = "oauth"
	mcpOAuthApp  = "oauth-app"
)

var mcpTransports = map[string]bool{"http": true, "sse": true, "stdio": true}

var mcpAuthKinds = map[string]bool{"none": true, "header": true, "oauth": true, "oauth-app": true}

var mcpScopes = map[string]bool{"mate": true, "project": true, "org": true}

// ProposeMcp validates a server for this Mate's agents and returns it as a
// proposal. It runs nothing: the person adds it on the card, typing any key
// there, and their client writes it for every agent.
func ProposeMcp(projectID, projectName string, newID func() string, in McpProposalInput) (*OrgProposalResult, error) {
	why := strings.TrimSpace(in.Why)
	if why == "" {
		return nil, invalid("a proposal needs a reason", "Say in one sentence what it is and why, for the person deciding")
	}
	// The card shows the reason and the conversation keeps it: a key never rides along.
	if err := refuseSecret("the reason", why); err != nil {
		return nil, err
	}
	params, err := mcpParams(in)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("encode proposal params: %w", err)
	}
	note := McpProposeNote
	if params.Scope != mcpScopeMate {
		note += McpScopeNote
	}
	return &OrgProposalResult{
		Proposal: OrgProposal{
			ID:          newID(),
			Kind:        ProposalMcpAdd,
			Summary:     "Set up " + params.Server.Title,
			Reason:      why,
			ProjectID:   projectID,
			ProjectName: projectName,
			Params:      raw,
		},
		Note: note,
	}, nil
}

func mcpParams(in McpProposalInput) (McpAddParams, error) {
	name := strings.TrimSpace(in.Name)
	if !mcpNamePattern.MatchString(name) {
		return McpAddParams{}, invalid(fmt.Sprintf("%q is not a server name every agent takes: letters, digits, - and _ only, at most 64", in.Name),
			"Name it after the service it connects: linear, github")
	}
	if strings.EqualFold(name, "zerops") {
		return McpAddParams{}, invalid("zerops is Zerops' own server", "Name it after the service it connects")
	}
	scope := strings.TrimSpace(in.Scope)
	if scope == "" {
		scope = mcpScopeMate
	}
	if !mcpScopes[scope] {
		return McpAddParams{}, invalid(fmt.Sprintf("%q is not a scope", in.Scope), "scope is mate, project or org")
	}
	transport, err := mcpTransport(in)
	if err != nil {
		return McpAddParams{}, err
	}
	remote := transport.Type != mcpStdio
	auth := strings.TrimSpace(in.Auth)
	if !mcpAuthKinds[auth] {
		return McpAddParams{}, invalid(fmt.Sprintf("%q is not an auth kind", in.Auth), "auth is none, header, oauth or oauth-app")
	}
	if (auth == mcpOAuth || auth == mcpOAuthApp || auth == mcpHeader) && !remote {
		return McpAddParams{}, invalid(fmt.Sprintf("only a remote server (http or sse) signs in with %s", auth),
			"Only a remote server (http or sse) signs in so; a command's key goes in its environment (auth none, a field with placement env)")
	}
	scopes, err := mcpOAuthScopes(in.Scopes, auth)
	if err != nil {
		return McpAddParams{}, err
	}
	fields, err := mcpFields(in.Fields, remote)
	if err != nil {
		return McpAddParams{}, err
	}
	if auth == mcpHeader && !hasHeaderField(fields) {
		return McpAddParams{}, invalid("a header sign-in needs a header field", "Add a field with placement header for the key the person types")
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = name
	}
	if err := refuseSecret("the title", title); err != nil {
		return McpAddParams{}, err
	}
	docs := strings.TrimSpace(in.DocsURL)
	if docs != "" {
		if err := mcpURL("docsUrl", docs); err != nil {
			return McpAddParams{}, err
		}
	}
	return McpAddParams{
		Scope: scope,
		Server: McpServerDefinition{
			Name: name, Title: title, Transport: transport,
			Auth: McpAuth{Kind: auth, Scopes: scopes}, DocsURL: docs,
		},
		Credentials: fields,
	}, nil
}

func mcpTransport(in McpProposalInput) (McpTransport, error) {
	kind := strings.TrimSpace(in.Transport)
	if !mcpTransports[kind] {
		return McpTransport{}, invalid(fmt.Sprintf("%q is not a transport", in.Transport), "transport is http, sse or stdio")
	}
	address := strings.TrimSpace(in.URL)
	command := strings.TrimSpace(in.Command)
	if kind != mcpStdio {
		if command != "" || len(in.Args) > 0 {
			return McpTransport{}, invalid("a remote server takes no command", "Give its url only, or use transport stdio")
		}
		if address == "" {
			return McpTransport{}, invalid(fmt.Sprintf("an %s server needs its url", kind), "Give the server's address")
		}
		if err := mcpURL("url", address); err != nil {
			return McpTransport{}, err
		}
		return McpTransport{Type: kind, URL: address}, nil
	}
	if address != "" {
		return McpTransport{}, invalid("a command takes no url", "Give command and args only, or use transport http")
	}
	if command == "" {
		return McpTransport{}, invalid("a stdio server needs its command", "Give the command, such as npx, and its args")
	}
	if err := refuseSecret("the command", command); err != nil {
		return McpTransport{}, err
	}
	args := make([]string, 0, len(in.Args))
	for i, arg := range in.Args {
		if err := refuseSecret("an argument", arg); err != nil {
			return McpTransport{}, err
		}
		if flag, value, ok := strings.Cut(arg, "="); ok && strings.HasPrefix(flag, "-") {
			if err := refuseSecretFlag(flag, value); err != nil {
				return McpTransport{}, err
			}
		} else if strings.HasPrefix(arg, "-") && i+1 < len(in.Args) && !strings.HasPrefix(in.Args[i+1], "-") {
			if err := refuseSecretFlag(arg, in.Args[i+1]); err != nil {
				return McpTransport{}, err
			}
		}
		args = append(args, arg)
	}
	return McpTransport{Type: mcpStdio, Command: command, Args: args}, nil
}

// mcpURL refuses an address that is not http(s) or that carries a
// credential: a password, a key-named query value, a token in its path.
func mcpURL(what, address string) error {
	parsed, err := url.Parse(address)
	if err != nil || (parsed.Scheme != httpsScheme && parsed.Scheme != httpScheme) || parsed.Host == "" {
		return invalid(fmt.Sprintf("%s %q is not an http or https address", what, address), "Give the full address, https://…")
	}
	if parsed.User != nil {
		return secretRefusal(what)
	}
	for key, values := range parsed.Query() {
		for _, value := range values {
			if value != "" && mcpSecretName.MatchString(key) {
				return secretRefusal(what)
			}
		}
	}
	return refuseSecret(what, parsed.Host+parsed.Path+"?"+parsed.RawQuery)
}

func mcpOAuthScopes(scopes []string, auth string) ([]string, error) {
	if len(scopes) == 0 {
		return nil, nil
	}
	if auth != mcpOAuth && auth != mcpOAuthApp {
		return nil, invalid("scopes are an OAuth sign-in's", "Drop scopes, or use auth oauth")
	}
	out := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		if err := refuseSecret("a scope", scope); err != nil {
			return nil, err
		}
		out = append(out, scope)
	}
	return out, nil
}

func mcpFields(fields []McpFieldInput, remote bool) ([]McpCredentialField, error) {
	out := make([]McpCredentialField, 0, len(fields))
	seen := map[string]bool{}
	for _, field := range fields {
		key := strings.TrimSpace(field.Key)
		if !mcpFieldPattern.MatchString(key) {
			return nil, invalid(fmt.Sprintf("%q is not a field name", field.Key), "Name each field like an environment variable: LINEAR_API_KEY")
		}
		if seen[key] {
			return nil, invalid(fmt.Sprintf("%s is a field twice", key), "Name each field once")
		}
		seen[key] = true
		label := strings.TrimSpace(field.Label)
		if label == "" {
			return nil, invalid(fmt.Sprintf("%s needs a label", key), "Say in a few words what the person types: \"Linear API key\"")
		}
		placement := strings.TrimSpace(field.Placement)
		switch {
		case placement != mcpEnv && placement != mcpHeader:
			return nil, invalid(fmt.Sprintf("%q is not a placement", field.Placement), "placement is env or header")
		case placement == mcpEnv && remote:
			return nil, invalid(fmt.Sprintf("%s: an environment variable is a command's", key), "A remote server's key goes in a header (placement header)")
		case placement == mcpHeader && !remote:
			return nil, invalid(fmt.Sprintf("%s: a header is a remote server's", key), "A command's key goes in its environment (placement env)")
		}
		header := strings.TrimSpace(field.HeaderName)
		if placement == mcpEnv && (header != "" || field.Prefix != "") {
			return nil, invalid(fmt.Sprintf("%s: an environment variable takes no header or prefix", key), "Drop headerName and prefix")
		}
		if header != "" && !mcpHeaderPattern.MatchString(header) {
			return nil, invalid(fmt.Sprintf("%q is not a header name", field.HeaderName), "Name a request header, such as Authorization")
		}
		for what, words := range map[string]string{"a label": label, "a prefix": field.Prefix, "a header name": header} {
			if err := refuseSecret(what, words); err != nil {
				return nil, err
			}
		}
		if len(field.Prefix) > 16 {
			return nil, secretRefusal("a prefix")
		}
		out = append(out, McpCredentialField{Key: key, Label: label, Placement: placement, HeaderName: header, Prefix: field.Prefix})
	}
	return out, nil
}

func hasHeaderField(fields []McpCredentialField) bool {
	for _, field := range fields {
		if field.Placement == mcpHeader {
			return true
		}
	}
	return false
}

// refuseSecret refuses words that hold what looks like a key or token: a
// provider's token prefix, or a long run of letters and digits that is not
// an id.
func refuseSecret(what, words string) error {
	for _, part := range strings.FieldsFunc(words, func(r rune) bool {
		return strings.ContainsRune(" /?&=:@,;\"'", r)
	}) {
		for _, prefix := range mcpTokenPrefixes {
			if strings.HasPrefix(part, prefix) && len(part) >= len(prefix)+16 {
				return secretRefusal(what)
			}
		}
		if mcpUUID.MatchString(part) {
			continue
		}
		for _, run := range mcpLongRun.FindAllString(part, -1) {
			if strings.ContainsAny(run, "0123456789") && strings.IndexFunc(run, isLetter) >= 0 {
				return secretRefusal(what)
			}
		}
	}
	return nil
}

func refuseSecretFlag(flag, value string) error {
	if value == "" || mcpEnvReference.MatchString(value) || !mcpSecretName.MatchString(strings.TrimLeft(flag, "-")) {
		return nil
	}
	return secretRefusal("an argument")
}

func isLetter(r rune) bool { return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') }

func secretRefusal(what string) error {
	return invalid(fmt.Sprintf("%s looks like it holds a secret value: a proposal never carries a key", what),
		"Leave the key out and declare a field for it (key, label, placement): the person types it on the card, never in chat")
}
