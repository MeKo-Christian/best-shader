// Command spheregen plans the cube ball of shaders/sphere.glsl and writes
// its data, shaders/sphere_data.glsl.
//
// The ball is a shell of equal cubes. At rest it is either regular (an
// equiangular cubed sphere, every cube facing outwards) or irregular (the
// cubes scattered and tumbled). The forward motion between the two never
// lets two cubes touch:
//
//  1. Breathe out: every cube moves radially outward to its shuffle radius.
//  2. Shuffle: at fixed radii the cubes slide along great circles to their
//     target directions and tumble to their target orientations.
//  3. Settle: every cube moves radially inward to its target radius.
//
// Lemma 1: if no cube hides another as seen from the ball centre, a plane
// through the centre separates each pair, so no two cubes touch. Lemma 2: a
// cube inside a convex cone from the centre stays inside it when it moves
// radially outward. Both rest states satisfy lemma 1, so breathing out and
// settling are safe whatever the timing. While shuffling, two cubes either
// keep their radii a bounding-sphere diameter apart or their spheres never
// meet along their paths (checked by sampling). The way back is the same
// motion reversed.
//
// Independently of these arguments, verify() tests every pair as boxes over
// the whole motion at time steps fine enough that no contact can slip
// between samples, and checks that no cube reaches into the floor.
package main

import (
	"flag"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
)

func main() {
	n := flag.Int("n", 7, "cubes per cube-face edge (odd: a cube rests on the bottom pole)")
	seed := flag.Uint64("seed", 1, "random seed")
	jitter := flag.Float64("jitter", 0.7, "scatter of the target directions in grid pitches")
	relax := flag.Int("relax", 15, "repulsion steps on the target directions")
	tries := flag.Int("tries", 16, "random orientations tried per target")
	passes := flag.Int("passes", 3, "jamming passes")
	improve := flag.Int("improve", 600, "steps lowering the highest targets")
	packDepth := flag.Float64("depth", 0.5, "how far below the regular radius a cube may sink when packing (cube edges)")
	orders := flag.Int("orders", 60, "random orders tried for the shuffle radii")
	marg := flag.Float64("margin", 0.03, "clearance between cubes (cube edges)")
	rest := flag.Float64("rest", 0, "radius of the regular ball at rest (cube edges), at least the densest packing")
	bevel := flag.Float64("bevel", 0.1, "edge radius of the cubes (cube edges)")
	segs := flag.Int("lift", 48, "segments of the lift spline")
	ballX := flag.Float64("x", 0.975, "ball centre x (world)")
	ballZ := flag.Float64("z", 5.51, "ball centre z (world)")
	radius := flag.Float64("radius", 1.48, "radius of the regular ball (world)")
	camX := flag.Float64("camx", 0.735, "camera x (world), the front face looks at it")
	out := flag.String("out", "shaders/sphere_data.glsl", "output file")
	flag.Parse()

	if err := run(*n, *seed, *jitter, *relax, *tries, *passes, *improve, *orders, *marg, *bevel, *packDepth, *segs,
		*ballX, *ballZ, *radius, *camX, *rest, *out); err != nil {
		fmt.Fprintln(os.Stderr, "spheregen:", err)
		os.Exit(1)
	}
}

const clusterSegs = 24

