// Package ui draws the overlay into a pixel buffer. It is platform neutral:
// the Windows overlay supplies GDI text rendering, the preview tool supplies
// a font rasteriser, and both share the shapes/icons implemented here.
package ui

import (
	"math"

	"aethermeter/internal/glyph"
)

// Raster is a 32-bit pixel buffer (0xAARRGGBB words, i.e. BGRA bytes — the
// layout of a Windows top-down DIB section).
type Raster struct {
	Pix  []uint32
	W, H int
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// blend composites colour c with opacity a over pixel i ("source over").
// Pixels are straight (non-premultiplied) ARGB; a fully transparent pixel is 0.
func (r *Raster) blend(i int, c uint32, a float64) {
	if a <= 0 {
		return
	}
	if a > 1 {
		a = 1
	}
	d := r.Pix[i]
	da := float64(d>>24) / 255
	oa := a + da*(1-a)
	if oa <= 0 {
		return
	}
	ch := func(s uint) uint32 {
		sv := float64((c >> s) & 0xFF)
		dv := float64((d >> s) & 0xFF)
		return uint32((sv*a+dv*da*(1-a))/oa+0.5) & 0xFF
	}
	r.Pix[i] = uint32(oa*255+0.5)<<24 | ch(16)<<16 | ch(8)<<8 | ch(0)
}

// Blend is the exported per-pixel composite (used by text renderers).
func (r *Raster) Blend(x, y int, c uint32, a float64) {
	if x < 0 || y < 0 || x >= r.W || y >= r.H {
		return
	}
	r.blend(y*r.W+x, c, a)
}

// Clear makes the whole buffer fully transparent.
func (r *Raster) Clear() {
	for i := range r.Pix {
		r.Pix[i] = 0
	}
}

// Fill paints an axis-aligned rectangle (integer pixels) with opacity a.
func (r *Raster) Fill(x0, y0, x1, y1 int, c uint32, a float64) {
	x0, y0 = max(x0, 0), max(y0, 0)
	x1, y1 = min(x1, r.W), min(y1, r.H)
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			r.blend(y*r.W+x, c, a)
		}
	}
}

// Premultiplied returns the buffer as premultiplied BGRA words, the format
// UpdateLayeredWindow expects.
func (r *Raster) Premultiplied(dst []uint32) {
	for i, p := range r.Pix {
		a := p >> 24
		if a == 0 {
			dst[i] = 0
			continue
		}
		m := func(s uint) uint32 { return ((p>>s)&0xFF*a + 127) / 255 }
		dst[i] = a<<24 | m(16)<<16 | m(8)<<8 | m(0)
	}
}

// RoundRect paints an anti-aliased rounded rectangle with opacity a.
func (r *Raster) RoundRect(x0, y0, x1, y1, rad float64, c uint32, a float64) {
	if x1 <= x0 || y1 <= y0 {
		return
	}
	rad = math.Min(rad, math.Min((x1-x0)/2, (y1-y0)/2))
	ix0, iy0 := int(math.Floor(x0)), int(math.Floor(y0))
	ix1, iy1 := int(math.Ceil(x1)), int(math.Ceil(y1))
	ix0, iy0 = max(ix0, 0), max(iy0, 0)
	ix1, iy1 = min(ix1, r.W), min(iy1, r.H)
	for py := iy0; py < iy1; py++ {
		cy := float64(py) + 0.5
		covY := clamp01(math.Min(cy-y0+0.5, y1-cy+0.5))
		for px := ix0; px < ix1; px++ {
			cx := float64(px) + 0.5
			cov := covY * clamp01(math.Min(cx-x0+0.5, x1-cx+0.5))
			// corners
			var ccx, ccy float64
			inCorner := false
			switch {
			case cx < x0+rad && cy < y0+rad:
				ccx, ccy, inCorner = x0+rad, y0+rad, true
			case cx > x1-rad && cy < y0+rad:
				ccx, ccy, inCorner = x1-rad, y0+rad, true
			case cx < x0+rad && cy > y1-rad:
				ccx, ccy, inCorner = x0+rad, y1-rad, true
			case cx > x1-rad && cy > y1-rad:
				ccx, ccy, inCorner = x1-rad, y1-rad, true
			}
			if inCorner {
				d := math.Hypot(cx-ccx, cy-ccy)
				cov = math.Min(cov, clamp01(rad-d+0.5))
			}
			if cov > 0 {
				r.blend(py*r.W+px, c, a*cov)
			}
		}
	}
}

// Circle paints an anti-aliased disc.
func (r *Raster) Circle(cx, cy, rad float64, c uint32, a float64) {
	r.RoundRect(cx-rad, cy-rad, cx+rad, cy+rad, rad, c, a)
}

// Glyph paints a named icon mask tinted with colour c.
func (r *Raster) Glyph(name string, x, y, size int, c uint32, a float64) {
	m := glyph.Mask(name, size)
	if m == nil {
		return
	}
	for gy := 0; gy < size; gy++ {
		py := y + gy
		if py < 0 || py >= r.H {
			continue
		}
		for gx := 0; gx < size; gx++ {
			px := x + gx
			if px < 0 || px >= r.W {
				continue
			}
			if v := m[gy*size+gx]; v != 0 {
				r.blend(py*r.W+px, c, a*float64(v)/255)
			}
		}
	}
}

// Mix blends colour a towards b by t (0..1).
func Mix(a, b uint32, t float64) uint32 {
	ch := func(s uint) uint32 {
		x := float64((a>>s)&0xFF)*(1-t) + float64((b>>s)&0xFF)*t
		return uint32(x+0.5) & 0xFF
	}
	return ch(16)<<16 | ch(8)<<8 | ch(0)
}
