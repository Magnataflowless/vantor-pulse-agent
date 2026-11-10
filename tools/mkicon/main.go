// Gera os ícones da bandeja (verde = monitorando, azul = pausa, cinza = parado). Uso: go run ./tools/mkicon
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

func main() {
	for name, c := range map[string]color.RGBA{"green": {34, 197, 94, 255}, "blue": {59, 130, 246, 255}, "gray": {140, 146, 156, 255}} {
		const n = 32
		img := image.NewRGBA(image.Rect(0, 0, n, n))
		bg := color.RGBA{21, 23, 26, 255}
		// fundo arredondado
		for y := 0; y < n; y++ {
			for x := 0; x < n; x++ {
				dx, dy := math.Max(0, math.Abs(float64(x)-15.5)-9), math.Max(0, math.Abs(float64(y)-15.5)-9)
				if math.Hypot(dx, dy) <= 7 {
					img.Set(x, y, bg)
				}
			}
		}
		// linha de pulso
		pts := [][2]float64{{5, 17}, {10.5, 17}, {13, 11}, {17, 22}, {20, 14.5}, {22, 17}, {27, 17}}
		for i := 0; i+1 < len(pts); i++ {
			a, b := pts[i], pts[i+1]
			for t := 0.0; t <= 1; t += 0.01 {
				x, y := a[0]+(b[0]-a[0])*t, a[1]+(b[1]-a[1])*t
				for oy := -1.2; oy <= 1.2; oy += 0.4 {
					for ox := -1.2; ox <= 1.2; ox += 0.4 {
						img.Set(int(x+ox), int(y+oy), c)
					}
				}
			}
		}
		var p bytes.Buffer
		_ = png.Encode(&p, img)
		var ico bytes.Buffer
		_ = binary.Write(&ico, binary.LittleEndian, []uint16{0, 1, 1})
		ico.Write([]byte{n, n, 0, 0})
		_ = binary.Write(&ico, binary.LittleEndian, []uint16{1, 32})
		_ = binary.Write(&ico, binary.LittleEndian, []uint32{uint32(p.Len()), 22})
		ico.Write(p.Bytes())
		_ = os.WriteFile("internal/agent/assets/"+name+".ico", ico.Bytes(), 0o644)
	}
}
