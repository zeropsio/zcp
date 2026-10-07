package ops

import (
	"context"
	"fmt"
	"maps"
	"sort"

	"github.com/zeropsio/zcp/internal/platform"
)

// The strict env model: a service's container reads exactly the entries of
// its zerops.yaml run.envVariables. A vault value — the project's Shared
// vault (project env) or a service's own vault (service env) — reaches a
// process only through an entry that references it. Zerops still injects
// unreferenced values today (non-strict); zcp acts as if it did not, so the
// reads below are what a service depends on.
//
// Resolution of `${NAME}` inside an entry of service S, measured live
// 2026-10-07 (docs/spec-zerops-env-lifecycle.md §3):
//
//  1. NAME is another entry of S → that entry's value (chains resolve).
//  2. NAME is the entry itself (`KEY: ${KEY}`) → the literal `${KEY}`; an
//     entry referencing a self-shadowed entry gets the literal too.
//  3. NAME is a key of S's own rows (its vault + platform intrinsics) →
//     S's own value.
//  4. NAME is a Shared key → the Shared value.
//  5. NAME is `<host>_<KEY>` of a service in the project → that service's
//     value.
//  6. Nothing → the literal `${NAME}`.

// EnvRefKind classifies one `${NAME}` reference of a run entry.
type EnvRefKind int

const (
	EnvRefEntry      EnvRefKind = iota + 1 // another entry of the same service
	EnvRefSelf                             // the entry itself — `KEY: ${KEY}`, literal
	EnvRefOwn                              // the service's own row (vault or platform)
	EnvRefShared                           // the project's Shared vault
	EnvRefHost                             // another service's value, `${host_KEY}`
	EnvRefUnresolved                       // nothing resolves — literal
)

// VaultKey names one stored value: Service "" is the project's Shared
// vault, otherwise the hostname of the service that owns it.
type VaultKey struct {
	Service string
	Key     string
}

// EnvRef is one classified `${NAME}` reference inside a run entry.
type EnvRef struct {
	Entry string     // the entry whose value holds the reference
	Name  string     // NAME inside `${NAME}`
	Kind  EnvRefKind // how it resolves
	// Target is the stored value read, for EnvRefOwn / EnvRefShared /
	// EnvRefHost; zero otherwise.
	Target VaultKey
}

// ServiceEnvScope is what resolves a reference inside one service: its run
// entries (deployed, or about to be deployed) and the keys of its own rows.
type ServiceEnvScope struct {
	Hostname string
	Entries  map[string]string // run.envVariables, templates unresolved
	Own      map[string]bool   // keys of the service's own env rows
}

// ProjectEnvScope is what every service of a project resolves against
// beyond its own scope: the Shared keys, and each service's keys for
// `${host_KEY}`.
type ProjectEnvScope struct {
	Shared map[string]bool
	Hosts  *EnvRefClassifier
	// HostKeys holds each service's own keys; a host reference to a key
	// absent here is unresolved. A nil map for a host means its keys are
	// unknown — the reference is taken as resolving.
	HostKeys map[string]map[string]bool
}

// platformRuntimeKeys are keys the platform provides to a service that a
// row read may not show (they appear on deploy, or with a subdomain).
// Referencing one is never unresolved.
var platformRuntimeKeys = map[string]bool{
	"hostname":            true,
	"serviceId":           true,
	"projectId":           true,
	"appVersionId":        true,
	"appVersionName":      true,
	"zeropsSubdomain":     true,
	"zeropsSubdomainHost": true,
	"PATH":                true,
}

// Refs classifies every `${NAME}` reference of the service's entries,
// ordered by entry then position.
func (s ServiceEnvScope) Refs(project ProjectEnvScope) []EnvRef {
	entries := make([]string, 0, len(s.Entries))
	for k := range s.Entries {
		entries = append(entries, k)
	}
	sort.Strings(entries)
	out := make([]EnvRef, 0, len(entries))
	for _, entry := range entries {
		for _, m := range FindEnvRefs(s.Entries[entry]) {
			out = append(out, s.classify(entry, m.Body, project))
		}
	}
	return out
}

func (s ServiceEnvScope) classify(entry, name string, project ProjectEnvScope) EnvRef {
	ref := EnvRef{Entry: entry, Name: name}
	switch {
	case name == entry:
		ref.Kind = EnvRefSelf
	case hasEntry(s.Entries, name):
		ref.Kind = EnvRefEntry
	case s.Own[name] || platformRuntimeKeys[name]:
		ref.Kind = EnvRefOwn
		ref.Target = VaultKey{Service: s.Hostname, Key: name}
	case project.Shared[name]:
		ref.Kind = EnvRefShared
		ref.Target = VaultKey{Key: name}
	default:
		host, key, ok := project.Hosts.Classify(name)
		if !ok {
			ref.Kind = EnvRefUnresolved
			return ref
		}
		if keys, known := project.HostKeys[host]; known && keys != nil && !keys[key] && !platformRuntimeKeys[key] {
			ref.Kind = EnvRefUnresolved
			return ref
		}
		ref.Kind = EnvRefHost
		ref.Target = VaultKey{Service: host, Key: key}
	}
	return ref
}

