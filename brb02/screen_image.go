package brb02

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"math"
)

// EXE 0x568fca..0x568fe9 checks width >= 428 and height >= 142 before
// cropping. The USB factory backup is exactly 428 * 142 * 2 bytes.
const ScreenWidth, ScreenHeight = 428, 142
const ScreenImageBytes = ScreenWidth * ScreenHeight * 2

// EncodeScreenImage center-crops to the panel ratio and resamples with bilinear
// interpolation. RGB565 is row-major, big endian (official QDataStream order 0).
func EncodeScreenImage(source image.Image) ([]byte, error) {
	if source == nil || source.Bounds().Empty() {
		return nil, fmt.Errorf("图片为空")
	}
	b := source.Bounds()
	scale := math.Min(float64(b.Dx())/ScreenWidth, float64(b.Dy())/ScreenHeight)
	xOffset := float64(b.Min.X) + (float64(b.Dx())-scale*ScreenWidth)/2
	yOffset := float64(b.Min.Y) + (float64(b.Dy())-scale*ScreenHeight)/2
	data := make([]byte, ScreenImageBytes)
	for y := 0; y < ScreenHeight; y++ {
		for x := 0; x < ScreenWidth; x++ {
			sx, sy := xOffset+(float64(x)+.5)*scale-.5, yOffset+(float64(y)+.5)*scale-.5
			x0, y0 := int(math.Floor(sx)), int(math.Floor(sy))
			fx, fy := sx-float64(x0), sy-float64(y0)
			var rgb [3]float64
			for j := 0; j < 2; j++ {
				for i := 0; i < 2; i++ {
					px, py := max(b.Min.X, min(b.Max.X-1, x0+i)), max(b.Min.Y, min(b.Max.Y-1, y0+j))
					r, g, blue, _ := source.At(px, py).RGBA() // premultiplied alpha composites onto black
					weight := (1 - fx) * (1 - fy)
					if i == 1 {
						weight = fx * (1 - fy)
					}
					if j == 1 {
						weight = (1 - fx) * fy
					}
					if i == 1 && j == 1 {
						weight = fx * fy
					}
					for k, v := range []uint32{r, g, blue} {
						rgb[k] += float64(v>>8) * weight
					}
				}
			}
			r, g, blue := uint16(math.Round(rgb[0])), uint16(math.Round(rgb[1])), uint16(math.Round(rgb[2]))
			binary.BigEndian.PutUint16(data[(y*ScreenWidth+x)*2:], (r>>3)<<11|(g>>2)<<5|blue>>3)
		}
	}
	return data, nil
}

func DecodeScreenImage(data []byte) (*image.RGBA, error) {
	if len(data) != ScreenImageBytes {
		return nil, fmt.Errorf("屏保图片长度无效")
	}
	out := image.NewRGBA(image.Rect(0, 0, ScreenWidth, ScreenHeight))
	for i := 0; i < ScreenWidth*ScreenHeight; i++ {
		pixel := binary.BigEndian.Uint16(data[i*2:])
		r, g, b := byte(pixel>>11), byte((pixel>>5)&63), byte(pixel&31)
		out.SetRGBA(i%ScreenWidth, i/ScreenWidth, color.RGBA{r<<3 | r>>2, g<<2 | g>>4, b<<3 | b>>2, 255})
	}
	return out, nil
}
