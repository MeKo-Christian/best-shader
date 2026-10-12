package main

import (
	"fmt"
	"io"
	"math"
	"path/filepath"
	"slices"
	"strings"
)

// component is one term of the style score.
//
// Normalisation: raw values have different units (OKLab dE, histogram
// distances, degrees, log ratios). Each is divided by its scale, the raw value
// a "knows nothing" render gets: a flat image in the reference's mean colour
// (#805e58) scores about 1.0 on every component, and the reference
// against itself scores 0. So "0.3" means "30% of the way from perfect to a
// featureless guess" for every component, and the weights below express
// importance, not unit conversion. The scales were measured once against
// reference/reference.png (see README) and are fixed so scores stay
// comparable over time.
type component struct {
	key    string // short column name for the table
	name   string
	raw    float64
	scale  float64
	weight float64
}

func (c component) norm() float64 { return c.raw / c.scale }

const (
	swdStride = 4   // sliced Wasserstein uses every 4th pixel per axis
	texLogMax = 4.6 // per-cell |ln energy ratio| saturates at ln(100)
	texFloor  = 1e-6
)

// compare scores render against ref. Both must have the same size.
func compare(ref, render *features) []component {
	dirCell, dirHist := directionDistance(ref, render)
	texEnergy, texAniso := textureDistance(ref, render)

	return []component{
		{"col16", "colour/composition: mean OKLab dE at 16x16", gridDeltaE(ref.pic, render.pic, 16), 0.224, 0.20},
		{"col32", "colour/composition: mean OKLab dE at 32x32", gridDeltaE(ref.pic, render.pic, 32), 0.230, 0.15},
		{"palH", "palette: Hellinger distance, 8^3 OKLab histogram", hellinger(ref.colorHist, render.colorHist), 0.873, 0.10},
		{"palW", "palette: sliced Wasserstein distance in OKLab (dE)", slicedWasserstein(ref.pic, render.pic), 0.121, 0.10},
		{"dirC", "direction: per-cell angle diff / 90, ref-coherence weighted", dirCell, 0.50, 0.15},
		{"dirH", "direction: orientation histogram circular EMD / 90 deg", dirHist, 0.245, 0.10},
		{"texE", "texture: per-cell |ln high-pass energy ratio| (cap ln 100)", texEnergy, texLogMax, 0.12},
		{"texA", "texture: per-cell |anisotropy diff| / 2", texAniso, 0.193, 0.08},
	}
}

func total(comps []component) float64 {
	sum, weights := 0.0, 0.0
	for _, c := range comps {
		sum += c.weight * c.norm()
		weights += c.weight
	}

	return sum / weights
}

// gridDeltaE is the mean OKLab distance between n x n box-filtered versions.
func gridDeltaE(a, b *picture, n int) float64 {
	ga, gb := a.gridLab(n), b.gridLab(n)
	sum := 0.0

	for i := range ga {
		sum += deltaE(ga[i], gb[i])
	}

	return sum / float64(len(ga))
}

// hellinger distance between two normalised histograms, 0..1.
func hellinger(p, q []float64) float64 {
	bc := 0.0
	for i := range p {
		bc += math.Sqrt(p[i] * q[i])
	}

	return math.Sqrt(max(0, 1-bc))
}

// slicedWasserstein approximates the earth mover's distance between the
// pixel colour distributions: the 1D Wasserstein-1 distance (mean difference
// of sorted projections) averaged over the three OKLab axes and the four
// space diagonals. Unit: OKLab dE.
func slicedWasserstein(a, b *picture) float64 {
	dirs := make([][3]float64, 0, 7)
	dirs = append(dirs, [3]float64{1, 0, 0}, [3]float64{0, 1, 0}, [3]float64{0, 0, 1})

	for _, s := range [][2]float64{{1, 1}, {1, -1}, {-1, 1}, {-1, -1}} {
		k := 1 / math.Sqrt(3)
		dirs = append(dirs, [3]float64{k, s[0] * k, s[1] * k})
	}

	sum := 0.0

	for _, d := range dirs {
		pa, pb := project(a, d), project(b, d)

		w1 := 0.0
		for i := range pa {
			w1 += math.Abs(pa[i] - pb[i])
		}

		sum += w1 / float64(len(pa))
	}

	return sum / float64(len(dirs))
}

// project returns the sorted projections of a subsample of p onto dir.
func project(p *picture, dir [3]float64) []float64 {
	out := make([]float64, 0, (p.w/swdStride+1)*(p.h/swdStride+1))

	for y := 0; y < p.h; y += swdStride {
		for x := 0; x < p.w; x += swdStride {
			i := y*p.w + x
			out = append(out, dir[0]*p.lab[0][i]+dir[1]*p.lab[1][i]+dir[2]*p.lab[2][i])
		}
	}

	slices.Sort(out)

	return out
}

