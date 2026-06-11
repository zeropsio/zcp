package recipe

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Run-31 Fix #1 closure — multi-file briefs.
//
// The cap-treadmill across runs 22-31 (44→48→52→56→60→64→72→76→66 KB)
// kept losing to the Read-tool 25K-real-token hard cap because briefs
// in production carry full fact corpora plus the embedded-parent
// fallback while unit-test fixtures used nil facts. v9.72.0's content-
// refinement swaps + byte-cap shrink saved ~10 KB but real briefs landed
// at 78-94 KB / 36-39K real tokens — still over.
//
// Multi-file shape: the composer emits an `index.md` plus N part files
// under `<outputRoot>/.briefs/<kind>-<codebase>-<unixnano>/`. Index
// lists parts in read order with one-line descriptions. The dispatch
// envelope returns `briefPath` pointing at the index; sub-agents read
// the index, then each part in order before authoring. Each part is
// bounded by `PerPartTokenCeiling`; the composed brief as a whole is
// unbounded.
//
// The split boundaries are author-controlled at semantic group edges
// (phase entry, platform, cross-service, yaml style, context); the
// runtime cap is a safety net that fires only when a single semantic
// group exceeds the per-part budget. Composers call StartPart() at
// known boundaries and Persist() at the end; the partWriter handles
// disk I/O.

// PerPartTokenCeiling is the per-part cap for multi-file briefs. Holds
// each emitted part comfortably under the Read-tool 25K real-token hard
// cap with margin for JSON envelope inflation. At 22000 estimated tokens
// (under len/2 estimator, ~44 KB bytes), even a 2.5 cpt worst case
// encodes to ~17.6K real tokens — half the Read-tool limit.
//
// Hold the token-side authority because the architectural answer is "fit
// under the Read-tool's token cap"; the byte-side cap is derived. The
// part writer enforces the token-side cap; the byte-side `PartFileCap`
// constant exists for byte-arithmetic call sites that don't want to
// re-multiply.
const PerPartTokenCeiling = 22000

// PartFileCap is the byte equivalent of `PerPartTokenCeiling` under the
// `len/2` estimator. Composers that work in bytes can use this directly
// without re-multiplying.
const PartFileCap = PerPartTokenCeiling * charsPerTokenEstimate // 44 KB

// briefPart is one (slug, description, body) tuple persisted to disk
// as a single part file. The slug is a kebab identifier rendered into
// the filename (`part-<n>-<slug>.md`); the description is one line in
// the index that tells the sub-agent why to read this part. Position
// in the parts slice determines the read order — composers append in
// the order the agent should Read.
type briefPart struct {
	slug string
	desc string
	body strings.Builder
}

// partWriter accumulates content into the current part. Composers call
// StartPart() at semantic boundaries; Write/WriteAtom append to the
// current part. Persist() writes every part to disk under
// `<outputRoot>/.briefs/<kind>-<codebase>/` and emits the index.md.
//
// The runtime per-part cap is enforced inside Write(): when the current
// part already approaches `PerPartTokenCeiling` and the appended chunk
// would push it past, Write returns ErrPartCapExceeded so the composer
// can split the offending semantic group into a sub-part. In normal
// operation the semantic boundaries the composer authors keep parts
// well under the cap; the runtime check is defense-in-depth.
//
// usedSlugs tracks every slug seen via StartPart so the writer can
// refuse a duplicate. Two parts sharing a slug produces filenames
// `part-N-<slug>.md` that are unique by N but ambiguous by slug
// semantics (which "context-tail" is THIS one?); refusing at composer
// edit time surfaces the bug instead of letting it ship.
type partWriter struct {
	parts     []*briefPart
	usedSlugs map[string]bool
}

// BriefMetrics describes the disk-emission shape of a multi-file brief.
// Populated by Persist alongside the index/part paths so the dispatch
// envelope and downstream observers (sim runs, drift detection) can
// see "brief composed: N parts, M total bytes, K max-part bytes"
// without re-stat-ing every file. Run-31 multi-file review Fix #4 —
// without this, the only signal the wire envelope carries is the index
// file's stat.Size() which says nothing about the parts the agent
// will actually Read.
type BriefMetrics struct {
	// Parts is the count of part-*.md files written (excludes index.md).
	Parts int `json:"parts"`
	// TotalBytes is the sum of every persisted file (index + parts).
	TotalBytes int `json:"totalBytes"`
	// MaxPartBytes is the byte size of the largest single part file.
	MaxPartBytes int `json:"maxPartBytes"`
	// MaxPartEstimatedTokens is EstimateTokens applied to the largest
	// single part body. Used as the leading drift indicator: if this
	// rises past PerPartTokenCeiling on any production run the cap-
	// fire recovery is the next thing to verify.
	MaxPartEstimatedTokens int `json:"maxPartEstimatedTokens"`
}

