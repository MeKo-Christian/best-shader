package main

import (
	"math"
	"math/rand/v2"
	"sort"
)

// Units: one cube edge. Ball space: origin at the ball centre, y up, the
// face 5 (normal -z) looks at the camera.

// faceBasis returns the normal and the two in-plane axes of cube face f.
// shaders/sphere.glsl mirrors this table.
func faceBasis(f int) (n, u, v vec) {
	switch f {
	case 0:
		return vec{1, 0, 0}, vec{0, 0, -1}, vec{0, 1, 0}
	case 1:
		return vec{-1, 0, 0}, vec{0, 0, 1}, vec{0, 1, 0}
	case 2:
		return vec{0, 1, 0}, vec{1, 0, 0}, vec{0, 0, -1}
	case 3:
		return vec{0, -1, 0}, vec{1, 0, 0}, vec{0, 0, 1}
	case 4:
		return vec{0, 0, 1}, vec{1, 0, 0}, vec{0, 1, 0}
	default:
		return vec{0, 0, -1}, vec{-1, 0, 0}, vec{0, 1, 0}
	}
}

// spherify maps the point (x, y) in [-1, 1]^2 of cube face f onto the unit
// sphere (Nowell's mapping): its cells vary less in size than those of the
// equiangular or the gnomonic cubed sphere, so the cubes pack denser.
func spherify(f int, x, y float64) vec {
	n, u, v := faceBasis(f)
	xx, yy := x*x, y*y
	return n.mul(math.Sqrt(1 - 0.5*xx - 0.5*yy + xx*yy/3)).
		add(u.mul(x * math.Sqrt(0.5-yy/6))).
		add(v.mul(y * math.Sqrt(0.5-xx/6)))
}

// regularPose is cube k of the cubed sphere with n x n cubes per face: its
// direction and its axes. z points outwards; x and y lie symmetric between
// the two grid lines through the cube, which need not cross at right angles.
func regularPose(k, n int) (vec, mat) {
	const e = 1.0 / 64
	f, j, i := k/(n*n), k%(n*n)/n, k%n
	x := (float64(i)+0.5)/float64(n)*2 - 1
	y := (float64(j)+0.5)/float64(n)*2 - 1
	d := spherify(f, x, y)
	tu := spherify(f, x+e, y).sub(spherify(f, x-e, y))
	tv := spherify(f, x, y+e).sub(spherify(f, x, y-e))
	tu = tu.sub(d.mul(tu.dot(d))).normalize()
	tv = tv.sub(d.mul(tv.dot(d))).normalize()
	a := tu.add(tv).normalize().add(tu.sub(tv).normalize()).normalize()
	return d, mat{a, d.cross(a), d}
}

// neighbours lists, for every direction, the others within maxAngle.
func neighbours(dirs []vec, maxAngle float64) [][]int {
	c := math.Cos(maxAngle)
	out := make([][]int, len(dirs))
	for i := range dirs {
		for j := range dirs {
			if i != j && dirs[i].dot(dirs[j]) > c {
				out[i] = append(out[i], j)
			}
		}
	}
	return out
}

// shadowsDisjoint reports whether two cubes, inflated by half the margin,
// do not hide each other as seen from the ball centre (lemma 1).
func shadowsDisjoint(ca vec, ma mat, cb vec, mb mat, half float64) bool {
	a, b := corners(ca, ma, half), corners(cb, mb, half)
	return conesDisjoint(a[:], b[:])
}

// regularRadius is the smallest common radius at which all regular cubes
// are radially separable.
func regularRadius(d []vec, m []mat, nb [][]int, half float64) float64 {
	ok := func(r float64) bool {
		for i := range d {
			for _, j := range nb[i] {
				if j > i && !shadowsDisjoint(d[i].mul(r), m[i], d[j].mul(r), m[j], half) {
					return false
				}
			}
		}
		return true
	}
	lo, hi := 1.0, 40.0
	for hi-lo > 1e-4 {
		mid := 0.5 * (lo + hi)
		if ok(mid) {
			hi = mid
		} else {
			lo = mid
		}
	}
	return hi
}

