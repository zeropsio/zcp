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

	mu       sync.Mutex
	typeByID map[string]string // serviceID -> type string (filled on discovery)
}

// New wires the adapter from an already-resolved client + auth info.
func New(client platform.Client, info *auth.Info) *Adapter {
	return &Adapter{client: client, info: info, typeByID: map[string]string{}}
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
	for i := range services {
		svc := &services[i]
		typ := svc.ServiceStackTypeInfo.ServiceStackTypeVersionName
		if !topology.IsManagedService(typ) {
			continue
		}
		idType[svc.ID] = typ
		out = append(out, console.ManagedServiceRef{
			ID:       svc.ID,
			Hostname: svc.Name,
			Type:     typ,
			Status:   svc.Status,
		})
	}
	a.mu.Lock()
	a.typeByID = idType
	a.mu.Unlock()
	return out, nil
}

// ConnectionInfo resolves a service's own env scalars via FetchServiceEnv and
// maps them to the console's neutral typed connection descriptor. Env-key names
// and service-type fallback rules live only in this bridge.
func (a *Adapter) ConnectionInfo(ctx context.Context, serviceID string) (console.ConnectionInfo, error) {
	envs, err := ops.FetchServiceEnv(ctx, a.client, serviceID)
	if err != nil {
		return console.ConnectionInfo{}, err
	}
	scalars := make(map[string]string, len(envs))
	for _, e := range envs {
		scalars[e.Key] = e.Content
	}
	a.mu.Lock()
	typ := a.typeByID[serviceID]
	a.mu.Unlock()
	desc, err := descriptorFor(typ, scalars)
	if err != nil {
		return console.ConnectionInfo{}, err
	}
	return console.ConnectionInfo{
		Type:       typ,
		Family:     desc.ConnectionFamily(),
		Descriptor: desc,
	}, nil
}

func descriptorFor(serviceType string, s map[string]string) (provider.ConnectionDescriptor, error) {
	base := provider.BaseType(serviceType)
	switch provider.Classify(serviceType) {
	case provider.FamilyObject:
		return provider.ObjectConn{
			Endpoint:  s["apiHost"],
			AccessKey: s["accessKeyId"],
			SecretKey: s["secretAccessKey"],
			Bucket:    s["bucketName"],
			Secure:    strings.HasPrefix(strings.ToLower(s["apiUrl"]), "https"),
		}, nil
	case provider.FamilyTabular:
		return sqlDescriptor(base, s)
	case provider.FamilyKV:
		return provider.KVConn{
			Host:     s["hostname"],
			Port:     s["port"],
			Password: s["password"],
		}, nil
	case provider.FamilyDocument:
		return documentDescriptor(base, s)
	case provider.FamilyStream:
		return provider.StreamConn{
			Engine:   base,
			Host:     s["hostname"],
			Port:     s["port"],
			User:     firstScalar(s, "user", "username"),
			Password: firstScalar(s, "password"),
		}, nil
	case provider.FamilyFile, provider.FamilyUnknown:
		return nil, fmt.Errorf("unsupported connection type %q", serviceType)
	default:
		return nil, fmt.Errorf("unsupported connection type %q", serviceType)
	}
}

func sqlDescriptor(base string, s map[string]string) (provider.SQLConn, error) {
	conn := provider.SQLConn{
		Dialect:  base,
		Host:     s["hostname"],
		Port:     s["port"],
		User:     s["user"],
		Password: s["password"],
		Database: s["dbName"],
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
		conn.Port = firstScalar(s, "portNative", "port")
		conn.User = firstScalar(s, "user", "superUser")
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

func documentDescriptor(base string, s map[string]string) (provider.DocumentConn, error) {
	conn := provider.DocumentConn{
		Engine:  base,
		BaseURL: "http://" + net.JoinHostPort(s["hostname"], s["port"]),
	}
	switch base {
	case "elasticsearch":
		conn.User = firstScalar(s, "user", "username")
		if conn.User == "" {
			conn.User = "elastic"
		}
		conn.APIKey = firstScalar(s, "password", "elasticPassword")
	case "meilisearch":
		conn.APIKey = firstScalar(s, "masterKey", "defaultAdminKey", "apiKey", "password")
	case "typesense":
		conn.APIKey = firstScalar(s, "apiKey", "adminApiKey", "password")
	case "qdrant":
		conn.APIKey = firstScalar(s, "apiKey", "password")
	default:
		return provider.DocumentConn{}, fmt.Errorf("unsupported document type %q", base)
	}
	return conn, nil
}

func firstScalar(s map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := s[k]; v != "" {
			return v
		}
	}
	return ""
}
