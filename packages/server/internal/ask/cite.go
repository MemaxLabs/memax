package ask

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
)

// The answer streams through a citer before any of it reaches a person.
// It turns "[M-0219]" into a citation, drops any citation of a memory the
// model wasn't given (a hallucinated or foreign ref never reaches the
// person, plan 25 §11), holds back the NOT_COVERED sentinel, and strips
// markdown bold. It works rune by rune, so a ref split across deltas
// ("[M-02" + "19]") is still one citation.

type pieceKind int

const (
	pieceText    pieceKind = iota // words for the person
	pieceCite                     // a citation of a memory the model was given
	pieceDropped                  // a citation of anything else, removed
)

type piece struct {
	kind pieceKind
	// text is the words, or the ref (normalised, "M-0219") of a citation.
	text string
}

// maxBracket bounds how long a bracket may run before it is plainly text.
const maxBracket = 96

var (
	citeSplit = regexp.MustCompile(`[\s,;、，]+`)
	// A display ID of any kind ("N-0882" and "D-214" are not memories the
	// model was given, so they're dropped), or a bare number ("[1]").
	citeToken = regexp.MustCompile(`^(?i:[a-z]-\d{1,9}|\d{1,3})$`)
)

type citer struct {
	allowed map[string]bool

	leading    bool // still deciding whether the answer is NOT_COVERED
	lead       strings.Builder
	notCovered bool

	space   strings.Builder // whitespace held until the next word (or dropped before a citation)
	bracket []rune          // an open bracket and what follows, until it closes
	star    bool            // a '*' that may be half of "**"

	out []piece
}

func newCiter(refs []string) *citer {
	c := &citer{allowed: map[string]bool{}, leading: true}
	for _, r := range refs {
		c.allowed[r] = true
	}
	return c
}

// write takes the next delta and returns what can go out now.
func (c *citer) write(s string) []piece {
	c.out = nil
	if c.notCovered {
		return nil
	}
	if c.leading {
		c.lead.WriteString(s)
		t := strings.TrimLeftFunc(c.lead.String(), unicode.IsSpace)
		up := strings.ToUpper(t)
		switch {
		case strings.HasPrefix(up, NotCovered):
			c.notCovered = true
			return nil
		case len(up) < len(NotCovered) && strings.HasPrefix(NotCovered, up):
			return nil // could still be the sentinel
		}
		c.leading = false
		c.lead.Reset()
		s = t
	}
	for _, r := range s {
		c.rune(r)
	}
	return c.out
}

// close flushes what the stream left open.
func (c *citer) close() []piece {
	c.out = nil
	if c.notCovered {
		return nil
	}
	if c.leading {
		c.leading = false
		t := strings.TrimSpace(c.lead.String())
		if t == "" {
			return nil
		}
		// A stream cut short inside the sentinel ("NOT_COV") is still it.
		if up := strings.ToUpper(t); len(up) >= 4 && strings.HasPrefix(NotCovered, up) {
			c.notCovered = true
			return nil
		}
		for _, r := range t {
			c.rune(r)
		}
	}
	c.flushStar()
	if c.bracket != nil {
		c.text(string(c.bracket))
		c.bracket = nil
	}
	c.space.Reset() // trailing whitespace goes nowhere
	return c.out
}

func (c *citer) rune(r rune) {
	switch {
	case c.bracket != nil:
		c.bracket = append(c.bracket, r)
		switch {
		case r == ']' || r == '】':
			c.closeBracket()
		case r == '\n' || len(c.bracket) > maxBracket:
			b := string(c.bracket)
			c.bracket = nil
			c.text(b)
		}
	case r == '[' || r == '【':
		c.flushStar()
		c.bracket = []rune{r}
	case r == '*':
		if c.star {
			c.star = false // "**": bold, which the answer doesn't use
			return
		}
		c.star = true
	case unicode.IsSpace(r):
		c.flushStar()
		c.space.WriteRune(r)
	default:
		c.flushStar()
		var buf [utf8.UTFMax]byte
		c.text(string(buf[:utf8.EncodeRune(buf[:], r)]))
	}
}

// closeBracket decides what a closed bracket is: a citation group, or text.
func (c *citer) closeBracket() {
	b := c.bracket
	c.bracket = nil
	inner := string(b[1 : len(b)-1])
	var tokens []string
	for _, tok := range citeSplit.Split(inner, -1) {
		if tok != "" {
			tokens = append(tokens, tok)
		}
	}
	if len(tokens) == 0 {
		c.text(string(b))
		return
	}
	for _, tok := range tokens {
		if !citeToken.MatchString(tok) {
			c.text(string(b)) // "[sic]", "[draft]": words, not a citation
			return
		}
	}
	// A citation attaches to the word before it.
	c.space.Reset()
	seen := map[string]bool{}
	for _, tok := range tokens {
		ref := strings.ToUpper(tok)
		if p, n, ok := ledger.ParseRef(tok); ok {
			ref = ledger.FormatRef(p, n)
		}
		if seen[ref] {
			continue
		}
		seen[ref] = true
		if c.allowed[ref] {
			c.out = append(c.out, piece{kind: pieceCite, text: ref})
		} else {
			c.out = append(c.out, piece{kind: pieceDropped, text: ref})
		}
	}
}

func (c *citer) flushStar() {
	if c.star {
		c.star = false
		c.text("*")
	}
}

// text emits words, after any whitespace held before them, joining
// neighbouring text into one piece.
func (c *citer) text(s string) {
	if c.space.Len() > 0 {
		s = c.space.String() + s
		c.space.Reset()
	}
	if n := len(c.out); n > 0 && c.out[n-1].kind == pieceText {
		c.out[n-1].text += s
		return
	}
	c.out = append(c.out, piece{kind: pieceText, text: s})
}
