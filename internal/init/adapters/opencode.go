package adapters

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// openCodeMCPServerKey is the MCP server name ZCP registers — the same
// "zerops" every other adapter writes (content.TestMCPServerNameCanonical).
// OpenCode names a server's tools "<server>_<tool>", so zcp's tools surface as
// zerops_zerops_discover, zerops_zerops_deploy, … and the "zerops_*" rule below
// matches every one of them.
const (
	openCodeMCPServerKey  = "zerops"
	openCodePermissionKey = "zerops_*"
)

// OpenCode implements Adapter for sst's OpenCode CLI (`opencode`, npm
// `opencode-ai`). Zerops Mate drives it through `opencode serve`; a user can
// also run the TUI in the container's terminal.
//
// Configuration target: ~/.config/opencode/opencode.json — OpenCode's global
// config. Live-verified against opencode 1.18.34 in an isolated HOME
// (2026-10-03):
//
//   - The global dir's config.json, opencode.json and opencode.jsonc are all
//     loaded and merged, so a user who keeps their settings in opencode.jsonc
//     keeps them; ZCP only ever writes opencode.json.
//   - A Mate starts `opencode serve` with OPENCODE_CONFIG_CONTENT="{}" unless
//     one is set; that layer merges OVER the global file, it does not replace
//     it — `opencode debug config` still shows the zerops server.
//   - A local server inherits OpenCode's own process env (a stub `zcp serve`
//     saw ZCP_API_KEY and serviceId), so the entry carries no `environment`
//     block — no secret on disk, no stale ids (as grok; unlike Cursor).
//
// The file is edited ORDER-PRESERVING. OpenCode evaluates `permission` rules
// in key order with the last match winning (measured: {"zerops_*":"allow",
// "*":"deny"} keeps that order in `opencode debug agent build`), so a rewrite
// through a Go map — which sorts keys — would silently change what a user's
// permission object, or an agent's nested one, means. Every top-level value
// ZCP does not own is carried through verbatim.
type OpenCode struct{}

// NewOpenCode returns a zero-value OpenCode adapter. Stateless; env knobs flow
// via Env.
func NewOpenCode() OpenCode { return OpenCode{} }

// Name returns "opencode" — the binary name and the Mate driver's kind.
func (OpenCode) Name() string { return "opencode" }

// Detect reports whether an OpenCode binary is installed: on PATH, or at the
// official installer's ~/.opencode/bin/opencode, which only a login shell's rc
// adds to PATH — `zcp init` runs as run.init and would otherwise miss it.
func (OpenCode) Detect(env Env) bool {
	_, ok := openCodeBinary(env)
	return ok
}

// Validate runs `<binary> --version` on the binary Detect found. No
// version-gated config today (the local-server shape predates the Mate's
// floor); probe trouble is a warning only.
func (OpenCode) Validate(env Env) ([]string, error) {
	bin, ok := openCodeBinary(env)
	if !ok {
		bin = "opencode"
	}
	cmd := env.CommandOutput
	if cmd == nil {
		cmd = DefaultCommandOutput
	}
	out, err := cmd(bin, "--version")
	if err != nil {
		return []string{
			fmt.Sprintf("opencode --version probe failed: %v (init will continue with defaults)", err),
		}, nil
	}
	if strings.TrimSpace(string(out)) == "" {
		return []string{"opencode --version returned empty output (init will continue)"}, nil
	}
	return nil, nil
}

