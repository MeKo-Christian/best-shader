package main

import (
	"fmt"
	"math"
	"runtime"
	"sync"
)

// report is the outcome of verify.
type report struct {
	samples    int
	minGap     float64 // smallest SAT gap lower bound between two cubes
	gapU       float64
	gapPair    [2]int
	slack      float64 // what the gap may shrink between two samples
	minFloor   float64 // smallest clearance above the floor
	floorU     float64
	regularOK  bool // lemma 1 holds after expanding (u = ua) and before packing (u = uz)
	targetOK   bool
	peakRadius float64 // largest centre distance of any cube point
	clusterOut float64 // how far a cube pokes out of its cluster sphere (must be <= 0)
}

func (r report) ok() bool {
	return r.regularOK && r.targetOK && r.minGap > r.slack && r.minFloor > 0 && r.clusterOut <= 0
}

func (r report) String() string {
	return fmt.Sprintf("samples %d, min gap %.4f at u=%.4f (cubes %d, %d), slack %.4f, "+
		"min floor clearance %.4f at u=%.4f, lemma 1 regular %v target %v, peak radius %.3f, cluster overhang %.4f",
		r.samples, r.minGap, r.gapU, r.gapPair[0], r.gapPair[1], r.slack,
		r.minFloor, r.floorU, r.regularOK, r.targetOK, r.peakRadius, r.clusterOut)
}

// verify checks the whole forward motion (the way back is the same motion
// reversed) independently of how it was planned:
//   - every pair of cubes, exactly as boxes (separating axis test), at time
//     steps so fine that the gap cannot close between two samples: it changes
//     at most at twice the speed bound, so a sampled gap above slack = v du
//     stays positive in between;
//   - the floor: the lowest point of every rounded cube stays above y = 0 at
//     every sample (the lift spline already carries the slack);
//   - lemma 1 where the plan relies on it: after expanding and before
//     packing.
func (c *cubes) verify(liftCtrl []float64, clusters [][][4]float64, bevel float64) report {
	nc := len(c.d0)
	v := c.speedBound()
	du := c.marg / (4 * v)
	steps := int(math.Ceil(1 / du))
	du = 1 / float64(steps)
	rho := c.half * math.Sqrt(3)

	type result struct {
		gap, gapU, floor, floorU, peak, out float64
		pair                                [2]int
	}
	workers := runtime.NumCPU()
	res := make([]result, workers)
	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			r := result{gap: math.Inf(1), floor: math.Inf(1), out: math.Inf(-1)}
			pos := make([]vec, nc)
			rot := make([]mat, nc)
			for s := w; s <= steps; s += workers {
				u := float64(s) * du
				h := liftAt(liftCtrl, u)
				for i := range nc {
					pos[i], rot[i] = c.pose(i, u)
					r.peak = math.Max(r.peak, pos[i].length()+rho)
					if f := h + pos[i][1] - lowest(rot[i], c.half, bevel); f < r.floor {
						r.floor, r.floorU = f, u
					}
					cl := clusters[c.clusterOf(i)]
					var cx [3][]float64
					rad := make([]float64, len(cl))
					for a := range 3 {
						cx[a] = make([]float64, len(cl))
						for j := range cl {
							cx[a][j] = cl[j][a]
						}
					}
					for j := range cl {
						rad[j] = cl[j][3]
					}
					ctr := vec{liftAt(cx[0], u), liftAt(cx[1], u), liftAt(cx[2], u)}
					r.out = math.Max(r.out, pos[i].sub(ctr).length()+rho-liftAt(rad, u))
				}
				for i := range nc {
					for j := i + 1; j < nc; j++ {
						if pos[i].sub(pos[j]).length() > 2*rho+r.gap {
							continue
						}
						if g := satGap(pos[i], rot[i], pos[j], rot[j], c.half); g < r.gap {
							r.gap, r.gapU, r.pair = g, u, [2]int{i, j}
						}
					}
				}
			}
			res[w] = r
		}(w)
	}
	wg.Wait()

	out := report{clusterOut: math.Inf(-1), samples: steps + 1, minGap: math.Inf(1), minFloor: math.Inf(1), slack: v * du}
	for _, r := range res {
		if r.gap < out.minGap {
			out.minGap, out.gapU, out.gapPair = r.gap, r.gapU, r.pair
		}
		if r.floor < out.minFloor {
			out.minFloor, out.floorU = r.floor, r.floorU
		}
		out.peakRadius = math.Max(out.peakRadius, r.peak)
		out.clusterOut = math.Max(out.clusterOut, r.out)
	}

	rest := func(u float64) bool {
		for i := range nc {
			pi, mi := c.pose(i, u)
			for j := i + 1; j < nc; j++ {
				pj, mj := c.pose(j, u)
				if pi.sub(pj).length() < 2*rho+1 && !shadowsDisjoint(pi, mi, pj, mj, c.half) {
					return false
				}
			}
		}
		return true
	}
	out.regularOK, out.targetOK = rest(ua), rest(uz)
	return out
}
