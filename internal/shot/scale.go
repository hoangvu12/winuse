package shot

import (
	"image"
	"math"
	"runtime"
	"sync"
)

// ScaleImage downscales src by scale (0 < scale ≤ 1) with a box filter
// (area averaging): every destination pixel is the weighted average of the
// source pixels its footprint covers, the weight being the overlap area.
// A box filter is the right filter for downscaling — no ringing, no
// aliasing — and it degenerates to an exact copy at scale 1. Alpha is
// forced opaque (screen captures are opaque; GDI leaves it undefined).
//
// The filter is separable, so it runs as a horizontal then a vertical
// pass, with the overlap weights precomputed per column and per row in
// 16.16 fixed point and each pass split across the CPUs (speed 08:
// the float64 filter took 130-195 ms a frame). A channel lands within 1
// of the exact average.
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
	cols, rows := boxTaps(w, dw, scale), boxTaps(h, dh, scale)

	// Horizontal pass: every source row to dw RGB sums, kept as 16.8
	// fixed point (a 16.16 sum shifted right by 8).
	mid := make([]uint32, dw*3*h)
	inParallel(h, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			row := src.Pix[src.PixOffset(b.Min.X, b.Min.Y+y):]
			o := y * dw * 3
			for dx, t := range cols {
				var r, g, bl uint32
				i := t.lo * 4
				for _, wt := range t.weights {
					r += uint32(row[i]) * wt
					g += uint32(row[i+1]) * wt
					bl += uint32(row[i+2]) * wt
					i += 4
				}
				mid[o+dx*3], mid[o+dx*3+1], mid[o+dx*3+2] = r>>8, g>>8, bl>>8
			}
		}
	})

	// Vertical pass: the row sums to destination pixels, rounded.
	inParallel(dh, func(y0, y1 int) {
		for dy := y0; dy < y1; dy++ {
			t := rows[dy]
			o := dy * dst.Stride
			for dx := 0; dx < dw; dx++ {
				var r, g, bl uint64
				i := t.lo*dw*3 + dx*3
				for _, wt := range t.weights {
					r += uint64(mid[i]) * uint64(wt)
					g += uint64(mid[i+1]) * uint64(wt)
					bl += uint64(mid[i+2]) * uint64(wt)
					i += dw * 3
				}
				dst.Pix[o+dx*4] = channel(r)
				dst.Pix[o+dx*4+1] = channel(g)
				dst.Pix[o+dx*4+2] = channel(bl)
				dst.Pix[o+dx*4+3] = 255
			}
		}
	})
	return dst
}

// tap is the source span one destination column (or row) averages: the
// first source index and each source pixel's 16.16 overlap weight, the
// weights summing to about 1<<16.
type tap struct {
	lo      int
	weights []uint32
}

// boxTaps precomputes the box filter's taps for n source pixels scaled
// to dn: destination pixel d covers source [d/scale, (d+1)/scale).
func boxTaps(n, dn int, scale float64) []tap {
	taps := make([]tap, dn)
	for d := range taps {
		f0, f1 := float64(d)/scale, float64(d+1)/scale
		lo, hi := int(math.Floor(f0)), min(int(math.Ceil(f1)), n)
		ws := make([]float64, 0, hi-lo)
		sum := 0.0
		for s := lo; s < hi; s++ {
			wt := overlap(float64(s), float64(s+1), f0, f1)
			ws = append(ws, wt)
			sum += wt
		}
		t := tap{lo: lo, weights: make([]uint32, len(ws))}
		for i, wt := range ws {
			t.weights[i] = uint32(math.Round(wt / sum * (1 << 16)))
		}
		taps[d] = t
	}
	return taps
}

// channel rounds a two-pass sum (16.8 times 16.16: 8.24 fixed point) to a
// channel value.
func channel(v uint64) uint8 {
	return uint8(min((v+1<<23)>>24, 255))
}

// inParallel runs work over [0, n) in one contiguous chunk per CPU.
func inParallel(n int, work func(lo, hi int)) {
	parts := min(runtime.GOMAXPROCS(0), n)
	if parts <= 1 {
		work(0, n)
		return
	}
	var wg sync.WaitGroup
	for p := range parts {
		lo, hi := n*p/parts, n*(p+1)/parts
		wg.Add(1)
		go func() {
			defer wg.Done()
			work(lo, hi)
		}()
	}
	wg.Wait()
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
