# gamut-lint

OKLCH lets you describe a color independently of any output device: a
lightness, a chroma (colorfulness), and a hue. That's what makes it nice for
design tokens and CSS custom properties. It's also what makes it easy to
write down a color that no sRGB monitor can actually show — the chroma you
asked for at that lightness and hue just isn't reachable in sRGB, and the
browser will silently clamp it to something else.

gamut-lint reads a palette file of named OKLCH colors and tells you which
ones are out of the sRGB gamut, before you find out from a washed-out
color on a customer's screen. It answers exactly one question: is this
color representable in sRGB, and if not, by how much would you need to cut
the chroma to fix it?

## Usage

```
go build -o gamut-lint .
./gamut-lint testdata/example.palette
```

```
testdata/example.palette:7:16: background           in gamut
testdata/example.palette:8:12: text                 in gamut
testdata/example.palette:9:21: brand-primary        in gamut
testdata/example.palette:10:20: brand-accent         OUT OF GAMUT (chroma 0.3500 exceeds max 0.1889 for this lightness/hue)
testdata/example.palette:11:16: danger               in gamut
testdata/example.palette:12:16: legacy-link          in gamut
testdata/example.palette:13:16: legacy-warn          in gamut

7 color(s) checked against sRGB, 1 out of gamut
```

The exit code is 0 if every color is in gamut, 1 if any color is out of
gamut, and 2 if the file failed to parse or couldn't be read.

By default colors are checked against sRGB. Pass `--gamut` to check against
a wider target instead:

```
./gamut-lint --gamut=p3 testdata/example.palette
./gamut-lint --gamut=rec2020 testdata/example.palette
```

Supported gamuts are `srgb` (the default), `p3` (Display P3), and
`rec2020` (Rec. 2020). A color out of sRGB gamut may still be in gamut for
P3 or Rec. 2020, since both cover a wider range of colors than a typical
monitor.

## Palette file format

One entry per line:

```
name: oklch(L C H)
```

- `L` is lightness: a number from 0 to 1, or a percentage (`70%`).
- `C` is chroma: a number from 0 upward, or a percentage of 0.4 (the
  reference maximum used by the CSS Color 4 spec).
- `H` is hue in degrees, optionally followed by `deg` (`145` or `145deg`).
- An optional alpha can follow as `/ A`, e.g. `oklch(0.7 0.1 30 / 50%)`.
- Lines starting with `#`, and blank lines, are ignored.
- The name and colon are optional; a bare `oklch(...)` is also valid.
- An entry can also be `rgb(R G B)`, e.g. `legacy: rgb(51 102 255)`. Each
  channel is a number from 0 to 255 or a percentage.
- An entry can also be `hsl(H S L)`, e.g. `legacy: hsl(220 100% 60%)`. Hue
  is in degrees as above; saturation and lightness must be percentages.
- Both `rgb()` and `hsl()` accept the same `/ A` alpha suffix as `oklch()`.
- A named entry can also be a hex color, e.g. `legacy: #3366ff`. Hex
  supports the 3, 4, 6, and 8 digit forms (RGB, RGBA, RRGGBB, RRGGBBAA).
- `rgb()`, `hsl()`, and hex all describe an sRGB value directly, so
  they're always in gamut; they're converted to OKLCH for reporting
  alongside `oklch()` entries. A hex color needs a name — a bare `#...`
  at the start of a line is indistinguishable from a comment.

## Error messages

Parse errors point at the exact line and column of the problem, not just
the line:

```
$ ./gamut-lint bad.palette
bad.palette:3:16: lightness 1.4 is out of range (expected 0 to 1, or 0% to 100%)
bad.palette:4:8: unsupported color function "lab" (supported: "oklch", "rgb", "hsl")
gamut-lint: 2 line(s) failed to parse
```

## How the gamut check works

Each OKLCH color is converted to OKLab. For sRGB, OKLab goes straight to
linear sRGB using the combined matrix from Björn Ottosson's OKLab reference
implementation. For Display P3 and Rec. 2020, OKLab is converted to CIE XYZ
(D65) first, then to that gamut's linear RGB using the standard matrix
derived from its primaries. A color is in gamut if all three linear RGB
channels fall within [0, 1]. When a color is out of gamut, gamut-lint
binary-searches chroma (holding lightness and hue fixed) to report the
largest chroma that would still fit.

## Limitations (for now)

- Input is `oklch()`, `rgb()`, `hsl()`, or hex. No `lab()` or `lch()`.
- Stops after reporting all parse errors in a file; doesn't attempt partial
  recovery mid-line.

## License

MIT, see LICENSE.
