package schema

import "testing"

func localStorageSchemas() *Schemas {
	return &Schemas{ImportYml: &ImportYmlSchema{ServiceTypes: []string{"local-storage:single@1"}}}
}

func TestSchemas_LocalStorageSingle_ExactCompositeAccepted(t *testing.T) {
	t.Parallel()
	if !localStorageSchemas().HasServiceType("local-storage:single@1") {
		t.Error("exact public Local Storage identifier was rejected")
	}
}

func TestSchemas_LocalStorageHA_AbsentVariantRejected(t *testing.T) {
	t.Parallel()
	if localStorageSchemas().HasServiceType("local-storage:ha@1") {
		t.Error("non-existent Local Storage HA variant was accepted")
	}
}

func TestSchemas_LocalStorageSingle_HAVariantUnsupported(t *testing.T) {
	t.Parallel()
	if localStorageSchemas().SupportsHAVariant("local-storage:single@1") {
		t.Error("single-only Local Storage reported an HA variant")
	}
}
