// Command imgstat describes an image quantitatively and scores renders against
// the reference painting by style and composition rather than pixel equality.
//
// Conventions used in every printout:
//   - xy  = pixel position, origin TOP-left, y pointing down (image files).
//   - uv  = shader position, origin BOTTOM-left, v = 1 - y/h (fragCoord/iResolution).
//   - Grids are printed in shader orientation: the top row is the highest v.
//   - Stroke orientation is the direction ALONG a stroke in degrees,
//     counter-clockwise in uv: 0 = horizontal, 45 = "/", 90 = vertical, 135 = "\".
//   - Colours are sRGB hex (paste as hexColor(0xrrggbb) in common.glsl).
package main

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
)

const usageText = `imgstat - image statistics and style scoring for the best-shader shader

Usage:
  go run ./cmd/imgstat analyze IMAGE.png
      Palette (k-means in OKLab), 8x8 colour map, stroke direction field,
      long horizontal/vertical block edges and fine texture statistics.

  go run ./cmd/imgstat score REF.png RENDER.png [RENDER.png ...]
      Compare one or more renders with REF (lower = better, REF vs itself = 0).
      One render prints a full breakdown, several print one table row each.

  go run ./cmd/imgstat diff REF.png RENDER.png OUT.png
      Write a 2x2 montage: render | ref / colour dE heatmap | orientation diff.

The reference is always the first image argument. Renders whose size differs
from REF are resampled to REF's size before comparison.
`

var errUsage = errors.New("bad arguments")

func main() {
	err := run(os.Args[1:])
	if errors.Is(err, errUsage) {
		fmt.Fprint(os.Stderr, usageText)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// subcommand describes one subcommand and how many arguments it takes.
type subcommand struct {
	minArgs, maxArgs int
	run              func(args []string) error
}

func run(args []string) error {
	if len(args) == 0 {
		return errUsage
	}

	if slices.Contains([]string{"help", "-h", "--help"}, args[0]) {
		fmt.Print(usageText)

		return nil
	}

	commands := map[string]subcommand{
		"analyze": {1, 1, func(a []string) error { return cmdAnalyze(a[0]) }},
		"score":   {2, math.MaxInt, func(a []string) error { return cmdScore(a[0], a[1:]) }},
		"diff":    {3, 3, func(a []string) error { return cmdDiff(a[0], a[1], a[2]) }},
	}

	cmd, ok := commands[args[0]]
	rest := args[1:]

	if !ok || len(rest) < cmd.minArgs || len(rest) > cmd.maxArgs {
		return errUsage
	}

	return cmd.run(rest)
}

func cmdAnalyze(path string) error {
	pic, err := loadPicture(path)
	if err != nil {
		return err
	}

	analyze(os.Stdout, filepath.Base(path), pic, extract(pic))

	return nil
}

func cmdScore(refPath string, renderPaths []string) error {
	ref, err := loadPicture(refPath)
	if err != nil {
		return err
	}

	refFeat := extract(ref)
	results := make([][]component, 0, len(renderPaths))

	for _, path := range renderPaths {
		feat, err := loadFeatures(path, ref.w, ref.h)
		if err != nil {
			return err
		}

		results = append(results, compare(refFeat, feat))
	}

	if len(renderPaths) == 1 {
		printBreakdown(os.Stdout, refPath, renderPaths[0], results[0])
	} else {
		printTable(os.Stdout, refPath, renderPaths, results)
	}

	return nil
}

func cmdDiff(refPath, renderPath, outPath string) error {
	ref, err := loadPicture(refPath)
	if err != nil {
		return err
	}

	feat, err := loadFeatures(renderPath, ref.w, ref.h)
	if err != nil {
		return err
	}

	return writeDiff(os.Stdout, extract(ref), feat, outPath)
}

// loadFeatures loads an image, resamples it to w x h if needed and extracts
// its features.
func loadFeatures(path string, w, h int) (*features, error) {
	pic, err := loadPicture(path)
	if err != nil {
		return nil, err
	}

	if pic.w != w || pic.h != h {
		fmt.Fprintf(os.Stderr, "note: %s is %dx%d, resampled to %dx%d\n", path, pic.w, pic.h, w, h)
		pic = pic.resampled(w, h)
	}

	return extract(pic), nil
}