// ErrPartCapExceeded is returned by Write when the current part would
// exceed PerPartTokenCeiling on append. Composers handle by calling
// StartPart() with a continuation slug and re-attempting the write on
// the new part.
var ErrPartCapExceeded = errors.New("part write would exceed PerPartTokenCeiling")

// newPartWriter returns an empty partWriter. The first StartPart() call
// initializes the first part.
func newPartWriter() *partWriter {
	return &partWriter{usedSlugs: map[string]bool{}}
}

// StartPart finalizes the current part and begins a new one with the
// given slug + description. Slug must be a kebab identifier (a-z, 0-9,
// hyphens); it ends up in the filename. Description is a single line
// rendered into the index.
//
// Returns an error when slug duplicates a slug that was already used in
// this composer call. Run-31 multi-file review Fix #5 — duplicates leak
// when both the facts-overflow and cross-codebase-overflow recovery
// paths fire in the same call, producing two part-N-context-tail.md
// files that share semantics. Refusing at the boundary surfaces the
// bug so the composer renames one path explicitly.
func (w *partWriter) StartPart(slug, desc string) error {
	if w.usedSlugs == nil {
		w.usedSlugs = map[string]bool{}
	}
	if w.usedSlugs[slug] {
		return fmt.Errorf("partWriter.StartPart: duplicate slug %q (each part needs a distinct slug for filename + index semantics)", slug)
	}
	w.usedSlugs[slug] = true
	w.parts = append(w.parts, &briefPart{slug: slug, desc: desc})
	return nil
}

// WriteOrSplit appends body to the current part when it fits; on cap
// overflow it opens a fresh part with the given slug + description and
// writes the body there. Used by composers that have already exited the
// "this lives on the current part" semantic group and don't need to
// retry on the prior part — the body MUST land on either the current
// part or a single fresh part. If the body itself exceeds
// PerPartTokenCeiling on a freshly-opened part the call returns
// ErrPartCapExceeded; that's caller-fatal (the body is too big for one
// part full stop) and means the composer's content composition is wrong.
//
// Run-31 multi-file review Fix #1 — closes the post-recovery "second
// overflow" hole where the composer landed three more sections on a
// part that had already been cap-recovered once. Without this helper
// each callsite would have to repeat the open-fresh-part-on-overflow
// dance and one of them was missed.
func (w *partWriter) WriteOrSplit(slug, desc, body string) error {
	if body == "" {
		return nil
	}
	if w.current() == nil {
		// First write — open the part with the given slug.
		if err := w.StartPart(slug, desc); err != nil {
			return err
		}
		return w.Write(body)
	}
	err := w.Write(body)
	if !errors.Is(err, ErrPartCapExceeded) {
		return err
	}
	// Open a fresh part and retry. Slug uniqueness is enforced by
	// StartPart; if the caller passes a slug already used, the error
	// surfaces here.
	if startErr := w.StartPart(slug, desc); startErr != nil {
		return startErr
	}
	return w.Write(body)
}

// current returns the part being written, or nil if StartPart hasn't
// been called yet. Internal helper.
func (w *partWriter) current() *briefPart {
	if len(w.parts) == 0 {
		return nil
	}
	return w.parts[len(w.parts)-1]
}

// Write appends s to the current part. Returns ErrPartCapExceeded when
// the appended chunk would push the part past PerPartTokenCeiling — the
// composer handles by calling StartPart() and re-writing on the new
// part. The cap check uses byte length under the len/2 estimator so it
// matches EstimateTokens semantics.
//
// Empty writes are no-ops (no cap check fires); StartPart is required
// before the first non-empty Write.
func (w *partWriter) Write(s string) error {
	if s == "" {
		return nil
	}
	cur := w.current()
	if cur == nil {
		return errors.New("partWriter.Write: StartPart not called")
	}
	if EstimateTokens(cur.body.String()+s) >= PerPartTokenCeiling {
		return ErrPartCapExceeded
	}
	cur.body.WriteString(s)
	return nil
}

// WriteAtom loads the named atom and appends it to the current part
// followed by a blank-line separator. Missing atoms (read error) return
// the error wrapped — this is unrecoverable; composers MUST not silently
// skip atoms.
func (w *partWriter) WriteAtom(atomPath string) error {
	body, err := readAtom(atomPath)
	if err != nil {
		return fmt.Errorf("partWriter.WriteAtom %q: %w", atomPath, err)
	}
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	body += "\n"
	if wErr := w.Write(body); wErr != nil {
		return fmt.Errorf("partWriter.WriteAtom %q: %w", atomPath, wErr)
	}
	return nil
}

