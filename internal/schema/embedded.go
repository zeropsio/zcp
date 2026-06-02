package schema

import "sync"

// Embedded schemas as the cold-start / fetch-failure seed.
//
// The committed testdata schemas (already //go:embed'd in
// validate_jsonschema.go) are parsed once into a *Schemas. NewCache seeds with
// this so Get is never nil: a cold-start or a failed/poisoned live fetch
// returns the embedded bootstrap instead of nil. It is a bootstrap seed, not an
// outage fallback — when the platform (and thus the schema endpoint) is down,
// nothing deploys anyway, so the seed is harmless; when the platform is up, the
// first Get replaces it with the live fetch within the bounded timeout.

var (
	embeddedSchemasOnce sync.Once
	embeddedSchemasVal  *Schemas
)

// embeddedSchemas parses the embedded schema bytes into a *Schemas, once.
// Returns nil only if the committed embedded JSON is malformed or extracts to
// empty enums — both build-time invariants pinned by TestEmbeddedSchemas_*.
// A nil return degrades the cache to its pre-seed behavior (Get may be nil),
// which never happens with valid committed schemas.
func embeddedSchemas() *Schemas {
	embeddedSchemasOnce.Do(func() {
		zerops, err := ParseZeropsYmlSchema(embeddedZeropsSchema)
		if err != nil {
			return
		}
		imp, err := ParseImportYmlSchema(embeddedImportSchema)
		if err != nil {
			return
		}
		if rejectEmptyEnums(zerops, imp) != nil {
			return
		}
		embeddedSchemasVal = &Schemas{ZeropsYml: zerops, ImportYml: imp}
	})
	return embeddedSchemasVal
}
