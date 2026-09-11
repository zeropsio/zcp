package observer

import "testing"

func TestNormalizeText_AppliesFM46Normalization(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"Do **NOT** `override`", "Do NOT override"},
		{"source  mount\\n/var/www/app\tmissing", "source mount /var/www/app missing"},
		{`\"x\"`, `"x"`},
		{"   ", ""},
	}
	for _, c := range cases {
		if got := NormalizeText(c.in); got != c.want {
			t.Errorf("NormalizeText(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDecodeJSONEscapes_Exported(t *testing.T) {
	t.Parallel()
	if got := DecodeJSONEscapes(`a\nb \u003c`); got != "a\nb <" {
		t.Errorf("DecodeJSONEscapes = %q", got)
	}
}
