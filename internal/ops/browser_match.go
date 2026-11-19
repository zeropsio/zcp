package ops

import (
	"math/bits"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// The matcher of browserRedactor (browser_redact.go): it finds a value in
// text however a listed form spells it, in one pass over the text.
//
// A value is its characters in order. A character's spellings (runeForms)
// are kept as a trie of tokens — a token is one byte of a spelling — and a
// token may stand in the text as its byte, as that byte's %HH escape, as `+`
// for a space, or as the HTML entity of its character (matcher.starts,
// matchRun.step). The text is read once: every reading of it that is still
// a prefix of some value is one thread, threads that have come to the same
// place are one thread (the earliest start wins, threadSet), and each
// character of the text moves every thread once. So no text is walked twice, and the work is
// at most the characters read times the threads alive, counted against the
// redactor's budget.

// matchToken is one byte of a spelling. raw holds the byte it is when it
// stands as itself — two for a hex digit that is a letter, which stands in
// either case — and none for a token only an entity spells. entity is the
// character whose HTML entity may stand for it, -1 for none.
type matchToken struct {
	raw    [2]byte
	rawLen uint8
	entity rune
}

func literalToken(b byte) matchToken {
	return matchToken{raw: [2]byte{b}, rawLen: 1, entity: rune(b)}
}

// hexToken is one hex digit of a \uXXXX escape, in either case.
func hexToken(nibble uint16) matchToken {
	const digits = "0123456789abcdef"
	lower := digits[nibble&0xf]
	if lower >= 'a' {
		return matchToken{raw: [2]byte{lower, lower - 'a' + 'A'}, rawLen: 2, entity: -1}
	}
	return matchToken{raw: [2]byte{lower}, rawLen: 1, entity: -1}
}

func (t matchToken) holds(b byte) bool {
	return (t.rawLen > 0 && t.raw[0] == b) || (t.rawLen > 1 && t.raw[1] == b)
}

// matchNode is one token of a character's spellings, after the tokens on the
// way from the root; terminal says a spelling ends with it.
type matchNode struct {
	token    matchToken
	children []int32
	terminal bool
}

// runeTrie is every spelling of one character. Node 0 is the root, which
// holds no token.
type runeTrie struct {
	nodes []matchNode
}

// runeForms are the spellings of one character of a value, before any byte
// of them is percent-encoded or written as an entity: the character itself;
// the escape a JSON or JavaScript string writes it with — a short one where
// the language has it, \uXXXX for any character (a surrogate pair where it
// takes one) and \xHH for one up to U+00FF; and those escapes JSON-escaped
// again, up to browserRedactJSONDepth levels in all.
func runeForms(r rune) [][]matchToken {
	var raw []matchToken
	if r < utf8.RuneSelf {
		raw = []matchToken{literalToken(byte(r))}
	} else {
		for _, b := range []byte(string(r)) {
			raw = append(raw, matchToken{raw: [2]byte{b}, rawLen: 1, entity: -1})
		}
	}
	forms := [][]matchToken{raw}
	if r >= utf8.RuneSelf {
		// Past ASCII a character's entity stands for all its bytes at once.
		forms = append(forms, []matchToken{{entity: r}})
	}
	backslash := literalToken('\\')
	var escapes [][]matchToken
	short := map[rune]byte{'"': '"', '\\': '\\', '/': '/', '\b': 'b', '\f': 'f', '\n': 'n', '\r': 'r', '\t': 't', '\'': '\'', '`': '`', '$': '$'}
	if letter, ok := short[r]; ok {
		escapes = append(escapes, []matchToken{backslash, literalToken(letter)})
	}
	units := []uint16{uint16(r)}
	if r > 0xFFFF {
		high, low := utf16.EncodeRune(r)
		units = []uint16{uint16(high), uint16(low)}
	}
	unicode := make([]matchToken, 0, 6*len(units))
	for _, unit := range units {
		unicode = append(unicode, backslash, literalToken('u'), hexToken(unit>>12), hexToken(unit>>8), hexToken(unit>>4), hexToken(unit))
	}
	escapes = append(escapes, unicode)
	if r <= 0xFF {
		// JavaScript's own short form of a character's code, as a console
		// writes a control character.
		escapes = append(escapes, []matchToken{backslash, literalToken('x'), hexToken(uint16(r) >> 4), hexToken(uint16(r))})
	}
	for level := 1; level <= browserRedactJSONDepth; level++ {
		forms = append(forms, escapes...)
		deeper := make([][]matchToken, 0, len(escapes))
		for _, escape := range escapes {
			deeper = append(deeper, jsonEscapedForms(escape)...)
		}
		escapes = deeper
	}
	return forms
}

// jsonEscapedForms are the ways a JSON encoder writes a spelling inside a
// string: each backslash doubled; a quote as \" or \u0022; and the other
// characters an escape can end in that are the value's own — a slash, an
// apostrophe, a backtick, a dollar — as they are or escaped again, as some
// encoders do. Nothing else in an escape needs one.
func jsonEscapedForms(form []matchToken) [][]matchToken {
	backslash := literalToken('\\')
	unicode := func(b byte) []matchToken {
		return []matchToken{backslash, literalToken('u'), hexToken(0), hexToken(0), hexToken(uint16(b) >> 4), hexToken(uint16(b))}
	}
	forms := [][]matchToken{nil}
	for _, token := range form {
		options := [][]matchToken{{token}}
		if token.rawLen == 1 {
			switch b := token.raw[0]; b {
			case '\\':
				options = [][]matchToken{{backslash, backslash}}
			case '"':
				options = [][]matchToken{{backslash, token}, unicode(b)}
			case '/':
				options = [][]matchToken{{token}, {backslash, token}, unicode(b)}
			case '\'', '`', '$':
				options = [][]matchToken{{token}, unicode(b)}
			}
		}
		next := make([][]matchToken, 0, len(forms)*len(options))
		for _, head := range forms {
			for _, option := range options {
				next = append(next, append(slices.Clone(head), option...))
			}
		}
		forms = next
	}
	return forms
}

func newRuneTrie(r rune) *runeTrie {
	trie := &runeTrie{nodes: []matchNode{{}}}
	for _, form := range runeForms(r) {
		at := int32(0)
		for _, token := range form {
			next := int32(-1)
			for _, child := range trie.nodes[at].children {
				if trie.nodes[child].token == token {
					next = child
					break
				}
			}
			if next < 0 {
				next = int32(len(trie.nodes))
				trie.nodes = append(trie.nodes, matchNode{token: token})
				trie.nodes[at].children = append(trie.nodes[at].children, next)
			}
			at = next
		}
		trie.nodes[at].terminal = true
	}
	return trie
}

// The places a thread can be inside one token.
const (
	// tokenDone: the token at the thread's node has been read whole (or the
	// node is the root); the next character starts one of its children.
	tokenDone uint8 = iota
	// tokenPercent, tokenPercentLow: after `%`, after its first hex digit.
	tokenPercent
	tokenPercentLow
	// tokenAmp: after `&`.
	tokenAmp
	// tokenName: inside a named entity; count characters of the name read.
	tokenName
	// tokenNumber: after `&#`; tokenDecimal, tokenHex: inside the digits,
	// count of them read past the leading zeros.
	tokenNumber
	tokenDecimal
	tokenHex
)

// entityNames are the named entities of the characters HTML writes by name.
var entityNames = map[rune]string{'&': "amp", '<': "lt", '>': "gt", '"': "quot", '\'': "apos", ' ': "nbsp"}

// matchThread is one reading of the text that is still the beginning of a
// value: it began at start, has read the value's first idx characters as
// pattern pat spells them — or, while it reads the first, as every pattern
// of group -pat-1 does — and stands at node of that character's trie, at
// place within the node's token (alt: which of the token's bytes an escape
// stands for; count: characters read of an entity's name or digits).
type matchThread struct {
	start int32
	pat   int32
	idx   int32
	node  int32
	place uint8
	alt   uint8
	count uint8
}

// key is the place a thread stands at: two threads with one key read the
// rest of the text alike.
func (t matchThread) key() uint64 {
	return uint64(uint32(t.pat))<<32 | uint64(t.idx)<<18 | uint64(t.node)<<8 | uint64(t.place)<<5 | uint64(t.alt)<<4 | uint64(t.count)
}

// matchPattern is one way a value may come back, character by character:
// the trie of each of its characters, in order.
type matchPattern struct {
	placeholder int32
	tries       []*runeTrie
}

// matcher finds the patterns of a redactor in a text.
type matcher struct {
	patterns []matchPattern
	tries    map[rune]*runeTrie
	// groups are the patterns that begin with one character: a text is read
	// once for all of them until that character is past.
	groups     [][]int32
	groupRune  []rune
	groupTries []*runeTrie
	// rawStarts are the groups a byte can begin a spelling of, as itself;
	// entityStarts those a character's entity can.
	rawStarts    [256][]int32
	entityStarts map[rune][]int32
}

func (m *matcher) trieOf(r rune) *runeTrie {
	trie, ok := m.tries[r]
	if !ok {
		trie = newRuneTrie(r)
		m.tries[r] = trie
	}
	return trie
}

func (m *matcher) add(value string, placeholder int32) {
	runes := []rune(value)
	pattern := int32(len(m.patterns))
	tries := make([]*runeTrie, len(runes))
	for i, r := range runes {
		tries[i] = m.trieOf(r)
	}
	m.patterns = append(m.patterns, matchPattern{placeholder: placeholder, tries: tries})
	group := slices.Index(m.groupRune, runes[0])
	if group < 0 {
		group = len(m.groups)
		m.groups = append(m.groups, nil)
		m.groupRune = append(m.groupRune, runes[0])
		trie := m.trieOf(runes[0])
		m.groupTries = append(m.groupTries, trie)
		for _, child := range trie.nodes[0].children {
			token := trie.nodes[child].token
			for i := range token.rawLen {
				m.rawStarts[token.raw[i]] = append(m.rawStarts[token.raw[i]], int32(group))
			}
			if token.entity >= 0 {
				m.entityStarts[token.entity] = append(m.entityStarts[token.entity], int32(group))
			}
		}
	}
	m.groups[group] = append(m.groups[group], pattern)
}

// matchSpan is a stretch of text that spells values: text[start:end] is one
// value, or several that overlap, by the placeholders of each in the order
// they were found.
type matchSpan struct {
	start, end   int32
	placeholders []int32
}

// matchRun is one pass of a matcher over one text.
type matchRun struct {
	m     *matcher
	text  string
	next  []matchThread
	spans []matchSpan
}

func (run *matchRun) trie(t matchThread) *runeTrie {
	if t.pat < 0 {
		return run.m.groupTries[-t.pat-1]
	}
	return run.m.patterns[t.pat].tries[t.idx]
}

// finish is a thread whose token at node has just been read whole at pos: it
// may go on to the token's children, and where a spelling ends there the
// character is read — on to the next character of the value, or the value
// is found.
func (run *matchRun) finish(t matchThread, trie *runeTrie, node int32, pos int32) {
	t.node, t.place, t.alt, t.count = node, tokenDone, 0, 0
	if len(trie.nodes[node].children) > 0 {
		run.next = append(run.next, t)
	}
	if !trie.nodes[node].terminal {
		return
	}
	advance := func(pattern int32, idx int32) {
		if int(idx) == len(run.m.patterns[pattern].tries) {
			run.found(t.start, pos+1, run.m.patterns[pattern].placeholder)
			return
		}
		run.next = append(run.next, matchThread{start: t.start, pat: pattern, idx: idx, place: tokenDone})
	}
	if t.pat >= 0 {
		advance(t.pat, t.idx+1)
		return
	}
	for _, pattern := range run.m.groups[-t.pat-1] {
		advance(pattern, 1)
	}
}

// found records a value spelled by text[start:end]. Values are found in the
// order they end, so one that overlaps the spans before it is joined with
// them: the spans stay apart from each other and as few as the text holds.
func (run *matchRun) found(start, end, placeholder int32) {
	span := matchSpan{start: start, end: end, placeholders: []int32{placeholder}}
	for n := len(run.spans); n > 0 && span.start < run.spans[n-1].end; n = len(run.spans) {
		last := run.spans[n-1]
		for _, p := range span.placeholders {
			if !slices.Contains(last.placeholders, p) {
				last.placeholders = append(last.placeholders, p)
			}
		}
		span = matchSpan{start: min(last.start, span.start), end: max(last.end, span.end), placeholders: last.placeholders}
		run.spans = run.spans[:n-1]
	}
	run.spans = append(run.spans, span)
}

// begin reads c as the first character of the token at child.
func (run *matchRun) begin(t matchThread, trie *runeTrie, child int32, c byte, pos int32) {
	token := trie.nodes[child].token
	if token.holds(c) || (c == '+' && token.holds(' ')) {
		run.finish(t, trie, child, pos)
	}
	t.node, t.alt, t.count = child, 0, 0
	if c == '%' && token.rawLen > 0 {
		t.place = tokenPercent
		run.next = append(run.next, t)
	}
	if c == '&' && token.entity >= 0 {
		t.place = tokenAmp
		run.next = append(run.next, t)
	}
}

func hexNibble(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

// step moves one thread over c, the character at pos.
func (run *matchRun) step(t matchThread, c byte, pos int32) {
	trie := run.trie(t)
	node := &trie.nodes[t.node]
	token := node.token
	switch t.place {
	case tokenDone:
		for _, child := range node.children {
			run.begin(t, trie, child, c, pos)
		}
	case tokenPercent:
		for alt := range token.rawLen {
			if hexNibble(c) == int(token.raw[alt]>>4) {
				t.place, t.alt = tokenPercentLow, alt
				run.next = append(run.next, t)
			}
		}
	case tokenPercentLow:
		if hexNibble(c) == int(token.raw[t.alt]&0xf) {
			run.finish(t, trie, t.node, pos)
		}
	case tokenAmp:
		if c == '#' {
			t.place = tokenNumber
			run.next = append(run.next, t)
		} else if name := entityNames[token.entity]; name != "" && name[0] == c {
			t.place, t.count = tokenName, 1
			run.next = append(run.next, t)
		}
	case tokenName:
		name := entityNames[token.entity]
		switch {
		case int(t.count) < len(name) && name[t.count] == c:
			t.count++
			run.next = append(run.next, t)
		case int(t.count) == len(name) && c == ';':
			run.finish(t, trie, t.node, pos)
		}
	case tokenNumber:
		if c == 'x' || c == 'X' {
			t.place = tokenHex
			run.next = append(run.next, t)
			return
		}
		t.place = tokenDecimal
		run.digit(t, trie, strconv.FormatInt(int64(token.entity), 10), c, pos)
	case tokenDecimal:
		run.digit(t, trie, strconv.FormatInt(int64(token.entity), 10), c, pos)
	case tokenHex:
		if c >= 'A' && c <= 'F' {
			c += 'a' - 'A'
		}
		run.digit(t, trie, strconv.FormatInt(int64(token.entity), 16), c, pos)
	}
}

// digit reads c inside a numeric entity whose digits, past any leading
// zeros, are digits.
func (run *matchRun) digit(t matchThread, trie *runeTrie, digits string, c byte, pos int32) {
	switch {
	case t.count == 0 && c == '0':
		run.next = append(run.next, t)
	case int(t.count) < len(digits) && digits[t.count] == c:
		t.count++
		run.next = append(run.next, t)
	case int(t.count) == len(digits) && c == ';':
		run.finish(t, trie, t.node, pos)
	}
}

// entityAt is the character an HTML entity at text[pos] stands for, -1 when
// none of the ones a value's character can be written as stands there.
func entityAt(text string, pos int) rune {
	rest := text[pos+1 : min(len(text), pos+16)]
	end := strings.IndexByte(rest, ';')
	if end < 1 {
		return -1
	}
	body := rest[:end]
	if body[0] != '#' {
		for r, name := range entityNames {
			if name == body {
				return r
			}
		}
		return -1
	}
	digits, base := body[1:], 10
	if len(digits) > 0 && (digits[0] == 'x' || digits[0] == 'X') {
		digits, base = digits[1:], 16
	}
	code, err := strconv.ParseInt(digits, base, 32)
	if err != nil {
		return -1
	}
	return rune(code)
}

// starts are the groups whose first character a spelling beginning with the
// character at pos may be: the byte as itself, the byte a %HH there stands
// for, a space for a `+`, the character of an entity.
func (m *matcher) starts(text string, pos int, groups []int32) []int32 {
	c := text[pos]
	groups = append(groups[:0], m.rawStarts[c]...)
	switch c {
	case '%':
		if pos+2 < len(text) {
			if high, low := hexNibble(text[pos+1]), hexNibble(text[pos+2]); high >= 0 && low >= 0 {
				groups = append(groups, m.rawStarts[high<<4|low]...)
			}
		}
	case '+':
		groups = append(groups, m.rawStarts[' ']...)
	case '&':
		if r := entityAt(text, pos); r >= 0 {
			groups = append(groups, m.entityStarts[r]...)
		}
	}
	return groups
}

// find reads text once and answers the spans of it that spell values, apart
// from each other and in order, and where the earliest reading still under way at its end began (len(text)
// when none is). budget is the steps it may still take: one per character,
// one per thread a character moves and one per thread it makes. ok is false
// when it ran out; the text was then not searched to its end.
func (m *matcher) find(text string, budget *int64) (spans []matchSpan, pending int, ok bool) {
	run := &matchRun{m: m, text: text}
	var threads []matchThread
	var places threadSet
	var groups []int32
	for pos := range len(text) {
		c := text[pos]
		*budget -= int64(len(threads)) + 1
		if *budget < 0 {
			return nil, 0, false
		}
		run.next = run.next[:0]
		for _, t := range threads {
			run.step(t, c, int32(pos))
		}
		groups = m.starts(text, pos, groups)
		slices.Sort(groups)
		for _, group := range slices.Compact(groups) {
			*budget--
			start := matchThread{start: int32(pos), pat: -group - 1, place: tokenDone}
			run.step(start, c, int32(pos))
		}
		// The readings this character made are work too, before those that
		// came to the same place are made one.
		*budget -= int64(len(run.next))
		threads = places.unique(run.next, threads[:0])
	}
	pending = len(text)
	for _, t := range threads {
		pending = min(pending, int(t.start))
	}
	return run.spans, pending, true
}

// threadSet makes the threads one character made that stand at one place
// one, in time linear in their number: an open-addressed table of their
// places, stamped per character so it is never cleared.
type threadSet struct {
	keys  []uint64
	at    []int32
	stamp []uint32
	now   uint32
}

// unique appends next to kept with the threads at one place made one: the
// one that began earliest, whose span holds the others'. kept keeps the
// order in which each place was first reached.
func (s *threadSet) unique(next, kept []matchThread) []matchThread {
	if len(next) == 0 {
		return kept
	}
	if size := len(s.keys); size < 2*len(next) {
		size = max(size, 64)
		for size < 2*len(next) {
			size *= 2
		}
		s.keys, s.at, s.stamp, s.now = make([]uint64, size), make([]int32, size), make([]uint32, size), 0
	}
	s.now++
	mask := uint64(len(s.keys) - 1)
	shift := 64 - bits.Len64(mask)
	for _, t := range next {
		key := t.key()
		slot := (key * 0x9E3779B97F4A7C15) >> shift
		for {
			if s.stamp[slot] != s.now {
				s.stamp[slot], s.keys[slot], s.at[slot] = s.now, key, int32(len(kept))
				kept = append(kept, t)
				break
			}
			if s.keys[slot] == key {
				if held := &kept[s.at[slot]]; t.start < held.start {
					held.start = t.start
				}
				break
			}
			slot = (slot + 1) & mask
		}
	}
	return kept
}
