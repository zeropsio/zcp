package seed

import (
	"regexp"
	"strings"
)

// Options configures one Service (seed) run.
type Options struct {
	// Namespace, when non-empty, is prefixed onto every artifact name a seed
	// run creates (tables/keys/objects/indices/collections/topics/streams) —
	// see the per-family Name functions below. An empty Namespace reproduces
	// today's static, human-browsable dcseed fixture set byte-for-byte
	// (every Name function is the identity function when Namespace == "").
	// A non-empty Namespace MUST satisfy ValidNamespace — Service and
	// Cleanup both reject one that doesn't, since it is about to be
	// interpolated into a SQL identifier, S3 key, HTTP path segment, or
	// Kafka/NATS topic/stream name.
	Namespace string
}

// validNamespacePattern is the safe charset for a namespace: letters,
// digits, underscore, hyphen. It is deliberately conservative — it excludes
// every family-specific separator (":" for kv, "/" for object, "." for SQL
// schema-qualification or NATS subjects) so a namespace value can never be
// mistaken for a separator or a deeper path/hierarchy segment, and it
// excludes every SQL/shell metacharacter so the tabular family's
// identifier interpolation (necessary — SQL has no placeholder syntax for
// identifiers, only values) is safe by construction rather than by escaping.
var validNamespacePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ValidNamespace reports whether ns is safe to interpolate into every
// family's artifact-naming scheme. An empty string is NOT valid as a
// namespace value (it is the separate, reserved "static mode" sentinel
// used by the Name functions and rejected outright by Cleanup).
func ValidNamespace(ns string) bool {
	return validNamespacePattern.MatchString(ns)
}

// TabularName derives a table name for the tabular family (postgresql,
// mariadb/mysql, clickhouse). SQL identifiers allow only letters, digits,
// and underscore, so a non-empty namespace joins with "_". base is the
// exact pre-existing static table name (e.g. "dc_customers", "users",
// "events" — dcseed's own names are not uniformly "dc_"-prefixed), so an
// empty namespace reproduces today's fixtures byte-for-byte (identity).
func TabularName(namespace, base string) string {
	if namespace == "" {
		return base
	}
	return namespace + "_" + base
}

// TabularOwns reports whether name was produced by TabularName(namespace,
// *) for a non-empty namespace — used by Cleanup to filter a live table
// listing (information_schema / system.tables) down to this namespace's
// own fixtures. Always false for an empty namespace: ownership of the
// unprefixed static dataset is never claimed by a sweep, so Cleanup can
// never mass-delete it.
func TabularOwns(namespace, name string) bool {
	if namespace == "" {
		return false
	}
	return strings.HasPrefix(name, namespace+"_")
}

// KVName derives a key name for the kv family (valkey). The provider's own
// keyspace tree already groups on ":" (provider/kv.keyOf joins Path.Segments
// with ":"), so a namespaced key becomes a first-class virtual container in
// the console's tree — dcconf:greeting, dcconf:session:abc123 — rather than
// a flat, unbrowsable prefix. base is the exact pre-existing static key name
// (e.g. "greeting", "session:abc123"), so an empty namespace reproduces
// today's fixtures byte-for-byte (identity).
func KVName(namespace, base string) string {
	if namespace == "" {
		return base
	}
	return namespace + ":" + base
}

// KVOwns reports whether key was produced by KVName(namespace, *) for a
// non-empty namespace — used by Cleanup to filter a keyspace SCAN down to
// this namespace's own fixtures. Always false for an empty namespace.
func KVOwns(namespace, key string) bool {
	if namespace == "" {
		return false
	}
	return strings.HasPrefix(key, namespace+":")
}

// ObjectName derives an object key for the object family (S3-compatible
// storage). Object keys are "/"-joined paths, so a non-empty namespace
// becomes a leading prefix "directory" — exactly the object-storage
// convention the provider's own tree already renders as a container. base
// is the exact pre-existing static object key (e.g. "readme.txt",
// "images/red.png"), so an empty namespace reproduces today's fixtures
// byte-for-byte (identity).
func ObjectName(namespace, base string) string {
	if namespace == "" {
		return base
	}
	return namespace + "/" + base
}

// ObjectOwns reports whether key was produced by ObjectName(namespace, *)
// for a non-empty namespace — used by Cleanup to filter a ListObjects
// listing down to this namespace's own fixtures. Always false for an empty
// namespace.
func ObjectOwns(namespace, key string) bool {
	if namespace == "" {
		return false
	}
	return strings.HasPrefix(key, namespace+"/")
}

// DocumentName derives an index/collection name for the document family
// (elasticsearch, meilisearch, typesense, qdrant): joins with "_" — the one
// separator every engine's naming rules accept in common (elasticsearch
// additionally requires lowercase, which every base/namespace this package
// uses already is). base is the exact pre-existing static index/collection
// name (e.g. "products", "articles", "items"), so an empty namespace
// reproduces today's fixtures byte-for-byte (identity).
func DocumentName(namespace, base string) string {
	if namespace == "" {
		return base
	}
	return namespace + "_" + base
}

// DocumentOwns reports whether name was produced by DocumentName(namespace,
// *) for a non-empty namespace — used by Cleanup to filter a live
// index/collection listing down to this namespace's own fixtures. Always
// false for an empty namespace.
func DocumentOwns(namespace, name string) bool {
	if namespace == "" {
		return false
	}
	return strings.HasPrefix(name, namespace+"_")
}

// StreamName derives a topic/stream name for the stream family (kafka,
// nats): joins with "_" — safe for kafka topic names and NATS stream names
// alike (NATS additionally forbids "." in a stream name, which this
// package's namespaces never contain — see ValidNamespace). base is the
// exact pre-existing static topic/stream name (e.g. "orders", "EVENTS"), so
// an empty namespace reproduces today's fixtures byte-for-byte (identity).
func StreamName(namespace, base string) string {
	if namespace == "" {
		return base
	}
	return namespace + "_" + base
}

// StreamOwns reports whether name was produced by StreamName(namespace, *)
// for a non-empty namespace — used by Cleanup to filter a live
// topic/stream listing down to this namespace's own fixtures. Always false
// for an empty namespace.
func StreamOwns(namespace, name string) bool {
	if namespace == "" {
		return false
	}
	return strings.HasPrefix(name, namespace+"_")
}