// minScale is the smallest factor by which the cube centres pos can be
// scaled about the ball centre (orientations fixed) and still keep every
// neighbour pair a gap of marg apart. The gap only grows with the factor:
// two cubes overlap when their offset lies in a convex set around zero, and
// scaling moves an offset outside it further out (lemma 3).
func minScale(pos []vec, m []mat, nb [][]int, half, marg float64) float64 {
	ok := func(s float64) bool {
		for i := range pos {
			for _, j := range nb[i] {
				if j > i && satGap(pos[i].mul(s), m[i], pos[j].mul(s), m[j], half) < marg {
					return false
				}
			}
		}
		return true
	}
	lo, hi := 0.1, 1.0
	for !ok(hi) {
		hi *= 2
	}
	for hi-lo > 1e-5 {
		mid := 0.5 * (lo + hi)
		if ok(mid) {
			hi = mid
		} else {
			lo = mid
		}
	}
	return hi
}

// randomRotation is uniformly distributed (Shoemake).
func randomRotation(rng *rand.Rand) mat {
	u1, u2, u3 := rng.Float64(), rng.Float64(), rng.Float64()
	a, b := math.Sqrt(1-u1), math.Sqrt(u1)
	q := [4]float64{a * math.Sin(2*math.Pi*u2), a * math.Cos(2*math.Pi*u2), b * math.Sin(2*math.Pi*u3), b * math.Cos(2*math.Pi*u3)}
	x, y, z, w := q[0], q[1], q[2], q[3]
	return mat{
		{1 - 2*(y*y+z*z), 2 * (x*y + w*z), 2 * (x*z - w*y)},
		{2 * (x*y - w*z), 1 - 2*(x*x+z*z), 2 * (y*z + w*x)},
		{2 * (x*z + w*y), 2 * (y*z - w*x), 1 - 2*(x*x+y*y)},
	}
}

// irregularDirs scatters the regular directions: a random tangential kick of
// up to jitter grid pitches, then a few steps of short-range repulsion so no
// two directions clump.
func irregularDirs(d0 []vec, pitch, jitter float64, steps int, rng *rand.Rand) []vec {
	d := make([]vec, len(d0))
	for i, p := range d0 {
		t := vec{rng.NormFloat64(), rng.NormFloat64(), rng.NormFloat64()}
		t = t.sub(p.mul(t.dot(p))).normalize()
		d[i] = p.add(t.mul(jitter * pitch * math.Sqrt(rng.Float64()))).normalize()
	}
	for range steps {
		f := make([]vec, len(d))
		for i := range d {
			for j := range d {
				if i == j {
					continue
				}
				e := d[i].sub(d[j])
				l := e.length()
				if l < 1.5*pitch {
					f[i] = f[i].add(e.mul((1.5*pitch - l) / l))
				}
			}
		}
		for i := range d {
			d[i] = d[i].add(f[i].mul(0.1)).normalize()
		}
	}
	return d
}

// bisectorRadius is the smallest radius at which a cube with axes m in
// direction d keeps to its side of the bisector planes towards its
// neighbours: then any two such cubes are radially separable.
func bisectorRadius(d vec, m mat, others []vec, half float64) float64 {
	r := 0.0
	for _, o := range others {
		n := d.sub(o).normalize()
		r = math.Max(r, support(m, half, n)/d.dot(n))
	}
	return r
}

// irregularTargets picks for every target direction the orientation (best of
// tries random ones) that lets the cube sit lowest, then jams the cubes
// inwards: in random order, each sinks as far as it stays radially separable
// from its neighbours. Returns the axes and radii.
func irregularTargets(d []vec, nb [][]int, half float64, tries, passes int, rng *rand.Rand) ([]mat, []float64) {
	m := make([]mat, len(d))
	r := make([]float64, len(d))
	for i := range d {
		others := make([]vec, len(nb[i]))
		for k, j := range nb[i] {
			others[k] = d[j]
		}
		best := math.Inf(1)
		for range tries {
			q := randomRotation(rng)
			if b := bisectorRadius(d[i], q, others, half); b < best {
				best, m[i] = b, q
			}
		}
		r[i] = best
	}
	fits := func(i int, ri float64) bool {
		for _, j := range nb[i] {
			if !shadowsDisjoint(d[i].mul(ri), m[i], d[j].mul(r[j]), m[j], half) {
				return false
			}
		}
		return true
	}
	for range passes {
		for _, i := range rng.Perm(len(d)) {
			lo, hi := 0.5*r[i], r[i]
			for hi-lo > 1e-3 {
				mid := 0.5 * (lo + hi)
				if fits(i, mid) {
					hi = mid
				} else {
					lo = mid
				}
			}
			r[i] = hi
		}
	}
	return m, r
}

