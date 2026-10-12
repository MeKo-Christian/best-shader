package main

import (
	"fmt"
	"io"
	"math"
	"strings"
)

// analyze prints the quantitative description of an image.
func analyze(out io.Writer, name string, p *picture, f *features) {
	fmt.Fprintf(out, "imgstat analyze %s (%dx%d)\n", name, p.w, p.h)
	fmt.Fprintln(out, "Conventions: xy = pixel position / size, origin TOP-left, y down.")
	fmt.Fprintln(out, "             uv = shader position, origin BOTTOM-left, v = 1 - y.")
	fmt.Fprintln(out, "             Grids: top row = highest v (as on screen); rows show their v range,")
	fmt.Fprintln(out, "             columns the u of their left edge. Colours: sRGB hex.")
	fmt.Fprintln(out, "             Orientation = along the stroke, CCW in uv: 0 = horizontal, 45 = '/',")
	fmt.Fprintln(out, "             90 = vertical, 135 = '\\'. L = OKLab lightness 0..1.")

	mean := p.gridLab(1)[0]
	fmt.Fprintf(out, "Mean colour %s, OKLab (%.3f, %+.3f, %+.3f)\n", hex(mean), mean[0], mean[1], mean[2])

	printPalette(out, kmeans(p, paletteK))
	printColorMap(out, p)
	printDirections(out, f)
	printEdges(out, p)
	printTexture(out, f)
}

func section(out io.Writer, title string) {
	fmt.Fprintf(out, "\n== %s ==\n", title)
}

func printPalette(out io.Writer, clusters []cluster) {
	section(out, fmt.Sprintf("Palette: k-means k=%d in OKLab over all pixels, by share", len(clusters)))
	fmt.Fprintln(out, " #  hex      share   OKLab L   a      b       xy (top-left)  uv (bottom-left)  spread u, v")

	for i, c := range clusters {
		fmt.Fprintf(out, "%2d  %s  %5.1f%%  %.3f %+.3f %+.3f    %.2f %.2f      %.2f %.2f         %.2f %.2f\n",
			i+1, hex(c.lab), 100*c.share, c.lab[0], c.lab[1], c.lab[2],
			c.x, c.y, c.x, 1-c.y, c.sx, c.sy)
	}
}

// printGrid prints gridN x gridN cells, top row first (= highest v), with
// u/v labels.
func printGrid(out io.Writer, width int, cell func(i int) string) {
	const n = gridN

	fmt.Fprintf(out, "%-10s", "v \\ u")

	for col := range n {
		fmt.Fprintf(out, " %*s", width, fmt.Sprintf("%.2f", float64(col)/float64(n)))
	}

	fmt.Fprintln(out)

	for row := range n {
		vHi := 1 - float64(row)/float64(n)
		vLo := 1 - float64(row+1)/float64(n)
		fmt.Fprintf(out, "%.2f-%.2f  ", vLo, vHi)

		for col := range n {
			fmt.Fprintf(out, " %*s", width, cell(row*n+col))
		}

		fmt.Fprintln(out)
	}
}

func printColorMap(out io.Writer, p *picture) {
	grid := p.gridLab(gridN)

	section(out, fmt.Sprintf("Coarse colour map %dx%d: mean colour per cell (box filter in linear RGB)", gridN, gridN))
	printGrid(out, 7, func(i int) string { return hex(grid[i]) })

	section(out, fmt.Sprintf("Mean OKLab L per cell %dx%d", gridN, gridN))
	printGrid(out, 7, func(i int) string { return fmt.Sprintf("%.2f", grid[i][0]) })
}

func printDirections(out io.Writer, f *features) {
	deg, coh, energy := f.cellOrientation(gridN)

	section(out, fmt.Sprintf("Direction field %dx%d: stroke orientation deg / coherence 0..1", gridN, gridN))
	fmt.Fprintf(out, "(structure tensor of OKLab L, Sobel, Gaussian sigma %.0f px, summed per cell)\n", tensorSigma)
	printGrid(out, 8, func(i int) string { return fmt.Sprintf("%3.0f/%.2f", deg[i], coh[i]) })

	maxE := 0.0
	for _, e := range energy {
		maxE = max(maxE, e)
	}

	section(out, "Gradient energy per cell, relative to the strongest cell")
	printGrid(out, 7, func(i int) string { return fmt.Sprintf("%.2f", energy[i]/maxE) })

	const glyphN = 16

	gDeg, gCoh, _ := f.cellOrientation(glyphN)

	section(out, fmt.Sprintf("Orientation glyphs %dx%d: - / | \\, doubled = coherence > 0.5, '.' = below 0.15",
		glyphN, glyphN))

	for row := range glyphN {
		var sb strings.Builder

		for col := range glyphN {
			i := row*glyphN + col
			sb.WriteString(" " + glyph(gDeg[i], gCoh[i]))
		}

		fmt.Fprintf(out, "v %.2f %s\n", 1-(float64(row)+0.5)/glyphN, sb.String())
	}

	section(out, "Global orientation histogram (coherence*energy weighted)")

	peak := 0.0
	for _, v := range f.orientHist {
		peak = max(peak, v)
	}

	for i, v := range f.orientHist {
		bar := strings.Repeat("#", int(math.Round(v/peak*50)))
		fmt.Fprintf(out, "%3d-%3d deg %5.1f%% %s\n", i*10, i*10+10, 100*v, bar)
	}
}

