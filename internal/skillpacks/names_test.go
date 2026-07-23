package skillpacks

import "testing"

func TestValidatePortableName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"plain lowercase", "foo", false},
		{"digits and hyphens", "foo-bar-123", false},
		{"single char", "a", false},
		{"max length 64", repeatChar('a', 64), false},
		{"too long 65", repeatChar('a', 65), true},
		{"empty", "", true},
		{"uppercase rejected", "Foo", true},
		{"leading hyphen", "-foo", true},
		{"trailing hyphen", "foo-", true},
		{"consecutive hyphens", "foo--bar", true},
		{"underscore rejected", "foo_bar", true},
		{"slash rejected", "foo/bar", true},
		{"windows device name con", "con", true},
		{"windows device name COM (already lowercase) com1", "com1", true},
		{"windows device name lpt9", "lpt9", true},
		{"not a device name: com10", "com10", false},
		{"not a device name: console", "console", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validatePortableName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePortableName(%q) err = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestValidateDestinationName_RejectsReservedGuided(t *testing.T) {
	t.Parallel()
	if err := validateDestinationName("guided"); err == nil {
		t.Fatal("expected an error for the reserved \"guided\" name")
	}
	if err := validateDestinationName("guided-tour"); err != nil {
		t.Errorf("a name merely containing \"guided\" as a prefix should be fine: %v", err)
	}
	if err := validateDestinationName("normal-skill"); err != nil {
		t.Errorf("unexpected error for a normal name: %v", err)
	}
}

func TestValidateSourcePath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"whole-repo dot", ".", false},
		{"single segment", "skills", false},
		{"nested", "skills/misc/foo", false},
		{"empty", "", true},
		{"absolute", "/skills/foo", true},
		{"backslash", `skills\foo`, true},
		{"traversal segment", "skills/../etc", true},
		{"trailing traversal", "skills/..", true},
		{"empty segment (double slash)", "skills//foo", true},
		{"dot segment mid-path", "skills/./foo", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateSourcePath(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateSourcePath(%q) err = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func repeatChar(c byte, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = c
	}
	return string(b)
}
