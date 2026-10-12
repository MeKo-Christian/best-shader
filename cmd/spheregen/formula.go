package main

import "math"

// The cube ball as a closed formula of (cube, time). shaders/sphere.glsl
// evaluates the same formula; this file mirrors it line for line so that the
// generator can check it and derive the bounds the shader culls with.
//
// Every cube owns a cell of a cubed sphere: the cone from the ball centre
// over a spherical quadrilateral whose corners are grid vertices. A vertex
// moves as a function of the vertex alone (its scatter, wobble and timing
// come from a hash of its grid position), so the two cells that share an
// edge always share both of its corners, and the cells partition the sphere
// at every moment. A cube sits at the radius where it just fits into its
// cell's cone (or further out); cubes in disjoint cones cannot meet. Hence
// no two cubes touch, whatever the cells, orientations and timings - as long
// as the cells do not fold over, which verify() checks.

// params are the knobs of the motion; emit() writes them into the shader.
type params struct {
	n      int     // cubes per cube-face edge
	bevel  float64 // edge radius
	marg   float64 // clearance from the cone walls (gap between cubes >= 2 marg)
	jitter float64 // vertex scatter in the irregular state (radians)
	warp   float64 // smooth flowing distortion of the whole grid in the irregular state (radians)
	offset float64 // cube offset inside its cell in the irregular state (share of the cell)
	tumble float64 // largest tumble angle (radians)
	rIrr   float64 // irregular state: cubes sink at most down to this radius
	rCap   float64 // a cube tumbles less where its cell is too narrow to hold it inside this radius
	sink   float64 // irregular state: how far a cube may stand inside its cone fit (edges); > 0 allows slight overlaps
	spread float64 // stagger of the morph: share of it a cube may start late
	sweep  float64 // share of that delay that follows a front over the ball

	period float64 // seconds of the loop regular -> irregular -> regular
	wobble float64 // vertex wobble, growing with the morph (radians)
	wobRot float64 // tumble wobble, growing with the morph (radians)
	breath float64 // radial breathing (edges)
	hops   int     // hops per period
	ripple float64 // outward ripple through the shell after each landing (edges)

	rReg float64 // radius of the regular ball (derived, see regularRadius)
}

// Angular frequencies of the wobble: whole multiples of the loop, so that
// the motion repeats exactly every period.
func (p *params) omega(k float64) float64 { return 2 * math.Pi * k / p.period }

const (
	wobK0, wobK1, wobK2, wobK3, breathK = 7, 9, 11, 5, 4
	smaxK                               = 0.15 // smoothness of max(fit radius, hold radius)
)

// ---- hashes -------------------------------------------------------------

func hashU(x uint32) uint32 {
	x ^= x >> 16
	x *= 0x7feb352d
	x ^= x >> 15
	x *= 0x846ca68b
	x ^= x >> 16
	return x
}

// rnd is the c-th random number in [0, 1) of the key k, exact in float32.
func rnd(k uint32, c uint32) float64 { return float64(hashU(k*8+c)>>8) / 16777216 }

// ---- grid ---------------------------------------------------------------

// faceBasis returns the normal and the two in-plane axes of cube face f.
func faceBasis(f int) (n, u, v [3]int) {
	switch f {
	case 0:
		return [3]int{1, 0, 0}, [3]int{0, 0, -1}, [3]int{0, 1, 0}
	case 1:
		return [3]int{-1, 0, 0}, [3]int{0, 0, 1}, [3]int{0, 1, 0}
	case 2:
		return [3]int{0, 1, 0}, [3]int{1, 0, 0}, [3]int{0, 0, -1}
	case 3:
		return [3]int{0, -1, 0}, [3]int{1, 0, 0}, [3]int{0, 0, 1}
	case 4:
		return [3]int{0, 0, 1}, [3]int{1, 0, 0}, [3]int{0, 1, 0}
	default:
		return [3]int{0, 0, -1}, [3]int{-1, 0, 0}, [3]int{0, 1, 0}
	}
}

// gridPoint is the vertex (a, b) of face f on the cube [-n, n]^3: integer,
// and the same from every face that shares it.
func (p *params) gridPoint(f, a, b int) [3]int {
	n, u, v := faceBasis(f)
	var k [3]int
	for c := range 3 {
		k[c] = p.n*n[c] + (2*a-p.n)*u[c] + (2*b-p.n)*v[c]
	}
	return k
}

// nowell maps a point of the cube [-1, 1]^3 onto the unit sphere.
func nowell(x vec) vec {
	q := vec{x[0] * x[0], x[1] * x[1], x[2] * x[2]}
	var d vec
	for c := range 3 {
		a, b := q[(c+1)%3], q[(c+2)%3]
		d[c] = x[c] * math.Sqrt(1-0.5*a-0.5*b+a*b/3)
	}
	return d
}

