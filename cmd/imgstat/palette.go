package main

import (
	"math"
	"math/rand/v2"
	"slices"
)

const (
	paletteK      = 10
	kmeansStride  = 2 // fit on every 2nd pixel per axis, assign all pixels afterwards
	kmeansMaxIter = 60
)

// cluster is one palette entry with its spatial distribution.
type cluster struct {
	lab    [3]float64
	share  float64 // fraction of all pixels
	x, y   float64 // mean position, x/w and y/h (origin top-left)
	sx, sy float64 // standard deviation of the position
	count  int     // pixels assigned

	sum, sumSq [2]float64 // position accumulators (x, y)
}

// kmeans clusters all pixels in OKLab into k colours (k-means++ seeding with
// a fixed seed, so the output is deterministic). Sorted by share, descending.
func kmeans(p *picture, k int) []cluster {
	var samples [][3]float64

	for y := 0; y < p.h; y += kmeansStride {
		for x := 0; x < p.w; x += kmeansStride {
			i := y*p.w + x
			samples = append(samples, [3]float64{p.lab[0][i], p.lab[1][i], p.lab[2][i]})
		}
	}

	// A fixed seed keeps the palette stable between runs; no crypto here.
	rng := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // deterministic seeding wanted
	centers := seedCenters(samples, k, rng)

	for range kmeansMaxIter {
		var moved float64

		centers, moved = lloydStep(samples, centers)
		if moved < 1e-5 {
			break
		}
	}

	return describeClusters(p, centers)
}

// lloydStep assigns samples to their nearest centre and moves every centre
// to the mean of its samples. It returns the new centres and the largest move.
func lloydStep(samples, centers [][3]float64) ([][3]float64, float64) {
	next := make([][3]float64, len(centers))
	counts := make([]int, len(centers))

	for _, s := range samples {
		j := nearest(centers, s)
		for c := range 3 {
			next[j][c] += s[c]
		}

		counts[j]++
	}

	moved := 0.0

	for j := range next {
		if counts[j] == 0 {
			next[j] = centers[j] // keep an empty cluster where it was

			continue
		}

		for c := range 3 {
			next[j][c] /= float64(counts[j])
		}

		moved = max(moved, deltaE(next[j], centers[j]))
	}

	return next, moved
}

// seedCenters implements k-means++ seeding.
func seedCenters(samples [][3]float64, k int, rng *rand.Rand) [][3]float64 {
	centers := [][3]float64{samples[rng.IntN(len(samples))]}
	dist := make([]float64, len(samples))

	for len(centers) < k {
		total := 0.0

		for i, s := range samples {
			d := deltaE(s, centers[nearest(centers, s)])
			dist[i] = d * d
			total += dist[i]
		}

		target := rng.Float64() * total
		pick := len(samples) - 1

		for i, d := range dist {
			target -= d
			if target <= 0 {
				pick = i

				break
			}
		}

		centers = append(centers, samples[pick])
	}

	return centers
}

func nearest(centers [][3]float64, s [3]float64) int {
	best, bestD := 0, math.Inf(1)

	for j, c := range centers {
		dl, da, db := s[0]-c[0], s[1]-c[1], s[2]-c[2]
		if d := dl*dl + da*da + db*db; d < bestD {
			best, bestD = j, d
		}
	}

	return best
}

// describeClusters assigns every pixel to its nearest centre and gathers
// share and position statistics.
func describeClusters(p *picture, centers [][3]float64) []cluster {
	out := make([]cluster, len(centers))
	for j := range out {
		out[j].lab = centers[j]
	}

	for y := range p.h {
		fy := (float64(y) + 0.5) / float64(p.h)
		for x := range p.w {
			fx := (float64(x) + 0.5) / float64(p.w)
			i := y*p.w + x
			cl := &out[nearest(centers, [3]float64{p.lab[0][i], p.lab[1][i], p.lab[2][i]})]
			cl.count++
			cl.sum[0] += fx
			cl.sum[1] += fy
			cl.sumSq[0] += fx * fx
			cl.sumSq[1] += fy * fy
		}
	}

	total := float64(p.w * p.h)

	for j := range out {
		cl := &out[j]
		if cl.count == 0 {
			continue
		}

		n := float64(cl.count)
		cl.share = n / total
		cl.x, cl.y = cl.sum[0]/n, cl.sum[1]/n
		cl.sx = math.Sqrt(max(cl.sumSq[0]/n-cl.x*cl.x, 0))
		cl.sy = math.Sqrt(max(cl.sumSq[1]/n-cl.y*cl.y, 0))
	}

	slices.SortFunc(out, func(a, b cluster) int { return b.count - a.count })

	return out
}
