package recipe

import (
	"strings"
	"testing"
)

func sample() Layout {
	return Layout{
		Name:  "aurora",
		Title: "Aurora",
		Intro: "A kudos wall for an office screen, on Zerops.",
		Tiers: []Tier{
			{
				Index: 4, Title: "Small Production", Slug: "small-production",
				Summary:    "**Small production** keeps two app containers running at all times.",
				ImportYAML: "#zeropsPreprocessor=on\nproject:\n  name: aurora-prod\nservices:\n  - hostname: app\n",
			},
			{
				Index: 0, Title: "AI Agent", Slug: "ai-agent",
				Summary:    "**AI agent** gives an agent somewhere to build and version the app.",
				ImportYAML: "#zeropsPreprocessor=on\nproject:\n  name: aurora-agent\nservices:\n  - hostname: appdev\n",
			},
		},
	}
}

func paths(files []File) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, f.Path)
	}
	return out
}

func body(t *testing.T, files []File, path string) string {
	t.Helper()
	for _, f := range files {
		if f.Path == path {
			return f.Body
		}
	}
	t.Fatalf("no file at %q; have %v", path, paths(files))
	return ""
}

func TestBuild_PublishedLayout(t *testing.T) {
	t.Parallel()
	files, err := Build(sample())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// The directory name carries the number and the em dash, because that is
	// how every recipe in zeropsio/recipes is laid out and how they order.
	want := []string{
		"0 — AI Agent/README.md",
		"0 — AI Agent/import.yaml",
		"4 — Small Production/README.md",
		"4 — Small Production/import.yaml",
		"README.md",
	}
	got := paths(files)
	if len(got) != len(want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("files[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuild_ImportYAMLIsCarriedVerbatim(t *testing.T) {
	t.Parallel()
	// The composers upstream own the document, header included; this package
	// must not reformat what they produced.
	files, _ := Build(sample())
	got := body(t, files, "4 — Small Production/import.yaml")
	if !strings.HasPrefix(got, "#zeropsPreprocessor=on\n") {
		t.Errorf("preprocessor header must stay first:\n%s", got)
	}
	if !strings.Contains(got, "hostname: app") {
		t.Errorf("services lost:\n%s", got)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("file should end in a newline")
	}
}

func TestBuild_ExtractMarkersBracketEveryIntro(t *testing.T) {
	t.Parallel()
	files, _ := Build(sample())
	for _, path := range []string{"README.md", "0 — AI Agent/README.md"} {
		text := body(t, files, path)
		start, end := strings.Index(text, introStart), strings.Index(text, introEnd)
		if start == -1 || end == -1 {
			t.Errorf("%s: missing extract markers", path)
			continue
		}
		if start > end {
			t.Errorf("%s: markers out of order", path)
		}
		if strings.TrimSpace(text[start+len(introStart):end]) == "" {
			t.Errorf("%s: markers bracket nothing", path)
		}
	}
}

func TestBuild_RootReadmeListsEveryTierInOrder(t *testing.T) {
	t.Parallel()
	files, _ := Build(sample())
	text := body(t, files, "README.md")
	agent := strings.Index(text, "AI Agent")
	prod := strings.Index(text, "Small Production")
	if agent == -1 || prod == -1 {
		t.Fatalf("tiers not listed:\n%s", text)
	}
	if agent > prod {
		t.Errorf("tiers listed out of order:\n%s", text)
	}
	if !strings.Contains(text, "https://app.zerops.io/recipes/aurora?environment=ai-agent") {
		t.Errorf("deploy link missing or malformed:\n%s", text)
	}
}

func TestBuild_IsDeterministic(t *testing.T) {
	t.Parallel()
	// A recipe that rewrites itself every pass makes a diff that says nothing,
	// so the same input must produce the same bytes whatever order it arrived in.
	first, _ := Build(sample())
	shuffled := sample()
	shuffled.Tiers[0], shuffled.Tiers[1] = shuffled.Tiers[1], shuffled.Tiers[0]
	second, err := Build(shuffled)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(first) != len(second) {
		t.Fatalf("file counts differ: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].Path != second[i].Path || first[i].Body != second[i].Body {
			t.Errorf("file %d differs between runs: %q", i, first[i].Path)
		}
	}
}

func TestBuild_Rejects(t *testing.T) {
	t.Parallel()
	base := sample()
	for _, tc := range []struct {
		name string
		with func(Layout) Layout
	}{
		{"no name", func(l Layout) Layout { l.Name = " "; return l }},
		{"no tiers", func(l Layout) Layout { l.Tiers = nil; return l }},
		{"tier with no slug", func(l Layout) Layout {
			l.Tiers = []Tier{{Index: 0, Title: "T", ImportYAML: "x"}}
			return l
		}},
		{"tier with no import yaml", func(l Layout) Layout {
			l.Tiers = []Tier{{Index: 0, Title: "T", Slug: "t", ImportYAML: "  "}}
			return l
		}},
		{"two tiers in one directory", func(l Layout) Layout {
			l.Tiers = []Tier{
				{Index: 0, Title: "T", Slug: "a", ImportYAML: "x"},
				{Index: 0, Title: "T", Slug: "b", ImportYAML: "x"},
			}
			return l
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Build(tc.with(base)); err == nil {
				t.Error("expected an error, got nil")
			}
		})
	}
}
