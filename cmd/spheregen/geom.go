package main

import "math"

// vec is a 3D vector; mat is a rotation matrix stored as its columns, the
// cube's local axes in ball space.
type vec [3]float64

type mat [3]vec

func (a vec) add(b vec) vec     { return vec{a[0] + b[0], a[1] + b[1], a[2] + b[2]} }
func (a vec) sub(b vec) vec     { return vec{a[0] - b[0], a[1] - b[1], a[2] - b[2]} }
func (a vec) mul(s float64) vec { return vec{a[0] * s, a[1] * s, a[2] * s} }
func (a vec) dot(b vec) float64 { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }
func (a vec) length() float64   { return math.Sqrt(a.dot(a)) }
func (a vec) normalize() vec    { return a.mul(1 / a.length()) }
func (a vec) cross(b vec) vec {
	return vec{a[1]*b[2] - a[2]*b[1], a[2]*b[0] - a[0]*b[2], a[0]*b[1] - a[1]*b[0]}
}
func mix(a, b, t float64) float64 { return a + (b-a)*t }

// apply maps local cube coordinates to ball space.
func (m mat) apply(v vec) vec { return m[0].mul(v[0]).add(m[1].mul(v[1])).add(m[2].mul(v[2])) }

func (m mat) mul(n mat) mat { return mat{m.apply(n[0]), m.apply(n[1]), m.apply(n[2])} }

func (m mat) transpose() mat {
	return mat{{m[0][0], m[1][0], m[2][0]}, {m[0][1], m[1][1], m[2][1]}, {m[0][2], m[1][2], m[2][2]}}
}

// axisAngle is the rotation by angle a about the unit axis k (Rodrigues).
func axisAngle(k vec, a float64) mat {
	c, s := math.Cos(a), math.Sin(a)
	col := func(e vec) vec {
		return e.mul(c).add(k.cross(e).mul(s)).add(k.mul(k.dot(e) * (1 - c)))
	}
	return mat{col(vec{1, 0, 0}), col(vec{0, 1, 0}), col(vec{0, 0, 1})}
}

// toAxisAngle inverts axisAngle; the angle is in [0, pi].
func toAxisAngle(m mat) (vec, float64) {
	tr := m[0][0] + m[1][1] + m[2][2]
	a := math.Acos(math.Max(-1, math.Min(1, (tr-1)/2)))
	if a < 1e-9 {
		return vec{1, 0, 0}, 0
	}
	if math.Pi-a < 1e-6 {
		// near pi: the axis is the column of m + I with the largest norm
		best := vec{}
		for i := range 3 {
			v := m[i]
			v[i]++
			if v.length() > best.length() {
				best = v
			}
		}
		return best.normalize(), a
	}
	// m[col][row]: the skew part is (m - m^T) / 2
	k := vec{m[1][2] - m[2][1], m[2][0] - m[0][2], m[0][1] - m[1][0]}
	return k.normalize(), a
}

// rotationAngle of m.
func rotationAngle(m mat) float64 {
	tr := m[0][0] + m[1][1] + m[2][2]
	return math.Acos(math.Max(-1, math.Min(1, (tr-1)/2)))
}

// cubeSymmetries are the 24 rotations that map the cube onto itself: signed
// permutation matrices with determinant +1.
func cubeSymmetries() []mat {
	var out []mat
	perms := [][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}}
	for _, p := range perms {
		for sgn := range 8 {
			var m mat
			for c := range 3 {
				s := 1.0
				if sgn>>c&1 == 1 {
					s = -1
				}
				m[c][p[c]] = s
			}
			if m[0].cross(m[1]).dot(m[2]) > 0 {
				out = append(out, m)
			}
		}
	}
	return out
}

// slerpDir moves along the great circle from a to b (unit vectors).
func slerpDir(a, b vec, t float64) vec {
	c := math.Max(-1, math.Min(1, a.dot(b)))
	th := math.Acos(c)
	if th < 1e-6 {
		return a.add(b.sub(a).mul(t)).normalize()
	}
	s := math.Sin(th)
	return a.mul(math.Sin((1-t)*th) / s).add(b.mul(math.Sin(t*th) / s))
}

// ---- boxes --------------------------------------------------------------

// corners of a cube with centre c, axes m and half edge h.
func corners(c vec, m mat, h float64) [8]vec {
	var out [8]vec
	for k := range 8 {
		v := vec{h, h, h}
		for a := range 3 {
			if k>>a&1 == 1 {
				v[a] = -h
			}
		}
		out[k] = c.add(m.apply(v))
	}
	return out
}

// support is the half extent of a cube (axes m, half edge h) along unit n.
func support(m mat, h float64, n vec) float64 {
	return h * (math.Abs(m[0].dot(n)) + math.Abs(m[1].dot(n)) + math.Abs(m[2].dot(n)))
}

// satGap is a lower bound on the distance between two cubes of half edge h:
// the largest gap between their projections on the 15 separating axes. It is
// <= 0 when they overlap.
func satGap(ca vec, ma mat, cb vec, mb mat, h float64) float64 {
	t := cb.sub(ca)
	best := math.Inf(-1)
	test := func(l vec) {
		n := l.length()
		if n < 1e-9 {
			return
		}
		l = l.mul(1 / n)
		g := math.Abs(t.dot(l)) - support(ma, h, l) - support(mb, h, l)
		if g > best {
			best = g
		}
	}
	for i := range 3 {
		test(ma[i])
		test(mb[i])
		for j := range 3 {
			test(ma[i].cross(mb[j]))
		}
	}
	return best
}

// ---- cones from the ball centre ------------------------------------------

// conesDisjoint reports whether the cones from the origin spanned by the two
// point sets meet only in the origin, i.e. whether a plane through the
// origin separates them. Both sets are projected gnomonically onto the plane
// facing their mean direction, where the cones become convex polygons that
// are tested on the normals of all segments within each set.
func conesDisjoint(a, b []vec) bool {
	w := vec{}
	for _, p := range a {
		w = w.add(p.normalize())
	}
	for _, p := range b {
		w = w.add(p.normalize())
	}
	w = w.normalize()
	e1 := vec{1, 0, 0}
	if math.Abs(w[0]) > 0.9 {
		e1 = vec{0, 1, 0}
	}
	e1 = e1.sub(w.mul(e1.dot(w))).normalize()
	e2 := w.cross(e1)

	proj := func(ps []vec) ([][2]float64, bool) {
		out := make([][2]float64, len(ps))
		for i, p := range ps {
			z := p.dot(w)
			if z < 1e-6 {
				return nil, false
			}
			out[i] = [2]float64{p.dot(e1) / z, p.dot(e2) / z}
		}
		return out, true
	}
	pa, okA := proj(a)
	pb, okB := proj(b)
	if !okA || !okB {
		return false
	}
	sep := func(poly [][2]float64) bool {
		for i := range poly {
			for j := i + 1; j < len(poly); j++ {
				ax := [2]float64{poly[i][1] - poly[j][1], poly[j][0] - poly[i][0]}
				lo1, hi1 := interval(pa, ax)
				lo2, hi2 := interval(pb, ax)
				if lo2-hi1 > 1e-9 || lo1-hi2 > 1e-9 {
					return true
				}
			}
		}
		return false
	}
	return sep(pa) || sep(pb)
}

func interval(ps [][2]float64, ax [2]float64) (float64, float64) {
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, p := range ps {
		d := p[0]*ax[0] + p[1]*ax[1]
		lo, hi = math.Min(lo, d), math.Max(hi, d)
	}
	return lo, hi
}