// WriteAtomIfFound is WriteAtom but silently skips a missing atom (the
// composer's appendAtomIfFound shape). Returns (true, nil) on
// successful append, (false, nil) when the atom doesn't exist, and
// (true, err) when the atom exists but the per-part cap fires —
// callers MUST react to err by calling StartPart() and re-attempting,
// otherwise the brief silently drops content.
func (w *partWriter) WriteAtomIfFound(atomPath string) (found bool, err error) {
	body, rErr := readAtom(atomPath)
	if rErr != nil {
		return false, nil //nolint:nilerr // missing atom is the WriteAtomIfFound contract — returns (false, nil) by design.
	}
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	body += "\n"
	if wErr := w.Write(body); wErr != nil {
		return true, wErr
	}
	return true, nil
}

// WriteRequiredAtom is WriteAtom but treats both a missing-atom error
// (read failure) and a cap-exceeded error as fatal — composers calling
// this assert that (a) the atom MUST exist and (b) its semantic part
// has been sized to fit. When the cap fires here, the part-split is
// wrong; surface the error so the composer's StartPart boundaries get
// fixed at edit time, not silently at runtime.
func (w *partWriter) WriteRequiredAtom(atomPath string) error {
	body, err := readAtom(atomPath)
	if err != nil {
		return fmt.Errorf("partWriter.WriteRequiredAtom %q: %w", atomPath, err)
	}
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	body += "\n"
	if wErr := w.Write(body); wErr != nil {
		return fmt.Errorf("partWriter.WriteRequiredAtom %q: %w (semantic part split may be wrong)", atomPath, wErr)
	}
	return nil
}

// WriteOptionalAtom appends an atom that's allowed to be missing.
// Returns nil when the atom doesn't exist (silent skip). Returns an
// error when the atom exists but the per-part cap fires — same
// semantics as WriteRequiredAtom for the cap path.
func (w *partWriter) WriteOptionalAtom(atomPath string) error {
	body, err := readAtom(atomPath)
	if err != nil {
		return nil //nolint:nilerr // missing atom is the WriteOptionalAtom contract — silent skip by design.
	}
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	body += "\n"
	if wErr := w.Write(body); wErr != nil {
		return fmt.Errorf("partWriter.WriteOptionalAtom %q: %w (semantic part split may be wrong)", atomPath, wErr)
	}
	return nil
}

// Persist writes every part file plus index.md under
// `<outputRoot>/.briefs/<kind>-<codebase>/` atomically (temp + rename
// per file). Returns the absolute index.md path, part file paths in
// read order, and a BriefMetrics describing the emission shape. The
// header parameter is rendered above the index's "Read order" section;
// the closingNotes are rendered below it.
//
// Empty kind/codebase falls back to "phase" — same shape as
// writeBriefToDisk for legacy single-file briefs.
//
// Run-31 multi-file review Fix #7 — the dir name is deterministic from
// kind + codebase. Re-emitting a brief with identical inputs overwrites
// the prior dir; sim runs that compose the same plan twice in a row
// produce byte-identical paths. Earlier `time.Now().UnixNano()` suffix
// produced fresh paths on every call which broke replay diffability.
//
// Run-31 multi-file review Fix #8 — on any persistence failure, the
// brief dir is removed before returning. Earlier shape returned on
// first error after writing N-1 parts, leaving a half-state dir an
// agent could Read.
//
// Run-31 multi-file review Fix #2 — index body is asserted under
// PerPartTokenCeiling before write. With many parts (each absolute
// path ~80-120 bytes plus description) the index could approach the
// per-part cap; we want to know loudly if it does so the composer
// shortens descriptions or splits.
func (w *partWriter) Persist(outputRoot, header, closingNotes string, kind BriefKind, codebase string) (indexPath string, partPaths []string, metrics BriefMetrics, err error) {
	if len(w.parts) == 0 {
		return "", nil, BriefMetrics{}, errors.New("partWriter.Persist: no parts (StartPart not called)")
	}
	cb := codebase
	if cb == "" {
		cb = "phase"
	}
	dirName := fmt.Sprintf("%s-%s", kind, cb)
	dir := filepath.Join(outputRoot, ".briefs", dirName)
	// Atomic-cleanup discipline: any error path below removes the brief
	// dir. The dir was either empty pre-call (first emit) or carried
	// the prior emission (re-emit) — caller's contract is that a re-emit
	// in the same session is idempotent, so wiping the dir on partial
	// failure is correct: the prior content is gone too, by design, and
	// the caller has either succeeded fully or has nothing.
	cleanup := func() {
		_ = os.RemoveAll(dir)
	}
	// Remove any prior emission so the deterministic-paths invariant
	// holds (no leftover part-N-foo.md from a longer prior emission).
	if rmErr := os.RemoveAll(dir); rmErr != nil {
		return "", nil, BriefMetrics{}, fmt.Errorf("clear prior briefs dir: %w", rmErr)
	}
	if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
		return "", nil, BriefMetrics{}, fmt.Errorf("create briefs dir: %w", mkErr)
	}

	partPaths = make([]string, 0, len(w.parts))
	maxPartBytes := 0
	totalBytes := 0
	for i, p := range w.parts {
		name := fmt.Sprintf("part-%d-%s.md", i+1, p.slug)
		dst := filepath.Join(dir, name)
		body := p.body.String()
		if writeErr := writePartFileAtomic(dir, dst, body); writeErr != nil {
			cleanup()
			return "", nil, BriefMetrics{}, writeErr
		}
		partPaths = append(partPaths, dst)
		if n := len(body); n > maxPartBytes {
			maxPartBytes = n
		}
		totalBytes += len(body)
	}

	idx := buildIndexBody(header, closingNotes, w.parts, partPaths)
	if EstimateTokens(idx) >= PerPartTokenCeiling {
		cleanup()
		return "", nil, BriefMetrics{}, fmt.Errorf("partWriter.Persist: index body %d estimated tokens >= PerPartTokenCeiling %d (too many parts or descriptions too long)", EstimateTokens(idx), PerPartTokenCeiling)
	}
	indexPath = filepath.Join(dir, "index.md")
	if writeErr := writePartFileAtomic(dir, indexPath, idx); writeErr != nil {
		cleanup()
		return "", nil, BriefMetrics{}, writeErr
	}
	totalBytes += len(idx)

	metrics = BriefMetrics{
		Parts:                  len(w.parts),
		TotalBytes:             totalBytes,
		MaxPartBytes:           maxPartBytes,
		MaxPartEstimatedTokens: EstimateTokens(maxPartBodies(w.parts)),
	}
	return indexPath, partPaths, metrics, nil
}

