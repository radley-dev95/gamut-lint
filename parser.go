package main

import (
	"fmt"
	"strconv"
	"strings"
)

// Pos is a 1-based line/column position within a palette file.
type Pos struct {
	Line int
	Col  int
}

// ParseError describes exactly where and why a line of a palette file
// failed to parse.
type ParseError struct {
	File string
	Pos  Pos
	Msg  string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("%s:%d:%d: %s", e.File, e.Pos.Line, e.Pos.Col, e.Msg)
}

// Entry is one parsed line of a palette file: an optional name and an
// OKLCH color.
type Entry struct {
	Name     string
	L, C, H  float64
	Alpha    float64
	HasAlpha bool
	Pos      Pos // position of the color function, e.g. "oklch("
	Raw      string
}

type scanner struct {
	file string
	line int
	text string
	pos  int
}

func (s *scanner) posAt(offset int) Pos {
	return Pos{Line: s.line, Col: offset + 1}
}

func (s *scanner) here() Pos {
	return s.posAt(s.pos)
}

func (s *scanner) errf(pos Pos, format string, args ...interface{}) error {
	return &ParseError{File: s.file, Pos: pos, Msg: fmt.Sprintf(format, args...)}
}

func (s *scanner) peek() byte {
	if s.pos >= len(s.text) {
		return 0
	}
	return s.text[s.pos]
}

func (s *scanner) advance() byte {
	c := s.peek()
	if c != 0 {
		s.pos++
	}
	return c
}

func (s *scanner) skipSpaces() {
	for s.peek() == ' ' || s.peek() == '\t' {
		s.advance()
	}
}

