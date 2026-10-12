package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg" // allow JPEG references
	"math"
	"os"
	"strings"
)

const montageGap = 8

func loadImage(path string) (image.Image, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}

	return img, nil
}

// montage places render on the left and ref, scaled to the render's height,
// on the right, separated by a small dark gap.
func montage(render, ref image.Image) *image.RGBA {
	bounds := render.Bounds()
	h := bounds.Dy()
	refW := int(math.Round(float64(ref.Bounds().Dx()) * float64(h) / float64(ref.Bounds().Dy())))
	scaled := resize(ref, refW, h)

	out := image.NewRGBA(image.Rect(0, 0, bounds.Dx()+montageGap+refW, h))
	draw.Draw(out, out.Bounds(), &image.Uniform{color.RGBA{16, 16, 16, 255}}, image.Point{}, draw.Src)
	draw.Draw(out, image.Rect(0, 0, bounds.Dx(), h), render, bounds.Min, draw.Src)
	draw.Draw(out, image.Rect(bounds.Dx()+montageGap, 0, out.Bounds().Dx(), h), scaled, image.Point{}, draw.Src)

	return out
}

// resize scales src to w x h. Each destination pixel averages n x n bilinear
// taps, with n chosen from the downscale factor, so it works as a bilinear
// filter when upscaling and as a box filter when shrinking.
func resize(src image.Image, w, h int) *image.RGBA {
	bounds := src.Bounds()
	scaleX := float64(bounds.Dx()) / float64(w)
	scaleY := float64(bounds.Dy()) / float64(h)
	tapsX := max(1, int(math.Ceil(scaleX)))
	tapsY := max(1, int(math.Ceil(scaleY)))

	// Convert once to a flat RGBA buffer for fast lookups.
	flat := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(flat, flat.Bounds(), src, bounds.Min, draw.Src)

	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			var acc [4]float64

			for j := range tapsY {
				for i := range tapsX {
					fx := (float64(x)+(float64(i)+0.5)/float64(tapsX))*scaleX - 0.5
					fy := (float64(y)+(float64(j)+0.5)/float64(tapsY))*scaleY - 0.5

					c := bilinear(flat, fx, fy)
					for k := range acc {
						acc[k] += c[k]
					}
				}
			}

			o := out.PixOffset(x, y)
			for k := range acc {
				out.Pix[o+k] = uint8(math.Round(acc[k] / float64(tapsX*tapsY)))
			}
		}
	}

	return out
}

func bilinear(img *image.RGBA, fx, fy float64) [4]float64 {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	x0, y0 := int(math.Floor(fx)), int(math.Floor(fy))
	fracX, fracY := fx-float64(x0), fy-float64(y0)
	clampi := func(v, hi int) int { return min(max(v, 0), hi-1) }
	px := func(x, y int) []uint8 {
		o := img.PixOffset(clampi(x, w), clampi(y, h))
		return img.Pix[o : o+4]
	}
	a, b, c, d := px(x0, y0), px(x0+1, y0), px(x0, y0+1), px(x0+1, y0+1)

	var res [4]float64
	for k := range res {
		top := float64(a[k])*(1-fracX) + float64(b[k])*fracX
		bot := float64(c[k])*(1-fracX) + float64(d[k])*fracX
		res[k] = top*(1-fracY) + bot*fracY
	}

	return res
}

// compareName turns "out/full_t10.0.png" into "out/full_t10.0_cmp.png".
func compareName(shot string) string {
	return strings.TrimSuffix(shot, ".png") + "_cmp.png"
}