func (p *params) gridDir(k [3]int) vec {
	n := float64(p.n)
	return nowell(vec{float64(k[0]) / n, float64(k[1]) / n, float64(k[2]) / n})
}

func (p *params) vertexKey(k [3]int) uint32 {
	m := 2*p.n + 1
	return uint32((k[0] + p.n) + m*((k[1]+p.n)+m*(k[2]+p.n)))
}

func cubeKey(i int) uint32 { return uint32(i) + 0x10000 }

// ---- timing -------------------------------------------------------------

func smootherstep(x float64) float64 { return x * x * x * (x*(x*6-15) + 10) }

func clamp01(x float64) float64 { return math.Max(0, math.Min(1, x)) }

// morph is the loop parameter: 0 regular, 1 irregular, never at rest.
func (p *params) morph(t float64) float64 { return 0.5 - 0.5*math.Cos(2*math.Pi*t/p.period) }

// front orders the directions for the staggered start: the change sweeps
// from the upper front of the ball over it.
var frontAxis = vec{0.3, 0.8, -0.52}.normalize()

// warpWaves are the waves of the swirl's stream function.
var warpWaves = []struct {
	k             vec
	cycles, phase float64
}{
	{vec{0.9, 1.3, -0.4}, 1, 0},
	{vec{-1.2, 0.3, 1.1}, 2, 1.7},
	{vec{0.5, -0.8, -1.3}, -1, 4.1},
}

// stage is the eased progress of something at direction d with the random
// number h, delayed by up to spread of the morph.
func (p *params) stage(u, h float64, d vec) float64 {
	delay := mix(h, 0.5-0.5*d.dot(frontAxis), p.sweep)
	return smootherstep(clamp01((u - p.spread*delay) / (1 - p.spread)))
}

// ---- poses --------------------------------------------------------------

// vertex is the direction of grid vertex k at time t (morph u).
func (p *params) vertex(k [3]int, t, u float64) vec {
	d := p.gridDir(k)
	h := p.vertexKey(k)
	s := p.stage(u, rnd(h, 3), d)
	o := vec{rnd(h, 0)*2 - 1, rnd(h, 1)*2 - 1, rnd(h, 2)*2 - 1}.mul(p.jitter * s)
	// a smooth swirl moves neighbouring vertices alike, so it can move them
	// far without folding a cell: the curl of the stream function
	// sum cos(k.d + phase) over three waves, which keeps areas; its timing
	// must be smooth too
	sw := p.warp * p.stage(u, 0.5, d)
	for _, k := range warpWaves {
		o = o.add(d.cross(k.k).mul(sw * math.Cos(k.k.dot(d)+p.omega(k.cycles)*t+k.phase)))
	}
	o = o.add(vec{
		math.Sin(p.omega(wobK0)*t + 2*math.Pi*rnd(h, 4)),
		math.Sin(p.omega(wobK1)*t + 2*math.Pi*rnd(h, 5)),
		math.Sin(p.omega(wobK2)*t + 2*math.Pi*rnd(h, 6)),
	}.mul(p.wobble * s))
	o = o.sub(d.mul(o.dot(d)))
	return d.add(o).normalize()
}

// cell returns the corners of the cell of cube i at time t, counter-clockwise
// seen from outside.
func (p *params) cell(i int, t, u float64) [4]vec {
	nn := p.n * p.n
	f, j, k := i/nn, i%nn/p.n, i%p.n
	return [4]vec{
		p.vertex(p.gridPoint(f, k, j), t, u),
		p.vertex(p.gridPoint(f, k+1, j), t, u),
		p.vertex(p.gridPoint(f, k+1, j+1), t, u),
		p.vertex(p.gridPoint(f, k, j+1), t, u),
	}
}

// cubePose is the centre and the axes of cube i at time t in ball space,
// before the ripple; s is its progress towards the irregular state.
func (p *params) cubePose(i int, t float64) (c vec, m mat, s float64) {
	return p.cubePoseAt(i, t, p.morph(t))
}

