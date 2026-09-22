package main

import "math"

// epsilon accounts for floating point error when checking the sRGB gamut
// boundary; without it colors that are mathematically exactly at 0 or 1
// (very common for pure black, white, and primaries) could be flagged as
// out of gamut due to rounding in the OKLab matrices below.
const epsilon = 1e-4

// oklchToOklab converts cylindrical OKLCH coordinates to the OKLab
// coordinates it wraps. Hue is in degrees.
func oklchToOklab(l, c, hDeg float64) (L, a, b float64) {
	hRad := hDeg * math.Pi / 180
	return l, c * math.Cos(hRad), c * math.Sin(hRad)
}

// oklabToLinearSRGB converts OKLab to linear-light sRGB using the matrices
// from Björn Ottosson's OKLab reference implementation. The result is not
// gamma encoded; that step is skipped here because gamma encoding is
// monotonic on [0, 1] and doesn't affect whether a color is in gamut.
func oklabToLinearSRGB(L, a, b float64) (r, g, bl float64) {
	l_ := L + 0.3963377774*a + 0.2158037573*b
	m_ := L - 0.1055613458*a - 0.0638541728*b
	s_ := L - 0.0894841775*a - 1.2914855480*b

	l := l_ * l_ * l_
	m := m_ * m_ * m_
	s := s_ * s_ * s_

	r = 4.0767416621*l - 3.3077115913*m + 0.2309699292*s
	g = -1.2684380046*l + 2.6097574011*m - 0.3413193965*s
	bl = -0.0041960863*l - 0.7034186147*m + 1.7076147010*s
	return
}

// srgbGammaToLinear undoes sRGB gamma encoding for a single channel in
// [0, 1], per the IEC 61966-2-1 piecewise curve.
func srgbGammaToLinear(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

// linearSRGBToOklab converts linear-light sRGB to OKLab. The matrices are
// Björn Ottosson's published forward coefficients; they are not an exact
// algebraic inverse of oklabToLinearSRGB's matrices (those are separately
// published, rounded constants), so round-tripping a color through both
// leaves a residual on the order of 1e-6 to 1e-5, well under epsilon.
func linearSRGBToOklab(r, g, b float64) (L, a, bOut float64) {
	l := 0.4122214708*r + 0.5363325363*g + 0.0514459929*b
	m := 0.2119034982*r + 0.6806995451*g + 0.1073969566*b
	s := 0.0883024619*r + 0.2817188376*g + 0.6299787005*b

	l_ := math.Cbrt(l)
	m_ := math.Cbrt(m)
	s_ := math.Cbrt(s)

	L = 0.2104542553*l_ + 0.7936177850*m_ - 0.0040720468*s_
	a = 1.9779984951*l_ - 2.4285922050*m_ + 0.4505937099*s_
	bOut = 0.0259040371*l_ + 0.7827717662*m_ - 0.8086757660*s_
	return
}

// oklabToOklch converts OKLab to cylindrical OKLCH. Hue is in degrees,
// normalized to [0, 360).
func oklabToOklch(L, a, b float64) (l, c, hDeg float64) {
	c = math.Hypot(a, b)
	hDeg = math.Atan2(b, a) * 180 / math.Pi
	if hDeg < 0 {
		hDeg += 360
	}
	return L, c, hDeg
}

// srgbToOKLCH converts a gamma-encoded sRGB color (each channel in [0, 1])
// to OKLCH. Used for input formats, like hex, that describe an sRGB value
// directly rather than an OKLCH one.
func srgbToOKLCH(r, g, b float64) (l, c, hDeg float64) {
	L, a, bLab := linearSRGBToOklab(srgbGammaToLinear(r), srgbGammaToLinear(g), srgbGammaToLinear(b))
	return oklabToOklch(L, a, bLab)
}

// hslToSRGB converts HSL (hue in degrees, saturation and lightness in
// [0, 1]) to gamma-encoded sRGB, each channel in [0, 1]. This is the
// standard CSS/HSL conversion, not an OKLab one; the result still needs to
// go through srgbToOKLCH like any other sRGB input.
func hslToSRGB(hDeg, sat, lig float64) (r, g, b float64) {
	h := math.Mod(hDeg, 360)
	if h < 0 {
		h += 360
	}
	chroma := (1 - math.Abs(2*lig-1)) * sat
	x := chroma * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := lig - chroma/2

	var r1, g1, b1 float64
	switch {
	case h < 60:
		r1, g1, b1 = chroma, x, 0
	case h < 120:
		r1, g1, b1 = x, chroma, 0
	case h < 180:
		r1, g1, b1 = 0, chroma, x
	case h < 240:
		r1, g1, b1 = 0, x, chroma
	case h < 300:
		r1, g1, b1 = x, 0, chroma
	default:
		r1, g1, b1 = chroma, 0, x
	}
	return r1 + m, g1 + m, b1 + m
}

func inRange01(v float64) bool {
	return v >= -epsilon && v <= 1+epsilon
}

func inSRGBGamut(r, g, b float64) bool {
	return inRange01(r) && inRange01(g) && inRange01(b)
}

// maxChromaInGamut finds the largest chroma <= cMax such that the OKLCH
// color (l, chroma, hDeg) still falls inside the sRGB gamut, by binary
// search over chroma. Chroma 0 (a neutral gray at the given lightness) is
// always in gamut, so the search always has a valid lower bound.
func maxChromaInGamut(l, hDeg, cMax float64) float64 {
	lo, hi := 0.0, cMax
	for i := 0; i < 40; i++ {
		mid := (lo + hi) / 2
		_, a, b := oklchToOklab(l, mid, hDeg)
		r, g, bl := oklabToLinearSRGB(l, a, b)
		if inSRGBGamut(r, g, bl) {
			lo = mid
		} else {
			hi = mid
		}
	}
	return lo
}

type gamutResult struct {
	InGamut   bool
	MaxChroma float64 // only meaningful when InGamut is false
}

func checkGamut(e *Entry) gamutResult {
	_, a, b := oklchToOklab(e.L, e.C, e.H)
	r, g, bl := oklabToLinearSRGB(e.L, a, b)
	if inSRGBGamut(r, g, bl) {
		return gamutResult{InGamut: true}
	}
	return gamutResult{InGamut: false, MaxChroma: maxChromaInGamut(e.L, e.H, e.C)}
}