// ContainerInit upserts mcp.zerops into ~/.config/opencode/opencode.json and
// pre-approves zcp's tools with permission["zerops_*"] = "allow" — the
// counterpart of Claude's `mcp__zerops__*` allow and Cursor's `Mcp(zerops:*)`.
//
// The permission rule is appended after the user's own rules (or overwritten
// where a "zerops_*" key already stands); a blanket string permission such as
// "ask" is the user's whole policy and is left untouched. Inside a Mate the
// session's own ruleset is evaluated after the config's, so the Mate's runtime
// mode still decides there (measured: a supervised session's "*": "ask" asks
// for zerops_zerops_discover despite this rule).
//
// A file ZCP cannot merge — JSONC comments, a non-object root, a non-object
// `mcp` — is refused and left byte-for-byte, never overwritten.
func (OpenCode) ContainerInit(env Env) error {
	if env.Home == "" {
		return fmt.Errorf("opencode adapter: Env.Home is empty")
	}
	configPath := filepath.Join(env.Home, ".config", "opencode", "opencode.json")

	raw, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", configPath, err)
	}
	var doc []jsonMember
	if len(bytes.TrimSpace(raw)) > 0 {
		if doc, err = decodeJSONObject(raw); err != nil {
			return fmt.Errorf("parse %s: %w", configPath, err)
		}
	}

	var servers []jsonMember
	if current, ok := lookupMember(doc, "mcp"); ok {
		if servers, err = decodeJSONObject(current); err != nil {
			return fmt.Errorf("parse %s: mcp: %w", configPath, err)
		}
	}
	entry, err := json.Marshal(openCodeMCPServerEntry())
	if err != nil {
		return fmt.Errorf("marshal opencode mcp entry: %w", err)
	}
	if doc, err = upsertObjectMember(doc, "mcp", upsertMember(servers, openCodeMCPServerKey, entry)); err != nil {
		return fmt.Errorf("encode %s: %w", configPath, err)
	}

	// A permission that is not an object — a blanket action such as "ask" — is
	// the user's whole policy and stays as it is.
	current, present := lookupMember(doc, "permission")
	if !present || bytes.HasPrefix(bytes.TrimSpace(current), []byte("{")) {
		var rules []jsonMember
		if present {
			if rules, err = decodeJSONObject(current); err != nil {
				return fmt.Errorf("parse %s: permission: %w", configPath, err)
			}
		}
		rules = upsertMember(rules, openCodePermissionKey, json.RawMessage(`"allow"`))
		if doc, err = upsertObjectMember(doc, "permission", rules); err != nil {
			return fmt.Errorf("encode %s: %w", configPath, err)
		}
	}

	compact, err := encodeJSONObject(doc)
	if err != nil {
		return fmt.Errorf("encode %s: %w", configPath, err)
	}
	var out bytes.Buffer
	if err := json.Indent(&out, compact, "", "  "); err != nil {
		return fmt.Errorf("format %s: %w", configPath, err)
	}
	out.WriteByte('\n')
	if err := atomicWrite(configPath, out.Bytes()); err != nil {
		return fmt.Errorf("write %s: %w", configPath, err)
	}
	return nil
}

// openCodeServerEntry is OpenCode's local (stdio) MCP server shape: the
// command is ONE argv array (no separate args), and no `environment` (see the
// OpenCode type doc). `opencode mcp list` reports this entry "connected". A
// struct, not a map, so the field order is fixed and the file reads the way
// OpenCode's own docs show it.
type openCodeServerEntry struct {
	Type    string   `json:"type"`
	Command []string `json:"command"`
	Enabled bool     `json:"enabled"`
}

func openCodeMCPServerEntry() openCodeServerEntry {
	return openCodeServerEntry{Type: "local", Command: []string{"zcp", "serve"}, Enabled: true}
}

// openCodeBinary resolves the OpenCode binary: PATH first, then the official
// installer's ~/.opencode/bin/opencode when it is an executable file.
func openCodeBinary(env Env) (string, bool) {
	lookPath := env.LookPath
	if lookPath == nil {
		lookPath = DefaultLookPath
	}
	if path, err := lookPath("opencode"); err == nil {
		return path, true
	}
	if env.Home == "" {
		return "", false
	}
	installed := filepath.Join(env.Home, ".opencode", "bin", "opencode")
	info, err := os.Stat(installed)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", false
	}
	return installed, true
}

// jsonMember is one key of a JSON object with its value kept verbatim, so an
// object can be edited without reordering or re-encoding what it does not own.
type jsonMember struct {
	key   string
	value json.RawMessage
}

// decodeJSONObject splits a JSON object into its members in document order.
// Anything but exactly one object — comments, an array, trailing data — is an
// error.
func decodeJSONObject(raw []byte) ([]jsonMember, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, errors.New("not a JSON object")
	}
	var members []jsonMember
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("decode key: %w", err)
		}
		key, _ := tok.(string)
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, fmt.Errorf("decode %q: %w", key, err)
		}
		members = append(members, jsonMember{key: key, value: value})
	}
	if _, err := dec.Token(); err != nil {
		return nil, fmt.Errorf("decode end: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing data after the JSON object")
	}
	return members, nil
}

// encodeJSONObject joins members back into a compact JSON object.
func encodeJSONObject(members []jsonMember) (json.RawMessage, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, m := range members {
		if i > 0 {
			b.WriteByte(',')
		}
		key, err := json.Marshal(m.key)
		if err != nil {
			return nil, fmt.Errorf("encode key %q: %w", m.key, err)
		}
		b.Write(key)
		b.WriteByte(':')
		b.Write(m.value)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// upsertObjectMember sets key to the object made of members.
func upsertObjectMember(doc []jsonMember, key string, members []jsonMember) ([]jsonMember, error) {
	value, err := encodeJSONObject(members)
	if err != nil {
		return nil, err
	}
	return upsertMember(doc, key, value), nil
}

func lookupMember(members []jsonMember, key string) (json.RawMessage, bool) {
	for _, m := range members {
		if m.key == key {
			return m.value, true
		}
	}
	return nil, false
}

// upsertMember replaces key's value where it stands, or appends it.
func upsertMember(members []jsonMember, key string, value json.RawMessage) []jsonMember {
	for i := range members {
		if members[i].key == key {
			members[i].value = value
			return members
		}
	}
	return append(members, jsonMember{key: key, value: value})
}
