// Package console: HTML pages (docs/spec-eval-farm.md §8.3 FM-51). This
// file holds what every page shares — the embedded template set, the
// template func map, formatting helpers and renderPage; each page's own
// handler and page-data type live in pages_home.go, pages_batch.go,
// pages_run.go and pages_findings.go. Every page is rendered through
// html/template (auto-escaping) from the read models in view.go, api.go and
// batches.go — this file adds no store writes and no new bucket keys.
package console

import (
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/eval/farm"
	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

//go:embed assets/layout.html assets/terms.html assets/batches.html assets/batch.html assets/run.html assets/findings.html
var pagesHTMLSrc embed.FS

// pageFuncs are the plain-string-returning helpers pages.html templates use.
// None of them return template.HTML — every value they produce still goes
// through {{...}}'s normal auto-escaping, so a hostile value from the
// bucket (a step result, a model-authored quote) is never trusted as markup
// (TestPages_HostileTextEscaped).
var pageFuncs = template.FuncMap{
	"fmtTime":     fmtTime,
	"fmtCost":     func(v float64) string { return fmt.Sprintf("$%.2f", v) },
	"fmtDuration": fmtDuration,
	"verdictIcon": verdictIcon,
	"verdictKey":  verdictKey,
	"shortTool": func(name string) string {
		return strings.TrimPrefix(strings.TrimPrefix(name, "mcp__zerops__"), "mcp__")
	},
	"preview":    preview,
	"capRaw":     capRaw,
	"joinInts":   joinInts,
	"stepAnchor": func(n int) string { return fmt.Sprintf("s%d", n) },
	"verifiedMark": func(v bool) string {
		if v {
			return "verified"
		}
		return "unverified"
	},
	"verdictClass": func(v string) string {
		switch v {
		case farm.VerdictPassed, farm.VerdictFailed, farm.VerdictBlocked, verdictRunning:
			return "verdict verdict-" + v
		case farm.VerdictNotRun:
			return "verdict verdict-not-run"
		default:
			return "verdict verdict-other"
		}
	},
	"severityClass": func(v string) string {
		switch v {
		case observer.SeverityHigh, observer.SeverityMedium, observer.SeverityLow:
			return "severity severity-" + v
		default:
			return "severity severity-other"
		}
	},
	"runLink": func(runID string, steps []int) string {
		if len(steps) == 0 {
			return "/r/" + runID
		}
		// steps is always FindingItem.Steps (api.go), which already
		// excludes the step-0 CHECKS citation — steps[0] is never 0 here.
		return fmt.Sprintf("/r/%s#s%d", runID, steps[0])
	},
	// verdictLabel/verdictTooltip/severityLabel/severityTooltip/causeLabel/
	// causeClass/assessmentOutcomeLabel/assessmentStateText are labels.go's
	// vocabulary helpers (§8.8 FM-56), registered here so /terms and every
	// later page template can call them.
	"verdictLabel":           verdictLabel,
	"verdictTooltip":         verdictTooltip,
	"severityLabel":          severityLabel,
	"severityTooltip":        severityTooltip,
	"causeLabel":             causeLabel,
	"causeClass":             causeClass,
	"assessmentOutcomeLabel": assessmentOutcomeLabel,
	"assessmentOutcomeClass": assessmentOutcomeClass,
	"assessmentStateLabel":   assessmentStateLabel,
	"assessmentStateTooltip": assessmentStateTooltip,
}

// fmtTime renders a timestamp for people: day, month, time, UTC.
func fmtTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.UTC().Format("2 Jan 2006, 15:04 UTC")
}

// fmtDuration renders seconds as "45s", "8m 04s" or "1h 02m".
func fmtDuration(sec float64) string {
	d := time.Duration(sec * float64(time.Second)).Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm %02ds", int(d.Minutes()), int(d.Seconds())%60)
	default:
		return fmt.Sprintf("%dh %02dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

// verdictIcon is the text mark shown next to every verdict label, so a
// verdict never depends on colour alone.
func verdictIcon(v string) string {
	switch v {
	case farm.VerdictPassed:
		return "✓"
	case farm.VerdictFailed:
		return "✗"
	case farm.VerdictBlocked:
		return "!"
	case verdictRunning:
		return "…"
	default:
		return "–"
	}
}

// verdictRank orders a batch page's runs problems first: failed, blocked,
// running, not-run, passed, anything else.
func verdictRank(v string) int {
	switch v {
	case farm.VerdictFailed:
		return 0
	case farm.VerdictBlocked:
		return 1
	case verdictRunning:
		return 2
	case farm.VerdictNotRun:
		return 3
	case farm.VerdictPassed:
		return 4
	default:
		return 5
	}
}

// verdictKey maps a verdict onto the stylesheet's known verdict classes.
func verdictKey(v string) string {
	switch v {
	case farm.VerdictPassed, farm.VerdictFailed, farm.VerdictBlocked, verdictRunning, farm.VerdictNotRun:
		return v
	default:
		return "other"
	}
}

// preview is the one-line summary of a step: whitespace collapsed, cut at
// n runes with an ellipsis.
func preview(s string, n int) string {
	flat := strings.Join(strings.Fields(s), " ")
	r := []rune(flat)
	if len(r) <= n {
		return flat
	}
	return string(r[:n]) + "…"
}

// observerRawCap is how much of a failed observation's raw answer the run
// page shows (item 3, matching §7.5's own "the first 2,000 chars of raw").
const observerRawCap = 2000

// capRaw caps s at observerRawCap runes, for the run page's collapsed raw-
// answer preview on an "unparsed" observation.
func capRaw(s string) string {
	r := []rune(s)
	if len(r) <= observerRawCap {
		return s
	}
	return string(r[:observerRawCap])
}

func joinInts(ns []int) string {
	parts := make([]string, len(ns))
	for i, n := range ns {
		parts[i] = fmt.Sprint(n)
	}
	return strings.Join(parts, ", ")
}

var pagesTemplate = template.Must(template.New("pages").Funcs(pageFuncs).ParseFS(pagesHTMLSrc, "assets/*.html"))

func renderPage(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pagesTemplate.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "template: "+err.Error(), http.StatusInternalServerError)
	}
}

