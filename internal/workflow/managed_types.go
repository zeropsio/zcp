package workflow

// Service-type name constants — used by serviceTypeKind in recipe_service_types.go
// to dedupe goconst on repeated literals.
const (
	svcPostgreSQL  = "postgresql"
	svcMariaDB     = "mariadb"
	svcMeilisearch = "meilisearch"
	svcStatic      = "static"
)

// Service-kind constants — returned by serviceTypeKind and used by validation.
const (
	kindDatabase     = "database"
	kindCache        = "cache"
	kindSearchEngine = "search engine"
	kindStorage      = "storage"
	kindMessaging    = "messaging"
	kindMailCatcher  = "mail catcher"
)
