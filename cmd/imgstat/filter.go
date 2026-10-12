package main

import "math"

// gaussianBlur returns a w x h plane blurred with a separable Gaussian of the
// given sigma in pixels. Borders are clamped.
func gaussianBlur(src []float64, w, h int, sigma float64) []float64 {
	r := int(math.Ceil(3 * sigma))
	kernel := make([]float64, 2*r+1)
	sum := 0.0

	for i := range kernel {
		d := float64(i - r)
		kernel[i] = math.Exp(-d * d / (2 * sigma * sigma))
		sum += kernel[i]
	}

	for i := range kernel {
		kernel[i] /= sum
	}

	tmp := make([]float64, w*h)
	for y := range h {
		row := src[y*w : (y+1)*w]
		for x := range w {
			acc := 0.0
			for i, k := range kernel {
				acc += k * row[clampInt(x+i-r, w)]
			}

			tmp[y*w+x] = acc
		}
	}

	out := make([]float64, w*h)
	for y := range h {
		for x := range w {
			acc := 0.0
			for i, k := range kernel {
				acc += k * tmp[clampInt(y+i-r, h)*w+x]
			}

			out[y*w+x] = acc
		}
	}

	return out
}

func clampInt(v, size int) int {
	return min(max(v, 0), size-1)
}

// sobel returns the x and y gradients of a plane. gy points UP (shader
// orientation), so angles derived from it are counter-clockwise in uv.
func sobel(src []float64, w, h int) ([]float64, []float64) {
	gx := make([]float64, w*h)
	gy := make([]float64, w*h)
	at := func(x, y int) float64 { return src[clampInt(y, h)*w+clampInt(x, w)] }

	for y := range h {
		for x := range w {
			gx[y*w+x] = (at(x+1, y-1) + 2*at(x+1, y) + at(x+1, y+1) -
				at(x-1, y-1) - 2*at(x-1, y) - at(x-1, y+1)) / 8
			gy[y*w+x] = (at(x-1, y-1) + 2*at(x, y-1) + at(x+1, y-1) -
				at(x-1, y+1) - 2*at(x, y+1) - at(x+1, y+1)) / 8
		}
	}

	return gx, gy
}

// tensor is a per-pixel structure tensor [xx xy; xy yy].
type tensor struct {
	xx, xy, yy []float64
}

// structureTensor computes the Gaussian-smoothed structure tensor of a plane.
func structureTensor(src []float64, w, h int, sigma float64) tensor {
	gx, gy := sobel(src, w, h)
	xx := make([]float64, w*h)
	xy := make([]float64, w*h)
	yy := make([]float64, w*h)

	for i := range gx {
		xx[i] = gx[i] * gx[i]
		xy[i] = gx[i] * gy[i]
		yy[i] = gy[i] * gy[i]
	}

	return tensor{
		xx: gaussianBlur(xx, w, h, sigma),
		xy: gaussianBlur(xy, w, h, sigma),
		yy: gaussianBlur(yy, w, h, sigma),
	}
}

// orientation turns tensor components into the stroke orientation (along the
// stroke, perpendicular to the dominant gradient) in degrees [0, 180), the
// coherence (l1-l2)/(l1+l2) in 0..1 and the energy l1+l2. Since summing
// tensors equals a coherence*energy weighted average of doubled angles, cell
// orientations are obtained by summing the tensor over the cell first.
func orientation(sxx, sxy, syy float64) (float64, float64, float64) {
	energy := sxx + syy
	if energy <= 1e-12 {
		return 0, 0, 0
	}

	grad := 0.5 * math.Atan2(2*sxy, sxx-syy)
	deg := math.Mod(grad*180/math.Pi+90+180, 180)
	coh := math.Hypot(sxx-syy, 2*sxy) / energy

	return deg, coh, energy
}

// angleDiff is the unsigned difference of two orientations in degrees, 0..90.
func angleDiff(a, b float64) float64 {
	d := math.Mod(math.Abs(a-b), 180)

	return min(d, 180-d)
}

// transpose swaps the axes of a w x h plane; the result is h x w.
func transpose(src []float64, w, h int) []float64 {
	out := make([]float64, w*h)
	for y := range h {
		for x := range w {
			out[x*h+y] = src[y*w+x]
		}
	}

	return out
}
