package cozmo

import "math"

// Bitmap 是 1-bit 单色位图，1 表示点亮。
// 光栅化规则逐条复刻 Pillow / pycozmo，保证和 cozmo-face 逐像素一致。
type Bitmap struct {
	Width  int
	Height int
	Data   []byte
}

func NewBitmap(width, height int) *Bitmap {
	n := width * height
	if n < 0 {
		n = 0
	}
	return &Bitmap{Width: width, Height: height, Data: make([]byte, n)}
}

func (b *Bitmap) Get(x, y int) byte {
	return b.Data[y*b.Width+x]
}

func (b *Bitmap) clone() *Bitmap {
	out := &Bitmap{Width: b.Width, Height: b.Height, Data: append([]byte(nil), b.Data...)}
	return out
}

func trunc(v float64) int { return int(v) }

// coord 复刻 Geometry.c 的 COORD 宏。
func coord(v float64) int {
	if v < 0 {
		return -1
	}
	return int(v)
}

// round15 复刻 Python round(x, 15)。
func round15(v float64) float64 {
	r := math.Round(v*1e15) / 1e15
	if r == 0 {
		return 0
	}
	return r
}

// hline 复刻 Draw.c 的 hline8。
func (b *Bitmap) hline(x0, y, x1 int, ink byte) {
	if y < 0 || y >= b.Height {
		return
	}
	if x0 < 0 {
		x0 = 0
	} else if x0 >= b.Width {
		return
	}
	if x1 < 0 {
		return
	} else if x1 >= b.Width {
		x1 = b.Width - 1
	}
	row := y * b.Width
	for x := x0; x <= x1; x++ {
		b.Data[row+x] = ink
	}
}

// rectangle 复刻 ImageDraw.rectangle(fill=)；反转的矩形跳过不画。
func (b *Bitmap) rectangle(x0, y0, x1, y1 int, ink byte) {
	if y0 > y1 {
		y0, y1 = y1, y0
	}
	if y0 < 0 {
		y0 = 0
	} else if y0 >= b.Height {
		return
	}
	if y1 < 0 {
		return
	} else if y1 > b.Height {
		y1 = b.Height
	}
	for y := y0; y <= y1; y++ {
		b.hline(x0, y, x1, ink)
	}
}

// pieslice 复刻 ImageDraw.pieslice，仅限轴对齐象限。
func (b *Bitmap) pieslice(x0, y0, x1, y1 int, quadrant string, ink byte) {
	bx := trunc(float64(x0))
	by := trunc(float64(y0))
	a := trunc(float64(x1)) - bx
	bb := trunc(float64(y1)) - by
	if a < 0 || bb < 0 {
		return
	}
	right := quadrant == "lower-right" || quadrant == "upper-right"
	lower := quadrant == "lower-right" || quadrant == "lower-left"
	degenerate := a == 0 || bb == 0

	ellipseFillSegments(a, bb, func(sx0, sy, sx1 int) {
		if !degenerate && (lower != (sy < 0)) {
			if lower && sy < 0 {
				return
			}
			if !lower && sy > 0 {
				return
			}
		}
		cx0, cx1 := sx0, sx1
		if !degenerate && right {
			if sx0 < 0 {
				cx0 = 0
			}
		}
		if !degenerate && !right {
			if sx1 > 0 {
				cx1 = 0
			}
		}
		if cx0 > cx1 {
			return
		}
		b.hline(bx+((cx0+a)>>1), by+((sy+bb)>>1), bx+((cx1+a)>>1), ink)
	})
}

// chordLower 复刻 ImageDraw.chord(0, 180)：下半椭圆连同弦。
func (b *Bitmap) chordLower(x0, y0, x1, y1 int, ink byte) {
	bx := trunc(float64(x0))
	by := trunc(float64(y0))
	a := trunc(float64(x1)) - bx
	bb := trunc(float64(y1)) - by
	if a < 0 || bb < 0 {
		return
	}
	degenerate := a == 0 || bb == 0
	ellipseFillSegments(a, bb, func(sx0, sy, sx1 int) {
		if !degenerate && sy < 0 {
			return
		}
		b.hline(bx+((sx0+a)>>1), by+((sy+bb)>>1), bx+((sx1+a)>>1), ink)
	})
}

