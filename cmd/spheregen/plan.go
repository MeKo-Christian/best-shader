package main

import (
	"math"
	"math/rand/v2"
	"sort"
)

// Timing of the forward motion, u in [0, 1] (shaders/sphere.glsl mirrors
// stage() and hashU()): expand in [0, ua], breathe out in [ua, ub], shuffle
// in [ub, us], settle in [us, uz], pack in [uz, 1]. Expanding scales the
// whole ball at once; in the other phases each cube starts after a random
// delay of up to spread of the phase (packSpread when packing, where the
// cubes go almost one after the other). All ease with smootherstep.
const (
	ua         = 0.1
	ub         = 0.3
	us         = 0.7
	uz         = 0.9
	spread     = 0.4
	packSpread = 0.85
	slope      = 1.875 / (1 - spread) // max d(progress)/d(phase progress)
	slopePack  = 1.875 / (1 - packSpread)
	slope1     = 1.875 // the same without delays
)

func hashU(x uint32) uint32 {
	x ^= x >> 16
	x *= 0x7feb352d
	x ^= x >> 15
	x *= 0x846ca68b
	x ^= x >> 16
	return x
}

// cubeHash is a per cube and phase random number in [0, 1), exact in float32.
func cubeHash(i, phase int) float64 {
	return float64(hashU(uint32(i)*3+uint32(phase))>>8) / 16777216
}

func smootherstep(x float64) float64 { return x * x * x * (x*(x*6-15) + 10) }

// stage is the eased progress of cube i (hash h) in the phase [a, b] whose
// delays spread over sp of it.
func stage(u, a, b, h, sp float64) float64 {
	x := math.Max(0, math.Min(1, (u-a)/(b-a)))
	x = math.Max(0, math.Min(1, (x-sp*h)/(1-sp)))
	return smootherstep(x)
}

// global is the eased progress of the whole ball in the phase [a, b].
func global(u, a, b float64) float64 {
	return smootherstep(math.Max(0, math.Min(1, (u-a)/(b-a))))
}

// cubes is the whole plan, per cube: the regular pose (d0, m0, common radius
// r0), the shuffle radius rs, the target pose (d1, m1, radius r1) and the
// target as a rotation (axis, ang) applied to m0. At r0 and r1 no cube hides
// another as seen from the centre; the rest states are denser: the regular
// ball rests at radius rRest, the irregular one at the radii rEnd, where the
// cubes interlock.
type cubes struct {
	n            int
	r0, rRest    float64
	d0, d1       []vec
	m0, m1       []mat
	r1, rs, rEnd []float64
	axis         []vec
	ang          []float64
	half, marg   float64
}

// pose of cube i at forward time u, relative to the ball centre.
func (c *cubes) pose(i int, u float64) (vec, mat) {
	ea := global(u, 0, ua)
	eb := stage(u, ua, ub, cubeHash(i, 0), spread)
	es := stage(u, ub, us, cubeHash(i, 1), spread)
	et := stage(u, us, uz, cubeHash(i, 2), spread)
	ep := stage(u, uz, 1, cubeHash(i, 3), packSpread)
	r := mix(mix(mix(mix(c.rRest, c.r0, ea), c.rs[i], eb), c.r1[i], et), c.rEnd[i], ep)
	d := slerpDir(c.d0[i], c.d1[i], es)
	m := axisAngle(c.axis[i], c.ang[i]*es).mul(c.m0[i])
	return d.mul(r), m
}

// pack sinks the cubes from their radially separable radii r1 one after
// the other, in the order of their pack delays, each until it touches (keeps
// a gap of marg to) its neighbours where they are by then, but not below
// rMin. Unlike r1 this lets cubes interlock, so the irregular ball ends up
// about as dense as the regular one. The sinking itself is collision free;
// the pack phase overlaps the cubes' windows a little, which verify() checks.
func (c *cubes) pack(nb [][]int, rMin float64) {
	const step = 0.01
	c.rEnd = append([]float64(nil), c.r1...)
	order := make([]int, len(c.rEnd))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(a, b int) bool { return cubeHash(order[a], 3) < cubeHash(order[b], 3) })
	clear := func(i int, r float64) bool {
		p := c.d1[i].mul(r)
		for _, j := range nb[i] {
			if satGap(p, c.m1[i], c.d1[j].mul(c.rEnd[j]), c.m1[j], c.half) < c.marg {
				return false
			}
		}
		return true
	}
	for _, i := range order {
		for c.rEnd[i]-step >= rMin && clear(i, c.rEnd[i]-step) {
			c.rEnd[i] -= step
		}
	}
}

