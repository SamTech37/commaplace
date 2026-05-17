package handlers

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"
)

// Avatar canvas size for the stored PNG. Source PNGs are 2048×2048; we
// downscale 8× by box-averaging during the layer copy.
const avatarSize = 256

// AvatarChoice is the persisted JSON shape — what the user picked in the
// builder. Stored so we can re-open the builder pre-populated.
type AvatarChoice struct {
	Face  string `json:"face"`
	Eyes  string `json:"eyes"`
	Mouth string `json:"mouth"`
	Acc   string `json:"acc"`
}

// AvatarParts lists the valid IDs per category. Used as a whitelist when
// validating builder POSTs and when enumerating thumbnails on the builder
// page. Keep in sync with files under static/avatar/.
var AvatarParts = struct {
	Face, Eyes, Mouth, Acc []string
}{
	Face:  []string{"f1", "f2", "f3", "f4", "f5", "f6", "f7", "f8", "f9", "f10"},
	Eyes:  []string{"e1", "e2", "e3", "e4", "e5"},
	Mouth: []string{"m1", "m2", "m3", "m4", "m5", "m6"},
	Acc:   []string{"a1", "a2", "a3", "a4", "a5", "a6", "a7"},
}

func validPart(id string, list []string) bool {
	for _, p := range list {
		if p == id {
			return true
		}
	}
	return false
}

// ComposeAvatar renders the four-layer avatar into a 256×256 PNG. Each layer
// keeps its native colors — no tinting.
func (s *Server) ComposeAvatar(c AvatarChoice) ([]byte, error) {
	face, err := loadAvatarLayer("face/" + c.Face + ".png")
	if err != nil {
		return nil, fmt.Errorf("face: %w", err)
	}
	eyes, err := loadAvatarLayer("eyes/" + c.Eyes + ".png")
	if err != nil {
		return nil, fmt.Errorf("eyes: %w", err)
	}
	mouth, err := loadAvatarLayer("mouth/" + c.Mouth + ".png")
	if err != nil {
		return nil, fmt.Errorf("mouth: %w", err)
	}
	acc, err := loadAvatarLayer("acc/" + c.Acc + ".png")
	if err != nil {
		return nil, fmt.Errorf("acc: %w", err)
	}

	target := image.NewNRGBA(image.Rect(0, 0, avatarSize, avatarSize))
	drawLayer(target, face)
	drawLayer(target, eyes)
	drawLayer(target, mouth)
	drawLayer(target, acc)

	var buf bytes.Buffer
	if err := png.Encode(&buf, target); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func loadAvatarLayer(rel string) (*image.NRGBA, error) {
	f, err := assetsFS.Open("static/avatar/" + rel)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	out := image.NewNRGBA(b)
	draw.Draw(out, b, img, b.Min, draw.Src)
	// The source PNGs have opaque-white backgrounds rather than transparency.
	// Soft-key the white so parts can be layered: pixels with min channel
	// >= whiteKeyHi fade to alpha 0 at pure white; anti-aliased halos are
	// preserved.
	const whiteKeyHi = 240
	for i := 0; i+3 < len(out.Pix); i += 4 {
		r, g, bl := out.Pix[i], out.Pix[i+1], out.Pix[i+2]
		m := r
		if g < m {
			m = g
		}
		if bl < m {
			m = bl
		}
		if m >= whiteKeyHi {
			scale := float64(255-m) / float64(255-whiteKeyHi)
			out.Pix[i+3] = uint8(float64(out.Pix[i+3]) * scale)
		}
	}
	return out, nil
}

// drawLayer downscales src (2048×2048 typically) by box-average into dst
// (256×256) and src-over composites the result onto whatever is already in dst.
func drawLayer(dst *image.NRGBA, src *image.NRGBA) {
	srcW := src.Bounds().Dx()
	srcH := src.Bounds().Dy()
	if srcW == 0 || srcH == 0 {
		return
	}
	bx := srcW / avatarSize
	by := srcH / avatarSize
	if bx < 1 {
		bx = 1
	}
	if by < 1 {
		by = 1
	}
	totalArea := float64(bx * by)
	for y := 0; y < avatarSize; y++ {
		for x := 0; x < avatarSize; x++ {
			var sumR, sumG, sumB, sumA, sumAW float64
			sx0 := x * bx
			sy0 := y * by
			for dy := 0; dy < by; dy++ {
				for dx := 0; dx < bx; dx++ {
					off := src.PixOffset(sx0+dx, sy0+dy)
					r := float64(src.Pix[off+0])
					g := float64(src.Pix[off+1])
					b := float64(src.Pix[off+2])
					a := float64(src.Pix[off+3])
					// Premultiplied accumulation so transparent pixels don't
					// pull the average toward black.
					aw := a / 255.0
					sumR += r * aw
					sumG += g * aw
					sumB += b * aw
					sumAW += aw
					sumA += a
				}
			}
			outA := sumA / totalArea
			var sr, sg, sb uint8
			if sumAW > 0 {
				sr = clamp8(sumR / sumAW)
				sg = clamp8(sumG / sumAW)
				sb = clamp8(sumB / sumAW)
			}
			out := dst.PixOffset(x, y)
			compositeOver(dst.Pix[out:out+4], sr, sg, sb, clamp8(outA))
		}
	}
}

// compositeOver does NRGBA src-over of a single source pixel onto dst.
func compositeOver(dst []uint8, sr, sg, sb, sa uint8) {
	if sa == 0 {
		return
	}
	if sa == 255 || dst[3] == 0 {
		dst[0], dst[1], dst[2], dst[3] = sr, sg, sb, sa
		return
	}
	srcA := float64(sa) / 255.0
	dstA := float64(dst[3]) / 255.0
	outA := srcA + dstA*(1-srcA)
	if outA <= 0 {
		dst[0], dst[1], dst[2], dst[3] = 0, 0, 0, 0
		return
	}
	src := [3]uint8{sr, sg, sb}
	for i := 0; i < 3; i++ {
		c := (float64(src[i])*srcA + float64(dst[i])*dstA*(1-srcA)) / outA
		dst[i] = clamp8(c)
	}
	dst[3] = clamp8(outA * 255.0)
}

func clamp8(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v + 0.5)
}
