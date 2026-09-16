package cozmo

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
)

// RenderGray 以 factor 倍超采样渲染并降采样成灰度覆盖图，得到抗锯齿边缘。
func (f *Face) RenderGray(width, height, factor int) *image.Gray {
	if factor < 1 {
		factor = 1
	}
	big := NewFace(f.Params(), width*factor, height*factor).Render()
	out := image.NewGray(image.Rect(0, 0, width, height))
	area := factor * factor
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			sum := 0
			for dy := 0; dy < factor; dy++ {
				row := (y*factor+dy)*big.Width + x*factor
				for dx := 0; dx < factor; dx++ {
					sum += int(big.Data[row+dx])
				}
			}
			out.SetGray(x, y, color.Gray{Y: uint8(sum * 255 / area)})
		}
	}
	return out
}

// PNG 把脸渲染成抗锯齿 PNG 字节。
func (f *Face) PNG(width, height, factor int) ([]byte, error) {
	img := f.RenderGray(width, height, factor)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ITermImage 把 PNG 包成 iTerm2 内联图片的 OSC 1337 转义序列。
// widthCells 是显示宽度（终端列数），0 表示按图片原始宽度。
func ITermImage(pngBytes []byte, widthCells int) string {
	head := "\x1b]1337;File=inline=1"
	if widthCells > 0 {
		head += fmt.Sprintf(";width=%d", widthCells)
	}
	head += ":"
	return head + base64.StdEncoding.EncodeToString(pngBytes) + "\a"
}
