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
	floorIn     float64 // deepest a cube reaches into the floor (edges, ideally <= 0)
	floorOff    float64 // highest the pressed ball stands off the floor (edges, ideally 0)
	maxAcc      float64 // largest acceleration of a cube about the ball centre (world/s^2)
	accT        float64
	accCube     int
	regRange    [2]float64 // radius range at t = 0
	irrRange    [3]float64 // min, median, max radius at t = period / 2
	maxTumble   float64
	medianSlide float64 // median move of a cube between the regular and the irregular state (pitches)
}

func (s stats) String() string {
	return fmt.Sprintf("samples %d, min gap %.4f at t=%.3f (cubes %d, %d), overlapping pair samples %d, "+
		"folded cells %d, min wall cos %.4f, radius %.3f..%.3f, into the floor %.4f, "+
		"off the floor when pressed %.4f, largest acceleration %.2f at t=%.3f (cube %d)",
		s.samples, s.minGap, s.gapT, s.gapPair[0], s.gapPair[1], s.overlaps, s.folded, s.minWall,
		s.rLo, s.rHi, s.floorIn, s.floorOff, s.maxAcc, s.accT, s.accCube)
}

// newStats is the empty result that every sample improves on.
func newStats() stats {
	return stats{minGap: math.Inf(1), minWall: math.Inf(1), rLo: math.Inf(1), floorIn: math.Inf(-1)}
}

// verify runs the formula over one loop at fps samples per second:
//   - every cell keeps its orientation (no fold-over), which is all the
//     no-contact argument needs, and every pair of cubes is tested as an
//     independent check of the implementation: a rounded cube is its core
//     box grown by the bevel, so the separating axis gap of the cores minus
//     twice the bevel bounds their distance from below;
//   - the radius bound the shader relies on, the floor (no cube reaches
//     into it, and the pressed ball stands on it), and how smoothly the
//     cubes move: the largest acceleration about the ball centre, by
//     differences between the frames.
func (p *params) verify(fps float64) stats {
	nc := 6 * p.n * p.n
	steps := int(math.Ceil(p.period * fps))

	workers := runtime.NumCPU()
	res := make([]stats, workers)

	var wg sync.WaitGroup
	for w := range workers {
		wg.Go(func() { res[w] = p.verifySteps(w, workers, steps, fps) })
	}

	wg.Wait()

	out := newStats()
	out.samples = steps

	for _, r := range res {
		out = out.merge(r)
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

// verifySteps checks the samples first, first+stride, ... below steps.
func (p *params) verifySteps(first, stride, steps int, fps float64) stats {
	nc := 6 * p.n * p.n
	r := newStats()
	pos := make([]vec, nc)
	rot := make([]mat, nc)

	for st := first; st < steps; st += stride {
		t := float64(st) / fps
		ph := p.phaseAt(t)
		low := math.Inf(-1)

		for i := range nc {
			pos[i], rot[i], _ = p.cubePoseAt(i, t, ph)
			p.checkCube(&r, i, t, fps, ph, pos[i])
			low = math.Max(low, -pos[i][1]+p.lowest(rot[i]))
		}

		r.floorIn = math.Max(r.floorIn, low-p.floor(ph))
		if ph.q > 0 {
			r.floorOff = math.Max(r.floorOff, p.floor(ph)-low)
		}

		p.checkPairs(&r, t, pos, rot)
	}

	return r
}

// checkCube records the acceleration, the cell walls and the radius of cube
// i at centre c.
func (p *params) checkCube(r *stats, i int, t, fps float64, ph phase, c vec) {
	prev, _ := p.pose(i, t-1/fps)

	next, _ := p.pose(i, t+1/fps)
	if acc := next.add(prev).sub(c.mul(2)).length() * p.scale * fps * fps; acc > r.maxAcc {
		r.maxAcc, r.accT, r.accCube = acc, t, i
	}

	q := p.cell(i, t, ph)

	d := c.normalize()
	for e := range 4 {
		w := q[e].cross(q[(e+1)%4]).normalize().dot(d)

		r.minWall = math.Min(r.minWall, w)
		if w <= 0 {
			r.folded++
		}
	}

	rad := c.length()
	r.rLo, r.rHi = math.Min(r.rLo, rad), math.Max(r.rHi, rad)
}

// checkPairs records the gaps between the cubes that are close enough to
// matter at time t.
func (p *params) checkPairs(r *stats, t float64, pos []vec, rot []mat) {
	for i := range pos {
		for j := i + 1; j < len(pos); j++ {
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

// merge is s with the findings of another worker added.
func (s stats) merge(r stats) stats {
	if r.minGap < s.minGap {
		s.minGap, s.gapT, s.gapPair = r.minGap, r.gapT, r.gapPair
	}

	s.folded += r.folded
	s.overlaps += r.overlaps
	s.minWall = math.Min(s.minWall, r.minWall)
	s.rLo, s.rHi = math.Min(s.rLo, r.rLo), math.Max(s.rHi, r.rHi)
	s.floorIn = math.Max(s.floorIn, r.floorIn)

	s.floorOff = math.Max(s.floorOff, r.floorOff)
	if r.maxAcc > s.maxAcc {
		s.maxAcc, s.accT, s.accCube = r.maxAcc, r.accT, r.accCube
	}

	return s
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