func hasEntry(entries map[string]string, name string) bool {
	_, ok := entries[name]
	return ok
}

// Reads is the set of stored values the service's process reads through its
// entries — every own, Shared and host value an entry reaches, directly or
// through a chain of entries. A self-shadowed entry (and every entry that
// reaches the value only through it) reads nothing: its process gets the
// literal.
func (s ServiceEnvScope) Reads(project ProjectEnvScope) map[VaultKey]bool {
	byEntry := map[string][]EnvRef{}
	for _, r := range s.Refs(project) {
		byEntry[r.Entry] = append(byEntry[r.Entry], r)
	}
	reads := map[VaultKey]bool{}
	visited := map[string]bool{}
	var walk func(entry string)
	walk = func(entry string) {
		if visited[entry] {
			return
		}
		visited[entry] = true
		for _, r := range byEntry[entry] {
			switch r.Kind {
			case EnvRefEntry:
				walk(r.Name)
			case EnvRefOwn, EnvRefShared, EnvRefHost:
				reads[r.Target] = true
			case EnvRefSelf, EnvRefUnresolved:
			}
		}
	}
	for entry := range s.Entries {
		walk(entry)
	}
	return reads
}

// EnvKeyReaders returns the hostnames of the services whose entries read the
// stored value target, sorted. The target counts as present in its scope
// whether or not it is stored yet (a just-set or just-deleted key), so the
// answer is "who reads this key", not "who reads it today".
func EnvKeyReaders(target VaultKey, services []ServiceEnvScope, project ProjectEnvScope) []string {
	project = withTarget(project, target)
	var readers []string
	for _, svc := range services {
		if target.Service != "" && svc.Hostname == target.Service {
			svc.Own = withKey(svc.Own, target.Key)
		}
		if svc.Reads(project)[target] {
			readers = append(readers, svc.Hostname)
		}
	}
	sort.Strings(readers)
	return readers
}

func withTarget(project ProjectEnvScope, target VaultKey) ProjectEnvScope {
	if target.Service == "" {
		project.Shared = withKey(project.Shared, target.Key)
		return project
	}
	if keys, ok := project.HostKeys[target.Service]; ok && keys != nil {
		hostKeys := make(map[string]map[string]bool, len(project.HostKeys))
		maps.Copy(hostKeys, project.HostKeys)
		hostKeys[target.Service] = withKey(keys, target.Key)
		project.HostKeys = hostKeys
	}
	return project
}

func withKey(set map[string]bool, key string) map[string]bool {
	out := make(map[string]bool, len(set)+1)
	maps.Copy(out, set)
	out[key] = true
	return out
}

// ProjectEnvScopes is a project's env as the strict model reads it: every
// service's deployed run entries and own keys, and the Shared keys.
type ProjectEnvScopes struct {
	Services []ServiceEnvScope
	Project  ProjectEnvScope
	// Unread names the services whose rows could not be read: whether they
	// read a key is unknown.
	Unread []string
}

// Readers returns the services whose deployed entries read target.
func (p *ProjectEnvScopes) Readers(target VaultKey) []string {
	return EnvKeyReaders(target, p.Services, p.Project)
}

// Service returns the named service's scope, or false.
func (p *ProjectEnvScopes) Service(hostname string) (ServiceEnvScope, bool) {
	for _, s := range p.Services {
		if s.Hostname == hostname {
			return s, true
		}
	}
	return ServiceEnvScope{}, false
}

// ReadProjectEnvScopes reads the scopes from the platform: the Shared rows
// (GET project env), each service's own rows (GET service env — its vault,
// the platform intrinsics and the read-only mirror of its entries) and its
// deployed run entries (the active app version's rows; none for a managed
// or never-deployed service). A service whose rows fail to read is listed in
// Unread and its keys count as unknown; a failed Shared read fails the call.
func ReadProjectEnvScopes(ctx context.Context, client platform.Client, projectID string, services []platform.ServiceStack) (*ProjectEnvScopes, error) {
	shared, err := client.GetProjectEnv(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("read shared env: %w", err)
	}
	out := &ProjectEnvScopes{Project: ProjectEnvScope{
		Shared:   map[string]bool{},
		Hosts:    NewEnvRefClassifier(services),
		HostKeys: map[string]map[string]bool{},
	}}
	for _, e := range shared {
		out.Project.Shared[e.Key] = true
	}
	for _, svc := range services {
		scope := ServiceEnvScope{Hostname: svc.Name, Entries: map[string]string{}, Own: map[string]bool{}}
		own, ownErr := FetchServiceEnv(ctx, client, svc.ID)
		entries, entriesErr := AppVersionEnvVars(ctx, client, svc)
		if ownErr != nil || entriesErr != nil {
			out.Unread = append(out.Unread, svc.Name)
			out.Project.HostKeys[svc.Name] = nil
			out.Services = append(out.Services, scope)
			continue
		}
		keys := map[string]bool{}
		for _, e := range own {
			scope.Own[e.Key] = true
			keys[e.Key] = true
		}
		for _, e := range entries {
			scope.Entries[e.Key] = e.Content
			keys[e.Key] = true
		}
		out.Project.HostKeys[svc.Name] = keys
		out.Services = append(out.Services, scope)
	}
	return out, nil
}
