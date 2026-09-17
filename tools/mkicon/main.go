// mkicon draws the application icon (an open book with an "L") and writes it
// as PNG files of several sizes into the winres directory.
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

func main() {
	out := "winres"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		panic(err)
	}
	for _, size := range []int{16, 24, 32, 48, 64, 128, 256} {
		img := draw(size)
		f, err := os.Create(filepath.Join(out, fmt.Sprintf("icon_%d.png", size)))
		if err != nil {
			panic(err)
		}
		if err := png.Encode(f, img); err != nil {
			panic(err)
		}
		f.Close()
	}
}

func draw(n int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	s := float64(n)
	blue := color.NRGBA{0x2b, 0x5f, 0xc2, 0xff}
	dark := color.NRGBA{0x1c, 0x3d, 0x7a, 0xff}
	page := color.NRGBA{0xff, 0xff, 0xff, 0xff}
	line := color.NRGBA{0xb0, 0xc4, 0xe8, 0xff}
	red := color.NRGBA{0xd0, 0x3a, 0x2f, 0xff}

	// rounded blue square background
	r := s * 0.18
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			fx, fy := float64(x)+0.5, float64(y)+0.5
			if inRoundRect(fx, fy, 0.5, 0.5, s-0.5, s-0.5, r) {
				img.SetNRGBA(x, y, blue)
			}
		}
	}
	// open book: two pages
	m := s * 0.14
	top, bottom := s*0.28, s*0.80
	mid := s / 2
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			fx, fy := float64(x)+0.5, float64(y)+0.5
			if fy < top || fy > bottom {
				continue
			}
			// left page
			if fx >= m && fx < mid-s*0.02 {
				img.SetNRGBA(x, y, page)
				if isLine(fy, top, s) && fx > m+s*0.08 && fx < mid-s*0.1 {
					img.SetNRGBA(x, y, line)
				}
			}
			// right page
			if fx > mid+s*0.02 && fx <= s-m {
				img.SetNRGBA(x, y, page)
				if isLine(fy, top, s) && fx > mid+s*0.1 && fx < s-m-s*0.08 {
					img.SetNRGBA(x, y, line)
				}
			}
			// spine
			if math.Abs(fx-mid) <= s*0.02 {
				img.SetNRGBA(x, y, dark)
			}
		}
	}
	// red bookmark on the right page
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			fx, fy := float64(x)+0.5, float64(y)+0.5
			if fx > s*0.70 && fx < s*0.78 && fy > top-s*0.04 && fy < s*0.55 {
				img.SetNRGBA(x, y, red)
			}
		}
	}
	// "L" on the left page
	lw := s * 0.07
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			fx, fy := float64(x)+0.5, float64(y)+0.5
			vertical := fx > s*0.24 && fx < s*0.24+lw && fy > s*0.36 && fy < s*0.70
			horizontal := fx > s*0.24 && fx < s*0.42 && fy > s*0.70-lw && fy < s*0.70
			if vertical || horizontal {
				img.SetNRGBA(x, y, dark)
			}
		}
	}
	return img
}

func isLine(fy, top, s float64) bool {
	rel := fy - top - s*0.12
	if rel < 0 {
		return false
	}
	step := s * 0.1
	k := math.Mod(rel, step)
	return k < s*0.03 && fy < top+s*0.46
}

func inRoundRect(x, y, x0, y0, x1, y1, r float64) bool {
	if x < x0 || x > x1 || y < y0 || y > y1 {
		return false
	}
	cx := math.Max(x0+r, math.Min(x, x1-r))
	cy := math.Max(y0+r, math.Min(y, y1-r))
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= r*r
}
