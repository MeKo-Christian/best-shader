package main

import (
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg" // allow JPEG inputs
	_ "image/png"
	"math"
	"os"
)

// picture holds an image as linear RGB and OKLab planes, row-major with the
// origin at the top-left (image convention, not shader convention).
type picture struct {
	w, h int
	lin  [3][]float64 // linear sRGB, 0..1
	lab  [3][]float64 // OKLab L, a, b
}

func newPicture(w, h int) *picture {
	p := &picture{w: w, h: h}
	for c := range 3 {
		p.lin[c] = make([]float64, w*h)
		p.lab[c] = make([]float64, w*h)
	}

	return p
}

func loadPicture(path string) (*picture, error) {
	file, err := os.Open(path) //nolint:gosec // reading user-given paths is the point of a CLI
	if err != nil {
		return nil, err
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}

	bounds := img.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(rgba, rgba.Bounds(), img, bounds.Min, draw.Src)

	var lut [256]float64
	for i := range lut {
		lut[i] = srgbDecode(float64(i) / 255)
	}

	p := newPicture(bounds.Dx(), bounds.Dy())
	for y := range p.h {
		for x := range p.w {
			o := rgba.PixOffset(x, y)
			for c := range 3 {
				p.lin[c][y*p.w+x] = lut[rgba.Pix[o+c]]
			}
		}
	}

	p.updateLab()

	return p, nil
}

// updateLab recomputes the OKLab planes from the linear RGB planes.
func (p *picture) updateLab() {
	for i := range p.lin[0] {
		lab := linearToOklab([3]float64{p.lin[0][i], p.lin[1][i], p.lin[2][i]})
		for c := range 3 {
			p.lab[c][i] = lab[c]
		}
	}
}

// resampled returns p scaled to w x h by area averaging in linear RGB.
func (p *picture) resampled(w, h int) *picture {
	out := newPicture(w, h)
	for c := range 3 {
		out.lin[c] = boxMeans(p.lin[c], p.w, p.h, w, h)
	}

	out.updateLab()

	return out
}

// gridLab returns the n x n box-filtered mean colours in OKLab. The means are
// taken in linear RGB, i.e. the colour the cell has when seen from afar.
func (p *picture) gridLab(n int) [][3]float64 {
	var means [3][]float64
	for c := range 3 {
		means[c] = boxMeans(p.lin[c], p.w, p.h, n, n)
	}

	out := make([][3]float64, n*n)
	for i := range out {
		out[i] = linearToOklab([3]float64{means[0][i], means[1][i], means[2][i]})
	}

	return out
}

// boxMeans averages a pw x ph plane over a gw x gh grid of cells. Cell
// borders are rounded to whole pixels; when upscaling every cell gets at
// least one source pixel (nearest neighbour).
func boxMeans(plane []float64, pw, ph, gw, gh int) []float64 {
	out := make([]float64, gw*gh)
	for j := range gh {
		y0, y1 := cellSpan(j, ph, gh)
		for i := range gw {
			x0, x1 := cellSpan(i, pw, gw)

			sum := 0.0

			for y := y0; y < y1; y++ {
				row := plane[y*pw : (y+1)*pw]
				for x := x0; x < x1; x++ {
					sum += row[x]
				}
			}

			out[j*gw+i] = sum / float64((x1-x0)*(y1-y0))
		}
	}

	return out
}

// cellSpan returns the pixel range [lo, hi) of cell i when size pixels are
// split into n cells.
func cellSpan(i, size, n int) (int, int) {
	lo, hi := i*size/n, (i+1)*size/n
	if hi <= lo {
		hi = min(lo+1, size)
		lo = hi - 1
	}

	return lo, hi
}

func srgbDecode(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}

	return math.Pow((c+0.055)/1.055, 2.4)
}

func srgbEncode(c float64) float64 {
	c = min(max(c, 0), 1)
	if c <= 0.0031308 {
		return c * 12.92
	}

	return 1.055*math.Pow(c, 1/2.4) - 0.055
}

// linearToOklab converts linear sRGB to OKLab (Björn Ottosson, 2020).
func linearToOklab(rgb [3]float64) [3]float64 {
	r, g, b := rgb[0], rgb[1], rgb[2]
	l := math.Cbrt(0.4122214708*r + 0.5363325363*g + 0.0514459929*b)
	m := math.Cbrt(0.2119034982*r + 0.6806995451*g + 0.1073969566*b)
	s := math.Cbrt(0.0883024619*r + 0.2817188376*g + 0.6299787005*b)

	return [3]float64{
		0.2104542553*l + 0.7936177850*m - 0.0040720468*s,
		1.9779984951*l - 2.4285922050*m + 0.4505937099*s,
		0.0259040371*l + 0.7827717662*m - 0.8086757660*s,
	}
}

func oklabToLinear(lab [3]float64) [3]float64 {
	l := lab[0] + 0.3963377774*lab[1] + 0.2158037573*lab[2]
	m := lab[0] - 0.1055613458*lab[1] - 0.0638541728*lab[2]
	s := lab[0] - 0.0894841775*lab[1] - 1.2914855480*lab[2]
	l, m, s = l*l*l, m*m*m, s*s*s

	return [3]float64{
		+4.0767416621*l - 3.3077115913*m + 0.2309699292*s,
		-1.2684380046*l + 2.6097574011*m - 0.3413193965*s,
		-0.0041960863*l - 0.7034186147*m + 1.7076147010*s,
	}
}

// srgb8 converts an OKLab colour to clamped 8-bit sRGB.
func srgb8(lab [3]float64) [3]uint8 {
	lin := oklabToLinear(lab)

	var out [3]uint8
	for c := range 3 {
		out[c] = uint8(math.Round(srgbEncode(lin[c]) * 255))
	}

	return out
}

// hex formats an OKLab colour as an sRGB hex string.
func hex(lab [3]float64) string {
	c := srgb8(lab)

	return fmt.Sprintf("#%02x%02x%02x", c[0], c[1], c[2])
}

// deltaE is the Euclidean OKLab distance (about 0.02 is just noticeable).
func deltaE(a, b [3]float64) float64 {
	dl, da, db := a[0]-b[0], a[1]-b[1], a[2]-b[2]

	return math.Sqrt(dl*dl + da*da + db*db)
}
