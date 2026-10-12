package main

import (
	"fmt"
	"math"
	"runtime"
	"sync"
)

const rho = 0.8660254 // circumradius of a cube

// stats is what one run over the loop finds.
type stats struct {
	samples     int
	minGap      float64 // smallest gap between two cubes (SAT lower bound)
	gapT        float64
	gapPair     [2]int
	overlaps    int     // pair samples closer than 0
	folded      int     // cells that turned inside out at some sample (must be 0)
	minWall     float64 // smallest cos between a cell's wall normal and its cube direction
	rLo, rHi    float64 // radius range of the cube centres
	liftMiss    float64 // how far a cube outside the lift candidates reached below them (must be <= 0)
	liftLo      float64 // lowest and highest lift (ball centre above the floor, edges)
	liftHi      float64
	regRange    [2]float64 // radius range at t = 0
	irrRange    [3]float64 // min, median, max radius at t = period / 2
	maxTumble   float64
	medianSlide float64 // median move of a cube between the regular and the irregular state (pitches)
	sinkLo      float64 // least share of the squash the ball centre follows (1 = all)
}

func (s stats) String() string {
	return fmt.Sprintf("samples %d, min gap %.4f at t=%.3f (cubes %d, %d), overlapping pair samples %d, folded cells %d, min wall cos %.4f, "+
		"radius %.3f..%.3f, lift %.3f..%.3f (miss %.4f), centre follows the squash by >= %.2f",
		s.samples, s.minGap, s.gapT, s.gapPair[0], s.gapPair[1], s.overlaps, s.folded, s.minWall,
		s.rLo, s.rHi, s.liftLo, s.liftHi, s.liftMiss, s.sinkLo)
}

// liftCands are the cubes the shader asks for the ball's lowest point: the
// middle w x w cells of the bottom face.
func (p *params) liftCands(w int) []int {
	var out []int
	lo := (p.n - w) / 2
	for j := lo; j < lo+w; j++ {
		for i := lo; i < lo+w; i++ {
			out = append(out, 3*p.n*p.n+j*p.n+i)
		}
	}
	return out
}

// verify runs the formula over one loop at fps samples per second:
//   - every cell keeps its orientation (no fold-over), which is all the
//     no-contact argument needs, and every pair of cubes is tested as an
//     independent check of the implementation: a rounded cube is its core
//     box grown by the bevel, so the separating axis gap of the cores minus
//     twice the bevel bounds their distance from below;
//   - the radius bound and the lift candidates the shader relies on.
func (p *params) verify(fps float64) stats {
	nc := 6 * p.n * p.n
	steps := int(math.Ceil(p.period * fps))
	cands := p.liftCands(p.liftW)
	isCand := make([]bool, nc)
	for _, i := range cands {
		isCand[i] = true
	}

	workers := runtime.NumCPU()
	res := make([]stats, workers)
	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			r := stats{minGap: math.Inf(1), minWall: math.Inf(1), rLo: math.Inf(1), liftLo: math.Inf(1), liftMiss: math.Inf(-1), sinkLo: 1}
			pos := make([]vec, nc)
			rot := make([]mat, nc)
			for st := w; st < steps; st += workers {
				t := float64(st) / fps
				ph := p.phaseAt(t)
				lift, other := math.Inf(-1), math.Inf(-1)
				for i := range nc {
					pos[i], rot[i], _ = p.cubePoseAt(i, t, ph)
					q := p.cell(i, t, ph.u)
					d := pos[i].normalize()
					for e := range 4 {
						c := q[e].cross(q[(e+1)%4]).normalize().dot(d)
						r.minWall = math.Min(r.minWall, c)
						if c <= 0 {
							r.folded++
						}
					}
					rad := pos[i].length()
					r.rLo, r.rHi = math.Min(r.rLo, rad), math.Max(r.rHi, rad)
					low := -pos[i][1] + p.lowest(rot[i])
					if isCand[i] {
						lift = math.Max(lift, low)
					} else {
						other = math.Max(other, low)
					}
				}
				r.liftMiss = math.Max(r.liftMiss, other-lift)
				r.liftLo, r.liftHi = math.Min(r.liftLo, lift), math.Max(r.liftHi, lift)
				if ph.q > 0.1 {
					// how much of the squash the ball centre followed
					r.sinkLo = math.Min(r.sinkLo, (ph.floor+ph.q-lift)/ph.q)
				}
				for i := range nc {
					for j := i + 1; j < nc; j++ {
						if pos[i].sub(pos[j]).length() > 2*rho+math.Min(r.minGap, 0.5) {
							continue
						}
						g := satGap(pos[i], rot[i], pos[j], rot[j], 0.5-p.bevel) - 2*p.bevel
						if g < 0 {
							r.overlaps++
						}
						if g < r.minGap {
							r.minGap, r.gapT, r.gapPair = g, t, [2]int{i, j}
						}
					}
				}
			}
			res[w] = r
		}(w)
	}
	wg.Wait()

	out := stats{samples: steps, minGap: math.Inf(1), minWall: math.Inf(1), rLo: math.Inf(1), liftLo: math.Inf(1), liftMiss: math.Inf(-1), sinkLo: 1}
	for _, r := range res {
		if r.minGap < out.minGap {
			out.minGap, out.gapT, out.gapPair = r.minGap, r.gapT, r.gapPair
		}
		out.folded += r.folded
		out.overlaps += r.overlaps
		out.minWall = math.Min(out.minWall, r.minWall)
		out.rLo, out.rHi = math.Min(out.rLo, r.rLo), math.Max(out.rHi, r.rHi)
		out.liftMiss = math.Max(out.liftMiss, r.liftMiss)
		out.liftLo, out.liftHi = math.Min(out.liftLo, r.liftLo), math.Max(out.liftHi, r.liftHi)
		out.sinkLo = math.Min(out.sinkLo, r.sinkLo)
	}

	// the two states the loop turns at
	pitch := math.Pi / 2 / float64(p.n)
	reg, irr := phase{}, phase{u: 1}
	radii := func(ph phase) []float64 {
		rs := make([]float64, nc)
		for i := range nc {
			c, _, _ := p.cubePoseAt(i, 0, ph)
			rs[i] = c.length()
		}
		return rs
	}
	lo, _, hi := quantiles(radii(reg))
	out.regRange = [2]float64{lo, hi}
	a, b, c := quantiles(radii(irr))
	out.irrRange = [3]float64{a, b, c}
	slide := make([]float64, nc)
	for i := range nc {
		c0, m0, _ := p.cubePoseAt(i, 0, reg)
		c1, m1, _ := p.cubePoseAt(i, 0, irr)
		slide[i] = math.Acos(math.Min(1, c0.normalize().dot(c1.normalize()))) / pitch
		out.maxTumble = math.Max(out.maxTumble, rotationAngle(m1.mul(m0.transpose())))
	}
	_, out.medianSlide, _ = quantiles(slide)
	return out
}

func quantiles(xs []float64) (float64, float64, float64) {
	s := append([]float64(nil), xs...)
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
	return s[0], s[len(s)/2], s[len(s)-1]
}
