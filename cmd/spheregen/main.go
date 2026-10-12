// Command spheregen checks the cube ball of shaders/sphere.glsl and writes
// its constants, shaders/sphere_data.glsl.
//
// The ball is a shell of equal cubes that bounces on the floor like a
// rubber ball, squashing on every landing: as a regular ball (a cubed
// sphere, every cube facing outwards) it touches the floor three times, the
// third time deeper, winding up, then jumps twice as high: on the way up its
// cubes turn irregular about the centre (scattered, tumbled and sunk to
// different depths), most at the top, and on the way down they return,
// regular again as it lands. The centre follows the physics
// of a ball alone. Every cube pose is a closed formula
// of the cube's index and the time (formula.go, mirrored by the shader), so
// the shader needs no tables.
//
// No two cubes can touch: every cube stays inside its own cone from the
// ball centre, and the cones partition space as long as no cell of the grid
// they stand on folds over. verify() checks that, tests every pair of cubes
// as boxes over the whole loop as an independent check, and derives the
// bounds the shader culls with.
package main

import (
	"flag"
	"fmt"
	"math"
	"os"
)

func main() {
	p := params{}
	flag.IntVar(&p.n, "n", 7, "cubes per cube-face edge")
	flag.Float64Var(&p.bevel, "bevel", 0.1, "edge radius of the cubes (cube edges)")
	flag.Float64Var(&p.marg, "margin", 0.03, "clearance of a cube from its cone walls (cube edges)")
	flag.Float64Var(&p.jitter, "jitter", 0.02, "vertex scatter in the irregular state (radians)")
	flag.Float64Var(&p.warp, "warp", 0.05, "smooth flowing distortion of the grid in the irregular state (radians)")
	flag.Float64Var(&p.offset, "offset", 0.1, "cube offset inside its cell in the irregular state (share of the cell)")
	tumble := flag.Float64("tumble", 60, "largest tumble angle (degrees)")
	flag.Float64Var(&p.rIrr, "floor", 5.6, "irregular state: radius the cubes may sink down to (cube edges)")
	flag.Float64Var(&p.sink, "sink", 0.6, "irregular state: how far a cube may stand inside its cone fit (cube edges); > 0 lets cubes overlap slightly")
	flag.Float64Var(&p.rCap, "cap", 7.5, "cubes in narrow cells tumble less to stay within about this radius (cube edges)")
	flag.Float64Var(&p.spread, "spread", 0.6, "the cubes' pace in the morph: u (1 + k (1 - u)) with k within +-spread (< 1)")
	flag.Float64Var(&p.sweep, "sweep", 0.7, "share of the delay that follows a front over the ball")
	flag.Float64Var(&p.period, "period", 8.5, "seconds of the loop: two bounces, the wind-up and the big jump; sets the gravity")
	flag.Float64Var(&p.hop, "hop", 0.18, "height of a small bounce (world)")
	flag.Float64Var(&p.big, "big", 2, "height of the big jump against a small bounce (it lasts sqrt(big) times as long)")
	flag.Float64Var(&p.squash, "squash", 0.45, "how deep a small bounce presses the ball in (cube edges); sets the time on the floor")
	flag.Float64Var(&p.deep, "deep", 2, "the wind-up before the big jump presses this many times as deep")
	flag.Float64Var(&p.deepT, "deept", 1.5, "... and lasts this many times as long")
	flag.Float64Var(&p.splay, "splay", 0.3, "pressed in by q (cube edges), the bottom cells widen by about splay q")
	flag.Float64Var(&p.top, "top", 0.4, "the top comes down by this share of the squash")
	flag.Float64Var(&p.bulge, "bulge", 0.6, "the equator swells by this share of the squash")
	flag.Float64Var(&p.damp, "damp", 2.5, "decay rate of the swing of the shape after take-off (1/s)")
	tol := flag.Float64("tolerance", 0.08, "deepest overlap of two cubes accepted (cube edges)")
	floorTol := flag.Float64("floortol", 0.03, "deepest a cube may reach into the floor (cube edges)")
	fps := flag.Float64("fps", 60, "samples per second for the check")
	ballX := flag.Float64("x", 0.975, "ball centre x (world)")
	ballZ := flag.Float64("z", 6.8, "ball centre z (world)")
	radius := flag.Float64("radius", 1.48, "radius of the regular ball (world)")
	camX := flag.Float64("camx", 0.735, "camera x (world), the front face looks at it")
	out := flag.String("out", "shaders/sphere_data.glsl", "output file")
	flag.Parse()
	p.tumble = *tumble * math.Pi / 180

	if err := run(&p, *tol, *floorTol, *fps, *ballX, *ballZ, *radius, *camX, *out); err != nil {
		fmt.Fprintln(os.Stderr, "spheregen:", err)
		os.Exit(1)
	}
}

func run(p *params, tol, floorTol float64, fps, ballX, ballZ, radius, camX float64, out string) error {
	p.derive(radius)
	s := p.verify(fps)
	lines := []string{
		fmt.Sprintf("%d cubes, edge %.4f world units, regular radius %.3f edges", 6*p.n*p.n, p.scale, p.rReg),
		fmt.Sprintf("loop %.3f s: bounces %.3f s in the air (%.2f high), %.3f s on the floor (pressed in %.2f edges), wind-up %.3f s, big jump %.3f s (%.2f high), gravity %.3f",
			p.period, p.air, p.hop, p.contact, p.squash, p.deepT*p.contact, p.bigAir, p.big*p.hop, p.gravity()),
		fmt.Sprintf("radius at t=0 %.2f..%.2f, at the irregular turn min/median/max %.2f / %.2f / %.2f",
			s.regRange[0], s.regRange[1], s.irrRange[0], s.irrRange[1], s.irrRange[2]),
		fmt.Sprintf("regular -> irregular: median slide %.2f pitches, largest turn %.0f degrees", s.medianSlide, s.maxTumble*180/math.Pi),
		"verify: " + s.String(),
	}
	for _, l := range lines {
		fmt.Println(l)
	}
	switch {
	case s.folded > 0:
		return fmt.Errorf("%d cell samples folded over: the scatter is too large, %s not written", s.folded, out)
	case s.minGap < -tol:
		return fmt.Errorf("cubes %d and %d overlap by %.4f at t=%.3f, %s not written", s.gapPair[0], s.gapPair[1], -s.minGap, s.gapT, out)
	case s.floorIn > floorTol:
		return fmt.Errorf("a cube reaches %.4f into the floor, %s not written", s.floorIn, out)
	}

	w := vec{camX - ballX, 0, -ballZ}.normalize()
	yaw := math.Atan2(-w[0], -w[2])
	return os.WriteFile(out, []byte(emit(p, s, lines, ballX, ballZ, yaw)), 0o644)
}
