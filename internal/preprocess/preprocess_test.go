package preprocess

import (
	"context"
	"strings"
	"testing"
)

func TestExpand_PlainValuePassthrough(t *testing.T) {
	t.Parallel()
	tests := []string{
		"",
		"literal-secret",
		"base64:abc=",
		"https://example.com/<not-a-function>",
		"plain ${env_var_ref}",
	}
	for _, in := range tests {
		got, err := Expand(context.Background(), in)
		if err != nil {
			t.Errorf("plain value %q: unexpected error: %v", in, err)
		}
		if got != in {
			t.Errorf("plain value %q: got %q (should pass through unchanged)", in, got)
		}
	}
}

func TestExpand_GenerateRandomString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		wantLen int
	}{
		{"bare_32", "<@generateRandomString(<32>)>", 32},
		{"bare_44", "<@generateRandomString(<44>)>", 44},
		{"bare_8", "<@generateRandomString(<8>)>", 8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Expand(context.Background(), tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != tt.wantLen {
				t.Errorf("length = %d, want %d (value: %q)", len(got), tt.wantLen, got)
			}
			// zParser's alphabet is [a-zA-Z0-9_-.] (64 chars, URL-safe-ish).
			// Each char is one ASCII byte, so char count == byte count —
			// which is what fixed-length ciphers like aes-256-cbc require.
			const allowed = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-."
			for _, r := range got {
				if !strings.ContainsRune(allowed, r) {
					t.Errorf("char %q outside zParser alphabet in output %q", r, got)
					break
				}
			}
			// Crucially: char count == byte count.
			if len([]byte(got)) != tt.wantLen {
				t.Errorf("byte count = %d, want %d — alphabet must be single-byte ASCII", len([]byte(got)), tt.wantLen)
			}
		})
	}
}

func TestExpand_RandomnessDiffers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	a, err := Expand(ctx, "<@generateRandomString(<32>)>")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Expand(ctx, "<@generateRandomString(<32>)>")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Errorf("two invocations produced identical strings %q — rng not seeded", a)
	}
}

func TestExpand_WithPrefix(t *testing.T) {
	t.Parallel()
	// A prefix/suffix around the function call should be preserved verbatim.
	got, err := Expand(context.Background(), "base64:<@generateRandomString(<32>)>")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "base64:") {
		t.Errorf("expected prefix preserved, got %q", got)
	}
	if len(got) != len("base64:")+32 {
		t.Errorf("length = %d, want %d", len(got), len("base64:")+32)
	}
}

func TestExpand_GenerateRandomInt(t *testing.T) {
	t.Parallel()
	got, err := Expand(context.Background(), "<@generateRandomInt(<10>, <20>)>")
	if err != nil {
		t.Fatal(err)
	}
	if got == "" {
		t.Fatal("empty output")
	}
	// Value should be an integer string in [10, 20].
	if len(got) < 2 || len(got) > 2 {
		t.Errorf("expected 2-digit int in [10,20], got %q", got)
	}
}

func TestExpand_ModifierUpper(t *testing.T) {
	t.Parallel()
	got, err := Expand(context.Background(), "<@generateRandomString(<16>) | upper>")
	if err != nil {
		t.Fatal(err)
	}
	if got != strings.ToUpper(got) {
		t.Errorf("expected uppercase output, got %q", got)
	}
	if len(got) != 16 {
		t.Errorf("length = %d, want 16", len(got))
	}
}

func TestExpand_ModifierToHex(t *testing.T) {
	t.Parallel()
	got, err := Expand(context.Background(), "<@generateRandomBytes(<16>) | toHex>")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 32 { // 16 bytes → 32 hex chars
		t.Errorf("length = %d, want 32 hex chars for 16 bytes", len(got))
	}
	for _, r := range got {
		ok := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
		if !ok {
			t.Errorf("non-hex char %q in output %q", r, got)
			break
		}
	}
}

func TestExpand_UnknownFunction(t *testing.T) {
	t.Parallel()
	_, err := Expand(context.Background(), "<@thisFunctionDoesNotExist(<1>)>")
	if err == nil {
		t.Fatal("expected error for unknown function")
	}
}

