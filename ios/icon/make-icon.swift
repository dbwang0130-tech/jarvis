import CoreGraphics
import Foundation
import ImageIO
import UniformTypeIdentifiers

// 画 Jarvis 的图标：深底上一圈冷光，中间一个向上的三角。
//
// 取的是「胸口那盏灯」的意思，不是照抄：一个发光的环、一个三角、一点中心
// 的亮。手机桌面上图标只有六十几个点宽，细节全会糊掉，所以只留三样东西，
// 每样都画得够粗。

let size: CGFloat = 1024
let cx = size / 2, cy = size / 2

let space = CGColorSpaceCreateDeviceRGB()
guard let ctx = CGContext(data: nil, width: Int(size), height: Int(size),
                          bitsPerComponent: 8, bytesPerRow: 0, space: space,
                          // 不带 alpha：iOS 的 app 图标不许透明，带了 Xcode 直接报错。
                          bitmapInfo: CGImageAlphaInfo.noneSkipLast.rawValue) else {
    fatalError("建不了画布")
}

func rgb(_ r: CGFloat, _ g: CGFloat, _ b: CGFloat, _ a: CGFloat = 1) -> CGColor {
    CGColor(colorSpace: space, components: [r / 255, g / 255, b / 255, a])!
}

let ink = rgb(10, 14, 22)        // 近黑的深蓝，不用纯黑——纯黑在深色桌面上会糊成一块
let inkEdge = rgb(4, 6, 10)
let glow = rgb(94, 226, 255)     // 冷青
let glowSoft = rgb(140, 238, 255)
let core = rgb(240, 253, 255)

// 先铺满底色——不带 alpha 的画布初值是黑的，但别指望这个。
ctx.setFillColor(inkEdge)
ctx.fill(CGRect(x: 0, y: 0, width: size, height: size))

// 底：中间略亮的径向渐变，让光看着是从里面透出来的。
if let bg = CGGradient(colorsSpace: space, colors: [ink, inkEdge] as CFArray,
                       locations: [0, 1]) {
    ctx.drawRadialGradient(bg, startCenter: CGPoint(x: cx, y: cy), startRadius: 0,
                           endCenter: CGPoint(x: cx, y: cy), endRadius: size * 0.74,
                           options: [.drawsAfterEndLocation])
}

/// 光晕用真正的模糊阴影叠出来。
///
/// 一开始是把同一条路径描几遍、一遍比一遍粗，结果每一遍的边都看得见，
/// 三角里套出好几层三角，像套娃不像发光。setShadow 是真模糊，边是连续的。
func halo(_ path: CGPath, width: CGFloat, color: CGColor, spread: CGFloat = 1) {
    ctx.setLineJoin(.round)
    ctx.setLineCap(.round)
    // 两遍：一遍散得远的底光，一遍收得紧的近光，再压一道实线。
    for (blur, alpha, w) in [(size * 0.075 * spread, 0.55, width * 1.15),
                             (size * 0.028 * spread, 0.85, width)] {
        ctx.saveGState()
        ctx.setShadow(offset: .zero, blur: blur, color: color.copy(alpha: alpha))
        ctx.setStrokeColor(color)
        ctx.setLineWidth(w)
        ctx.addPath(path)
        ctx.strokePath()
        ctx.restoreGState()
    }
    ctx.setStrokeColor(color)
    ctx.setLineWidth(width)
    ctx.addPath(path)
    ctx.strokePath()
}

// 外环
let outer = CGMutablePath()
outer.addArc(center: CGPoint(x: cx, y: cy), radius: size * 0.360,
             startAngle: 0, endAngle: .pi * 2, clockwise: false)
halo(outer, width: size * 0.028, color: glow)

// 内环，细一点，跟外环之间留出呼吸
let inner = CGMutablePath()
inner.addArc(center: CGPoint(x: cx, y: cy), radius: size * 0.268,
             startAngle: 0, endAngle: .pi * 2, clockwise: false)
halo(inner, width: size * 0.011, color: glowSoft, spread: 0.6)

// 三角：正三角形，顶点朝上，外接圆比内环再小一圈。
let r = size * 0.190
let tri = CGMutablePath()
for i in 0..<3 {
    let a = .pi / 2 + CGFloat(i) * (.pi * 2 / 3)   // 从正上方起，逆时针
    let p = CGPoint(x: cx + r * cos(a), y: cy + r * sin(a))
    if i == 0 { tri.move(to: p) } else { tri.addLine(to: p) }
}
tri.closeSubpath()
halo(tri, width: size * 0.034, color: glow)

// 中心那点亮：三角中间一小团白。图标缩到桌面上只有六十几个点，
// 环和三角都会细成一条线，最后还认得出的就是这团光，所以要够亮够实。
if let c = CGGradient(colorsSpace: space,
                      colors: [core, core.copy(alpha: 0.85)!,
                               glowSoft.copy(alpha: 0.35)!,
                               glow.copy(alpha: 0)!] as CFArray,
                      locations: [0, 0.22, 0.5, 1]) {
    ctx.drawRadialGradient(c, startCenter: CGPoint(x: cx, y: cy), startRadius: 0,
                           endCenter: CGPoint(x: cx, y: cy), endRadius: size * 0.115,
                           options: [])
}

guard let image = ctx.makeImage() else { fatalError("画不出来") }
let out = URL(fileURLWithPath: CommandLine.arguments.count > 1
              ? CommandLine.arguments[1] : "icon.png")
guard let dest = CGImageDestinationCreateWithURL(out as CFURL, UTType.png.identifier as CFString, 1, nil)
else { fatalError("写不了 \(out.path)") }
CGImageDestinationAddImage(dest, image, nil)
guard CGImageDestinationFinalize(dest) else { fatalError("写失败") }
print("画好了：\(out.path)")