// Navigation items for the top nav (§8.3 FM-51) — pageMeta.Nav's values.
// Each is matched against the current page so exactly one link carries
// aria-current="page"; a detail page (/b/<batch>, /r/<runId>) is under
// none of them, so its Nav is "".
const (
	navOverview = "overview"
	navProblems = "problems"
	navFindings = "findings"
	navTerms    = "terms"
)

// pageMeta carries every page's shared layout inputs (§8.3 FM-51): the
// title, which top-nav item is current, the observer status line, an
// optional notice callout from a ?notice= action-result code (§8.5
// FM-53), and whether the page should auto-refresh while one of its own
// jobs is in flight (§8.5: "the page carries <meta http-equiv=refresh
// content=20>"). Every page handler builds one via Server.pageMeta and
// renders it through layout.html's "head" template as its own Meta field.
type pageMeta struct {
	Title    string
	Nav      string
	Observer observerStatus
	Notice   *noticeView
	Refresh  bool
}

// observerStatus is pageMeta.Observer: the rendered §8.3 status line text,
// and whether the console's Assess forms should stay hidden — true only
// for the "unavailable" case (§8.3: "in which last case every Assess form
// is hidden"). Wiring Hidden into an Assess form is left to the slice that
// restructures batch.html/run.html (item 5's closing note) — it is exposed
// here so that slice has it ready.
type observerStatus struct {
	Text   string
	Hidden bool
}

// pageMeta builds one page's layout inputs: the observer status line from
// the server's static config and the queue's live counts, and the notice
// callout from the request's ?notice= (and, for "queued", ?n=) query
// parameters. refresh is the caller's own §8.5 "a job of this page is in
// flight" verdict — each page handler knows its own scope (one run, one
// batch, or the whole console) better than this shared helper does.
func (s *Server) pageMeta(r *http.Request, title, nav string, refresh bool) pageMeta {
	return pageMeta{
		Title:    title,
		Nav:      nav,
		Observer: s.observerStatusLine(),
		Notice:   noticeFromQuery(r.URL.Query()),
		Refresh:  refresh,
	}
}

// observerStatusLine resolves §8.3 FM-51's observer status line, in the
// same precedence actions.go's observerUnavailable uses for the 503 cases
// (credential/claude-path checked before the kill switch — a missing
// credential or an unresolved claude path makes actions unavailable
// whatever the kill switch says): a missing credential or an unresolved
// --claude path reads "unavailable — <reason>" (Assess forms hidden);
// otherwise the kill switch (ZCP_FARM_OBSERVER=off) reads "off … Assess
// buttons still work"; otherwise "on" with the queue's live counts
// (worker.go's Queue.Stats).
func (s *Server) observerStatusLine() observerStatus {
	const prefix = "Automatic assessment: "
	switch {
	case s.cfg.ObserverCredentialMissing:
		return observerStatus{Text: prefix + "unavailable — credential missing", Hidden: true}
	case s.cfg.ObserverClaudePathUnresolved:
		return observerStatus{Text: prefix + "unavailable — claude not found", Hidden: true}
	case s.cfg.ObserverDisabled:
		return observerStatus{Text: prefix + "off on this console (ZCP_FARM_OBSERVER=off) — Assess buttons still work"}
	default:
		n := 0
		if s.cfg.Queue != nil {
			queued, running := s.cfg.Queue.Stats()
			n = queued + running
		}
		return observerStatus{Text: fmt.Sprintf("%son · %s · %d queued/running", prefix, observer.DefaultModel, n)}
	}
}

// noticeView is pageMeta.Notice: the rendered text for one ?notice=<code>
// action-result code (§8.5 FM-53's queued/busy/not-finished/bad-model/
// unavailable). noticeFromQuery returns nil for no code or an unrecognized
// one (§8.3: "an unknown code renders nothing").
type noticeView struct{ Text string }

func noticeFromQuery(q url.Values) *noticeView {
	switch q.Get("notice") {
	case "queued":
		n, _ := strconv.Atoi(q.Get("n"))
		if n < 1 {
			n = 1
		}
		plural := "s"
		if n == 1 {
			plural = ""
		}
		return &noticeView{Text: fmt.Sprintf("Queued %d run%s for assessment.", n, plural)}
	case "busy":
		return &noticeView{Text: "Already queued or running — try again once it settles."}
	case "not-finished":
		return &noticeView{Text: "That run hasn't finished yet — nothing to assess."}
	case "bad-model":
		return &noticeView{Text: "That model isn't one of the ones this console supports."}
	case "unavailable":
		return &noticeView{Text: "The observer isn't available right now."}
	default:
		return nil
	}
}
