package main

import "math"

// The cube ball as a closed formula of (cube, time). shaders/sphere.glsl
// evaluates the same formula; this file mirrors it line for line so that the
// generator can check it and derive the bounds the shader culls with.
//
// Every cube owns a cell of a cubed sphere: the cone from the ball centre
// over a spherical quadrilateral whose corners are grid vertices. A vertex
// moves as a function of the vertex alone (its scatter, swirl and timing
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
	spread float64 // stagger of the morph: how far the cubes' pace varies (< 1)
	sweep  float64 // share of that delay that follows a front over the ball

	period float64 // seconds of the loop: two bounces, the wind-up and the big jump

	// The bounces: the ball touches the floor three times with two small
	// bounces between, the third time deeper and longer, winding up; then
	// it jumps big times as high, under the same gravity, turning
	// irregular and back.
	hop    float64 // height of a small bounce (world)
	big    float64 // height of the big jump against a small bounce
	squash float64 // how deep a small bounce presses the ball onto the floor (edges)
	deep   float64 // the wind-up presses this many times as deep
	deepT  float64 // ... and lasts this many times as long
	splay  float64 // pressed in by q, the bottom cells widen by about splay q
	top    float64 // the top comes down by this share of the squash
	bulge  float64 // the equator swells by this share of the squash
	damp   float64 // decay rate of the swing of the shape after take-off (1/s)

	rReg    float64 // radius of the regular ball (derived, see regularRadius)
	scale   float64 // world units per cube edge (derived)
	low     float64 // how far the regular ball reaches below its centre (edges, derived)
	air     float64 // air time of a small bounce (derived from the period)
	bigAir  float64 // air time of the big jump (derived)
	contact float64 // time on the floor per small bounce (derived)
	vLand   float64 // landing speed of a small bounce (edges/s, derived)
	vBig    float64 // landing speed of the big jump (edges/s, derived)
}

// derive fits the bounces into the period. One gravity rules all arcs, so
// the big jump, big times as high, lasts sqrt(big) times as long and lands
// sqrt(big) times as fast. On the floor the ball is a spring: a small
// bounce presses it in by squash in contact = pi squash / landing speed, so
// contact and air time scale alike and the air time follows from the period.
func (p *params) derive(radius float64) {
	p.rReg = p.regularRadius()
	p.scale = radius / (p.rReg + 0.5)

	p.low = 0
	for i := range 6 * p.n * p.n {
		c, m, _ := p.cubePoseAt(i, 0, phase{})
		p.low = math.Max(p.low, -c[1]+p.lowest(m))
	}

	ratio := math.Pi * p.squash * p.scale / (4 * p.hop) // contact / air
	p.air = p.period / ((2+p.deepT)*ratio + 2 + math.Sqrt(p.big))
	p.contact = ratio * p.air
	p.bigAir = math.Sqrt(p.big) * p.air
	p.vLand = 4 * p.hop / p.air / p.scale
	p.vBig = math.Sqrt(p.big) * p.vLand
}

// deepExtra is how much further the wind-up presses in than its spring
// alone would, so that it presses in deep times as far as a small bounce.
func (p *params) deepExtra() float64 {
	return math.Max(0, p.deep*p.squash-p.deepT*p.contact/math.Pi*(p.vLand+p.vBig)/2)
}

// gravity is the acceleration of every arc (world units/s^2).
func (p *params) gravity() float64 { return 8 * p.hop / (p.air * p.air) }

// omega is the angular frequency of k cycles per loop, so that the swirl
// repeats exactly every period.
func (p *params) omega(k float64) float64 { return 2 * math.Pi * k / p.period }

