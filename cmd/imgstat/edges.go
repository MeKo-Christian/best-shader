package main

import (
	"math"
	"slices"
)

// Long straight edge detection. The painting is built from overlapping
// rectangular blocks; their borders are long horizontal and vertical colour
// steps. Brush texture also has strong gradients, but they are short and
// change sign along the edge, so averaging the SIGNED colour step along the
// edge direction cancels texture while a straight block border survives.
const (
	edgeBand   = 3    // rows averaged on each side of a candidate edge
	edgeAlong  = 33   // box length along the edge in pixels
	edgeNMS    = 3    // non-maximum suppression radius across the edge
	edgeThresh = 0.04 // minimum smoothed OKLab step
	edgeGap    = 6    // gap (pixels) tolerated inside one run
	edgeMinLen = 48   // shortest reported run
	edgeSide   = 6    // depth of the colour samples on either side
	edgeTop    = 15   // runs reported per direction
)

// edgeSeg is a straight edge running along the rows of the analysed plane.
type edgeSeg struct {
	pos      int        // row of the edge
	from, to int        // extent along the row, inclusive
	contrast float64    // mean smoothed OKLab step
	strength float64    // contrast summed over the run
	before   [3]float64 // OKLab mean colour on the lower-index side
	after    [3]float64 // OKLab mean colour on the higher-index side
}

// horizontalEdges finds edges running along x. Before/after are above/below.
func horizontalEdges(p *picture) ([]edgeSeg, int) {
	return findEdges(p.lab, p.w, p.h)
}

// verticalEdges finds edges running along y by transposing the image.
// pos is the column; before/after are left/right; from/to are rows.
func verticalEdges(p *picture) ([]edgeSeg, int) {
	var lab [3][]float64
	for c := range 3 {
		lab[c] = transpose(p.lab[c], p.w, p.h)
	}

	return findEdges(lab, p.h, p.w)
}

// findEdges detects edges along the rows of a w x h OKLab image and returns
// the strongest runs plus the number of edge pixels after suppression.
func findEdges(lab [3][]float64, w, h int) ([]edgeSeg, int) {
	step := edgeStep(lab, w, h)
	keep := make([]bool, w*h)
	pixels := 0

	for y := range h {
		for x := range w {
			s := step[y*w+x]
			if s < edgeThresh {
				continue
			}

			isMax := true
			for k := 1; k <= edgeNMS && isMax; k++ {
				isMax = s >= step[clampInt(y-k, h)*w+x] && s >= step[clampInt(y+k, h)*w+x]
			}

			if isMax {
				keep[y*w+x] = true
				pixels++
			}
		}
	}

	var cands []edgeSeg
	for y := edgeSide; y < h-edgeSide; y++ {
		cands = append(cands, rowRuns(step, keep, w, h, y)...)
	}

	segs := pickDistinct(cands, edgeTop)
	for i := range segs {
		segs[i].before = meanBand(lab, w, segs[i], segs[i].pos-edgeSide, segs[i].pos-2)
		segs[i].after = meanBand(lab, w, segs[i], segs[i].pos+2, segs[i].pos+edgeSide)
	}

	return segs, pixels
}

// edgeStep is the magnitude of the signed OKLab step across each row
// (mean of edgeBand rows below minus edgeBand rows above), averaged along
// the row over edgeAlong pixels before taking the norm.
func edgeStep(lab [3][]float64, w, h int) []float64 {
	sumSq := make([]float64, w*h)
	diff := make([]float64, w)
	prefix := make([]float64, w+1)

	for c := range 3 {
		plane := lab[c]

		for y := range h {
			for x := range w {
				d := 0.0
				for k := 1; k <= edgeBand; k++ {
					d += plane[clampInt(y+k, h)*w+x] - plane[clampInt(y-k, h)*w+x]
				}

				diff[x] = d / edgeBand
			}

			for x := range w {
				prefix[x+1] = prefix[x] + diff[x]
			}

			for x := range w {
				lo, hi := max(x-edgeAlong/2, 0), min(x+edgeAlong/2+1, w)
				m := (prefix[hi] - prefix[lo]) / float64(hi-lo)
				sumSq[y*w+x] += m * m
			}
		}
	}

	for i := range sumSq {
		sumSq[i] = math.Sqrt(sumSq[i])
	}

	return sumSq
}

// rowRuns collects runs of edge pixels around row y. Pixels kept in the
// neighbouring rows count too, so a slightly wobbly edge stays one run.
func rowRuns(step []float64, keep []bool, w, h, y int) []edgeSeg {
	var runs []edgeSeg

	start, last, sum := -1, -1, 0.0
	flush := func() {
		if start >= 0 && last-start+1 >= edgeMinLen {
			runs = append(runs, edgeSeg{
				pos: y, from: start, to: last,
				strength: sum, contrast: sum / float64(last-start+1),
			})
		}

		start, sum = -1, 0
	}

	for x := range w {
		best := keptStep(step, keep, w, h, x, y)
		if best == 0 {
			if start >= 0 && x-last > edgeGap {
				flush()
			}

			continue
		}

		if start < 0 {
			start = x
		}

		last = x
		sum += best
	}

	flush()

	return runs
}

// keptStep returns the strongest kept edge step at column x in rows y-1..y+1,
// or 0 if none is kept.
func keptStep(step []float64, keep []bool, w, h, x, y int) float64 {
	best := 0.0

	for yy := max(y-1, 0); yy <= min(y+1, h-1); yy++ {
		if keep[yy*w+x] {
			best = max(best, step[yy*w+x])
		}
	}

	return best
}

// meanBand averages the OKLab colour of rows y0..y1 over the extent of seg.
func meanBand(lab [3][]float64, w int, seg edgeSeg, y0, y1 int) [3]float64 {
	var acc [3]float64

	n := 0.0

	for y := y0; y <= y1; y++ {
		for x := seg.from; x <= seg.to; x++ {
			for c := range 3 {
				acc[c] += lab[c][y*w+x]
			}

			n++
		}
	}

	for c := range acc {
		acc[c] /= n
	}

	return acc
}

// pickDistinct returns up to n runs by strength, skipping runs that mostly
// overlap an already picked run within a few rows (the same edge).
func pickDistinct(cands []edgeSeg, n int) []edgeSeg {
	slices.SortFunc(cands, func(a, b edgeSeg) int {
		if a.strength > b.strength {
			return -1
		}

		if a.strength < b.strength {
			return 1
		}

		return 0
	})

	var out []edgeSeg

	for _, c := range cands {
		if len(out) == n {
			break
		}

		dup := false

		for _, o := range out {
			overlap := min(c.to, o.to) - max(c.from, o.from) + 1
			shorter := min(c.to-c.from, o.to-o.from) + 1

			if abs(c.pos-o.pos) <= 10 && 2*overlap > shorter {
				dup = true

				break
			}
		}

		if !dup {
			out = append(out, c)
		}
	}

	return out
}

func abs(v int) int {
	if v < 0 {
		return -v
	}

	return v
}