// paste 复刻 Image.paste；mask 为空时整块覆盖（连 0 一起写）。
func (b *Bitmap) paste(src *Bitmap, dx, dy int, mask *Bitmap) {
	for sy := 0; sy < src.Height; sy++ {
		ty := dy + sy
		if ty < 0 || ty >= b.Height {
			continue
		}
		for sx := 0; sx < src.Width; sx++ {
			tx := dx + sx
			if tx < 0 || tx >= b.Width {
				continue
			}
			if mask != nil && mask.Data[sy*mask.Width+sx] == 0 {
				continue
			}
			b.Data[ty*b.Width+tx] = src.Data[sy*src.Width+sx]
		}
	}
}

// resize 复刻 Image.resize(NEAREST)，取样点 (i+0.5)*src/dst。
func (b *Bitmap) resize(width, height int) *Bitmap {
	if width <= 0 || height <= 0 {
		return nil
	}
	if width == b.Width && height == b.Height {
		return b.clone()
	}
	out := NewBitmap(width, height)
	ax := float64(b.Width) / float64(width)
	ay := float64(b.Height) / float64(height)

	xmap := make([]int, width)
	xo := ax * 0.5
	for x := 0; x < width; x++ {
		xin := coord(xo)
		if xin >= 0 && xin < b.Width {
			xmap[x] = xin
		} else {
			xmap[x] = -1
		}
		xo += ax
	}

	yo := ay * 0.5
	for y := 0; y < height; y++ {
		yin := coord(yo)
		yo += ay
		if yin < 0 || yin >= b.Height {
			continue
		}
		srow := yin * b.Width
		drow := y * width
		for x := 0; x < width; x++ {
			if xmap[x] >= 0 {
				out.Data[drow+x] = b.Data[srow+xmap[x]]
			}
		}
	}
	return out
}

// rotate 复刻 Image.rotate(angle, NEAREST, expand)，逆时针，绕中心。
func (b *Bitmap) rotate(angle float64, expand bool) *Bitmap {
	angle = math.Mod(math.Mod(angle, 360)+360, 360)
	if angle == 0 {
		return b.clone()
	}
	if angle == 180 {
		return b.transpose180()
	}
	if (angle == 90 || angle == 270) && (expand || b.Width == b.Height) {
		return b.transpose90(angle)
	}

	w, h := float64(b.Width), float64(b.Height)
	cx, cy := w/2, h/2
	rad := -angle * math.Pi / 180
	cos := round15(math.Cos(rad))
	sin := round15(math.Sin(rad))
	m := [6]float64{cos, sin, 0, -sin, cos, 0}
	apply := func(x, y float64) (float64, float64) {
		return m[0]*x + m[1]*y + m[2], m[3]*x + m[4]*y + m[5]
	}

	m[2], m[5] = apply(-cx, -cy)
	m[2] += cx
	m[5] += cy

	if expand {
		xs := []float64{}
		ys := []float64{}
		for _, p := range [][2]float64{{0, 0}, {w, 0}, {w, h}, {0, h}} {
			tx, ty := apply(p[0], p[1])
			xs = append(xs, tx)
			ys = append(ys, ty)
		}
		minx, maxx := minMax(xs)
		miny, maxy := minMax(ys)
		nw := math.Ceil(maxx) - math.Floor(minx)
		nh := math.Ceil(maxy) - math.Floor(miny)
		m[2], m[5] = apply(-(nw-w)/2, -(nh-h)/2)
		w, h = nw, nh
	}
	return b.transformAffine(int(w), int(h), m)
}

