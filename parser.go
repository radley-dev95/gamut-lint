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
	Pos      Pos // position of the color literal, e.g. "oklch(" or "#"
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

func hexDigitVal(c byte) (int, bool) {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0'), true
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10, true
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10, true
	}
	return 0, false
}

func isHexDigit(c byte) bool {
	_, ok := hexDigitVal(c)
	return ok
}

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

// scanHexColor parses a CSS hex color: '#' followed by 3, 4, 6, or 8 hex
// digits (RGB, RGBA, RRGGBB, or RRGGBBAA). The single-digit forms are
// duplicated, e.g. "f" means "ff". Returned channels are in [0, 1].
func (s *scanner) scanHexColor() (r, g, b, a float64, hasAlpha bool, pos Pos, err error) {
	pos = s.here()
	if err := s.expect('#', "'#'"); err != nil {
		return 0, 0, 0, 0, false, pos, err
	}
	start := s.pos
	for isHexDigit(s.peek()) {
		s.advance()
	}
	digits := s.text[start:s.pos]
	channel := func(hex string) float64 {
		v, _ := strconv.ParseUint(hex, 16, 32)
		if len(hex) == 1 {
			v *= 17
		}
		return float64(v) / 255
	}
	a = 1.0
	switch len(digits) {
	case 3:
		r, g, b = channel(digits[0:1]), channel(digits[1:2]), channel(digits[2:3])
	case 4:
		r, g, b = channel(digits[0:1]), channel(digits[1:2]), channel(digits[2:3])
		a = channel(digits[3:4])
		hasAlpha = true
	case 6:
		r, g, b = channel(digits[0:2]), channel(digits[2:4]), channel(digits[4:6])
	case 8:
		r, g, b = channel(digits[0:2]), channel(digits[2:4]), channel(digits[4:6])
		a = channel(digits[6:8])
		hasAlpha = true
	default:
		return 0, 0, 0, 0, false, pos, s.errf(pos, "expected 3, 4, 6, or 8 hex digits after '#', found %d", len(digits))
	}
	return r, g, b, a, hasAlpha, pos, nil
}

// finishHexEntry parses a hex color literal (after "name:") and converts it
// to OKLCH so the rest of the pipeline can treat every entry the same way.
// A hex color describes an sRGB value directly, so it's always in gamut;
// the OKLCH numbers exist only so it can be reported alongside oklch()
// entries.
func (s *scanner) finishHexEntry(name string) (*Entry, error) {
	r, g, b, a, hasAlpha, pos, err := s.scanHexColor()
	if err != nil {
		return nil, err
	}
	s.skipSpaces()
	if s.peek() != 0 && s.peek() != '#' {
		return nil, s.errf(s.here(), "unexpected trailing text %s after color", describeRest(s.text[s.pos:]))
	}
	l, c, h := srgbToOKLCH(r, g, b)
	return &Entry{
		Name:     name,
		L:        l,
		C:        c,
		H:        h,
		Alpha:    a,
		HasAlpha: hasAlpha,
		Pos:      pos,
		Raw:      strings.TrimSpace(s.text),
	}, nil
}

// scanHue parses a bare number optionally followed by "deg", as used by
// both oklch()'s hue component and hsl()'s. Unlike scanComponent, the
// result isn't normalized to [0, 360); the callers that need degrees
// wrapped (hslToSRGB) do that themselves.
func (s *scanner) scanHue() (float64, Pos, error) {
	lit, pos, err := s.scanRawNumber()
	if err != nil {
		return 0, pos, err
	}
	h, err := strconv.ParseFloat(lit, 64)
	if err != nil {
		return 0, pos, s.errf(pos, "invalid number %q", lit)
	}
	if strings.HasPrefix(s.text[s.pos:], "deg") {
		s.pos += len("deg")
	}
	return h, pos, nil
}

