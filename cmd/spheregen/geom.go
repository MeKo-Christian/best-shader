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

// rotationAngle of m.
func rotationAngle(m mat) float64 {
	tr := m[0][0] + m[1][1] + m[2][2]
	return math.Acos(math.Max(-1, math.Min(1, (tr-1)/2)))
}

// ---- boxes --------------------------------------------------------------

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
