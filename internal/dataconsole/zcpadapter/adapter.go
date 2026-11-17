// Package zcpadapter implements console.Host from ZCP's existing primitives —
// the ONLY package in the Data Console module permitted to import zcp core
// (pinned by depguard `dataconsole-adapter-allowlist`). On extraction to its own
// repo, this package is what gets rewritten (into a standalone env/discovery
// layer); the console/ engine moves byte-for-byte.
//
// ZCP API credentials are NOT resolved here: the composition root (cmd/zcp/
// studio_console.go) runs the token-blind studioInit() — which lives in package
// main and is unreachable from internal/ — and injects the ready (client, info)
// so there is one owner of the .mcp.json bridge.
package zcpadapter

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strings"
	"sync"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/dataconsole/console"
	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// Adapter bridges the console engine to a live Zerops project.
type Adapter struct {
	client platform.Client
	info   *auth.Info

	// env reads one key of the container's live env store, fresh per call:
	// where a value the API withholds from this key is read.
	env func(key string) (string, bool, error)

	mu       sync.Mutex
	typeByID map[string]string // serviceID -> type string (filled on discovery)
	hostByID map[string]string // serviceID -> hostname (filled on discovery)
}

// New wires the adapter from an already-resolved client + auth info.
func New(client platform.Client, info *auth.Info) *Adapter {
	return &Adapter{
		client:   client,
		info:     info,
		env:      ops.LiveContainerEnv,
		typeByID: map[string]string{},
		hostByID: map[string]string{},
	}
}

// Project returns the neutral project identity.
func (a *Adapter) Project(_ context.Context) (console.ProjectRef, error) {
	return console.ProjectRef{ID: a.info.ProjectID, Name: a.info.ProjectName}, nil
}

// ManagedServices lists the project's managed services, classified via the
// single core owner topology.IsManagedService — never a Data-Console-local list.
func (a *Adapter) ManagedServices(ctx context.Context) ([]console.ManagedServiceRef, error) {
	services, err := ops.ListProjectServices(ctx, a.client, a.info.ProjectID)
	if err != nil {
		return nil, err
	}
	out := make([]console.ManagedServiceRef, 0, len(services))
	idType := map[string]string{}
	idHost := map[string]string{}
	for i := range services {
		svc := &services[i]
		typ := svc.ServiceStackTypeInfo.ServiceStackTypeVersionName
		if !topology.IsManagedService(typ) {
			continue
		}
		idType[svc.ID] = typ
		idHost[svc.ID] = svc.Name
		out = append(out, console.ManagedServiceRef{
			ID:       svc.ID,
			Hostname: svc.Name,
			Type:     typ,
			Status:   svc.Status,
		})
	}
	a.mu.Lock()
	a.typeByID = idType
	a.hostByID = idHost
	a.mu.Unlock()
	return out, nil
}

// ConnectionInfo resolves a service's own env scalars via FetchServiceEnv and
// maps them to the console's neutral typed connection descriptor. Env-key names
// and service-type fallback rules live only in this bridge.
//
// The API answers a sensitive value (a password, a secret key) as masked to a
// project-scoped key; the container's live env store holds it as
// <hostname>_<key> when the service's envIsolation admits this container, so
// each withheld key is read there, fresh on every call. A value the
// connection needs that neither holds is ErrNoCredentials; a withheld key the
// connection never reads (Meilisearch's search keys) refuses nothing. The
// error names the service and the keys, never a value.
func (a *Adapter) ConnectionInfo(ctx context.Context, serviceID string) (console.ConnectionInfo, error) {
	envs, err := ops.FetchServiceEnv(ctx, a.client, serviceID)
	if err != nil {
		return console.ConnectionInfo{}, fmt.Errorf("read %s's variables: %w: %w", serviceID, provider.ErrUpstream, err)
	}
	scalars, masked := ops.EnvScalars(envs)
	a.mu.Lock()
	typ := a.typeByID[serviceID]
	host, ok := a.hostByID[serviceID]
	a.mu.Unlock()
	if !ok {
		host = serviceID
	}
	vars := connVars{values: scalars, absent: map[string]bool{}}
	for _, key := range masked {
		v, found, err := a.env(host + "_" + key)
		if err != nil {
			return console.ConnectionInfo{}, fmt.Errorf("read %s's %s from the container's env: %w: %w", host, key, provider.ErrUpstream, err)
		}
		if !found {
			vars.absent[key] = true
			continue
		}
		scalars[key] = v
	}
	desc, err := descriptorFor(typ, &vars)
	if err != nil {
		return console.ConnectionInfo{}, fmt.Errorf("%w: %w", provider.ErrUnsupported, err)
	}
	if len(vars.missing) > 0 {
		return console.ConnectionInfo{}, fmt.Errorf("%s does not share %s with this container: %w", host, strings.Join(vars.missing, ", "), provider.ErrNoCredentials)
	}
	return console.ConnectionInfo{
		Type:       typ,
		Family:     desc.ConnectionFamily(),
		Descriptor: desc,
	}, nil
}

