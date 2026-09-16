package cozmo

import "math"

const (
	defaultWidth     = 128
	defaultHeight    = 64
	defaultEyeWidth  = 28
	defaultEyeHeight = 40
	xFactor          = 0.55
	yFactor          = 0.25
)

// ProceduralBase 持有共享的尺度换算。
type ProceduralBase struct {
	params []float64
	offset int
	width  int
	height int

	eyeWidth             float64
	eyeHeight            float64
	halfEyeWidth         float64
	halfEyeHeight        float64
	scaleFactorLidHeight float64
	scaleFactorLidBend   float64
	xF                   float64
	yF                   float64
}

func (p *ProceduralBase) init(params []float64, offset, width, height int) {
	p.params = params
	p.offset = offset
	p.width = width
	p.height = height
	p.eyeWidth = float64(width) * (defaultEyeWidth / float64(defaultWidth))
	p.eyeHeight = float64(height) * (defaultEyeHeight / float64(defaultHeight))
	p.halfEyeWidth = p.eyeWidth / 2
	p.halfEyeHeight = p.eyeHeight / 2
	p.scaleFactorLidHeight = 1.2 * p.eyeWidth
	p.scaleFactorLidBend = 1.2 * p.halfEyeWidth
	p.xF = xFactor * (float64(width) / float64(defaultWidth))
	p.yF = yFactor * (float64(height) / float64(defaultHeight))
}

// Lid 是一片眼睑：一块从眼睛上方/下方压下来的黑色遮挡。
type Lid struct {
	ProceduralBase
	yOffset     float64
	angleOffset float64
}

func (l *Lid) y() float64     { return l.params[l.offset] }
func (l *Lid) angle() float64 { return l.params[l.offset+1] }
func (l *Lid) bend() float64  { return l.params[l.offset+2] }

func (l *Lid) render(eye *Bitmap) {
	lid := NewBitmap(l.width*2, l.height*2)

	lidHeight := trunc(l.eyeHeight * l.y())
	lid.rectangle(
		trunc(float64(l.width)-l.scaleFactorLidHeight),
		trunc(float64(l.height)-1-l.halfEyeHeight),
		trunc(float64(l.width)+l.scaleFactorLidHeight),
		trunc(float64(l.height)-1+float64(lidHeight)),
		1,
	)

	bendHeight := trunc(l.eyeHeight * (1 - l.y()) * l.bend())
	lid.chordLower(
		trunc(float64(l.width)-l.scaleFactorLidBend),
		trunc(float64(l.height)-1+float64(lidHeight-bendHeight)),
		trunc(float64(l.width)+l.scaleFactorLidBend),
		trunc(float64(l.height)-1+float64(lidHeight+bendHeight)),
		1,
	)

	rotated := lid.rotate(l.angle()+l.angleOffset, false)
	black := NewBitmap(rotated.Width, rotated.Height)
	eye.paste(
		black,
		trunc((float64(eye.Width)-float64(rotated.Width))/2),
		trunc((float64(eye.Height)-float64(rotated.Height))/2+l.yOffset),
		rotated,
	)
}

// Eye 是一只眼睛：四角半径独立的圆角矩形 + 上下眼睑修剪。
type Eye struct {
	ProceduralBase
	xOffset      float64
	cornerRadius float64
	lids         [2]*Lid
}

func (e *Eye) init(params []float64, offset int, xOffset float64, width, height int) {
	e.ProceduralBase.init(params, offset, width, height)
	e.xOffset = xOffset
	e.cornerRadius = float64(e.width)/20 + float64(e.height)/10
	e.lids[0] = &Lid{}
	e.lids[0].init(params, offset+13, width, height)
	e.lids[0].yOffset = -e.halfEyeHeight
	e.lids[0].angleOffset = 0
	e.lids[1] = &Lid{}
	e.lids[1].init(params, offset+16, width, height)
	e.lids[1].yOffset = e.halfEyeHeight + 1
	e.lids[1].angleOffset = 180
}