// improveTargets lowers the highest targets: it repeatedly takes the cube
// standing out furthest (weighted, so the bottom cap, which lifts the whole
// ball, counts more), tries new random orientations and small nudges of its
// direction, and keeps the one that lets it sink deepest while it stays
// radially separable from its neighbours. Cubes that do not improve are
// left alone. A final jamming pass lets the neighbours follow.
func improveTargets(d []vec, m []mat, r []float64, nb [][]int, half, pitch float64, iters, tries int, rng *rand.Rand) {
	lowest := func(i int, di vec, mi mat) float64 {
		fits := func(ri float64) bool {
			for _, j := range nb[i] {
				if !shadowsDisjoint(di.mul(ri), mi, d[j].mul(r[j]), m[j], half) {
					return false
				}
			}
			return true
		}
		lo, hi := 0.5*r[i], 2*r[i]
		if !fits(hi) {
			return math.Inf(1)
		}
		for hi-lo > 1e-3 {
			mid := 0.5 * (lo + hi)
			if fits(mid) {
				hi = mid
			} else {
				lo = mid
			}
		}
		return hi
	}
	weight := func(i int) float64 { return r[i] * (1 + 0.15*math.Max(-d[i][1], 0)) }
	stuck := make([]bool, len(d))
	for range iters {
		worst := -1
		for i := range d {
			if !stuck[i] && (worst < 0 || weight(i) > weight(worst)) {
				worst = i
			}
		}
		if worst < 0 {
			break
		}
		i := worst
		bestR, bestD, bestM := r[i], d[i], m[i]
		for range tries {
			mi := randomRotation(rng)
			di := d[i]
			if rng.Float64() < 0.5 {
				t := vec{rng.NormFloat64(), rng.NormFloat64(), rng.NormFloat64()}
				t = t.sub(di.mul(t.dot(di))).normalize()
				di = di.add(t.mul(0.15 * pitch * rng.Float64())).normalize()
			}
			if ri := lowest(i, di, mi); ri < bestR-1e-3 {
				bestR, bestD, bestM = ri, di, mi
			}
		}
		if bestR < r[i]-1e-3 {
			r[i], d[i], m[i] = bestR, bestD, bestM
		} else {
			stuck[i] = true
		}
	}
	for _, i := range rng.Perm(len(d)) {
		if ri := lowest(i, d[i], m[i]); ri < r[i] {
			r[i] = ri
		}
	}
}

// assign matches regular cubes to target slots, minimising the summed squared
// angle (Hungarian algorithm). out[i] is the slot of cube i.
func assign(a, b []vec) []int {
	n := len(a)
	cost := func(i, j int) float64 {
		t := math.Acos(math.Max(-1, math.Min(1, a[i].dot(b[j]))))
		return t * t
	}
	inf := math.Inf(1)
	u, v := make([]float64, n+1), make([]float64, n+1)
	p, way := make([]int, n+1), make([]int, n+1)
	for i := 1; i <= n; i++ {
		p[0] = i
		j0 := 0
		minv := make([]float64, n+1)
		used := make([]bool, n+1)
		for k := range minv {
			minv[k] = inf
		}
		for {
			used[j0] = true
			i0, delta, j1 := p[j0], inf, 0
			for j := 1; j <= n; j++ {
				if used[j] {
					continue
				}
				c := cost(i0-1, j-1) - u[i0] - v[j]
				if c < minv[j] {
					minv[j], way[j] = c, j0
				}
				if minv[j] < delta {
					delta, j1 = minv[j], j
				}
			}
			for j := 0; j <= n; j++ {
				if used[j] {
					u[p[j]] += delta
					v[j] -= delta
				} else {
					minv[j] -= delta
				}
			}
			j0 = j1
			if p[j0] == 0 {
				break
			}
		}
		for j0 != 0 {
			j1 := way[j0]
			p[j0] = p[j1]
			j0 = j1
		}
	}
	out := make([]int, n)
	for j := 1; j <= n; j++ {
		out[p[j]-1] = j - 1
	}
	return out
}

// nearestSymmetric returns the orientation among the 24 that show the cube
// m1 identically which is the smallest rotation away from m0.
func nearestSymmetric(m0, m1 mat, syms []mat) mat {
	best, bestA := m1, math.Inf(1)
	for _, g := range syms {
		c := m1.mul(g)
		if a := rotationAngle(c.mul(m0.transpose())); a < bestA {
			best, bestA = c, a
		}
	}
	return best
}

func quantiles(xs []float64) (float64, float64, float64) {
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	return s[0], s[len(s)/2], s[len(s)-1]
}
