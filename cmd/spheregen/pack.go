package main

import "math"

// Packing of one cube into a uvec4 (shaders/sphere.glsl decodes it):
//
//	x: target direction d1, octahedral, 2 x snorm16
//	y: tumble axis, octahedral, 2 x snorm16
//	z: tumble angle / pi, target radius r1 / rMax, 2 x unorm16
//	w: shuffle radius rs / rMax, packed radius rEnd / rMax, 2 x unorm16
//
// Only integer operations decode it, so it works in GLSL 4.10 and WebGL2.
const rMax = 32.0

func snorm16(v float64) uint32 {
	return uint32(uint16(int16(math.Round(math.Max(-1, math.Min(1, v)) * 32767))))
}

func unorm16(v float64) uint32 {
	return uint32(math.Round(math.Max(0, math.Min(1, v)) * 65535))
}

func unSnorm(x uint32) float64 { return math.Max(float64(int16(uint16(x)))/32767, -1) }
func unUnorm(x uint32) float64 { return float64(x&0xffff) / 65535 }

func octEncode(v vec) (float64, float64) {
	p := v.mul(1 / (math.Abs(v[0]) + math.Abs(v[1]) + math.Abs(v[2])))
	if p[2] < 0 {
		x, y := p[0], p[1]
		p[0] = (1 - math.Abs(y)) * signNotZero(x)
		p[1] = (1 - math.Abs(x)) * signNotZero(y)
	}
	return p[0], p[1]
}

func octDecode(x, y float64) vec {
	v := vec{x, y, 1 - math.Abs(x) - math.Abs(y)}
	if v[2] < 0 {
		v[0] = (1 - math.Abs(y)) * signNotZero(x)
		v[1] = (1 - math.Abs(x)) * signNotZero(y)
	}
	return v.normalize()
}

func signNotZero(x float64) float64 {
	if x >= 0 {
		return 1
	}
	return -1
}

func packDir(v vec) uint32 {
	x, y := octEncode(v)
	return snorm16(x) | snorm16(y)<<16
}

func unpackDir(p uint32) vec { return octDecode(unSnorm(p), unSnorm(p>>16)) }

func (c *cubes) encode(i int) [4]uint32 {
	return [4]uint32{
		packDir(c.d1[i]),
		packDir(c.axis[i]),
		unorm16(c.ang[i]/math.Pi) | unorm16(c.r1[i]/rMax)<<16,
		unorm16(c.rs[i]/rMax) | unorm16(c.rEnd[i]/rMax)<<16,
	}
}

// unpack replaces the plan's targets by what the shader will decode, so the
// lift and the verification see exactly the shader's data. The target axes
// m1 follow from the decoded tumble.
func (c *cubes) unpack(data [][4]uint32) {
	for i, p := range data {
		c.d1[i] = unpackDir(p[0])
		c.axis[i] = unpackDir(p[1])
		c.ang[i] = unUnorm(p[2]) * math.Pi
		c.r1[i] = unUnorm(p[2]>>16) * rMax
		c.rs[i] = unUnorm(p[3]) * rMax
		c.rEnd[i] = unUnorm(p[3]>>16) * rMax
		c.m1[i] = axisAngle(c.axis[i], c.ang[i]).mul(c.m0[i])
	}
}