func (e *Eye) render(face *Bitmap) {
	eye := NewBitmap(e.width, e.height)
	r := e.cornerRadius
	x1 := int(math.Floor(float64(eye.Width)/2) - e.halfEyeWidth)
	y1 := int(math.Floor(float64(eye.Height)/2) - e.halfEyeHeight)
	x2 := int(math.Floor(float64(eye.Width)/2) + e.halfEyeWidth)
	y2 := int(math.Floor(float64(eye.Height)/2) + e.halfEyeHeight)

	upperInnerX := e.params[e.offset+9]
	upperInnerY := e.params[e.offset+10]
	upperOuterX := e.params[e.offset+11]
	upperOuterY := e.params[e.offset+12]
	lowerInnerX := e.params[e.offset+5]
	lowerInnerY := e.params[e.offset+6]
	lowerOuterX := e.params[e.offset+7]
	lowerOuterY := e.params[e.offset+8]

	// 内侧
	eye.rectangle(
		x2-trunc(r*math.Max(upperInnerX, lowerInnerX)),
		y1+trunc(r*upperInnerY),
		x2,
		y2-trunc(r*lowerInnerY),
		1,
	)
	// 上侧
	eye.rectangle(
		x1+trunc(r*upperOuterX),
		y1,
		x2-trunc(r*upperInnerX),
		y1+trunc(r*math.Max(upperOuterY, upperInnerY)),
		1,
	)
	// 外侧
	eye.rectangle(
		x1,
		y1+trunc(r*upperOuterY),
		x1+trunc(r*math.Max(upperOuterX, lowerOuterX)),
		y2-trunc(r*lowerOuterY),
		1,
	)
	// 下侧
	eye.rectangle(
		x1+trunc(r*lowerOuterX),
		y2-trunc(r*math.Max(lowerOuterY, lowerInnerY)),
		x2-trunc(r*lowerInnerX),
		y2,
		1,
	)
	// 中心补偿
	eye.rectangle(
		x1+trunc(r*math.Max(upperOuterX, lowerOuterX))-2,
		y1+trunc(r*math.Max(upperOuterY, upperInnerY))-1,
		x2-trunc(r*math.Max(upperInnerY, lowerInnerY))+2,
		y2-trunc(r*math.Max(lowerOuterY, lowerInnerY))+1,
		1,
	)

	// 四个角各补一个椭圆象限
	eye.pieslice(
		x2-2*trunc(r*lowerInnerX), y2-2*trunc(r*lowerInnerY), x2, y2, "lower-right", 1,
	)
	eye.pieslice(
		x2-2*trunc(r*upperInnerX), y1, x2, y1+2*trunc(r*upperInnerY), "upper-right", 1,
	)
	eye.pieslice(
		x1, y1, x1+2*trunc(r*upperOuterX), y1+2*trunc(r*upperOuterY), "upper-left", 1,
	)
	eye.pieslice(
		x1, y2-2*trunc(r*lowerOuterY), x1+2*trunc(r*lowerOuterX), y2, "lower-left", 1,
	)

	for _, lid := range e.lids {
		lid.render(eye)
	}

	rotated := eye.rotate(e.angle(), true)
	scaled := rotated.resize(trunc(float64(rotated.Width)*e.scaleX()), trunc(float64(rotated.Height)*e.scaleY()))
	if scaled == nil {
		return
	}
	face.paste(
		scaled,
		trunc(float64(face.Width)-float64(scaled.Width))/2+trunc(e.centerX()*e.xF)+trunc(e.xOffset),
		trunc(float64(face.Height)-float64(scaled.Height))/2+trunc(e.centerY()*e.yF),
		scaled,
	)
}

func (e *Eye) centerX() float64 { return e.params[e.offset] }
func (e *Eye) centerY() float64 { return e.params[e.offset+1] }
func (e *Eye) scaleX() float64  { return e.params[e.offset+2] }
func (e *Eye) scaleY() float64  { return e.params[e.offset+3] }
func (e *Eye) angle() float64   { return e.params[e.offset+4] }

// Face 是一张程序化表情脸。
type Face struct {
	ProceduralBase
	eyes [2]*Eye
}

// NewFace 用 43 个参数构造一张脸；params 为 nil 时用中性脸。
func NewFace(params []float64, width, height int) *Face {
	if params == nil {
		params = append([]float64(nil), NeutralParams...)
	}
	f := &Face{}
	f.ProceduralBase.init(params, 0, width, height)
	eyeOffset := trunc(float64(width) / 5)
	f.eyes[0] = &Eye{}
	f.eyes[0].init(params, 5, -float64(eyeOffset), width, height)
	f.eyes[1] = &Eye{}
	f.eyes[1].init(params, 5+19, float64(eyeOffset), width, height)
	return f
}

func (f *Face) Params() []float64 { return f.params }

// Render 渲染一张 width×height 的单色位图，1 表示点亮。
func (f *Face) Render() *Bitmap {
	screen := NewBitmap(f.width, f.height)
	face := NewBitmap(f.width, f.height)
	for _, eye := range f.eyes {
		eye.render(face)
	}
	rotated := face.rotate(f.angle(), true)
	scaled := rotated.resize(trunc(float64(rotated.Width)*f.scaleX()), trunc(float64(rotated.Height)*f.scaleY()))
	if scaled == nil {
		return screen
	}
	screen.paste(
		scaled,
		trunc(float64(screen.Width)-float64(scaled.Width))/2+trunc(f.centerX()*f.xF),
		trunc(float64(screen.Height)-float64(scaled.Height))/2+trunc(f.centerY()*f.yF),
		scaled,
	)
	return screen
}

func (f *Face) centerX() float64 { return f.params[0] }
func (f *Face) centerY() float64 { return f.params[1] }
func (f *Face) scaleX() float64  { return f.params[2] }
func (f *Face) scaleY() float64  { return f.params[3] }
func (f *Face) angle() float64   { return f.params[4] }

// LerpParams 把两组参数按 t 线性插值。
func LerpParams(a, b []float64, t float64) []float64 {
	out := make([]float64, 43)
	for i := 0; i < 43; i++ {
		out[i] = a[i] + t*(b[i]-a[i])
	}
	return out
}