// directionDistance compares stroke directions.
//
// cell: per gridN cell the angle difference / 90 (0..1), weighted by the
// reference cell's coherence, so cells where the reference has no clear
// direction do not count. A render cell without any structure counts as
// 0.5 (the expectation for a random orientation).
//
// hist: circular earth mover's distance between the global orientation
// histograms in degrees / 90 (0..1; 1 = all mass rotated by 90 degrees).
func directionDistance(ref, render *features) (float64, float64) {
	rDeg, rCoh, _ := ref.cellOrientation(gridN)
	sDeg, _, sEnergy := render.cellOrientation(gridN)

	sum, weights := 0.0, 0.0

	for i := range rDeg {
		d := 0.5
		if sEnergy[i] > 1e-12 {
			d = angleDiff(rDeg[i], sDeg[i]) / 90
		}

		sum += rCoh[i] * d
		weights += rCoh[i]
	}

	cell := 0.0
	if weights > 0 {
		cell = sum / weights
	}

	return cell, circularEMD(ref.orientHist, render.orientHist) * 180 / orientBins / 90
}

// circularEMD is the Wasserstein-1 distance between two normalised
// histograms on a circle, in bins: sum |F_k - median(F)| over the cumulative
// differences F.
func circularEMD(p, q []float64) float64 {
	cum := make([]float64, len(p))
	acc := 0.0

	for i := range p {
		acc += p[i] - q[i]
		cum[i] = acc
	}

	sorted := slices.Clone(cum)
	slices.Sort(sorted)
	med := sorted[len(sorted)/2]

	dist := 0.0
	for _, c := range cum {
		dist += math.Abs(c - med)
	}

	return dist
}

// textureDistance compares the fine texture per gridN cell.
//
// energy: mean over cells of |ln(E_render / E_ref)| of the high-pass energy,
// each cell capped at ln(100) so a flat render saturates instead of
// diverging.
//
// aniso: mean over cells of |A_render - A_ref| / 2 with A in -1..1 the
// vertical-vs-horizontal streak anisotropy (0..1).
func textureDistance(ref, render *features) (float64, float64) {
	n := float64(len(ref.hfEnergy))
	energy, aniso := 0.0, 0.0

	for i := range ref.hfEnergy {
		ratio := (render.hfEnergy[i] + texFloor) / (ref.hfEnergy[i] + texFloor)
		energy += min(math.Abs(math.Log(ratio)), texLogMax)

		aRef := anisotropy(ref.hfEx[i], ref.hfEy[i])
		aRen := anisotropy(render.hfEx[i], render.hfEy[i])
		aniso += math.Abs(aRen-aRef) / 2
	}

	return energy / n, aniso / n
}

func printBreakdown(out io.Writer, refPath, renderPath string, comps []component) {
	fmt.Fprintf(out, "imgstat score %s vs %s (lower = better)\n\n", renderPath, refPath)
	fmt.Fprintf(out, "%-6s %-62s %8s %7s %6s %6s %7s\n", "key", "component", "raw", "scale", "norm", "weight", "contrib")

	weights := 0.0
	for _, c := range comps {
		weights += c.weight
	}

	for _, c := range comps {
		fmt.Fprintf(out, "%-6s %-62s %8.4f %7.3f %6.3f %6.2f %7.4f\n",
			c.key, c.name, c.raw, c.scale, c.norm(), c.weight, c.weight*c.norm()/weights)
	}

	fmt.Fprintf(out, "\nscore %.4f   (weighted mean of norm; 0 = identical, ~1 = flat mean-colour image)\n", total(comps))
}

func printTable(out io.Writer, refPath string, renderPaths []string, results [][]component) {
	fmt.Fprintf(out, "imgstat score vs %s (normalised components, lower = better; 0 = identical,\n", refPath)
	fmt.Fprintln(out, "~1 = flat mean-colour image)")

	legend := make([]string, 0, len(results[0]))
	for _, c := range results[0] {
		legend = append(legend, fmt.Sprintf("%s w=%.2f", c.key, c.weight))
	}

	fmt.Fprintf(out, "weights: %s\n\n", strings.Join(legend, ", "))

	width := 6
	for _, p := range renderPaths {
		width = max(width, len(filepath.Base(p)))
	}

	fmt.Fprintf(out, "%-*s", width, "render")

	for _, c := range results[0] {
		fmt.Fprintf(out, " %6s", c.key)
	}

	fmt.Fprintf(out, " %7s\n", "score")

	for i, comps := range results {
		fmt.Fprintf(out, "%-*s", width, filepath.Base(renderPaths[i]))

		for _, c := range comps {
			fmt.Fprintf(out, " %6.3f", c.norm())
		}

		fmt.Fprintf(out, " %7.4f\n", total(comps))
	}
}
