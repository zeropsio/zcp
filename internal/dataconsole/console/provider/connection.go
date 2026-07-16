package provider

// ConnectionDescriptor is the typed, neutral connection payload that crosses
// the host-to-console seam. It carries service connection facts, never Zerops
// env-key names and never a provider-specific prebuilt DSN.
type ConnectionDescriptor interface {
	ConnectionFamily() Family
	connectionDescriptor()
}

// SQLConn describes a relational/native SQL endpoint.
type SQLConn struct {
	Driver   string
	Dialect  string
	Host     string
	Port     string
	User     string
	Password string
	Database string
}

func (SQLConn) ConnectionFamily() Family { return FamilyTabular }
func (SQLConn) connectionDescriptor()    {}

// ObjectConn describes an S3-compatible object-storage endpoint.
type ObjectConn struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	Secure    bool
}

func (ObjectConn) ConnectionFamily() Family { return FamilyObject }
func (ObjectConn) connectionDescriptor()    {}

// KVConn describes a Redis-shape key-value endpoint.
type KVConn struct {
	Host     string
	Port     string
	Password string
}

func (KVConn) ConnectionFamily() Family { return FamilyKV }
func (KVConn) connectionDescriptor()    {}

// DocumentConn describes a search/vector document-engine HTTP endpoint.
type DocumentConn struct {
	Engine  string
	BaseURL string
	User    string
	APIKey  string
}

func (DocumentConn) ConnectionFamily() Family { return FamilyDocument }
func (DocumentConn) connectionDescriptor()    {}

// StreamConn describes a messaging service endpoint.
type StreamConn struct {
	Engine   string
	Host     string
	Port     string
	User     string
	Password string
}

func (StreamConn) ConnectionFamily() Family { return FamilyStream }
func (StreamConn) connectionDescriptor()    {}
