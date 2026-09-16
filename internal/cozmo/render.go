package cozmo

import "strings"

// ToBlocks 把单色位图渲染成半块字符行：每两个像素压成一行。
// 点亮像素用实心块，其余留空。返回的行数与 Height/2 相等。
func ToBlocks(b *Bitmap) []string {
	rows := b.Height / 2
	out := make([]string, 0, rows)
	for r := 0; r < rows; r++ {
		var sb strings.Builder
		for x := 0; x < b.Width; x++ {
			top := b.Get(x, r*2) != 0
			bot := b.Get(x, r*2+1) != 0
			switch {
			case top && bot:
				sb.WriteRune('█')
			case top:
				sb.WriteRune('▀')
			case bot:
				sb.WriteRune('▄')
			default:
				sb.WriteRune(' ')
			}
		}
		out = append(out, sb.String())
	}
	return out
}

// quadrantChars 把 2×2 像素组合映射到象限块字符。
// 位序：tl=1, tr=2, bl=4, br=8。
var quadrantChars = [16]rune{
	' ', '▘', '▝', '▀', '▖', '▌', '▞', '▛',
	'▗', '▚', '▐', '▜', '▄', '▙', '▟', '█',
}

// ToQuadrantBlocks 用象限块渲染：1 个字符 = 2×2 像素，分辨率比半块翻倍。
func ToQuadrantBlocks(b *Bitmap) []string {
	rows := b.Height / 2
	out := make([]string, 0, rows)
	for y := 0; y < rows; y++ {
		var sb strings.Builder
		for x := 0; x < b.Width/2; x++ {
			code := 0
			if b.Get(2*x, 2*y) != 0 {
				code |= 1
			}
			if b.Get(2*x+1, 2*y) != 0 {
				code |= 2
			}
			if b.Get(2*x, 2*y+1) != 0 {
				code |= 4
			}
			if b.Get(2*x+1, 2*y+1) != 0 {
				code |= 8
			}
			sb.WriteRune(quadrantChars[code])
		}
		out = append(out, sb.String())
	}
	return out
}
