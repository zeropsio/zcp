package seed

import "testing"

// TestTabularName_EmptyNamespaceIsIdentity pins the static-mode contract: an
// empty namespace must reproduce today's dcseed fixture names byte-for-byte
// (dc_customers/dc_orders for postgres, users/products for mysql — dcseed's
// own names are NOT uniformly "dc_"-prefixed, so the identity function is
// the only rule that can honor both without special-casing per engine).
func TestTabularName_EmptyNamespaceIsIdentity(t *testing.T) {
	t.Parallel()
	for _, base := range []string{"dc_customers", "dc_orders", "users", "products", "events"} {
		if got := TabularName("", base); got != base {
			t.Errorf("TabularName(%q, %q) = %q, want %q (identity)", "", base, got, base)
		}
	}
}

func TestTabularName_NamespacedPrefixesWithUnderscore(t *testing.T) {
	t.Parallel()
	tests := []struct{ namespace, base, want string }{
		{"dcconf", "dc_customers", "dcconf_dc_customers"},
		{"dcconf_run1", "users", "dcconf_run1_users"},
		{"ns", "events", "ns_events"},
	}
	for _, tt := range tests {
		if got := TabularName(tt.namespace, tt.base); got != tt.want {
			t.Errorf("TabularName(%q, %q) = %q, want %q", tt.namespace, tt.base, got, tt.want)
		}
	}
}

func TestTabularOwns(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name              string
		namespace, tblame string
		want              bool
	}{
		{"owned by exact namespace prefix", "dcconf", "dcconf_dc_customers", true},
		{"unrelated static name not owned", "dcconf", "dc_customers", false},
		{"different namespace not owned", "dcconf", "other_dc_customers", false},
		{"empty namespace never owns anything", "", "dcconf_dc_customers", false},
		{"empty namespace never owns the static name either", "", "dc_customers", false},
		{"prefix collision without separator is not owned", "dc", "dcconf_dc_customers", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := TabularOwns(tt.namespace, tt.tblame); got != tt.want {
				t.Errorf("TabularOwns(%q, %q) = %v, want %v", tt.namespace, tt.tblame, got, tt.want)
			}
		})
	}
}

func TestKVName(t *testing.T) {
	t.Parallel()
	if got := KVName("", "greeting"); got != "greeting" {
		t.Errorf("KVName(%q, %q) = %q, want identity %q", "", "greeting", got, "greeting")
	}
	if got := KVName("", "session:abc123"); got != "session:abc123" {
		t.Errorf("KVName(%q, %q) = %q, want identity %q", "", "session:abc123", got, "session:abc123")
	}
	if got := KVName("dcconf", "greeting"); got != "dcconf:greeting" {
		t.Errorf("KVName(%q, %q) = %q, want %q", "dcconf", "greeting", got, "dcconf:greeting")
	}
	if got := KVName("dcconf", "session:abc123"); got != "dcconf:session:abc123" {
		t.Errorf("KVName(%q, %q) = %q, want %q", "dcconf", "session:abc123", got, "dcconf:session:abc123")
	}
}

func TestKVOwns(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		namespace, k string
		want         bool
	}{
		{"owned flat key", "dcconf", "dcconf:greeting", true},
		{"owned nested key", "dcconf", "dcconf:session:abc123", true},
		{"static key not owned", "dcconf", "greeting", false},
		{"empty namespace never owns", "", "dcconf:greeting", false},
		{"different namespace not owned", "dcconf", "other:greeting", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := KVOwns(tt.namespace, tt.k); got != tt.want {
				t.Errorf("KVOwns(%q, %q) = %v, want %v", tt.namespace, tt.k, got, tt.want)
			}
		})
	}
}

func TestObjectName(t *testing.T) {
	t.Parallel()
	if got := ObjectName("", "readme.txt"); got != "readme.txt" {
		t.Errorf("ObjectName(%q, %q) = %q, want identity", "", "readme.txt", got)
	}
	if got := ObjectName("", "images/red.png"); got != "images/red.png" {
		t.Errorf("ObjectName(%q, %q) = %q, want identity", "", "images/red.png", got)
	}
	if got := ObjectName("dcconf", "readme.txt"); got != "dcconf/readme.txt" {
		t.Errorf("ObjectName(%q, %q) = %q, want %q", "dcconf", "readme.txt", got, "dcconf/readme.txt")
	}
	if got := ObjectName("dcconf", "images/red.png"); got != "dcconf/images/red.png" {
		t.Errorf("ObjectName(%q, %q) = %q, want %q", "dcconf", "images/red.png", got, "dcconf/images/red.png")
	}
}

