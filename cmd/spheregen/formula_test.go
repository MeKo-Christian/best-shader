package main

import (
	"math"
	"math/rand/v2"
	"testing"
)

func testParams() params {
	p := params{
		n: 7, bevel: 0.1, marg: 0.03, jitter: 0.02, warp: 0.05, offset: 0.1, tumble: 1.05,
		rIrr: 5.6, rCap: 7.5, sink: 0.6, spread: 0.5, sweep: 0.5, period: 24, wobble: 0.01, wobRot: 0.1,
		breath: 0.12, hops: 24, ripple: 0.3,
	}
	p.rReg = p.regularRadius(2)
	return p
}

func randomUnit(rng *rand.Rand) vec {
	return vec{rng.NormFloat64(), rng.NormFloat64(), rng.NormFloat64()}.normalize()
}

func randomRotation(rng *rand.Rand) mat {
	return axisAngle(randomUnit(rng), math.Pi*rng.Float64())
}

// The cells form a closed, consistently oriented surface: every directed
// edge of a cell appears reversed in exactly one other cell, and the grid
// has the 6 n^2 + 2 vertices of a cubed sphere. So neighbouring cells share
// their walls.
func TestCellsCloseUp(t *testing.T) {
	p := testParams()
	type edge struct{ a, b uint32 }
	edges := map[edge]int{}
	verts := map[uint32]bool{}
	nn := p.n * p.n
	for i := range 6 * nn {
		f, j, k := i/nn, i%nn/p.n, i%p.n
		ks := [4][3]int{p.gridPoint(f, k, j), p.gridPoint(f, k+1, j), p.gridPoint(f, k+1, j+1), p.gridPoint(f, k, j+1)}
		for e := range 4 {
			a, b := p.vertexKey(ks[e]), p.vertexKey(ks[(e+1)%4])
			edges[edge{a, b}]++
			verts[a] = true
		}
	}
	if len(verts) != 6*nn+2 {
		t.Fatalf("%d grid vertices, want %d", len(verts), 6*nn+2)
	}
	for e, c := range edges {
		if c != 1 || edges[edge{e.b, e.a}] != 1 {
			t.Fatalf("edge %v appears %d times, reversed %d times", e, c, edges[edge{e.b, e.a}])
		}
	}
}

// Without sink every cube stays inside its cone: all corners of its core box,
// grown by the bevel, keep the margin from each wall.
func TestCubesInsideCones(t *testing.T) {
	p := testParams()
	p.sink = 0
	for _, tm := range []float64{0, 3.3, 7.1, 12, 17.9} {
		u := p.morph(tm)
		for i := range 6 * p.n * p.n {
			c, m, _ := p.cubePose(i, tm)
			q := p.cell(i, tm, u)
			for e := range 4 {
				w := q[e].cross(q[(e+1)%4]).normalize()
				for k := range 8 {
					v := vec{0.5 - p.bevel, 0.5 - p.bevel, 0.5 - p.bevel}
					for a := range 3 {
						if k>>a&1 == 1 {
							v[a] = -v[a]
						}
					}
					if d := c.add(m.apply(v)).dot(w) - p.bevel; d < p.marg-1e-9 {
						t.Fatalf("t=%.1f cube %d: corner %d is %.4f from wall %d", tm, i, k, d, e)
					}
				}
			}
		}
	}
}

// Without sink no two cubes meet.
func TestNoContactWithoutSink(t *testing.T) {
	p := testParams()
	p.sink = 0
	if s := p.verify(1, 7); s.minGap <= 0 || s.folded > 0 {
		t.Fatalf("min gap %.4f, folded %d", s.minGap, s.folded)
	}
}

// The SAT gap never exceeds the true distance (sampled on the surfaces), and
// it is not positive when the cubes overlap.
func TestSatGapLowerBound(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	inside := func(x, c vec, m mat) bool {
		d := x.sub(c)
		for a := range 3 {
			if math.Abs(d.dot(m[a])) > 0.5 {
				return false
			}
		}
		return true
	}
	surface := func(c vec, m mat) []vec {
		var out []vec
		for a := range 3 {
			for _, s := range []float64{-0.5, 0.5} {
				for i := 0; i <= 8; i++ {
					for j := 0; j <= 8; j++ {
						v := vec{}
						v[a] = s
						v[(a+1)%3] = -0.5 + float64(i)/8
						v[(a+2)%3] = -0.5 + float64(j)/8
						out = append(out, c.add(m.apply(v)))
					}
				}
			}
		}
		return out
	}
	for range 300 {
		ma, mb := randomRotation(rng), randomRotation(rng)
		ca, cb := vec{}, randomUnit(rng).mul(0.6+1.4*rng.Float64())
		g := satGap(ca, ma, cb, mb, 0.5)
		best, overlap := math.Inf(1), false
		pa, pb := surface(ca, ma), surface(cb, mb)
		for _, x := range pa {
			overlap = overlap || inside(x, cb, mb)
			for _, y := range pb {
				best = math.Min(best, x.sub(y).length())
			}
		}
		for _, y := range pb {
			overlap = overlap || inside(y, ca, ma)
		}
		if g > best+1e-9 {
			t.Fatalf("gap %.4f exceeds the sampled distance %.4f", g, best)
		}
		if overlap && g > 0 {
			t.Fatalf("overlapping cubes report gap %.4f", g)
		}
	}
}
