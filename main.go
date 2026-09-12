// gamut-lint checks a palette of OKLCH colors against the sRGB gamut and
// reports which ones a typical monitor can't actually display.
package main

import (
	"bufio"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: gamut-lint <palette-file>")
		os.Exit(2)
	}
	path := os.Args[1]

	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gamut-lint: %v\n", err)
		os.Exit(2)
	}
	defer f.Close()

	var entries []*Entry
	parseErrors := 0

	sc := bufio.NewScanner(f)
	lineNum := 0
	for sc.Scan() {
		lineNum++
		entry, err := ParseLine(path, lineNum, sc.Text())
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			parseErrors++
			continue
		}
		if entry != nil {
			entries = append(entries, entry)
		}
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "gamut-lint: reading %s: %v\n", path, err)
		os.Exit(2)
	}
	if parseErrors > 0 {
		fmt.Fprintf(os.Stderr, "gamut-lint: %d line(s) failed to parse\n", parseErrors)
		os.Exit(2)
	}

	outOfGamut := 0
	for _, e := range entries {
		label := e.Name
		if label == "" {
			label = "(unnamed)"
		}
		result := checkGamut(e)
		if result.InGamut {
			fmt.Printf("%s:%d:%d: %-20s in gamut\n", path, e.Pos.Line, e.Pos.Col, label)
			continue
		}
		outOfGamut++
		fmt.Printf("%s:%d:%d: %-20s OUT OF GAMUT (chroma %.4f exceeds max %.4f for this lightness/hue)\n",
			path, e.Pos.Line, e.Pos.Col, label, e.C, result.MaxChroma)
	}

	fmt.Printf("\n%d color(s) checked, %d out of gamut\n", len(entries), outOfGamut)
	if outOfGamut > 0 {
		os.Exit(1)
	}
}
