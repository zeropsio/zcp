package workflow

import (
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"sync"

	"github.com/zeropsio/zcp/internal/content"
)

// v39 Commit 3 — annotated content-surface example bank.
//
// The writer sub-agent's dispatch brief learned the v38 classification
// taxonomy + seven per-surface single-question tests AND still shipped
// two CRITs editorial-review caught (folk-doctrine + wrong-surface).
// That data argued for tighter context at authoring time rather than
// more teaching up-front. The example bank is the tight context: 20
// annotated files showing concrete pass/fail shapes for each of the
// seven surfaces, seeded from spec-content-surfaces.md §11 counter-
// examples (bad) and v38 post-correction content (good).
//
// The engine samples 2-3 examples per surface into the writer brief's
// pre-loaded input block at dispatch time. Pattern-matching against
// concrete shapes beats parsing prose rules.

// ExampleSurface enumerates the recipe content surfaces an example can
// belong to. Matches spec-content-surfaces.md §2-§8 exactly — do not
// invent new surface names here.
type ExampleSurface string

const (
	ExampleSurfaceGotcha            ExampleSurface = "gotcha"
	ExampleSurfaceIGItem            ExampleSurface = "ig-item"
	ExampleSurfaceIntro             ExampleSurface = "intro"
	ExampleSurfaceClaudeSection     ExampleSurface = "claude-section"
	ExampleSurfaceEnvComment        ExampleSurface = "env-comment"
	ExampleSurfaceZeropsYAMLComment ExampleSurface = "zerops-yaml-comment"
)

// allExampleSurfaces is the frozen surface whitelist. Loader rejects
// frontmatter that names any other value — prevents silent typos.
var allExampleSurfaces = map[ExampleSurface]bool{
	ExampleSurfaceGotcha:            true,
	ExampleSurfaceIGItem:            true,
	ExampleSurfaceIntro:             true,
	ExampleSurfaceClaudeSection:     true,
	ExampleSurfaceEnvComment:        true,
	ExampleSurfaceZeropsYAMLComment: true,
}

// ExampleVerdict tags an example as either (a) a fail case that the
// author should NOT emulate, or (b) a pass case that models the
// surface's contract correctly. Sampling mixes both so the writer sees
// the shape of success AND the shape of each classified failure mode.
type ExampleVerdict string

const (
	ExampleVerdictPass ExampleVerdict = "pass"
	ExampleVerdictFail ExampleVerdict = "fail"
)

var allExampleVerdicts = map[ExampleVerdict]bool{
	ExampleVerdictPass: true,
	ExampleVerdictFail: true,
}

// ContentExample is a single annotated example from the bank. Body
// carries the rendered markdown minus the frontmatter; the frontmatter
// fields are parsed out into the typed struct.
type ContentExample struct {
	Filename string         // e.g. "gotcha_fail_folk_doctrine_resolver_timing.md"
	Surface  ExampleSurface // frontmatter.surface
	Verdict  ExampleVerdict // frontmatter.verdict
	Reason   string         // frontmatter.reason (tag; empty allowed)
	Title    string         // frontmatter.title (short descriptive title)
	Body     string         // rendered markdown after the frontmatter
}

// exampleCache holds the parsed bank. Populated once per process; any
// file that fails to parse raises an error at first access so a
// mis-committed example file fails loudly in tests / startup, not at
// silent sampling time.
var (
	exampleCacheInit    sync.Once
	exampleCacheMu      sync.RWMutex
	exampleCache        []ContentExample
	errExampleCacheInit error
)

// loadExamples walks the embedded examples FS once and populates
// exampleCache. Called by every public helper via sync.Once.
func loadExamples() {
	entries, err := fs.ReadDir(content.ExamplesFS, "examples")
	if err != nil {
		errExampleCacheInit = fmt.Errorf("read examples dir: %w", err)
		return
	}
	var out []ContentExample
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		// Skip the README — it's documentation, not an example.
		if entry.Name() == "README.md" {
			continue
		}
		raw, err := fs.ReadFile(content.ExamplesFS, "examples/"+entry.Name())
		if err != nil {
			errExampleCacheInit = fmt.Errorf("read example %s: %w", entry.Name(), err)
			return
		}
		ex, err := parseExample(entry.Name(), string(raw))
		if err != nil {
			errExampleCacheInit = fmt.Errorf("parse example %s: %w", entry.Name(), err)
			return
		}
		out = append(out, ex)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Filename < out[j].Filename })
	exampleCacheMu.Lock()
	exampleCache = out
	exampleCacheMu.Unlock()
}