func run(n int, seed uint64, jitter float64, relax, tries, passes, improve, orders int, marg, bevel, packDepth float64,
	segs int, ballX, ballZ, radius, camX, restRadius float64, out string,
) error {
	rng := rand.New(rand.NewPCG(seed, 0x5eed))
	nc := 6 * n * n
	half := 0.5
	pitch := math.Pi / 2 / float64(n)
	c := &cubes{n: n, half: half, marg: marg}

	c.d0, c.m0 = make([]vec, nc), make([]mat, nc)
	for k := range nc {
		c.d0[k], c.m0[k] = regularPose(k, n)
	}
	inflated := half + 0.5*marg
	c.r0 = regularRadius(c.d0, c.m0, neighbours(c.d0, 2.5*pitch), inflated)

	slots := irregularDirs(c.d0, pitch, jitter, relax, rng)
	slotNb := neighbours(slots, 3*pitch)
	slotM, slotR := irregularTargets(slots, slotNb, inflated, tries, passes, rng)
	improveTargets(slots, slotM, slotR, slotNb, inflated, pitch, improve, 24, rng)
	match := assign(c.d0, slots)

	syms := cubeSymmetries()
	c.d1, c.m1, c.r1 = make([]vec, nc), make([]mat, nc), make([]float64, nc)
	c.axis, c.ang = make([]vec, nc), make([]float64, nc)
	for i, k := range match {
		c.d1[i], c.r1[i] = slots[k], slotR[k]
		c.m1[i] = nearestSymmetric(c.m0[i], slotM[k], syms)
		c.axis[i], c.ang[i] = toAxisAngle(c.m1[i].mul(c.m0[i].transpose()))
	}
	c.shuffleRadii(orders, rng)

	// The rest states may be denser than radial separability allows: from
	// any state without contacts the whole ball can expand (lemma 3).
	nb0 := neighbours(c.d0, 2.5*pitch)
	c.rRest = math.Max(minScale(c.d0, c.m0, nb0, half, marg), restRadius)
	c.pack(neighbours(c.d1, 3*pitch), c.rRest-packDepth)

	data := make([][4]uint32, nc)
	for i := range nc {
		data[i] = c.encode(i)
	}
	c.unpack(data)
	liftCtrl := c.lift(segs, bevel)
	clusters := c.clusterSpheres(clusterSegs)
	rep := c.verify(liftCtrl, clusters, bevel)

	scale := radius / (c.rRest + half)
	mid := 4*n*n + n/2*n + n/2 // face centre cube and its neighbour
	gap := satGap(c.d0[mid].mul(c.rRest), c.m0[mid], c.d0[mid+1].mul(c.rRest), c.m0[mid+1], half)
	endLo, endMd, endHi := quantiles(c.rEnd)
	slide, tumble := make([]float64, nc), make([]float64, nc)
	for i := range nc {
		slide[i] = math.Acos(math.Max(-1, math.Min(1, c.d0[i].dot(c.d1[i])))) / pitch
		tumble[i] = c.ang[i] * 180 / math.Pi
	}
	r1lo, r1md, r1hi := quantiles(c.r1)
	rslo, rsmd, rshi := quantiles(c.rs)
	_, slmd, slhi := quantiles(slide)
	_, tumd, tuhi := quantiles(tumble)
	stats := []string{
		fmt.Sprintf("%d cubes, edge %.4f world units, regular radius %.3f edges at rest (face-centre gap %.2f edges), %.3f radially separable", nc, scale, c.rRest, gap, c.r0),
		fmt.Sprintf("irregular radius min/median/max %.2f / %.2f / %.2f at rest, %.2f / %.2f / %.2f radially separable", endLo, endMd, endHi, r1lo, r1md, r1hi),
		fmt.Sprintf("shuffle radius min/median/max %.2f / %.2f / %.2f", rslo, rsmd, rshi),
		fmt.Sprintf("slide median/max %.2f / %.2f pitches, tumble median/max %.0f / %.0f degrees", slmd, slhi, tumd, tuhi),
		fmt.Sprintf("lift %.3f at rest, %.3f max (edges); peak radius %.2f = %.2f x regular", liftAt(liftCtrl, 0), maxOf(liftCtrl), rep.peakRadius, rep.peakRadius/(c.rRest+half)),
		"verify: " + rep.String(),
	}
	for _, s := range stats {
		fmt.Println(s)
	}
	if !rep.ok() {
		return fmt.Errorf("verification failed, %s not written", out)
	}

	w := vec{camX - ballX, 0, -ballZ}.normalize()
	yaw := math.Atan2(-w[0], -w[2])
	return os.WriteFile(out, []byte(emit(c, data, liftCtrl, c.bound(segs), clusters, stats, scale, ballX, ballZ, yaw, bevel)), 0o644)
}

func maxOf(xs []float64) float64 {
	m := math.Inf(-1)
	for _, x := range xs {
		m = math.Max(m, x)
	}
	return m
}