// scanOptionalAlpha parses an optional "/ A" alpha suffix shared by
// oklch(), rgb(), and hsl(). It returns (1.0, false, nil) when there is no
// alpha suffix.
func (s *scanner) scanOptionalAlpha() (float64, bool, error) {
	if s.peek() != '/' {
		return 1.0, false, nil
	}
	s.advance()
	s.skipSpaces()
	a, aPos, err := s.scanComponent(1.0)
	if err != nil {
		return 0, false, err
	}
	if a < 0 || a > 1+epsilon {
		return 0, false, s.errf(aPos, "alpha %.4g is out of range (expected 0 to 1, or 0%% to 100%%)", a)
	}
	s.skipSpaces()
	return a, true, nil
}

// scanRGBChannel parses one rgb() channel: a bare number in [0, 255] or a
// percentage of 255. The returned value is scaled to [0, 1].
func (s *scanner) scanRGBChannel(name string) (float64, Pos, error) {
	lit, pos, err := s.scanRawNumber()
	if err != nil {
		return 0, pos, err
	}
	raw, err := strconv.ParseFloat(lit, 64)
	if err != nil {
		return 0, pos, s.errf(pos, "invalid number %q", lit)
	}
	var v float64
	if s.peek() == '%' {
		s.advance()
		v = raw / 100
	} else {
		v = raw / 255
	}
	if v < -epsilon || v > 1+epsilon {
		return 0, pos, s.errf(pos, "%s channel %.4g is out of range (expected 0 to 255, or 0%% to 100%%)", name, raw)
	}
	return v, pos, nil
}

// scanPercentComponent parses a number that must be followed by "%", as
// hsl() requires for its saturation and lightness components. The returned
// value is scaled to [0, 1].
func (s *scanner) scanPercentComponent(name string) (float64, Pos, error) {
	lit, pos, err := s.scanRawNumber()
	if err != nil {
		return 0, pos, err
	}
	raw, err := strconv.ParseFloat(lit, 64)
	if err != nil {
		return 0, pos, s.errf(pos, "invalid number %q", lit)
	}
	if s.peek() != '%' {
		return 0, pos, s.errf(pos, "%s must be a percentage, found %s", name, describeRest(s.text[s.pos:]))
	}
	s.advance()
	v := raw / 100
	if v < -epsilon || v > 1+epsilon {
		return 0, pos, s.errf(pos, "%s %.4g%% is out of range (expected 0%% to 100%%)", name, raw)
	}
	return v, pos, nil
}

// finishOKLCHEntry parses the inside of an "oklch(...)" literal, with the
// opening paren already consumed.
func (s *scanner) finishOKLCHEntry(name string, funcPos Pos, raw string) (*Entry, error) {
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

	h, _, err := s.scanHue()
	if err != nil {
		return nil, err
	}
	s.skipSpaces()

	alpha, hasAlpha, err := s.scanOptionalAlpha()
	if err != nil {
		return nil, err
	}

	if err := s.expect(')', "')'"); err != nil {
		return nil, err
	}
	s.skipSpaces()
	if s.peek() != 0 && s.peek() != '#' {
		return nil, s.errf(s.here(), "unexpected trailing text %s after color", describeRest(s.text[s.pos:]))
	}

	return &Entry{
		Name: name, L: l, C: c, H: h, Alpha: alpha, HasAlpha: hasAlpha,
		Pos: funcPos, Raw: strings.TrimSpace(raw),
	}, nil
}

// finishRGBEntry parses the inside of an "rgb(...)" literal, with the
// opening paren already consumed, and converts it to OKLCH.
func (s *scanner) finishRGBEntry(name string, funcPos Pos, raw string) (*Entry, error) {
	r, _, err := s.scanRGBChannel("red")
	if err != nil {
		return nil, err
	}
	s.skipSpaces()

	g, _, err := s.scanRGBChannel("green")
	if err != nil {
		return nil, err
	}
	s.skipSpaces()

	b, _, err := s.scanRGBChannel("blue")
	if err != nil {
		return nil, err
	}
	s.skipSpaces()

	alpha, hasAlpha, err := s.scanOptionalAlpha()
	if err != nil {
		return nil, err
	}

	if err := s.expect(')', "')'"); err != nil {
		return nil, err
	}
	s.skipSpaces()
	if s.peek() != 0 && s.peek() != '#' {
		return nil, s.errf(s.here(), "unexpected trailing text %s after color", describeRest(s.text[s.pos:]))
	}

	l, c, h := srgbToOKLCH(r, g, b)
	return &Entry{
		Name: name, L: l, C: c, H: h, Alpha: alpha, HasAlpha: hasAlpha,
		Pos: funcPos, Raw: strings.TrimSpace(raw),
	}, nil
}