func isIdentStart(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isIdentChar(c byte) bool {
	return isIdentStart(c) || c >= '0' && c <= '9' || c == '-' || c == '_'
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func (s *scanner) scanIdent() (string, Pos) {
	start := s.pos
	pos := s.here()
	for isIdentChar(s.peek()) {
		s.advance()
	}
	return s.text[start:s.pos], pos
}

// scanRawNumber consumes an optional sign, digits, and an optional decimal
// point followed by more digits. It does not consume a trailing unit.
func (s *scanner) scanRawNumber() (string, Pos, error) {
	startPos := s.here()
	start := s.pos
	if s.peek() == '-' || s.peek() == '+' {
		s.advance()
	}
	digitsBefore := 0
	for isDigit(s.peek()) {
		s.advance()
		digitsBefore++
	}
	digitsAfter := 0
	if s.peek() == '.' {
		s.advance()
		for isDigit(s.peek()) {
			s.advance()
			digitsAfter++
		}
	}
	if digitsBefore == 0 && digitsAfter == 0 {
		return "", startPos, s.errf(startPos, "expected a number, found %s", describeRest(s.text[s.pos:]))
	}
	return s.text[start:s.pos], startPos, nil
}

func describeRest(rest string) string {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return "end of line"
	}
	if len(rest) > 12 {
		rest = rest[:12] + "..."
	}
	return fmt.Sprintf("%q", rest)
}

// scanComponent parses a number optionally followed by "%", scaling it by
// pctScale when a percent sign is present. This matches the CSS Color 4
// rule for oklch(): 100% lightness is 1.0, and 100% chroma is 0.4.
func (s *scanner) scanComponent(pctScale float64) (float64, Pos, error) {
	lit, pos, err := s.scanRawNumber()
	if err != nil {
		return 0, pos, err
	}
	v, err := strconv.ParseFloat(lit, 64)
	if err != nil {
		return 0, pos, s.errf(pos, "invalid number %q", lit)
	}
	if s.peek() == '%' {
		s.advance()
		v = v / 100 * pctScale
	}
	return v, pos, nil
}

func (s *scanner) expect(c byte, what string) error {
	if s.peek() != c {
		return s.errf(s.here(), "expected %s, found %s", what, describeRest(s.text[s.pos:]))
	}
	s.advance()
	return nil
}

// ParseLine parses a single line of a palette file. It returns (nil, nil)
// for blank lines and comment lines (lines whose first non-space
// character is '#'). The palette line grammar is:
//
//	[name ':'] 'oklch(' lightness chroma hue ['/' alpha] ')'
//
// lightness and alpha accept a bare number or a percentage; chroma accepts
// a bare number or a percentage of 0.4; hue accepts a bare number or a
// number followed by "deg".
func ParseLine(file string, lineNum int, raw string) (*Entry, error) {
	s := &scanner{file: file, line: lineNum, text: raw}
	s.skipSpaces()
	if s.peek() == 0 || s.peek() == '#' {
		return nil, nil
	}

	if !isIdentStart(s.peek()) {
		return nil, s.errf(s.here(), "expected a color name or \"oklch(...)\", found %s", describeRest(s.text[s.pos:]))
	}
	ident, identPos := s.scanIdent()

	var name string
	var funcPos Pos
	s.skipSpaces()
	switch s.peek() {
	case ':':
		s.advance()
		s.skipSpaces()
		name = ident
		if !isIdentStart(s.peek()) {
			return nil, s.errf(s.here(), "expected a color function after %q, found %s", name+":", describeRest(s.text[s.pos:]))
		}
		fnIdent, pos := s.scanIdent()
		if fnIdent != "oklch" {
			return nil, s.errf(pos, "unsupported color function %q (only \"oklch\" is supported)", fnIdent)
		}
		funcPos = pos
	case '(':
		if ident != "oklch" {
			return nil, s.errf(identPos, "unsupported color function %q (only \"oklch\" is supported)", ident)
		}
		funcPos = identPos
	default:
		return nil, s.errf(s.here(), "expected ':' or '(' after %q, found %s", ident, describeRest(s.text[s.pos:]))
	}

	if err := s.expect('(', "'('"); err != nil {
		return nil, err
	}
	s.skipSpaces()

	l, lPos, err := s.scanComponent(1.0)
	if err != nil {
		return nil, err
	}
	if l < 0 || l > 1+epsilon {
		return nil, s.errf(lPos, "lightness %.4g is out of range (expected 0 to 1, or 0%% to 100%%)", l)
	}
	s.skipSpaces()

	c, cPos, err := s.scanComponent(0.4)
	if err != nil {
		return nil, err
	}
	if c < 0 {
		return nil, s.errf(cPos, "chroma %.4g cannot be negative", c)
	}
	s.skipSpaces()

	hLit, hPos, err := s.scanRawNumber()
	if err != nil {
		return nil, err
	}
	h, err := strconv.ParseFloat(hLit, 64)
	if err != nil {
		return nil, s.errf(hPos, "invalid number %q", hLit)
	}
	if strings.HasPrefix(s.text[s.pos:], "deg") {
		s.pos += len("deg")
	}
	s.skipSpaces()

	alpha := 1.0
	hasAlpha := false
	if s.peek() == '/' {
		s.advance()
		s.skipSpaces()
		a, aPos, err := s.scanComponent(1.0)
		if err != nil {
			return nil, err
		}
		if a < 0 || a > 1+epsilon {
			return nil, s.errf(aPos, "alpha %.4g is out of range (expected 0 to 1, or 0%% to 100%%)", a)
		}
		alpha = a
		hasAlpha = true
		s.skipSpaces()
	}

	if err := s.expect(')', "')'"); err != nil {
		return nil, err
	}
	s.skipSpaces()
	if s.peek() != 0 && s.peek() != '#' {
		return nil, s.errf(s.here(), "unexpected trailing text %s after color", describeRest(s.text[s.pos:]))
	}

	return &Entry{
		Name:     name,
		L:        l,
		C:        c,
		H:        h,
		Alpha:    alpha,
		HasAlpha: hasAlpha,
		Pos:      funcPos,
		Raw:      strings.TrimSpace(raw),
	}, nil
}
