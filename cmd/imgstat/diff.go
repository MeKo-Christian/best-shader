package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"math"
	"os"
)

const (
	diffGridN   = 32  // cells per axis for both difference maps
	diffTile    = 512 // tile size of the montage
	diffGap     = 8
	diffMaxDE   = 0.3 // dE heatmap saturates here
	diffMinCohW = 0.1 // orientation map brightness floor for incoherent ref cells
)

// writeDiff writes a 2x2 montage:
//
//	render            | reference
//	dE heatmap 32x32  | orientation difference 32x32
//
// Both maps use an inferno-like colour ramp (black = equal). In the
// orientation map the brightness also scales with the reference coherence,
// so cells where the reference has no clear stroke direction stay dark.
func writeDiff(out io.Writer, ref, render *features, path string) error {
	tile := min(diffTile, ref.pic.w, ref.pic.h)
	img := image.NewRGBA(image.Rect(0, 0, 2*tile+diffGap, 2*tile+diffGap))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{16, 16, 16, 255}}, image.Point{}, draw.Src)

	maxDE, meanDE := drawDeltaE(img, ref.pic, render.pic, tile)
	meanDiff := drawOrientationDiff(img, ref, render, tile)

	drawPicture(img, render.pic, image.Pt(0, 0), tile)
	drawPicture(img, ref.pic, image.Pt(tile+diffGap, 0), tile)

	file, err := os.Create(path) //nolint:gosec // writing a user-given path is the point of a CLI
	if err != nil {
		return err
	}

	err = png.Encode(file, img)
	if cerr := file.Close(); err == nil {
		err = cerr
	}

	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	fmt.Fprintf(out, "wrote %s: render | ref / dE %dx%d | orientation diff %dx%d\n", path,
		diffGridN, diffGridN, diffGridN, diffGridN)
	fmt.Fprintf(out, "dE map: black = 0, yellow = %.2f+ (mean %.3f, max %.3f)\n", diffMaxDE, meanDE, maxDE)
	fmt.Fprintf(out, "orientation map: black = same, yellow = 90 deg off, dimmed where ref coherence is low "+
		"(coherence-weighted mean %.1f deg)\n", meanDiff)

	return nil
}

// drawPicture draws p scaled to tile x tile at origin.
func drawPicture(img *image.RGBA, p *picture, origin image.Point, tile int) {
	small := p.resampled(tile, tile)
	for y := range tile {
		for x := range tile {
			i := y*tile + x

			var c [3]uint8
			for k := range 3 {
				c[k] = uint8(math.Round(srgbEncode(small.lin[k][i]) * 255))
			}

			img.SetRGBA(origin.X+x, origin.Y+y, color.RGBA{c[0], c[1], c[2], 255})
		}
	}
}

// drawCells fills a diffGridN x diffGridN grid of nearest-upscaled cells.
func drawCells(img *image.RGBA, origin image.Point, tile int, cell func(i int) color.RGBA) {
	for y := range tile {
		cy := y * diffGridN / tile
		for x := range tile {
			img.SetRGBA(origin.X+x, origin.Y+y, cell(cy*diffGridN+x*diffGridN/tile))
		}
	}
}

// drawDeltaE draws the per-cell dE heatmap and returns the maximum and mean dE.
func drawDeltaE(img *image.RGBA, ref, render *picture, tile int) (float64, float64) {
	ga, gb := ref.gridLab(diffGridN), render.gridLab(diffGridN)
	de := make([]float64, len(ga))
	maxDE, meanDE := 0.0, 0.0

	for i := range ga {
		de[i] = deltaE(ga[i], gb[i])
		maxDE = max(maxDE, de[i])
		meanDE += de[i] / float64(len(ga))
	}

	drawCells(img, image.Pt(0, tile+diffGap), tile, func(i int) color.RGBA {
		return inferno(de[i]/diffMaxDE, 1)
	})

	return maxDE, meanDE
}

// drawOrientationDiff draws the per-cell orientation difference map and
// returns the coherence-weighted mean difference in degrees.
func drawOrientationDiff(img *image.RGBA, ref, render *features, tile int) float64 {
	rDeg, rCoh, _ := ref.cellOrientation(diffGridN)
	sDeg, _, _ := render.cellOrientation(diffGridN)

	maxCoh, weights, meanDiff := 0.0, 0.0, 0.0
	diff := make([]float64, len(rDeg))

	for i := range rDeg {
		diff[i] = angleDiff(rDeg[i], sDeg[i])
		maxCoh = max(maxCoh, rCoh[i])
		meanDiff += rCoh[i] * diff[i]
		weights += rCoh[i]
	}

	drawCells(img, image.Pt(tile+diffGap, tile+diffGap), tile, func(i int) color.RGBA {
		bright := diffMinCohW + (1-diffMinCohW)*rCoh[i]/max(maxCoh, 1e-9)

		return inferno(diff[i]/90, bright)
	})

	return meanDiff / max(weights, 1e-12)
}

// inferno maps t in 0..1 to an inferno-like ramp, scaled by brightness.
func inferno(t, brightness float64) color.RGBA {
	stops := [][3]float64{{0, 0, 4}, {87, 16, 110}, {188, 55, 84}, {249, 142, 9}, {252, 255, 164}}
	t = min(max(t, 0), 1) * float64(len(stops)-1)
	i := min(int(t), len(stops)-2)
	f := t - float64(i)

	var c [3]uint8
	for k := range 3 {
		c[k] = uint8(math.Round((stops[i][k]*(1-f) + stops[i+1][k]*f) * brightness))
	}

	return color.RGBA{c[0], c[1], c[2], 255}
}