// finishHSLEntry parses the inside of an "hsl(...)" literal, with the
// opening paren already consumed, and converts it to OKLCH.
func (s *scanner) finishHSLEntry(name string, funcPos Pos, raw string) (*Entry, error) {
	hDeg, _, err := s.scanHue()
	if err != nil {
		return nil, err
	}
	s.skipSpaces()

	sat, _, err := s.scanPercentComponent("saturation")
	if err != nil {
		return nil, err
	}
	s.skipSpaces()

	lig, _, err := s.scanPercentComponent("lightness")
	if err != nil {
		return nil, err
	}
	s.skipSpaces()

	alpha, hasAlpha, err := s.scanOptionalAlpha()
	if err != nil {
		return nil, err
	}

	if err := s.expect(')', "')'"); err != nil {
		return nil, err
	}
	s.skipSpaces()
	if s.peek() != 0 && s.peek() != '#' {
		return nil, s.errf(s.here(), "unexpected trailing text %s after color", describeRest(s.text[s.pos:]))
	}

	r, g, b := hslToSRGB(hDeg, sat, lig)
	l, c, h := srgbToOKLCH(r, g, b)
	return &Entry{
		Name: name, L: l, C: c, H: h, Alpha: alpha, HasAlpha: hasAlpha,
		Pos: funcPos, Raw: strings.TrimSpace(raw),
	}, nil
}

func isSupportedFunc(name string) bool {
	switch name {
	case "oklch", "rgb", "hsl":
		return true
	}
	return false
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
//	[name ':'] 'rgb(' red green blue ['/' alpha] ')'
//	[name ':'] 'hsl(' hue saturation lightness ['/' alpha] ')'
//	name ':' '#' hex-digits
//
// lightness (oklch) and alpha accept a bare number or a percentage; chroma
// accepts a bare number or a percentage of 0.4; hue accepts a bare number
// or a number followed by "deg". rgb()'s channels accept a bare number
// from 0 to 255 or a percentage; hsl()'s saturation and lightness require
// a percentage. A hex color requires a name, since a bare "#" at the start
// of a line is indistinguishable from a comment; hex-digits is 3, 4, 6, or
// 8 hex digits (RGB, RGBA, RRGGBB, or RRGGBBAA).
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

	var name, fnIdent string
	var funcPos Pos
	s.skipSpaces()
	switch s.peek() {
	case ':':
		s.advance()
		s.skipSpaces()
		name = ident
		if s.peek() == '#' {
			return s.finishHexEntry(name)
		}
		if !isIdentStart(s.peek()) {
			return nil, s.errf(s.here(), "expected a color function after %q, found %s", name+":", describeRest(s.text[s.pos:]))
		}
		id, pos := s.scanIdent()
		if !isSupportedFunc(id) {
			return nil, s.errf(pos, "unsupported color function %q (supported: \"oklch\", \"rgb\", \"hsl\")", id)
		}
		fnIdent, funcPos = id, pos
	case '(':
		if !isSupportedFunc(ident) {
			return nil, s.errf(identPos, "unsupported color function %q (supported: \"oklch\", \"rgb\", \"hsl\")", ident)
		}
		fnIdent, funcPos = ident, identPos
	default:
		return nil, s.errf(s.here(), "expected ':' or '(' after %q, found %s", ident, describeRest(s.text[s.pos:]))
	}

	if err := s.expect('(', "'('"); err != nil {
		return nil, err
	}
	s.skipSpaces()

	switch fnIdent {
	case "rgb":
		return s.finishRGBEntry(name, funcPos, raw)
	case "hsl":
		return s.finishHSLEntry(name, funcPos, raw)
	default:
		return s.finishOKLCHEntry(name, funcPos, raw)
	}
}