func minMax(vs []float64) (float64, float64) {
	lo, hi := vs[0], vs[0]
	for _, v := range vs[1:] {
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	return lo, hi
}

// transformAffine 复刻 Geometry.c 的 ImagingTransformAffine（16.16 定点分支）。
func (b *Bitmap) transformAffine(w, h int, m [6]float64) *Bitmap {
	out := NewBitmap(w, h)
	corners := [][2]int{{0, 0}, {w, h}, {0, h}, {w, 0}}
	fixedOk := true
	for _, c := range corners {
		x, y := float64(c[0]), float64(c[1])
		if math.Abs(x*m[0]+y*m[1]+m[2]) >= 32768 || math.Abs(x*m[3]+y*m[4]+m[5]) >= 32768 {
			fixedOk = false
			break
		}
	}

	if fixedOk {
		fix := func(v float64) int64 { return int64(math.Floor(v*65536 + 0.5)) }
		a0 := fix(m[0])
		a1 := fix(m[1])
		a3 := fix(m[3])
		a4 := fix(m[4])
		a2 := fix(m[2] + m[0]*0.5 + m[1]*0.5)
		a5 := fix(m[5] + m[3]*0.5 + m[4]*0.5)
		for y := 0; y < h; y++ {
			xx := a2
			yy := a5
			drow := y * w
			for x := 0; x < w; x++ {
				xin := int(xx >> 16)
				if xin >= 0 && xin < b.Width {
					yin := int(yy >> 16)
					if yin >= 0 && yin < b.Height {
						out.Data[drow+x] = b.Data[yin*b.Width+xin]
					}
				}
				xx += a0
				yy += a3
			}
			a2 += a1
			a5 += a4
		}
		return out
	}

	xo := m[2] + m[1]*0.5 + m[0]*0.5
	yo := m[5] + m[4]*0.5 + m[3]*0.5
	for y := 0; y < h; y++ {
		xx := xo + m[1]*float64(y)
		yy := yo + m[4]*float64(y)
		drow := y * w
		for x := 0; x < w; x++ {
			xin := coord(xx)
			yin := coord(yy)
			if xin >= 0 && xin < b.Width && yin >= 0 && yin < b.Height {
				out.Data[drow+x] = b.Data[yin*b.Width+xin]
			}
			xx += m[0]
			yy += m[3]
		}
	}
	return out
}

func (b *Bitmap) transpose180() *Bitmap {
	out := &Bitmap{Width: b.Width, Height: b.Height, Data: make([]byte, len(b.Data))}
	n := len(b.Data)
	for i := 0; i < n; i++ {
		out.Data[i] = b.Data[n-1-i]
	}
	return out
}

func (b *Bitmap) transpose90(angle float64) *Bitmap {
	out := NewBitmap(b.Height, b.Width)
	for y := 0; y < b.Height; y++ {
		for x := 0; x < b.Width; x++ {
			v := b.Data[y*b.Width+x]
			if angle == 90 {
				out.Data[(b.Width-1-x)*b.Height+y] = v
			} else {
				out.Data[x*b.Height+(b.Height-1-y)] = v
			}
		}
	}
	return out
}

// quarterArc 复刻 Draw.c 的 Bresenham 椭圆四分弧，通过回调逐点产出。
func quarterArc(a, b int, yield func(x, y int)) {
	if a < 0 || b < 0 {
		return
	}
	a2, b2 := a*a, b*b
	a2b2 := a2 * b2
	delta := func(x, y int) float64 {
		return math.Abs(float64(a2*y*y + b2*x*x - a2b2))
	}
	cx, cy := a, b%2
	ex, ey := a%2, b
	for {
		yield(cx, cy)
		if cx == ex && cy == ey {
			return
		}
		nx, ny := cx, cy+2
		nd := delta(nx, ny)
		if cx > 1 {
			d := delta(cx-2, cy+2)
			if nd > d {
				nx, ny, nd = cx-2, cy+2, d
			}
			d = delta(cx-2, cy)
			if nd > d {
				nx, ny = cx-2, cy
			}
		}
		cx, cy = nx, ny
	}
}

// ellipseFillSegments 复刻 Draw.c 的 ellipse 填充，产出水平段 [x0,y,x1]。
func ellipseFillSegments(a, b int, yield func(x0, y, x1 int)) {
	leftmost := a % 2
	pts := [][2]int{}
	quarterArc(a, b, func(x, y int) { pts = append(pts, [2]int{x, y}) })
	if len(pts) == 0 {
		return
	}
	l := leftmost
	if l == 0 {
		l = 2
	}

	// 生成器语义：先取第一个点作为当前行，再推进到下一行。
	i := 1
	pr, py := pts[0][0], pts[0][1]
	finished := false
	for !finished {
		y, r := py, pr
		for i < len(pts) && pts[i][1] <= y {
			i++
		}
		if i >= len(pts) {
			finished = true
		} else {
			pr, py = pts[i][0], pts[i][1]
		}
		hasSide := leftmost > 0 || leftmost < r
		if hasSide && y > 0 {
			yield(l, y, r)
		}
		if y > 0 {
			yield(-r, y, -leftmost)
		}
		if hasSide {
			yield(l, -y, r)
		}
		yield(-r, -y, -leftmost)
	}
}