// cubePoseAt is cubePose at the morph u.
func (p *params) cubePoseAt(i int, t, u float64) (c vec, m mat, s float64) {
	q := p.cell(i, t, u)
	h := cubeKey(i)

	// the regular state stands in the middle of the cell, the irregular one
	// off it
	d0 := q[0].add(q[1]).add(q[2]).add(q[3]).normalize()
	s = p.stage(u, rnd(h, 0), d0)
	x := 0.5 + p.offset*s*(rnd(h, 1)-0.5)
	y := 0.5 + p.offset*s*(rnd(h, 2)-0.5)
	d := q[0].mul((1 - x) * (1 - y)).add(q[1].mul(x * (1 - y))).add(q[2].mul(x * y)).add(q[3].mul((1 - x) * y)).normalize()

	// axes: z outwards, x and y symmetric between the cell's two directions
	tu := q[1].add(q[2]).sub(q[0]).sub(q[3])
	tv := q[2].add(q[3]).sub(q[0]).sub(q[1])
	tu = tu.sub(d.mul(tu.dot(d))).normalize()
	tv = tv.sub(d.mul(tv.dot(d))).normalize()
	a := tu.add(tv).normalize().add(tu.sub(tv).normalize()).normalize()
	m = mat{a, d.cross(a), d}

	// the cell's walls: planes through the centre and two neighbouring
	// corners, normals inwards; wd is how far d stays from them
	var w [4]vec
	var wd [4]float64
	narrow := 1.0
	for e := range 4 {
		w[e] = q[e].cross(q[(e+1)%4]).normalize()
		wd[e] = w[e].dot(d)
		narrow = math.Min(narrow, wd[e])
	}

	// A tumbled cube reaches further towards the walls (by up to the
	// rounded cube's circumradius minus its half edge); in a narrow cell it
	// tumbles less, so that it need not stand out far beyond rCap.
	room := clamp01((p.rCap*narrow - 0.5 - p.marg) / ((0.5 - p.bevel) * (math.Sqrt(3) - 1)))
	axis := vec{rnd(h, 3)*2 - 1, rnd(h, 4)*2 - 1, rnd(h, 5)*2 - 1}.add(vec{0, 0, 1e-3}).normalize()
	ang := (p.tumble*mix(0.35, 1, rnd(h, 6))*room + p.wobRot*math.Sin(p.omega(wobK3)*t+2*math.Pi*rnd(h, 7))) * s
	m = axisAngle(axis, ang).mul(m)

	// the smallest radius that keeps the cube inside the cone: the rounded
	// cube's support along each wall normal against the wall's distance
	fit := 0.0
	for e := range 4 {
		sup := (0.5-p.bevel)*(math.Abs(m[0].dot(w[e]))+math.Abs(m[1].dot(w[e]))+math.Abs(m[2].dot(w[e]))) + p.bevel + p.marg
		fit = math.Max(fit, sup/wd[e])
	}
	hold := mix(p.rReg, p.rIrr, s) + p.breath*(0.5-0.5*math.Cos(p.omega(breathK)*t))
	r := smax(fit-p.sink*s, hold, smaxK)
	return d.mul(r), m, s
}

// smax >= max(a, b), smooth.
func smax(a, b, k float64) float64 { return 0.5 * (a + b + math.Sqrt((a-b)*(a-b)+k*k)) }

// hop is the phase in [0, 1) of the current hop.
func (p *params) hop(t float64) float64 {
	x := t / p.period * float64(p.hops)
	return x - math.Floor(x)
}

// rippleAt is the outward push of a cube at direction d: a wave that starts
// at the floor contact when the ball lands and runs up the shell, fading.
func (p *params) rippleAt(d vec, t float64) float64 {
	front := math.Acos(math.Max(-1, math.Min(1, -d[1]))) / math.Pi // 0 bottom, 1 top
	x := p.hop(t)*2.5 - front
	if x <= 0 || x >= 1 {
		return 0
	}
	return p.ripple * (1 - 0.7*front) * math.Pow(math.Sin(math.Pi*x), 2)
}

// pose is the final centre and axes of cube i at time t (ball space).
func (p *params) pose(i int, t float64) (vec, mat) {
	c, m, _ := p.cubePose(i, t)
	d := c.normalize()
	return c.add(d.mul(p.rippleAt(d, t))), m
}

// lowest is how far the rounded cube reaches below its centre.
func (p *params) lowest(m mat) float64 {
	return (0.5-p.bevel)*(math.Abs(m[0][1])+math.Abs(m[1][1])+math.Abs(m[2][1])) + p.bevel
}

// regularRadius is the smallest radius at which every cube of the regular
// ball fits its cell, wobbling, over the loop.
func (p *params) regularRadius(fps float64) float64 {
	q := *p
	q.breath, q.rReg, q.rIrr = 0, 0, 0
	r := 0.0
	for st := range int(p.period * fps) {
		for i := range 6 * p.n * p.n {
			c, _, _ := q.cubePoseAt(i, float64(st)/fps, 0)
			r = math.Max(r, c.length())
		}
	}
	return r
}
