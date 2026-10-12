package main

import (
	"math"
	"math/rand/v2"
	"testing"
)

func randomUnit(rng *rand.Rand) vec {
	return vec{rng.NormFloat64(), rng.NormFloat64(), rng.NormFloat64()}.normalize()
}

// inside reports whether p lies in the cube (centre c, axes m, half edge h).
func inside(p, c vec, m mat, h float64) bool {
	d := p.sub(c)
	for a := range 3 {
		if math.Abs(d.dot(m[a])) > h {
			return false
		}
	}
	return true
}

// surfacePoints samples the surface of a cube on a grid.
func surfacePoints(c vec, m mat, h float64, n int) []vec {
	var out []vec
	for a := range 3 {
		for _, s := range []float64{-h, h} {
			for i := 0; i <= n; i++ {
				for j := 0; j <= n; j++ {
					v := vec{}
					v[a] = s
					v[(a+1)%3] = -h + 2*h*float64(i)/float64(n)
					v[(a+2)%3] = -h + 2*h*float64(j)/float64(n)
					out = append(out, c.add(m.apply(v)))
				}
			}
		}
	}
	return out
}

// The SAT gap never exceeds the true distance, and it is not positive when
// the cubes overlap.
func TestSatGapLowerBound(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for range 300 {
		ma, mb := randomRotation(rng), randomRotation(rng)
		ca := vec{}
		cb := randomUnit(rng).mul(0.6 + 1.4*rng.Float64())
		g := satGap(ca, ma, cb, mb, 0.5)
		pa, pb := surfacePoints(ca, ma, 0.5, 8), surfacePoints(cb, mb, 0.5, 8)
		best := math.Inf(1)
		overlap := false
		for _, p := range pa {
			overlap = overlap || inside(p, cb, mb, 0.5)
			for _, q := range pb {
				best = math.Min(best, p.sub(q).length())
			}
		}
		for _, q := range pb {
			overlap = overlap || inside(q, ca, ma, 0.5)
		}
		if g > best+1e-9 {
			t.Fatalf("gap %.4f exceeds the sampled distance %.4f", g, best)
		}
		if overlap && g > 0 {
			t.Fatalf("overlapping cubes report gap %.4f", g)
		}
	}
}

// Lemma 2: cubes that do not hide each other as seen from the origin stay
// apart however far each moves outward along its own ray.
func TestRadialSeparability(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	tested := 0
	for range 2000 {
		da := randomUnit(rng)
		db := da.add(randomUnit(rng).mul(0.3)).normalize()
		ra, rb := 3+3*rng.Float64(), 3+3*rng.Float64()
		ma, mb := randomRotation(rng), randomRotation(rng)
		if !shadowsDisjoint(da.mul(ra), ma, db.mul(rb), mb, 0.5) {
			continue
		}
		tested++
		for range 20 {
			sa, sb := ra*(1+2*rng.Float64()), rb*(1+2*rng.Float64())
			if g := satGap(da.mul(sa), ma, db.mul(sb), mb, 0.5); g <= 0 {
				t.Fatalf("radially separable cubes meet after moving outward (gap %.4f)", g)
			}
		}
	}
	if tested < 100 {
		t.Fatalf("only %d separable pairs tested", tested)
	}
}

// Lemma 3: scaling a contact-free configuration up keeps it contact free.
func TestUniformScaling(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 6))
	for range 500 {
		ma, mb := randomRotation(rng), randomRotation(rng)
		ca, cb := randomUnit(rng).mul(3), randomUnit(rng).mul(3)
		g := satGap(ca, ma, cb, mb, 0.5)
		for s := 1.0; s < 3; s += 0.1 {
			if g2 := satGap(ca.mul(s), ma, cb.mul(s), mb, 0.5); g > 0 && g2 < g-1e-9 {
				t.Fatalf("gap shrinks from %.4f to %.4f when scaling by %.1f", g, g2, s)
			}
		}
	}
}

func TestAxisAngleRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 8))
	angles := []float64{0, 1e-4, 0.5, 2, math.Pi - 1e-7, math.Pi}
	for _, a := range angles {
		for range 20 {
			k := randomUnit(rng)
			m := axisAngle(k, a)
			k2, a2 := toAxisAngle(m)
			if d := matDiff(axisAngle(k2, a2), m); d > 1e-6 {
				t.Fatalf("angle %.6f: round trip differs by %.2e", a, d)
			}
		}
	}
}

func matDiff(a, b mat) float64 {
	d := 0.0
	for i := range 3 {
		d = math.Max(d, a[i].sub(b[i]).length())
	}
	return d
}

func TestCubeSymmetries(t *testing.T) {
	syms := cubeSymmetries()
	if len(syms) != 24 {
		t.Fatalf("%d symmetries, want 24", len(syms))
	}
}

func TestPackRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewPCG(9, 10))
	for range 10000 {
		v := randomUnit(rng)
		if d := unpackDir(packDir(v)).sub(v).length(); d > 1e-4 {
			t.Fatalf("direction %v decodes %.2e off", v, d)
		}
	}
	for _, r := range []float64{0, 1.234, 7.5, 31.9} {
		if d := math.Abs(unUnorm(unorm16(r/rMax))*rMax - r); d > rMax/65535 {
			t.Fatalf("radius %.3f decodes %.2e off", r, d)
		}
	}
}

// The Hungarian matching finds the cheapest assignment (checked against all
// permutations of small sets).
func TestAssignOptimal(t *testing.T) {
	rng := rand.New(rand.NewPCG(11, 12))
	for range 50 {
		n := 6
		a, b := make([]vec, n), make([]vec, n)
		for i := range n {
			a[i], b[i] = randomUnit(rng), randomUnit(rng)
		}
		cost := func(p []int) float64 {
			s := 0.0
			for i, j := range p {
				th := math.Acos(math.Max(-1, math.Min(1, a[i].dot(b[j]))))
				s += th * th
			}
			return s
		}
		got := cost(assign(a, b))
		best := math.Inf(1)
		perm := []int{0, 1, 2, 3, 4, 5}
		var rec func(k int)
		rec = func(k int) {
			if k == n {
				best = math.Min(best, cost(perm))
				return
			}
			for i := k; i < n; i++ {
				perm[k], perm[i] = perm[i], perm[k]
				rec(k + 1)
				perm[k], perm[i] = perm[i], perm[k]
			}
		}
		rec(0)
		if got > best+1e-9 {
			t.Fatalf("assignment cost %.6f, optimum %.6f", got, best)
		}
	}
}

// The B-spline of upperCurve stays above the function it bounds.
func TestUpperCurve(t *testing.T) {
	c := &cubes{d0: []vec{{1, 0, 0}}, d1: []vec{{1, 0, 0}}, rs: []float64{1}, r1: []float64{1},
		rEnd: []float64{1}, ang: []float64{0}, r0: 3, rRest: 0, half: 0.5}
	// expanding from 0 to 3 in u = 0.1 bounds the speed by 56 > max |f'| = 18.3
	f := func(_ int, u float64) float64 { return math.Sin(9*u) + 0.3*math.Cos(31*u) }
	ctrl := c.upperCurve(16, 0, f)
	for k := 0; k <= 10000; k++ {
		u := float64(k) / 10000
		if liftAt(ctrl, u) < f(0, u)-1e-9 {
			t.Fatalf("curve %.4f below %.4f at u=%.4f", liftAt(ctrl, u), f(0, u), u)
		}
	}
}