// shuffleRadii lifts every cube to the radius at which it slides and
// tumbles. Two cubes need radii at least one bounding-sphere diameter apart
// (of the cubes grown by half the margin), unless they keep the margin as
// boxes (separating axis test) all along their paths, sampled finely enough
// for their speeds. Greedy, lowest
// feasible radius first, over several orders that place the bottom cubes
// first (whatever they lift, lifts the whole ball off the floor); keeps the
// plan with the smallest height, the reach of the cubes above plus below
// the centre, with a little weight on the total lift.
func (c *cubes) shuffleRadii(orders int, rng *rand.Rand) {
	const samples = 400
	nc := len(c.d0)
	rho := (c.half + 0.5*c.marg) * math.Sqrt(3)
	dmin := 2 * rho

	path := make([][]vec, nc)
	rot := make([][]mat, nc)
	theta := make([]float64, nc)
	for i := range nc {
		path[i] = make([]vec, samples)
		rot[i] = make([]mat, samples)
		h := cubeHash(i, 1)
		for k := range samples {
			x := float64(k) / (samples - 1)
			e := stage(x, 0, 1, h, spread)
			path[i][k] = slerpDir(c.d0[i], c.d1[i], e)
			rot[i][k] = axisAngle(c.axis[i], c.ang[i]*e).mul(c.m0[i])
		}
		theta[i] = math.Acos(math.Max(-1, math.Min(1, c.d0[i].dot(c.d1[i]))))
	}
	rhoBox := c.half * math.Sqrt(3)
	// pairs whose paths come close at all (at the largest radius)
	near := make([][]int, nc)
	for i := range nc {
		for j := range nc {
			if i == j {
				continue
			}
			minAng := math.Inf(1)
			for k := 0; k < samples; k += 8 {
				minAng = math.Min(minAng, math.Acos(math.Max(-1, math.Min(1, path[i][k].dot(path[j][k])))))
			}
			if minAng < 0.6 {
				near[i] = append(near[i], j)
			}
		}
	}
	ok := func(i int, ri float64, j int, rj float64) bool {
		if math.Abs(ri-rj) >= dmin {
			return true
		}
		slack := (ri*theta[i] + rj*theta[j] + (c.ang[i]+c.ang[j])*rhoBox) * slope / (samples - 1) / 2
		for k := range samples {
			pi, pj := path[i][k].mul(ri), path[j][k].mul(rj)
			if pi.sub(pj).length() >= 2*rhoBox+c.marg+slack {
				continue
			}
			if satGap(pi, rot[i][k], pj, rot[j][k], c.half) < c.marg+slack {
				return false
			}
		}
		return true
	}

	lo := make([]float64, nc)
	for i := range nc {
		lo[i] = math.Max(c.r0, c.r1[i])
	}
	bestScore := math.Inf(1)
	var best []float64
	for o := range orders {
		order := rng.Perm(nc)
		low := func(i int) float64 { return math.Min(c.d0[i][1], c.d1[i][1]) }
		if o == 0 { // from the bottom up
			sort.Slice(order, func(a, b int) bool { return low(order[a]) < low(order[b]) })
		} else { // the bottom cap first, otherwise random
			sort.SliceStable(order, func(a, b int) bool { return low(order[a]) < -0.6 && low(order[b]) >= -0.6 })
		}
		rs := make([]float64, nc)
		placed := make([]bool, nc)
		for _, i := range order {
			cand := []float64{lo[i]}
			for _, j := range near[i] {
				if placed[j] && rs[j]+dmin > lo[i] {
					cand = append(cand, rs[j]+dmin)
				}
			}
			sort.Float64s(cand)
			for _, r := range cand {
				good := true
				for _, j := range near[i] {
					if placed[j] && !ok(i, r, j, rs[j]) {
						good = false
						break
					}
				}
				if good {
					rs[i] = r
					break
				}
			}
			placed[i] = true
		}
		top, bottom, sum := 0.0, 0.0, 0.0
		for i := range nc {
			top = math.Max(top, rs[i]*math.Max(c.d0[i][1], c.d1[i][1]))
			bottom = math.Max(bottom, -rs[i]*low(i))
			sum += rs[i] - lo[i]
		}
		if score := top + bottom + 0.2*sum/float64(nc); score < bestScore {
			bestScore, best = score, rs
		}
	}
	c.rs = best
}

// speedBound bounds, per unit of u, how fast any cube point moves (centre
// speed plus angular speed times the circumradius).
func (c *cubes) speedBound() float64 {
	rho := c.half * math.Sqrt(3)
	v := 0.0
	for i := range c.d0 {
		theta := math.Acos(math.Max(-1, math.Min(1, c.d0[i].dot(c.d1[i]))))
		v = math.Max(v, (c.r0-c.rRest)*slope1/ua)
		v = math.Max(v, math.Abs(c.rs[i]-c.r0)*slope/(ub-ua))
		v = math.Max(v, (c.rs[i]*theta+c.ang[i]*rho)*slope/(us-ub))
		v = math.Max(v, math.Abs(c.rs[i]-c.r1[i])*slope/(uz-us))
		v = math.Max(v, (c.r1[i]-c.rEnd[i])*slopePack/(1-uz))
	}
	return v
}

// lowest is how far the rounded cube reaches below its centre.
func lowest(m mat, half, bevel float64) float64 {
	return (half-bevel)*(math.Abs(m[0][1])+math.Abs(m[1][1])+math.Abs(m[2][1])) + bevel
}

