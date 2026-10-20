// Extracts the logo + text from the source image onto a transparent background
// using colour-to-alpha against a locally estimated background colour.
//
// Usage: go run . <input.png> <output.png>
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

const (
	cell        = 24   // background estimation grid size (px)
	bgMaxChan   = 45   // pixels whose brightest channel is below this count as background
	alphaCutLo  = 0.10 // alpha below this becomes fully transparent (removes noise/haze)
	alphaCutHi  = 0.22 // alpha above this is kept unchanged; linear ramp in between
	cropPadding = 6
)

type rgb struct{ r, g, b float64 }

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: extract <input.png> <output.png>")
		os.Exit(2)
	}
	f, err := os.Open(os.Args[1])
	check(err)
	src, _, err := image.Decode(f)
	check(err)
	f.Close()

	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	px := make([]rgb, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, bb, _ := src.At(b.Min.X+x, b.Min.Y+y).RGBA()
			px[y*w+x] = rgb{float64(r >> 8), float64(g >> 8), float64(bb >> 8)}
		}
	}

	bg := estimateBackground(px, w, h)

	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	minX, minY, maxX, maxY := w, h, -1, -1
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			p := px[y*w+x]
			k := bilinear(bg, w, h, x, y)
			a := math.Max(chanAlpha(p.r, k.r), math.Max(chanAlpha(p.g, k.g), chanAlpha(p.b, k.b)))
			if a <= 0 {
				continue
			}
			fr := clamp((p.r-k.r)/a + k.r)
			fg := clamp((p.g-k.g)/a + k.g)
			fb := clamp((p.b-k.b)/a + k.b)

			switch {
			case a < alphaCutLo:
				a = 0
			case a < alphaCutHi:
				a = a * (a - alphaCutLo) / (alphaCutHi - alphaCutLo)
			}
			if a <= 0 {
				continue
			}
			out.SetNRGBA(x, y, color.NRGBA{uint8(fr + .5), uint8(fg + .5), uint8(fb + .5), uint8(a*255 + .5)})
			if a*255 >= 8 {
				minX, minY = min(minX, x), min(minY, y)
				maxX, maxY = max(maxX, x), max(maxY, y)
			}
		}
	}

	crop := image.Rect(max(0, minX-cropPadding), max(0, minY-cropPadding),
		min(w, maxX+1+cropPadding), min(h, maxY+1+cropPadding))
	res := image.NewNRGBA(image.Rect(0, 0, crop.Dx(), crop.Dy()))
	for y := 0; y < crop.Dy(); y++ {
		for x := 0; x < crop.Dx(); x++ {
			res.SetNRGBA(x, y, out.NRGBAAt(crop.Min.X+x, crop.Min.Y+y))
		}
	}

	o, err := os.Create(os.Args[2])
	check(err)
	check(png.Encode(o, res))
	check(o.Close())
	fmt.Printf("wrote %s (%dx%d, crop %v)\n", os.Args[2], crop.Dx(), crop.Dy(), crop)
}

// chanAlpha is the GIMP colour-to-alpha formula for one channel, restricted to
// pixels brighter than the background: the background channels are so close to
// zero that the darker-than-background branch only amplifies sensor noise.
func chanAlpha(p, k float64) float64 {
	if p > k {
		return (p - k) / (255 - k)
	}
	return 0
}

// estimateBackground averages dark pixels per grid cell and fills cells with
// no background samples (covered by the logo) from their neighbours.
func estimateBackground(px []rgb, w, h int) [][]rgb {
	gw, gh := (w+cell-1)/cell, (h+cell-1)/cell
	grid := make([][]rgb, gh)
	known := make([][]bool, gh)
	for gy := range grid {
		grid[gy] = make([]rgb, gw)
		known[gy] = make([]bool, gw)
		for gx := range grid[gy] {
			var s rgb
			n := 0
			for y := gy * cell; y < min(h, (gy+1)*cell); y++ {
				for x := gx * cell; x < min(w, (gx+1)*cell); x++ {
					p := px[y*w+x]
					if math.Max(p.r, math.Max(p.g, p.b)) < bgMaxChan {
						s.r, s.g, s.b = s.r+p.r, s.g+p.g, s.b+p.b
						n++
					}
				}
			}
			if n >= cell*cell/8 {
				grid[gy][gx] = rgb{s.r / float64(n), s.g / float64(n), s.b / float64(n)}
				known[gy][gx] = true
			}
		}
	}
	for changed := true; changed; {
		changed = false
		next := make([][]bool, gh)
		for gy := range grid {
			next[gy] = append([]bool(nil), known[gy]...)
			for gx := range grid[gy] {
				if known[gy][gx] {
					continue
				}
				var s rgb
				n := 0
				for dy := -1; dy <= 1; dy++ {
					for dx := -1; dx <= 1; dx++ {
						yy, xx := gy+dy, gx+dx
						if yy >= 0 && yy < gh && xx >= 0 && xx < gw && known[yy][xx] {
							c := grid[yy][xx]
							s.r, s.g, s.b = s.r+c.r, s.g+c.g, s.b+c.b
							n++
						}
					}
				}
				if n > 0 {
					grid[gy][gx] = rgb{s.r / float64(n), s.g / float64(n), s.b / float64(n)}
					next[gy][gx] = true
					changed = true
				}
			}
		}
		known = next
	}
	return grid
}

func bilinear(grid [][]rgb, w, h, x, y int) rgb {
	gh, gw := len(grid), len(grid[0])
	fx := (float64(x)+.5)/cell - .5
	fy := (float64(y)+.5)/cell - .5
	x0, y0 := int(math.Floor(fx)), int(math.Floor(fy))
	tx, ty := fx-float64(x0), fy-float64(y0)
	at := func(gx, gy int) rgb {
		gx = max(0, min(gw-1, gx))
		gy = max(0, min(gh-1, gy))
		return grid[gy][gx]
	}
	lerp := func(a, b rgb, t float64) rgb {
		return rgb{a.r + (b.r-a.r)*t, a.g + (b.g-a.g)*t, a.b + (b.b-a.b)*t}
	}
	return lerp(lerp(at(x0, y0), at(x0+1, y0), tx), lerp(at(x0, y0+1), at(x0+1, y0+1), tx), ty)
}

func clamp(v float64) float64 { return math.Max(0, math.Min(255, v)) }

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