// maxPartBodies returns the largest part body so the metrics estimator
// runs against the same body that produced MaxPartBytes. (Plain max-by-
// len walk — extracted for test readability.)
func maxPartBodies(parts []*briefPart) string {
	var best string
	for _, p := range parts {
		body := p.body.String()
		if len(body) > len(best) {
			best = body
		}
	}
	return best
}

// buildIndexBody composes the index.md content. Header is rendered as
// the leading section (recipe-level context, codebase identity); the
// "Read order" section lists every part with absolute path + one-line
// description; closingNotes follow (close-phase invocation).
func buildIndexBody(header, closingNotes string, parts []*briefPart, partPaths []string) string {
	var b strings.Builder
	b.WriteString(header)
	if !strings.HasSuffix(header, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString("\n## Read order\n\n")
	b.WriteString("This brief is composed from multiple part files so each fits under the Read-tool single-shot token cap. Read each path below in order BEFORE you author any fragment. Skipping a part means missing teaching that landed there for a reason.\n\n")
	for i, p := range parts {
		fmt.Fprintf(&b, "%d. `%s` — %s\n", i+1, partPaths[i], p.desc)
	}
	b.WriteByte('\n')
	if closingNotes != "" {
		b.WriteString(closingNotes)
		if !strings.HasSuffix(closingNotes, "\n") {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// writePartFileAtomic writes body to dst via temp + sync + rename so a
// partial write never leaves a half-state file. Mirrors the
// writeBriefToDisk pattern. Indirection through writePartFileAtomicHook
// lets tests inject failure mid-Persist without filesystem gymnastics.
func writePartFileAtomic(dir, dst, body string) error {
	if writePartFileAtomicHook != nil {
		return writePartFileAtomicHook(dir, dst, body)
	}
	return writePartFileAtomicReal(dir, dst, body)
}

// writePartFileAtomicHook is the test-injectable override; production
// callers leave it nil. Set during a test via t.Cleanup.
var writePartFileAtomicHook func(dir, dst, body string) error

func writePartFileAtomicReal(dir, dst, body string) error {
	tmp, err := os.CreateTemp(dir, ".brief.tmp.*")
	if err != nil {
		return fmt.Errorf("create temp brief: %w", err)
	}
	tmpPath := tmp.Name()
	if _, wErr := tmp.WriteString(body); wErr != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp brief: %w", wErr)
	}
	if sErr := tmp.Sync(); sErr != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("sync temp brief: %w", sErr)
	}
	if cErr := tmp.Close(); cErr != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp brief: %w", cErr)
	}
	if rErr := os.Rename(tmpPath, dst); rErr != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp brief: %w", rErr)
	}
	return nil
}