// TestExpand_ErrorQuotesNoInput: an expansion's error carries zParser's
// reason, never the input — a value is often a secret.
func TestExpand_ErrorQuotesNoInput(t *testing.T) {
	t.Parallel()
	secret := "s3cr3t-" + "value-0c9d"
	tests := []string{
		secret + "<@nosuchfn(<1>)>",
		"<@nosuchfn(<" + secret + ">)>",
	}
	for _, in := range tests {
		_, err := Expand(context.Background(), in)
		if err == nil {
			t.Fatalf("Expand(%q): want an error", in)
		}
		if strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), "nosuchfn") {
			t.Errorf("Expand error = %q, want zParser's reason without the input", err)
		}
	}
	_, err := Batch(context.Background(), []string{"A", "B"}, map[string]string{"A": secret, "B": "<@nosuchfn(<1>)>"})
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Errorf("Batch error = %v, want one without another entry's value", err)
	}
}

// TestExpand_CommaWithoutSpace: zParser v2.1.2 panics on a call whose
// argument follows its comma directly — <@generateRandomInt(<10>,<50>)>.
// The panic is caught and answered as an error that says how to write it,
// never the input; the process lives. With the space, the call expands.
func TestExpand_CommaWithoutSpace(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in      string
		wantErr bool
	}{
		{in: "<@generateRandomInt(<10>,<50>)>", wantErr: true},
		{in: "<@setVar(<k>,<v-91f2>)><@getVar(k)>", wantErr: true},
		{in: "<@pickRandom(<a>,\\<b>)>", wantErr: true},
		{in: "<@generateRandomInt(<10>, <50>)>"},
		{in: "<@setVar(<k>, <v-91f2>)><@getVar(k)>"},
	}
	for _, tt := range tests {
		got, err := Expand(context.Background(), tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("Expand(%q) = %q, %v; want error %v", tt.in, got, err, tt.wantErr)
			continue
		}
		if err != nil && (!strings.Contains(err.Error(), "space after") || strings.Contains(err.Error(), "v-91f2")) {
			t.Errorf("Expand(%q) error = %q, want how to write it and no input", tt.in, err)
		}
	}
	if _, err := Batch(context.Background(), []string{"A"}, map[string]string{"A": "<@generateRandomInt(<1>,<2>)>"}); err == nil {
		t.Error("Batch: want an error for the comma without a space")
	}
}

func TestBatch_SimpleKeys(t *testing.T) {
	t.Parallel()
	keys := []string{"A", "B", "C"}
	inputs := map[string]string{
		"A": "<@generateRandomString(<10>)>",
		"B": "<@generateRandomString(<20>)>",
		"C": "literal",
	}
	got, err := Batch(context.Background(), keys, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got["A"]) != 10 {
		t.Errorf("A length = %d, want 10", len(got["A"]))
	}
	if len(got["B"]) != 20 {
		t.Errorf("B length = %d, want 20", len(got["B"]))
	}
	if got["C"] != "literal" {
		t.Errorf("C = %q, want literal", got["C"])
	}
}

func TestBatch_VariableSharing(t *testing.T) {
	t.Parallel()
	// setVar in A, getVar in B — the batch must share a variable store so
	// B resolves to the value A generated.
	keys := []string{"A", "B"}
	inputs := map[string]string{
		"A": "<@generateRandomStringVar(<myToken>, <16>)>",
		"B": "<@getVar(myToken) | upper>",
	}
	got, err := Batch(context.Background(), keys, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got["A"]) != 16 {
		t.Errorf("A length = %d, want 16", len(got["A"]))
	}
	if got["B"] != strings.ToUpper(got["A"]) {
		t.Errorf("B %q should be uppercase of A %q", got["B"], got["A"])
	}
}

func TestBatch_PreservesOrder(t *testing.T) {
	t.Parallel()
	keys := []string{"first", "second", "third"}
	inputs := map[string]string{
		"first":  "alpha",
		"second": "beta",
		"third":  "gamma",
	}
	got, err := Batch(context.Background(), keys, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if got["first"] != "alpha" || got["second"] != "beta" || got["third"] != "gamma" {
		t.Errorf("got %+v, want ordered passthrough", got)
	}
}

func TestBatch_MissingKey(t *testing.T) {
	t.Parallel()
	_, err := Batch(context.Background(), []string{"A", "B"}, map[string]string{"A": "value"})
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestBatch_Empty(t *testing.T) {
	t.Parallel()
	got, err := Batch(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("want empty map, got %+v", got)
	}
}

func TestContainsSyntax(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  bool
	}{
		{"", false},
		{"plain value", false},
		{"base64:abc", false},
		{"<not-a-function>", false},
		{"<@generateRandomString(<32>)>", true},
		{"prefix<@foo(<1>)>suffix", true},
		{"<@", true}, // edge: prefix alone triggers
	}
	for _, tt := range tests {
		if got := containsSyntax(tt.input); got != tt.want {
			t.Errorf("containsSyntax(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
