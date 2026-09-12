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
