package shot

import (
	"image"
	"math"
)

// ScaleImage downscales src by scale (0 < scale ≤ 1) with a box filter
// (area averaging): every destination pixel is the weighted average of the
// source pixels its footprint covers, the weight being the overlap area.
// A box filter is the right filter for downscaling — no ringing, no
// aliasing — and it degenerates to an exact copy at scale 1. Alpha is
// forced opaque (screen captures are opaque; GDI leaves it undefined).
//
// Width and height round exactly like scaledSize, so the MappingFor of the
// same scale describes this image's dimensions.
func ScaleImage(src *image.RGBA, scale float64) *image.RGBA {
	if src == nil {
		return nil
	}
	if scale <= 0 || scale > 1 {
		scale = 1
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return image.NewRGBA(image.Rect(0, 0, 0, 0))
	}
	dw, dh := scaledSize(w, scale), scaledSize(h, scale)
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))

	for dy := 0; dy < dh; dy++ {
		fy0, fy1 := float64(dy)/scale, float64(dy+1)/scale
		syLo, syHi := int(math.Floor(fy0)), min(int(math.Ceil(fy1)), h)
		for dx := 0; dx < dw; dx++ {
			fx0, fx1 := float64(dx)/scale, float64(dx+1)/scale
			sxLo, sxHi := int(math.Floor(fx0)), min(int(math.Ceil(fx1)), w)
			var r, g, bl, weight float64
			for sy := syLo; sy < syHi; sy++ {
				wy := overlap(float64(sy), float64(sy+1), fy0, fy1)
				if wy <= 0 {
					continue
				}
				rowOff := (sy - b.Min.Y) * src.Stride
				for sx := sxLo; sx < sxHi; sx++ {
					wx := overlap(float64(sx), float64(sx+1), fx0, fx1)
					if wx <= 0 {
						continue
					}
					wt := wx * wy
					i := rowOff + (sx-b.Min.X)*4
					r += wt * float64(src.Pix[i])
					g += wt * float64(src.Pix[i+1])
					bl += wt * float64(src.Pix[i+2])
					weight += wt
				}
			}
			o := dst.PixOffset(dx, dy)
			if weight > 0 {
				dst.Pix[o] = uint8(math.Round(r / weight))
				dst.Pix[o+1] = uint8(math.Round(g / weight))
				dst.Pix[o+2] = uint8(math.Round(bl / weight))
			}
			dst.Pix[o+3] = 255
		}
	}
	return dst
}

// overlap returns the length of the intersection of the half-open
// intervals [a0, a1) and [b0, b1).
func overlap(a0, a1, b0, b1 float64) float64 {
	lo, hi := math.Max(a0, b0), math.Min(a1, b1)
	if hi <= lo {
		return 0
	}
	return hi - lo
}
