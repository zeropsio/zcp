package skillpacks

import "testing"

func TestParseFrontmatter_Valid(t *testing.T) {
	t.Parallel()
	fm, err := parseFrontmatter([]byte("---\nname: foo\ndescription: does foo things\n---\n\n# Foo\n"))
	if err != nil {
		t.Fatalf("parseFrontmatter: %v", err)
	}
	if fm.Name != "foo" {
		t.Errorf("Name = %q, want foo", fm.Name)
	}
	if fm.Description != "does foo things" {
		t.Errorf("Description = %q, want %q", fm.Description, "does foo things")
	}
}

func TestParseFrontmatter_IgnoresUnknownFields(t *testing.T) {
	t.Parallel()
	fm, err := parseFrontmatter([]byte("---\nname: foo\ndescription: does foo things\nlicense: MIT\nversion: 3\n---\nbody\n"))
	if err != nil {
		t.Fatalf("parseFrontmatter: %v", err)
	}
	if fm.Name != "foo" || fm.Description != "does foo things" {
		t.Errorf("fm = %+v, want name=foo description=%q", fm, "does foo things")
	}
}

func TestParseFrontmatter_MissingOpeningDelimiter_Errors(t *testing.T) {
	t.Parallel()
	_, err := parseFrontmatter([]byte("name: foo\ndescription: does foo things\n"))
	if err == nil {
		t.Fatal("expected an error for a SKILL.md with no leading \"---\"")
	}
}

func TestParseFrontmatter_MissingClosingDelimiter_Errors(t *testing.T) {
	t.Parallel()
	_, err := parseFrontmatter([]byte("---\nname: foo\ndescription: does foo things\n"))
	if err == nil {
		t.Fatal("expected an error for a SKILL.md with no closing \"---\"")
	}
}

func TestParseFrontmatter_MissingName_Errors(t *testing.T) {
	t.Parallel()
	_, err := parseFrontmatter([]byte("---\ndescription: does foo things\n---\n"))
	if err == nil {
		t.Fatal("expected an error for missing name")
	}
}

func TestParseFrontmatter_MissingDescription_Errors(t *testing.T) {
	t.Parallel()
	_, err := parseFrontmatter([]byte("---\nname: foo\n---\n"))
	if err == nil {
		t.Fatal("expected an error for missing description")
	}
}

func TestParseFrontmatter_EmptyNameString_Errors(t *testing.T) {
	t.Parallel()
	_, err := parseFrontmatter([]byte("---\nname: \"\"\ndescription: does foo things\n---\n"))
	if err == nil {
		t.Fatal("expected an error for an empty (but present) name field")
	}
}

func TestParseFrontmatter_MalformedYAML_Errors(t *testing.T) {
	t.Parallel()
	_, err := parseFrontmatter([]byte("---\nname: [unterminated\ndescription: x\n---\n"))
	if err == nil {
		t.Fatal("expected an error for malformed YAML")
	}
}