const (
	smaxK    = 0.15  // smoothness of max(fit radius, hold radius)
	smaxIrrK = 0.8   // ... in the irregular state
	fitK     = 0.2   // smoothness of the max over the cone walls (edges)
	narrowK  = 0.002 // smoothness of the min over the walls' distances
	flatTurn = 0.3   // a cube lies flat once the floor pressed it in this far (edges)
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
func faceBasis(f int) ([3]int, [3]int, [3]int) {
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

// phase is the state of the bounce at a moment.
type phase struct {
	u   float64 // morph: 0 regular, 1 irregular
	q   float64 // how far the floor presses the ball in (edges), > 0 only on the floor
	a   float64 // shape: > 0 squashed, < 0 stretched (edges)
	air float64 // height of the ball centre above its resting height in the air (world)
}

// height is how far the ball centre stands above its resting height
// (world): up the arc in the air, down by q on the floor.
func (p *params) height(ph phase) float64 { return ph.air - ph.q*p.scale }

// floor is the level of the floor below the ball centre (edges).
func (p *params) floor(ph phase) float64 { return p.low + p.height(ph)/p.scale }

// phaseAt is the bounce at time t. The ball touches the floor three times
// with two small bounces between, the third time deeper and longer, and
// then jumps high; in that jump it turns irregular on the way up and is
// regular again as it lands. The centre moves like a ball's: on parabolas of
// one gravity in the air, and on the floor as a spring that presses in by q,
// from the landing speed of the last arc to the take-off speed of the next
// (the wind-up presses further in on top). The shape follows q on the
// floor; after take-off it swings on at the spring's frequency, damped and
// faded out before the next landing, about the centre.
func (p *params) phaseAt(t float64) phase {
	t -= p.period * math.Floor(t/p.period)
	for i := range 3 {
		tc, ta, h, v := p.contact, p.air, p.hop, p.vLand
		if i == 2 {
			tc, ta, h, v = p.deepT*p.contact, p.bigAir, p.big*p.hop, p.vBig
		}

		if t < tc {
			x := t / tc

			vIn := p.vLand
			if i == 0 {
				vIn = p.vBig
			}

			q := tc / math.Pi * math.Sin(math.Pi*x) * mix(vIn, v, smootherstep(x))
			if i == 2 {
				q += p.deepExtra() * math.Pow(math.Sin(math.Pi*x), 2)
			}

			return phase{q: q, a: q}
		}

		t -= tc
		if t < ta || i == 2 {
			x := t / ta

			u := 0.0
			if i == 2 {
				u = p.morph(x)
			}

			w := math.Pi / p.contact
			a := -v / w * math.Exp(-p.damp*t) * math.Sin(w*t) * (1 - smootherstep(x))

			return phase{u: u, a: a, air: 4 * h * x * (1 - x)}
		}

		t -= ta
	}

	return phase{}
}

// morph is how irregular the ball is at the share x of the big jump: it
// swells from take-off on, is fully irregular at the top and returns the
// same way, regular again as the ball lands.
func (p *params) morph(x float64) float64 {
	return math.Pow(math.Sin(math.Pi*x), 2)
}

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

// stage is the progress of something at direction d with the random number
// h. Everything starts at take-off and arrives at the top, but at its own
// pace: u (1 + k (1 - u)) with k within +-spread, ahead mostly as a front
// that sweeps from the upper front over the ball.
func (p *params) stage(u, h float64, d vec) float64 {
	late := mix(h, 0.5-0.5*d.dot(frontAxis), p.sweep)
	return u * (1 + p.spread*(1-2*late)*(1-u))
}

// ---- poses --------------------------------------------------------------

// vertex is the direction of grid vertex k at time t in the phase ph.
func (p *params) vertex(k [3]int, t float64, ph phase) vec {
	d := p.gridDir(k)
	u := ph.u
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

	o = o.sub(d.mul(o.dot(d)))
	d = d.add(o).normalize()
	// pressed onto the floor, the bottom spreads: the vertices move away
	// from the bottom pole, most near it, monotonically in the angle from
	// it, so the cells widen there (and make room for the cubes to come in)
	// and narrow a little above the waist
	if ph.q > 0 {
		c := -d[1]
		w := p.splay * ph.q * (1 + c) * (1 + c) / 4
		d = d.add(vec{d[0] * c, 1 - c*c, d[2] * c}.mul(w)).normalize()
	}

	return d
}

// cell returns the corners of the cell of cube i at time t, counter-clockwise
// seen from outside.
func (p *params) cell(i int, t float64, ph phase) [4]vec {
	nn := p.n * p.n
	f, j, k := i/nn, i%nn/p.n, i%p.n

	return [4]vec{
		p.vertex(p.gridPoint(f, k, j), t, ph),
		p.vertex(p.gridPoint(f, k+1, j), t, ph),
		p.vertex(p.gridPoint(f, k+1, j+1), t, ph),
		p.vertex(p.gridPoint(f, k, j+1), t, ph),
	}
}

// cubePose is the centre and the axes of cube i at time t in ball space,
// and its progress towards the irregular state.
func (p *params) cubePose(i int, t float64) (vec, mat, float64) {
	return p.cubePoseAt(i, t, p.phaseAt(t))
}

// cubePoseAt is cubePose in the bounce phase ph.
func (p *params) cubePoseAt(i int, t float64, ph phase) (vec, mat, float64) {
	return p.cubePoseRamp(i, t, ph, true)
}

// cubePoseRamp is cubePoseAt; with ramp, the radius a cube holds moves
// straight from the regular radius to its own irregular one (the pose at
// u = 1, without ramp), so that the cube does not swing in first and out
// again where its tumble makes it need more room.
func (p *params) cubePoseRamp(i int, t float64, ph phase, ramp bool) (vec, mat, float64) {
	u := ph.u
	q := p.cell(i, t, ph)
	h := cubeKey(i)

	// the regular state stands in the middle of the cell, the irregular one
	// off it
	d0 := q[0].add(q[1]).add(q[2]).add(q[3]).normalize()
	s := p.stage(u, rnd(h, 0), d0)
	x := 0.5 + p.offset*s*(rnd(h, 1)-0.5)
	y := 0.5 + p.offset*s*(rnd(h, 2)-0.5)
	d := q[0].mul((1 - x) * (1 - y)).add(q[1].mul(x * (1 - y))).add(q[2].mul(x * y)).add(q[3].mul((1 - x) * y)).normalize()

	m := cellAxes(q, d)

	// the cell's walls: planes through the centre and two neighbouring
	// corners, normals inwards; wd is how far d stays from them
	var w [4]vec
	var wd [4]float64

	for e := range 4 {
		w[e] = q[e].cross(q[(e+1)%4]).normalize()
		wd[e] = w[e].dot(d)
	}

	narrow := -smax(smax(-wd[0], -wd[1], narrowK), smax(-wd[2], -wd[3], narrowK), narrowK)

	// A tumbled cube reaches further towards the walls (by up to the
	// rounded cube's circumradius minus its half edge); in a narrow cell it
	// tumbles less, so that it need not stand out far beyond rCap.
	room := clamp01((p.rCap*narrow - 0.5 - p.marg) / ((0.5 - p.bevel) * (math.Sqrt(3) - 1)))
	axis := vec{rnd(h, 3)*2 - 1, rnd(h, 4)*2 - 1, rnd(h, 5)*2 - 1}.add(vec{0, 0, 1e-3}).normalize()
	ang := p.tumble * mix(0.35, 1, rnd(h, 6)) * room * s
	m = axisAngle(axis, ang).mul(m)

	fitOf := func(m mat) float64 { return p.coneFit(m, w, wd) }

	hold, sk := p.holdRadius(i, t, s, ramp)
	lo := fitOf(m) - p.sink*s
	r := smax(lo, hold, sk)

	// The squash: the shell swells at the equator and comes down at the
	// top, and on the floor its lowest points are cut flat by the floor. A
	// cube the floor reaches turns onto the face that looks most downwards
	// (cubes that stand on an edge do not turn). The ball is regular
	// whenever it touches the floor.
	dy := d[1]

	r1 := r + ph.a*(p.bulge*(1-dy*dy)-p.top*dy*dy)
	if ph.q > 0 && i/(p.n*p.n) == 3 {
		floor := p.floor(ph)
		press := r*(-dy) + p.lowest(m) - floor

		f := downFace(m)
		if wt := smoothstep(0, flatTurn, press) * smoothstep(0.75, 0.85, -f[1]); wt > 0 && f[1] > -0.99999 {
			m = axisAngle(f.cross(vec{0, -1, 0}).normalize(), wt*math.Acos(-f[1])).mul(m)
			lo = fitOf(m) - p.sink*s
			r = smax(lo, hold, sk)
			r1 = r + ph.a*(p.bulge*(1-dy*dy)-p.top*dy*dy)
		}

		if press > 0 {
			r1 = math.Min(r1, (floor-p.lowest(m))/(-dy))
		}
	}
	// the squash moves a cube in no further than to lo
	if in := r - r1; in > 0 {
		r -= sminP(in, r-lo)
	} else {
		r = r1
	}

	return d.mul(r), m, s
}

// cellAxes are the axes of a cube at direction d in the cell q: z outwards,
// x and y symmetric between the cell's two directions.
func cellAxes(q [4]vec, d vec) mat {
	tu := q[1].add(q[2]).sub(q[0]).sub(q[3])
	tv := q[2].add(q[3]).sub(q[0]).sub(q[1])
	tu = tu.sub(d.mul(tu.dot(d))).normalize()
	tv = tv.sub(d.mul(tv.dot(d))).normalize()
	a := tu.add(tv).normalize().add(tu.sub(tv).normalize()).normalize()

	return mat{a, d.cross(a), d}
}

// coneFit is the smallest radius that keeps a cube with axes m inside the
// cone of the walls w at the distances wd: the rounded cube's support along
// each wall normal against the wall's distance (a smooth maximum, never
// below the true one, so that the cube does not jerk when another wall
// takes over).
func (p *params) coneFit(m mat, w [4]vec, wd [4]float64) float64 {
	var fit [4]float64

	for e := range 4 {
		sup := (0.5-p.bevel)*(math.Abs(m[0].dot(w[e]))+math.Abs(m[1].dot(w[e]))+math.Abs(m[2].dot(w[e]))) + p.bevel + p.marg
		fit[e] = sup / wd[e]
	}

	return smax(smax(fit[0], fit[1], fitK), smax(fit[2], fit[3], fitK), fitK)
}

// holdRadius is the radius cube i holds at progress s and the smoothness of
// its maximum with the cone fit (see cubePoseRamp).
func (p *params) holdRadius(i int, t, s float64, ramp bool) (float64, float64) {
	if !ramp {
		return p.rIrr, smaxIrrK
	}

	if s <= 0 {
		return p.rReg, smaxK
	}

	c1, _, _ := p.cubePoseRamp(i, t, phase{u: 1}, false)

	return mix(p.rReg, c1.length(), s), smaxK
}

// downFace is the axis of m that looks most downwards, pointing down.
func downFace(m mat) vec {
	k := 0
	for c := 1; c < 3; c++ {
		if math.Abs(m[c][1]) > math.Abs(m[k][1]) {
			k = c
		}
	}

	if m[k][1] > 0 {
		return m[k].mul(-1)
	}

	return m[k]
}

// sminP <= min(a, b) for a, b >= 0, smooth, and 0 where either is 0.
func sminP(a, b float64) float64 {
	if a <= 0 || b <= 0 {
		return 0
	}

	return a * b / math.Pow(a*a*a*a+b*b*b*b, 0.25)
}

func smoothstep(a, b, x float64) float64 {
	x = clamp01((x - a) / (b - a))
	return x * x * (3 - 2*x)
}

// smax >= max(a, b), smooth.
func smax(a, b, k float64) float64 { return 0.5 * (a + b + math.Sqrt((a-b)*(a-b)+k*k)) }

// pose is the centre and axes of cube i at time t (ball space).
func (p *params) pose(i int, t float64) (vec, mat) {
	c, m, _ := p.cubePose(i, t)
	return c, m
}

// lowest is how far the rounded cube reaches below its centre.
func (p *params) lowest(m mat) float64 {
	return (0.5-p.bevel)*(math.Abs(m[0][1])+math.Abs(m[1][1])+math.Abs(m[2][1])) + p.bevel
}

// regularRadius is the smallest radius at which every cube of the regular
// ball, which stands still but for the squash, fits its cell.
func (p *params) regularRadius() float64 {
	q := *p
	q.rReg, q.rIrr = 0, 0
	r := 0.0

	for i := range 6 * p.n * p.n {
		c, _, _ := q.cubePoseAt(i, 0, phase{})
		r = math.Max(r, c.length())
	}

	return r
}