func TestObjectOwns(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		namespace, k string
		want         bool
	}{
		{"owned top-level object", "dcconf", "dcconf/readme.txt", true},
		{"owned nested object", "dcconf", "dcconf/images/red.png", true},
		{"static object not owned", "dcconf", "readme.txt", false},
		{"empty namespace never owns", "", "dcconf/readme.txt", false},
		{"sibling prefix not owned", "dcconf", "dcconf2/readme.txt", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ObjectOwns(tt.namespace, tt.k); got != tt.want {
				t.Errorf("ObjectOwns(%q, %q) = %v, want %v", tt.namespace, tt.k, got, tt.want)
			}
		})
	}
}

func TestDocumentName(t *testing.T) {
	t.Parallel()
	if got := DocumentName("", "products"); got != "products" {
		t.Errorf("DocumentName(%q, %q) = %q, want identity", "", "products", got)
	}
	if got := DocumentName("dcconf", "products"); got != "dcconf_products" {
		t.Errorf("DocumentName(%q, %q) = %q, want %q", "dcconf", "products", got, "dcconf_products")
	}
}

func TestDocumentOwns(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		namespace, k string
		want         bool
	}{
		{"owned index", "dcconf", "dcconf_products", true},
		{"static index not owned", "dcconf", "products", false},
		{"empty namespace never owns", "", "dcconf_products", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := DocumentOwns(tt.namespace, tt.k); got != tt.want {
				t.Errorf("DocumentOwns(%q, %q) = %v, want %v", tt.namespace, tt.k, got, tt.want)
			}
		})
	}
}

func TestStreamName(t *testing.T) {
	t.Parallel()
	if got := StreamName("", "orders"); got != "orders" {
		t.Errorf("StreamName(%q, %q) = %q, want identity", "", "orders", got)
	}
	if got := StreamName("", "EVENTS"); got != "EVENTS" {
		t.Errorf("StreamName(%q, %q) = %q, want identity", "", "EVENTS", got)
	}
	if got := StreamName("dcconf", "orders"); got != "dcconf_orders" {
		t.Errorf("StreamName(%q, %q) = %q, want %q", "dcconf", "orders", got, "dcconf_orders")
	}
}

func TestStreamOwns(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		namespace, k string
		want         bool
	}{
		{"owned topic", "dcconf", "dcconf_orders", true},
		{"owned stream", "dcconf", "dcconf_EVENTS", true},
		{"static topic not owned", "dcconf", "orders", false},
		{"empty namespace never owns", "", "dcconf_orders", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := StreamOwns(tt.namespace, tt.k); got != tt.want {
				t.Errorf("StreamOwns(%q, %q) = %v, want %v", tt.namespace, tt.k, got, tt.want)
			}
		})
	}
}

// TestValidNamespace pins the safe charset every namespace must satisfy
// before it is ever interpolated into a SQL identifier, S3 key, HTTP path,
// or Kafka/NATS topic/stream name: letters, digits, underscore, hyphen —
// nothing a SQL identifier, URL path segment, or messaging topic name
// disagrees on, and nothing that could break out of a composed statement.
func TestValidNamespace(t *testing.T) {
	t.Parallel()
	valid := []string{"dcconf", "dcconf_run1", "dcconf-run1", "a", "A1_b-2"}
	for _, ns := range valid {
		if !ValidNamespace(ns) {
			t.Errorf("ValidNamespace(%q) = false, want true", ns)
		}
	}
	invalid := []string{"", " ", "dcconf run1", "dcconf:run1", "dcconf/run1", "dcconf.run1", "dcconf;DROP TABLE x", "dc'conf"}
	for _, ns := range invalid {
		if ValidNamespace(ns) {
			t.Errorf("ValidNamespace(%q) = true, want false", ns)
		}
	}
}