// upperCurve is a uniform quadratic B-spline over u that never falls below
// max_i f(i, u) + pad. A B-spline is a convex combination of the three
// control points of its segment, so each control point is the largest value
// over the segments it touches, plus the sampling slack (f changes at most at
// the speed bound).
func (c *cubes) upperCurve(segs int, pad float64, f func(i int, u float64) float64) []float64 {
	const sub = 64
	v := c.speedBound()
	du := 1.0 / float64(segs*sub)
	need := make([]float64, segs)
	for s := range segs {
		need[s] = math.Inf(-1)
		for k := 0; k <= sub; k++ {
			u := (float64(s) + float64(k)/sub) / float64(segs)
			for i := range c.d0 {
				need[s] = math.Max(need[s], f(i, u))
			}
		}
		need[s] += v*du/2 + pad
	}
	ctrl := make([]float64, segs+2)
	for j := range ctrl {
		ctrl[j] = math.Inf(-1)
		for s := j - 2; s <= j; s++ {
			if s >= 0 && s < segs {
				ctrl[j] = math.Max(ctrl[j], need[s])
			}
		}
	}
	return ctrl
}

// lift is the height of the ball centre over the floor: no rounded cube
// ever reaches below the floor, and they keep the margin to it.
func (c *cubes) lift(segs int, bevel float64) []float64 {
	return c.upperCurve(segs, c.marg, func(i int, u float64) float64 {
		p, m := c.pose(i, u)
		return lowest(m, c.half, bevel) - p[1]
	})
}

// bound is a radius around the ball centre that contains every cube.
func (c *cubes) bound(segs int) []float64 {
	return c.upperCurve(segs, 0, func(i int, u float64) float64 {
		p, _ := c.pose(i, u)
		return p.length() + c.half*math.Sqrt(3)
	})
}

func liftAt(ctrl []float64, u float64) float64 {
	segs := len(ctrl) - 2
	x := math.Max(0, math.Min(1, u)) * float64(segs)
	k := min(int(x), segs-1)
	f := x - float64(k)
	return 0.5*(1-f)*(1-f)*ctrl[k] + (0.5+f-f*f)*ctrl[k+1] + 0.5*f*f*ctrl[k+2]
}

// Clusters group the cubes for culling: on every cube face the cells split
// into 3 x 3 blocks of rows and columns (for n = 7: 2, 3 and 2), so a ray
// first meets the bounding spheres of 54 clusters and only then the cubes of
// the clusters it hits. shaders/sphere.glsl mirrors clusterBlock().
func clusterBlock(k, n int) int {
	switch {
	case 3*k < n-1:
		return 0
	case 3*k < 2*n+1:
		return 1
	}
	return 2
}

func (c *cubes) clusterOf(i int) int {
	n := c.n
	f, j, k := i/(n*n), i%(n*n)/n, i%n
	return f*9 + clusterBlock(j, n)*3 + clusterBlock(k, n)
}

// clusterSpheres returns per cluster the B-spline control points (segs + 2)
// of its bounding sphere: the centre follows the mean of its cubes at the
// knots, the radius is an upper curve over the distance of every cube point
// to that centre, so the sphere contains its cubes at every u.
func (c *cubes) clusterSpheres(segs int) [][][4]float64 {
	nc := len(c.d0)
	out := make([][][4]float64, 54)
	members := make([][]int, 54)
	for i := range nc {
		k := c.clusterOf(i)
		members[k] = append(members[k], i)
	}
	rho := c.half * math.Sqrt(3)
	for k, mem := range members {
		ctrl := make([][4]float64, segs+2)
		cx := make([][]float64, 3)
		for a := range 3 {
			cx[a] = make([]float64, segs+2)
		}
		// control point j weighs most at u = (j - 0.5) / segs
		for j := range ctrl {
			u := math.Max(0, math.Min(1, (float64(j)-0.5)/float64(segs)))
			m := vec{}
			for _, i := range mem {
				p, _ := c.pose(i, u)
				m = m.add(p)
			}
			m = m.mul(1 / float64(len(mem)))
			for a := range 3 {
				cx[a][j] = m[a]
			}
		}
		centre := func(u float64) vec { return vec{liftAt(cx[0], u), liftAt(cx[1], u), liftAt(cx[2], u)} }
		// the centre curve moves at most as fast as its control polygon allows
		vc := 0.0
		for j := 1; j < len(ctrl); j++ {
			d := vec{cx[0][j] - cx[0][j-1], cx[1][j] - cx[1][j-1], cx[2][j] - cx[2][j-1]}
			vc = math.Max(vc, d.length()*float64(segs))
		}
		rad := c.upperCurve(segs, rho+vc/float64(segs*64)/2, func(i int, u float64) float64 {
			if c.clusterOf(i) != k {
				return math.Inf(-1)
			}
			p, _ := c.pose(i, u)
			return p.sub(centre(u)).length()
		})
		for j := range ctrl {
			ctrl[j] = [4]float64{cx[0][j], cx[1][j], cx[2][j], rad[j]}
		}
		out[k] = ctrl
	}
	return out
}
