// Command appicon renders the OTONEIGO application icon: the OTONEI brand mark
// (the circle ring plus note stem used by frontend/public/logo.svg) on a light
// rounded tile, written as a square PNG for `wails3 generate icons`.
//
// Icon artwork is an optical variant, not a copy of the web logo: the stroke is
// heavier than logo.svg's 3 units so the ring still reads after downscaling to
// 16px, and the mark is centered on its own bounding box. Keep -out in Git so a
// deleted reference/ checkout can still rebuild the icon.
//
// Example:
//
//	go run ./cmd/appicon -out packaging/windows/appicon.png -previews output/icon-preview
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// geometry mirrors frontend/public/logo.svg in its 0..64 viewBox.
const (
	ringCX = 32.0
	ringCY = 32.0
	ringR  = 20.0
	stemX  = 37.0
	stemY0 = 14.5
	stemY1 = 49.5
)

// Artwork parameters are fixed, not flags: the committed appicon.png must always
// be reproducible from this file. The stroke is heavier than logo.svg's 3 units
// because a 3-unit ring disappears below about 24px once the shell downscales.
const (
	strokeUnits = 4.6
	markFill    = 0.68 // share of the canvas the mark's extent covers
	tileFrac    = 0.96 // share of the canvas the rounded tile covers
	tileRadius  = 0.22 // corner radius as a share of the tile width
	tileRed     = 0xF7 / 255.0
	tileGreen   = 0xF7 / 255.0 // #f7f7f5, the app background
	tileBlue    = 0xF5 / 255.0
	markRed     = 0.0
	markGreen   = 0.0 // #000000, the logo.svg stroke colour
	markBlue    = 0.0
)

// coverage turns a signed distance to a stroke edge into 0..1 alpha with a
// one-pixel linear ramp, which is what the antialiased ring/stem edges need.
func coverage(distance, aa float64) float64 {
	a := (aa/2 - distance) / aa
	if a <= 0 {
		return 0
	}
	if a >= 1 {
		return 1
	}
	return a
}

// markDistance returns the distance in viewBox units from (x, y) to the outside
// of the stroked ring and stem; negative values are inside the strokes. Both
// shapes use round caps, matching stroke-linecap="round".
func markDistance(x, y float64) float64 {
	half := strokeUnits / 2
	ring := math.Abs(math.Hypot(x-ringCX, y-ringCY)-ringR) - half
	// Closest point on the stem segment; clamping y reproduces its round caps.
	py := math.Max(stemY0, math.Min(stemY1, y))
	stem := math.Hypot(x-stemX, py-y) - half
	return math.Min(ring, stem)
}

// tileDistance returns the signed pixel distance from (dx, dy) relative to the
// tile centre to the outside of a rounded rectangle.
func tileDistance(dx, dy, half, radius float64) float64 {
	qx := math.Abs(dx) - half + radius
	qy := math.Abs(dy) - half + radius
	outer := math.Hypot(math.Max(qx, 0), math.Max(qy, 0))
	inner := math.Min(math.Max(qx, qy), 0)
	return outer + inner - radius
}

// render draws the master icon at the requested edge length.
func render(size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	centre := float64(size) / 2
	// Pixels per viewBox unit, chosen so the mark's full extent covers markFill of
	// the canvas instead of touching the tile edges.
	k := float64(size) * markFill / (2 * (ringR + strokeUnits/2))
	aaUnits := 1 / k
	halfTile := float64(size) * tileFrac / 2
	radius := halfTile * tileRadius
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			ux := (float64(px)+0.5-centre)/k + ringCX
			uy := (float64(py)+0.5-centre)/k + ringCY
			ink := coverage(markDistance(ux, uy), aaUnits)
			alpha := coverage(tileDistance(float64(px)+0.5-centre, float64(py)+0.5-centre, halfTile, radius), 1)
			r := tileRed*(1-ink) + markRed*ink
			g := tileGreen*(1-ink) + markGreen*ink
			b := tileBlue*(1-ink) + markBlue*ink
			img.SetNRGBA(px, py, color.NRGBA{
				R: uint8(math.Round(r * 255)),
				G: uint8(math.Round(g * 255)),
				B: uint8(math.Round(b * 255)),
				A: uint8(math.Round(alpha * 255)),
			})
		}
	}
	return img
}

// downscale averages source pixels with premultiplied alpha so thin strokes keep
// their density instead of collapsing onto one sample point.
func downscale(src *image.NRGBA, size int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	bounds := src.Bounds()
	factor := float64(bounds.Dx()) / float64(size)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			x0 := int(math.Floor(float64(x) * factor))
			y0 := int(math.Floor(float64(y) * factor))
			x1 := int(math.Ceil(math.Min(float64(x+1)*factor, float64(bounds.Dx()))))
			y1 := int(math.Ceil(math.Min(float64(y+1)*factor, float64(bounds.Dy()))))
			var r, g, b, a float64
			n := 0
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					p := src.NRGBAAt(sx, sy)
					af := float64(p.A) / 255
					r += float64(p.R) * af
					g += float64(p.G) * af
					b += float64(p.B) * af
					a += af
					n++
				}
			}
			if a == 0 {
				continue
			}
			dst.SetNRGBA(x, y, color.NRGBA{
				R: uint8(math.Round(r / a)),
				G: uint8(math.Round(g / a)),
				B: uint8(math.Round(b / a)),
				A: uint8(math.Round(a / float64(n) * 255)),
			})
		}
	}
	return dst
}

func writePNG(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(file, img); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func main() {
	out := flag.String("out", "packaging/windows/appicon.png", "master PNG path")
	size := flag.Int("size", 1024, "master PNG edge length in pixels")
	previews := flag.String("previews", "", "directory for downscaled preview PNGs")
	previewSizes := flag.String("preview-sizes", "16,24,32,48,64", "preview edge lengths")
	flag.Parse()

	img := render(*size)
	if err := writePNG(*out, img); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%dx%d)\n", *out, *size, *size)
	if *previews == "" {
		return
	}
	for _, field := range strings.Split(*previewSizes, ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		n, err := strconv.Atoi(field)
		if err != nil || n <= 0 || n > *size {
			fmt.Fprintf(os.Stderr, "invalid preview size %q\n", field)
			os.Exit(1)
		}
		path := filepath.Join(*previews, fmt.Sprintf("appicon-%d.png", n))
		if err := writePNG(path, downscale(img, n)); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("wrote %s\n", path)
	}
}