// glyph draws an orientation as one of - / | \ and doubles it ("||") for
// strongly coherent cells.
func glyph(deg, coh float64) string {
	if coh < 0.15 {
		return " ."
	}

	g := []string{"-", "/", "|", "\\"}[int(math.Mod(deg+22.5, 180)/45)]
	if coh > 0.5 {
		return g + g
	}

	return " " + g
}

func printEdges(out io.Writer, p *picture) {
	hSegs, hPix := horizontalEdges(p)
	vSegs, vPix := verticalEdges(p)
	total := float64(p.w * p.h)

	section(out, "Long straight edges (rectangular block borders)")
	fmt.Fprintf(out, "Signed OKLab step across the edge (%d px bands), averaged over %d px ALONG the edge,\n",
		edgeBand, edgeAlong)
	fmt.Fprintf(out, "non-max suppressed, threshold dE %.2f, runs >= %d px. dE = mean step over the run.\n",
		edgeThresh, edgeMinLen)
	fmt.Fprintf(out, "Edge pixels: horizontal %d (%.2f%%), vertical %d (%.2f%%), vertical/horizontal %.2f\n",
		hPix, 100*float64(hPix)/total, vPix, 100*float64(vPix)/total, float64(vPix)/max(float64(hPix), 1))

	fmt.Fprintf(out, "\nHorizontal edges (block tops/bottoms), strongest %d by dE x length:\n", edgeTop)
	fmt.Fprintln(out, "  y px    v      u from-to    len px   dE     above    below")

	for _, s := range hSegs {
		fmt.Fprintf(out, "  %4d   %.3f   %.3f-%.3f   %4d    %.3f  %s  %s\n",
			s.pos, 1-float64(s.pos)/float64(p.h),
			float64(s.from)/float64(p.w), float64(s.to+1)/float64(p.w),
			s.to-s.from+1, s.contrast, hex(s.before), hex(s.after))
	}

	fmt.Fprintf(out, "\nVertical edges (block sides), strongest %d by dE x length:\n", edgeTop)
	fmt.Fprintln(out, "  x px    u      v from-to    len px   dE     left     right    (y px from-to)")

	for _, s := range vSegs {
		fmt.Fprintf(out, "  %4d   %.3f   %.3f-%.3f   %4d    %.3f  %s  %s  (%d-%d)\n",
			s.pos, float64(s.pos)/float64(p.w),
			1-float64(s.to+1)/float64(p.h), 1-float64(s.from)/float64(p.h),
			s.to-s.from+1, s.contrast, hex(s.before), hex(s.after), s.from, s.to)
	}
}

func printTexture(out io.Writer, f *features) {
	section(out, fmt.Sprintf("Fine texture: RMS of L - blur(L, sigma %.0f px) per cell", hfSigma))
	printGrid(out, 7, func(i int) string { return fmt.Sprintf("%.4f", math.Sqrt(f.hfEnergy[i])) })

	section(out, "Texture anisotropy per cell: energy of d/dx vs d/dy of the high-pass")
	fmt.Fprintln(out, "(> 1 = vertical streaks dominate, < 1 = horizontal streaks, 1 = isotropic)")
	printGrid(out, 7, func(i int) string {
		return fmt.Sprintf("%.2f", f.hfEx[i]/max(f.hfEy[i], 1e-12))
	})

	var e, ex, ey float64
	for i := range f.hfEnergy {
		e += f.hfEnergy[i]
		ex += f.hfEx[i]
		ey += f.hfEy[i]
	}

	n := float64(len(f.hfEnergy))
	fmt.Fprintf(out, "\nWhole image: high-pass RMS %.4f, d/dx : d/dy energy = %.2f (anisotropy %+.2f)\n",
		math.Sqrt(e/n), ex/max(ey, 1e-12), anisotropy(ex, ey))
}
