package main

import (
	"net"
	"strings"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
	"github.com/zeropsio/zcp/internal/dataconsole/console/provider/conformance"
)

// mapServiceEnv maps one managed service's neutral type string + its own raw
// env scalars (as fetched by ops.FetchServiceEnv — the same call
// zcpadapter/adapter.go's ConnectionInfo makes) into a conformance-shaped
// ServiceEntry. It mirrors adapter.go's descriptorFor family (lines 102-193)
// field-for-field, but targets conformance's JSON-shaped descriptor blocks
// (SQLDescriptor, KVDescriptor, ...) rather than provider's live connection
// types — a second, independently-testable implementation of the same
// wire-contract mapping, since the two packages serialize different concrete
// types for it.
//
// ok is false when serviceType classifies to no supported console family
// (provider.FamilyFile / FamilyUnknown) — the caller skips the service
// silently rather than emitting a config entry no live case can use.
func mapServiceEnv(hostname, serviceType string, s map[string]string) (conformance.ServiceEntry, bool) {
	base := provider.BaseType(serviceType)
	entry := conformance.ServiceEntry{Hostname: hostname, Type: serviceType}
	switch provider.Classify(serviceType) {
	case provider.FamilyTabular:
		entry.Tabular = sqlDescriptorFor(base, s)
	case provider.FamilyKV:
		entry.KV = &conformance.KVDescriptor{
			Host:     s["hostname"],
			Port:     s["port"],
			Password: s["password"],
		}
	case provider.FamilyObject:
		entry.Object = &conformance.ObjectDescriptor{
			Endpoint:  s["apiHost"],
			AccessKey: s["accessKeyId"],
			SecretKey: s["secretAccessKey"],
			Bucket:    s["bucketName"],
			Secure:    strings.HasPrefix(strings.ToLower(s["apiUrl"]), "https"),
		}
	case provider.FamilyDocument:
		entry.Document = documentDescriptorFor(base, s)
	case provider.FamilyStream:
		entry.Stream = &conformance.StreamDescriptor{
			Engine:   base,
			Host:     s["hostname"],
			Port:     s["port"],
			User:     firstScalar(s, "user", "username"),
			Password: firstScalar(s, "password"),
		}
	case provider.FamilyFile, provider.FamilyUnknown:
		return conformance.ServiceEntry{}, false
	default:
		return conformance.ServiceEntry{}, false
	}
	return entry, true
}

// sqlDescriptorFor mirrors adapter.go's sqlDescriptor (lines 138-169):
// clickhouse reads the native port and falls back user/database; mariadb/
// mysql default an empty database to the connecting user.
func sqlDescriptorFor(base string, s map[string]string) *conformance.SQLDescriptor {
	d := &conformance.SQLDescriptor{
		Dialect:  base,
		Host:     s["hostname"],
		Port:     s["port"],
		User:     s["user"],
		Password: s["password"],
		Database: s["dbName"],
	}
	switch base {
	case "mariadb", "mysql":
		if d.Database == "" {
			d.Database = d.User
		}
	case "clickhouse":
		d.Port = firstScalar(s, "portNative", "port")
		d.User = firstScalar(s, "user", "superUser")
		if d.User == "" {
			d.User = "zerops"
		}
		if d.Database == "" {
			d.Database = "default"
		}
	}
	return d
}

// documentDescriptorFor mirrors adapter.go's documentDescriptor (lines 171-193).
func documentDescriptorFor(base string, s map[string]string) *conformance.DocumentDescriptor {
	d := &conformance.DocumentDescriptor{
		Engine:  base,
		BaseURL: "http://" + net.JoinHostPort(s["hostname"], s["port"]),
	}
	switch base {
	case "elasticsearch":
		d.User = firstScalar(s, "user", "username")
		if d.User == "" {
			d.User = "elastic"
		}
		d.APIKey = firstScalar(s, "password", "elasticPassword")
	case "meilisearch":
		d.APIKey = firstScalar(s, "masterKey", "defaultAdminKey", "apiKey", "password")
	case "typesense":
		d.APIKey = firstScalar(s, "apiKey", "adminApiKey", "password")
	case "qdrant":
		d.APIKey = firstScalar(s, "apiKey", "password")
	}
	return d
}

func firstScalar(s map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := s[k]; v != "" {
			return v
		}
	}
	return ""
}
