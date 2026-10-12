// Command spheregen checks the cube ball of shaders/sphere.glsl and writes
// its constants, shaders/sphere_data.glsl.
//
// The ball is a shell of equal cubes that morphs, without ever resting,
// between a regular state (a cubed sphere, every cube facing outwards) and
// an irregular one (the cubes scattered, tumbled and sunk to different
// depths), while it hops on the floor. Every cube pose is a closed formula
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
	flag.Float64Var(&p.spread, "spread", 0.5, "share of the morph a cube may start late")
	flag.Float64Var(&p.sweep, "sweep", 0.5, "share of the delay that follows a front over the ball")
	flag.Float64Var(&p.period, "period", 24, "seconds of the loop regular -> irregular -> regular")
	flag.Float64Var(&p.wobble, "wobble", 0.01, "vertex wobble (radians)")
	wobRot := flag.Float64("wobrot", 6, "tumble wobble (degrees)")
	flag.Float64Var(&p.breath, "breath", 0.12, "radial breathing (cube edges)")
	flag.IntVar(&p.hops, "hops", 24, "hops per loop")
	flag.Float64Var(&p.ripple, "ripple", 0.3, "outward ripple after each landing (cube edges)")
	hop := flag.Float64("hop", 0.05, "hop height (world)")
	tol := flag.Float64("tolerance", 0.08, "deepest overlap of two cubes accepted (cube edges)")
	liftW := flag.Int("liftw", 5, "the shader finds the lowest point among the middle w x w cubes of the bottom face")
	fps := flag.Float64("fps", 60, "samples per second for the check")
	ballX := flag.Float64("x", 0.975, "ball centre x (world)")
	ballZ := flag.Float64("z", 5.51, "ball centre z (world)")
	radius := flag.Float64("radius", 1.48, "radius of the regular ball (world)")
	camX := flag.Float64("camx", 0.735, "camera x (world), the front face looks at it")
	out := flag.String("out", "shaders/sphere_data.glsl", "output file")
	flag.Parse()
	p.tumble = *tumble * math.Pi / 180
	p.wobRot = *wobRot * math.Pi / 180

	if err := run(&p, *hop, *tol, *liftW, *fps, *ballX, *ballZ, *radius, *camX, *out); err != nil {
		fmt.Fprintln(os.Stderr, "spheregen:", err)
		os.Exit(1)
	}
}

func run(p *params, hop, tol float64, liftW int, fps, ballX, ballZ, radius, camX float64, out string) error {
	p.rReg = p.regularRadius(10)
	s := p.verify(fps, liftW)
	scale := radius / (p.rReg + 0.5)
	lines := []string{
		fmt.Sprintf("%d cubes, edge %.4f world units, regular radius %.3f edges", 6*p.n*p.n, scale, p.rReg),
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
	case s.liftMiss > 0:
		return fmt.Errorf("a cube outside the middle %dx%d of the bottom face reaches %.4f lower, %s not written", liftW, liftW, s.liftMiss, out)
	}

	w := vec{camX - ballX, 0, -ballZ}.normalize()
	yaw := math.Atan2(-w[0], -w[2])
	return os.WriteFile(out, []byte(emit(p, s, lines, scale, hop, liftW, ballX, ballZ, yaw)), 0o644)
}
