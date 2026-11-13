// Dark-basemap rendering for the tile proxy (S14-T3 / Issue #443).
//
// CARTO Dark Matter — the basemap the issue names — now answers keyless requests
// with an "API KEY REQUIRED" placeholder on every style, so it cannot be the
// default. The proxy therefore renders its own dark cartography from the
// OpenStreetMap raster it already caches: invert luminance, rotate the hue back
// by 180° so water stays blue and parks stay green, then lower brightness,
// raise contrast and pull saturation down. The result is a genuinely dark,
// high-contrast basemap that costs nothing at request time (the darkened PNG is
// what gets cached) and needs no third-party key.
//
// A licensed native dark provider (CARTO with MAP_TILE_API_KEY) skips this path.
package tilecache

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
)

// darkenLumaThreshold is the mean channel value below which a tile is already
// dark. Used only by tests and operators to reason about the transform.
const darkenLumaThreshold = 96

// photometric constants, matched to the CSS recipe
// `invert(1) hue-rotate(180deg) brightness(0.92) contrast(1.18) saturate(0.35)`.
const (
	darkenBrightness = 0.92
	darkenContrast   = 1.18
	darkenSaturation = 0.35
	// Pure-white OSM land inverts to pure black, which reads as a void on a phone
	// in daylight. A small lift gives the dark surface the same #1a1a1a-ish floor
	// a designed dark basemap uses, without touching road and label contrast.
	darkenLift = 0.10
)

// darkenPNG re-renders a light raster tile as high-contrast dark cartography.
//
// Decoding failures are returned to the caller, which serves the original bytes
// rather than failing the map: a lighter tile is a degraded view, a 502 is a
// blank map.
func darkenPNG(data []byte) ([]byte, error) {
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode tile png: %w", err)
	}

	bounds := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r16, g16, b16, a16 := src.At(x, y).RGBA()
			// RGBA() returns alpha-premultiplied 16-bit values. Tiles are opaque,
			// but handle the general case so a transparent edge does not turn black.
			if a16 == 0 {
				continue
			}
			alpha := float64(a16) / 65535.0
			r := float64(r16) / 65535.0 / alpha
			g := float64(g16) / 65535.0 / alpha
			b := float64(b16) / 65535.0 / alpha

			r, g, b = invertHueRotate(r, g, b)
			r, g, b = brightenContrastSaturate(r, g, b)

			dst.SetNRGBA(
				x-bounds.Min.X,
				y-bounds.Min.Y,
				color.NRGBA{
					R: channel(r),
					G: channel(g),
					B: channel(b),
					A: channel(alpha),
				},
			)
		}
	}

	var out bytes.Buffer
	if err := png.Encode(&out, dst); err != nil {
		return nil, fmt.Errorf("encode darkened tile: %w", err)
	}
	return out.Bytes(), nil
}

// invertHueRotate mirrors CSS `invert(1) hue-rotate(180deg)`, which preserves the
// semantic hue of roads, parks and water while flipping luminance.
func invertHueRotate(r, g, b float64) (float64, float64, float64) {
	h, s, l := rgbToHSL(1-r, 1-g, 1-b)
	h += 0.5
	if h >= 1 {
		h -= 1
	}
	return hslToRGB(h, s, l)
}

// brightenContrastSaturate mirrors `brightness() contrast() saturate()` in the
// same order CSS applies them.
func brightenContrastSaturate(r, g, b float64) (float64, float64, float64) {
	r, g, b = r*darkenBrightness, g*darkenBrightness, b*darkenBrightness
	r, g, b = contrast(r)+darkenLift, contrast(g)+darkenLift, contrast(b)+darkenLift

	l := 0.2126*r + 0.7152*g + 0.0722*b
	r = l + (r-l)*darkenSaturation
	g = l + (g-l)*darkenSaturation
	b = l + (b-l)*darkenSaturation
	return r, g, b
}

func contrast(c float64) float64 {
	return (c-0.5)*darkenContrast + 0.5
}

func channel(c float64) uint8 {
	switch {
	case c <= 0:
		return 0
	case c >= 1:
		return 255
	default:
		return uint8(c*255 + 0.5)
	}
}

// rgbToHSL converts sRGB components in [0,1] to hue, saturation and lightness.
func rgbToHSL(r, g, b float64) (float64, float64, float64) {
	maxC := max3(r, g, b)
	minC := min3(r, g, b)
	l := (maxC + minC) / 2
	if maxC == minC {
		return 0, 0, l
	}

	delta := maxC - minC
	s := delta / (1 - abs(2*l-1))

	var h float64
	switch maxC {
	case r:
		h = (g - b) / delta
		if g < b {
			h += 6
		}
	case g:
		h = (b-r)/delta + 2
	default:
		h = (r-g)/delta + 4
	}
	return h / 6, s, l
}

// hslToRGB converts back to sRGB components in [0,1].
func hslToRGB(h, s, l float64) (float64, float64, float64) {
	c := (1 - abs(2*l-1)) * s
	hp := h * 6
	x := c * (1 - abs(mod2(hp)-1))
	var r, g, b float64
	switch {
	case hp < 1:
		r, g, b = c, x, 0
	case hp < 2:
		r, g, b = x, c, 0
	case hp < 3:
		r, g, b = 0, c, x
	case hp < 4:
		r, g, b = 0, x, c
	case hp < 5:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	m := l - c/2
	return r + m, g + m, b + m
}

func mod2(v float64) float64 {
	for v >= 2 {
		v -= 2
	}
	return v
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func max3(a, b, c float64) float64 {
	m := a
	if b > m {
		m = b
	}
	if c > m {
		m = c
	}
	return m
}

func min3(a, b, c float64) float64 {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
