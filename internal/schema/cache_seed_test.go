package schema

import (
	"testing"
	"time"
)

func TestEmbeddedSchemas_NonEmpty(t *testing.T) {
	s := embeddedSchemas()
	if s == nil {
		t.Fatal("embeddedSchemas() returned nil — committed schema bytes malformed?")
	}
	if s.ZeropsYml == nil || s.ImportYml == nil {
		t.Fatal("embedded seed missing a schema half")
	}
	if len(s.ZeropsYml.BuildBases) == 0 || len(s.ZeropsYml.RunBases) == 0 {
		t.Errorf("embedded zerops enums empty: build=%d run=%d", len(s.ZeropsYml.BuildBases), len(s.ZeropsYml.RunBases))
	}
	if len(s.ImportYml.ServiceTypes) == 0 {
		t.Errorf("embedded import service types empty")
	}
}

// TestNewCache_SeedsAndStaysCold pins the state-machine entry point: the cache
// is seeded (Get never nil) but fetchedAt is zero so the FIRST Get still fetches
// live rather than serving the stale seed as if fresh.
func TestNewCache_SeedsAndStaysCold(t *testing.T) {
	c := NewCache(15*time.Minute, "")
	if c.schemas == nil {
		t.Fatal("NewCache did not seed schemas — Get could return nil and recipe checks would silently skip")
	}
	if !c.fetchedAt.IsZero() {
		t.Fatal("seed must leave fetchedAt zero so the first Get performs a live fetch")
	}
}

func TestRejectEmptyEnums(t *testing.T) {
	full := &Schemas{
		ZeropsYml: &ZeropsYmlSchema{BuildBases: []string{"nodejs@22"}, RunBases: []string{"nodejs@22"}},
		ImportYml: &ImportYmlSchema{ServiceTypes: []string{"nodejs@22"}},
	}
	if err := rejectEmptyEnums(full.ZeropsYml, full.ImportYml); err != nil {
		t.Fatalf("full schema rejected: %v", err)
	}

	cases := []struct {
		name   string
		zerops *ZeropsYmlSchema
		imp    *ImportYmlSchema
	}{
		{"empty build bases", &ZeropsYmlSchema{RunBases: []string{"x"}}, &ImportYmlSchema{ServiceTypes: []string{"x"}}},
		{"empty run bases", &ZeropsYmlSchema{BuildBases: []string{"x"}}, &ImportYmlSchema{ServiceTypes: []string{"x"}}},
		{"empty service types", &ZeropsYmlSchema{BuildBases: []string{"x"}, RunBases: []string{"x"}}, &ImportYmlSchema{}},
		{"nil halves", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := rejectEmptyEnums(tc.zerops, tc.imp); err == nil {
				t.Errorf("expected poison guard to reject %s", tc.name)
			}
		})
	}
}