func descriptorFor(serviceType string, s *connVars) (provider.ConnectionDescriptor, error) {
	base := provider.BaseType(serviceType)
	switch provider.Classify(serviceType) {
	case provider.FamilyObject:
		return provider.ObjectConn{
			Endpoint:  s.get("apiHost"),
			AccessKey: s.get("accessKeyId"),
			SecretKey: s.get("secretAccessKey"),
			Bucket:    s.get("bucketName"),
			Secure:    strings.HasPrefix(strings.ToLower(s.get("apiUrl")), "https"),
		}, nil
	case provider.FamilyTabular:
		return sqlDescriptor(base, s)
	case provider.FamilyKV:
		return provider.KVConn{
			Host:     s.get("hostname"),
			Port:     s.get("port"),
			Password: s.get("password"),
		}, nil
	case provider.FamilyDocument:
		return documentDescriptor(base, s)
	case provider.FamilyStream:
		return provider.StreamConn{
			Engine:   base,
			Host:     s.get("hostname"),
			Port:     s.get("port"),
			User:     s.get("user", "username"),
			Password: s.get("password"),
		}, nil
	case provider.FamilyFile, provider.FamilyUnknown:
		return nil, fmt.Errorf("unsupported connection type %q", serviceType)
	default:
		return nil, fmt.Errorf("unsupported connection type %q", serviceType)
	}
}

func sqlDescriptor(base string, s *connVars) (provider.SQLConn, error) {
	conn := provider.SQLConn{
		Dialect:  base,
		Host:     s.get("hostname"),
		Port:     s.get("port"),
		User:     s.get("user"),
		Password: s.get("password"),
		Database: s.get("dbName"),
	}
	switch base {
	case "postgresql":
		conn.Driver = "pgx"
	case "mariadb", "mysql":
		conn.Driver = "mysql"
		if conn.Database == "" {
			conn.Database = conn.User
		}
	case "clickhouse":
		conn.Driver = "clickhouse"
		conn.Port = s.get("portNative", "port")
		conn.User = s.get("user", "superUser")
		if conn.User == "" {
			conn.User = "zerops"
		}
		if conn.Database == "" {
			conn.Database = "default"
		}
	default:
		return provider.SQLConn{}, fmt.Errorf("unsupported tabular type %q", base)
	}
	return conn, nil
}

func documentDescriptor(base string, s *connVars) (provider.DocumentConn, error) {
	conn := provider.DocumentConn{
		Engine:  base,
		BaseURL: "http://" + net.JoinHostPort(s.get("hostname"), s.get("port")),
	}
	switch base {
	case "elasticsearch":
		conn.User = s.get("user", "username")
		if conn.User == "" {
			conn.User = "elastic"
		}
		conn.APIKey = s.get("password", "elasticPassword")
	case "meilisearch":
		conn.APIKey = s.get("masterKey", "defaultAdminKey", "apiKey", "password")
	case "typesense":
		conn.APIKey = s.get("apiKey", "adminApiKey", "password")
	case "qdrant":
		conn.APIKey = s.get("apiKey", "password")
	default:
		return provider.DocumentConn{}, fmt.Errorf("unsupported document type %q", base)
	}
	return conn, nil
}

// connVars are a service's variables as its connection reads them: the API's
// plain values with the withheld ones the env store holds filled in (values),
// and the withheld keys it does not hold (absent).
type connVars struct {
	values map[string]string
	absent map[string]bool
	// missing are the absent keys a read needed: a read that found no value
	// among keys a withheld key could have answered.
	missing []string
}

// get is the first non-empty value of keys, a connection field's fallback
// order. One that finds none while an absent withheld key could have
// answered records those keys as missing.
func (s *connVars) get(keys ...string) string {
	for _, k := range keys {
		if v := s.values[k]; v != "" {
			return v
		}
	}
	for _, k := range keys {
		if s.absent[k] && !slices.Contains(s.missing, k) {
			s.missing = append(s.missing, k)
		}
	}
	return ""
}