// parseExample splits the frontmatter from the body and validates that
// every required field is present and that surface / verdict are in the
// whitelists. No silent defaults — a missing or invalid field is an
// error so commits that break the bank fail CI, not dispatch.
func parseExample(filename, raw string) (ContentExample, error) {
	// Frontmatter is delimited by '---' lines. The file MUST start
	// with "---\n" (no leading blank lines).
	if !strings.HasPrefix(raw, "---\n") {
		return ContentExample{}, fmt.Errorf("missing frontmatter opener '---' at start of file")
	}
	rest := strings.TrimPrefix(raw, "---\n")
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return ContentExample{}, fmt.Errorf("missing frontmatter closer '---'")
	}
	header := rest[:end]
	body := strings.TrimPrefix(rest[end:], "\n---\n")

	ex := ContentExample{Filename: filename, Body: body}
	for line := range strings.SplitSeq(header, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			return ContentExample{}, fmt.Errorf("malformed frontmatter line %q (want key: value)", line)
		}
		key = strings.TrimSpace(key)
		val = strings.Trim(strings.TrimSpace(val), `"`)
		switch key {
		case "surface":
			s := ExampleSurface(val)
			if !allExampleSurfaces[s] {
				return ContentExample{}, fmt.Errorf("unknown surface %q (see examples.go ExampleSurface constants)", val)
			}
			ex.Surface = s
		case "verdict":
			v := ExampleVerdict(val)
			if !allExampleVerdicts[v] {
				return ContentExample{}, fmt.Errorf("unknown verdict %q (expected pass|fail)", val)
			}
			ex.Verdict = v
		case "reason":
			ex.Reason = val
		case "title":
			ex.Title = val
		default:
			return ContentExample{}, fmt.Errorf("unknown frontmatter key %q (expected surface|verdict|reason|title)", key)
		}
	}

	if ex.Surface == "" {
		return ContentExample{}, fmt.Errorf("frontmatter missing required field 'surface'")
	}
	if ex.Verdict == "" {
		return ContentExample{}, fmt.Errorf("frontmatter missing required field 'verdict'")
	}
	if ex.Title == "" {
		return ContentExample{}, fmt.Errorf("frontmatter missing required field 'title'")
	}
	return ex, nil
}

// AllExamples returns every parsed example in the bank, sorted by
// filename. Primarily used by tests (frontmatter validation, coverage
// checks). Dispatch-time consumers want SampleFor instead.
func AllExamples() ([]ContentExample, error) {
	exampleCacheInit.Do(loadExamples)
	if errExampleCacheInit != nil {
		return nil, errExampleCacheInit
	}
	exampleCacheMu.RLock()
	defer exampleCacheMu.RUnlock()
	out := make([]ContentExample, len(exampleCache))
	copy(out, exampleCache)
	return out, nil
}

// SampleFor returns up to n examples for the given surface, mixing
// pass + fail verdicts when available. Sampling is deterministic per
// process run (sorted-by-filename order) — the engine dispatches are
// reproducible and sub-agent briefs are byte-stable across retries.
//
// When the bank has fewer than n examples for the surface, returns
// whatever is present. When the surface has examples of only one
// verdict, returns the full single-verdict set up to n.
func SampleFor(surface ExampleSurface, n int) ([]ContentExample, error) {
	if n <= 0 {
		return nil, nil
	}
	all, err := AllExamples()
	if err != nil {
		return nil, err
	}
	var pass, fail []ContentExample
	for _, ex := range all {
		if ex.Surface != surface {
			continue
		}
		if ex.Verdict == ExampleVerdictPass {
			pass = append(pass, ex)
		} else {
			fail = append(fail, ex)
		}
	}

	out := make([]ContentExample, 0, n)
	pi, fi := 0, 0
	// Alternate pass/fail until either side runs out.
	for len(out) < n && (pi < len(pass) || fi < len(fail)) {
		if pi < len(pass) && len(out) < n {
			out = append(out, pass[pi])
			pi++
		}
		if fi < len(fail) && len(out) < n {
			out = append(out, fail[fi])
			fi++
		}
	}
	return out, nil
}

// RenderExampleBlock formats a slice of examples as a markdown block
// suitable for dropping into a sub-agent brief's input section. Each
// example is rendered with a ### heading containing the verdict +
// title, a one-line reason tag, and the example body.
func RenderExampleBlock(examples []ContentExample) string {
	if len(examples) == 0 {
		return ""
	}
	var b strings.Builder
	for i, ex := range examples {
		if i > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "### [%s] %s\n", strings.ToUpper(string(ex.Verdict)), ex.Title)
		if ex.Reason != "" {
			fmt.Fprintf(&b, "_reason: %s_\n\n", ex.Reason)
		} else {
			b.WriteString("\n")
		}
		b.WriteString(strings.TrimSpace(ex.Body))
	}
	return b.String()
}

// surfacesRelevantToWriter returns the ordered list of surfaces the
// writer sub-agent is authoring — every surface the writer touches
// directly, excluding ones owned by other roles (intro = main agent
// via GenerateRecipeREADME; env-comment = main agent at generate-
// finalize). The writer brief input block samples each surface in
// this list.
func surfacesRelevantToWriter() []ExampleSurface {
	return []ExampleSurface{
		ExampleSurfaceGotcha,
		ExampleSurfaceIGItem,
		ExampleSurfaceClaudeSection,
		ExampleSurfaceZeropsYAMLComment,
	}
}

// BuildWriterExampleInputBlock renders the annotated-example input
// section the engine appends to the writer's dispatch brief. Samples
// up to `perSurface` examples per surface relevant to the writer
// (mixing pass + fail). Returns empty string if the bank is empty or
// `perSurface` is 0 — callers handle the empty case by omitting the
// section rather than emitting a stub.
func BuildWriterExampleInputBlock(perSurface int) (string, error) {
	if perSurface <= 0 {
		return "", nil
	}
	var b strings.Builder
	first := true
	for _, surface := range surfacesRelevantToWriter() {
		samples, err := SampleFor(surface, perSurface)
		if err != nil {
			return "", fmt.Errorf("sample %s: %w", surface, err)
		}
		if len(samples) == 0 {
			continue
		}
		if !first {
			b.WriteString("\n\n")
		}
		first = false
		fmt.Fprintf(&b, "## %s — annotated examples\n\n", surface)
		b.WriteString("Pattern-match your output against these. FAIL examples show shapes to avoid; PASS examples model the surface's contract. If a bullet you plan to write looks like a FAIL example, classify and reroute before publishing.\n\n")
		b.WriteString(RenderExampleBlock(samples))
	}
	return b.String(), nil
}
