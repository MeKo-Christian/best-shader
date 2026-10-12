package main

import "math"

const (
	gridN       = 8   // cells per axis for the direction and texture grids
	tensorSigma = 3.0 // structure tensor smoothing, pixels
	hfSigma     = 4.0 // fine texture = L - blur(L, hfSigma)
	orientBins  = 18  // global orientation histogram, 10 degrees per bin
	histBins    = 8   // OKLab colour histogram bins per axis

	// OKLab histogram ranges. a and b of the sRGB gamut lie in about
	// [-0.31, 0.28]; values outside are clamped into the edge bins.
	labABRange = 0.3
)

// features holds everything that analyze, score and diff derive from an image.
type features struct {
	pic        *picture
	tensor     tensor    // smoothed structure tensor of OKLab L
	orientHist []float64 // orientBins, coherence*energy weighted, sums to 1
	colorHist  []float64 // histBins^3 soft OKLab histogram, sums to 1

	// Fine texture per gridN x gridN cell (row-major, top row first).
	hfEnergy []float64 // mean squared high-pass of L
	hfEx     []float64 // mean squared x-derivative of the high-pass (vertical streaks)
	hfEy     []float64 // mean squared y-derivative of the high-pass (horizontal streaks)
}

func extract(p *picture) *features {
	f := &features{pic: p}
	f.tensor = structureTensor(p.lab[0], p.w, p.h, tensorSigma)
	f.orientHist = orientationHistogram(f.tensor)
	f.colorHist = colorHistogram(p)
	f.fineTexture()

	return f
}

// cellOrientation sums the tensor over an n x n grid and returns orientation
// (degrees), coherence and mean energy per cell.
func (f *features) cellOrientation(n int) ([]float64, []float64, []float64) {
	w, h := f.pic.w, f.pic.h
	xx := boxMeans(f.tensor.xx, w, h, n, n)
	xy := boxMeans(f.tensor.xy, w, h, n, n)
	yy := boxMeans(f.tensor.yy, w, h, n, n)

	deg := make([]float64, n*n)
	coh := make([]float64, n*n)
	energy := make([]float64, n*n)

	for i := range deg {
		deg[i], coh[i], energy[i] = orientation(xx[i], xy[i], yy[i])
	}

	return deg, coh, energy
}

// orientationHistogram bins per-pixel stroke orientations, weighted by
// coherence*energy, with linear interpolation between neighbouring bins
// (circular). An image without any structure gets a uniform histogram.
func orientationHistogram(t tensor) []float64 {
	hist := make([]float64, orientBins)
	total := 0.0

	for i := range t.xx {
		deg, coh, energy := orientation(t.xx[i], t.xy[i], t.yy[i])
		weight := coh * energy

		pos := deg/180*orientBins - 0.5
		i0 := int(math.Floor(pos))
		frac := pos - float64(i0)
		hist[(i0+orientBins)%orientBins] += weight * (1 - frac)
		hist[(i0+1)%orientBins] += weight * frac
		total += weight
	}

	return normalized(hist, total)
}

// colorHistogram is a soft (trilinear) OKLab histogram over all pixels.
func colorHistogram(p *picture) []float64 {
	hist := make([]float64, histBins*histBins*histBins)
	lo := [3]float64{0, -labABRange, -labABRange}
	hi := [3]float64{1, labABRange, labABRange}

	for i := range p.lab[0] {
		var idx [3][2]int

		var wt [3][2]float64

		for c := range 3 {
			pos := (p.lab[c][i]-lo[c])/(hi[c]-lo[c])*histBins - 0.5
			pos = min(max(pos, 0), histBins-1)
			i0 := min(int(pos), histBins-2)
			frac := pos - float64(i0)
			idx[c] = [2]int{i0, i0 + 1}
			wt[c] = [2]float64{1 - frac, frac}
		}

		for a := range 2 {
			for b := range 2 {
				for c := range 2 {
					bin := (idx[0][a]*histBins+idx[1][b])*histBins + idx[2][c]
					hist[bin] += wt[0][a] * wt[1][b] * wt[2][c]
				}
			}
		}
	}

	return normalized(hist, float64(len(p.lab[0])))
}

func normalized(hist []float64, total float64) []float64 {
	for i := range hist {
		if total > 0 {
			hist[i] /= total
		} else {
			hist[i] = 1 / float64(len(hist))
		}
	}

	return hist
}

// fineTexture measures the high-pass of OKLab L per cell: its energy and how
// much of it varies along x (vertical streaks) versus along y (horizontal
// streaks).
func (f *features) fineTexture() {
	w, h := f.pic.w, f.pic.h
	lum := f.pic.lab[0]
	blur := gaussianBlur(lum, w, h, hfSigma)

	hf := make([]float64, w*h)
	for i := range hf {
		hf[i] = lum[i] - blur[i]
	}

	sq := make([]float64, w*h)
	ex := make([]float64, w*h)
	ey := make([]float64, w*h)

	for y := range h {
		for x := range w {
			i := y*w + x
			dx := (hf[y*w+clampInt(x+1, w)] - hf[y*w+clampInt(x-1, w)]) / 2
			dy := (hf[clampInt(y+1, h)*w+x] - hf[clampInt(y-1, h)*w+x]) / 2
			sq[i] = hf[i] * hf[i]
			ex[i] = dx * dx
			ey[i] = dy * dy
		}
	}

	f.hfEnergy = boxMeans(sq, w, h, gridN, gridN)
	f.hfEx = boxMeans(ex, w, h, gridN, gridN)
	f.hfEy = boxMeans(ey, w, h, gridN, gridN)
}

// anisotropy maps the x/y derivative energies of the high-pass to -1..1:
// +1 = pure vertical streaks, -1 = pure horizontal streaks, 0 = isotropic.
func anisotropy(ex, ey float64) float64 {
	if ex+ey <= 1e-12 {
		return 0
	}

	return (ex - ey) / (ex + ey)
}
