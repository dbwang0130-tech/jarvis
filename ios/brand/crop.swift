import CoreGraphics
import Foundation
import ImageIO
import UniformTypeIdentifiers

// 按比例裁一块方形出来再缩放。
// 用法：crop.swift 源图 目标图 中心x 中心y 边长 输出边长 [circle]
// （中间三个是 0~1 的比例，输出边长是像素；给了 circle 就裁成圆的、边上透明）
//
// 圆形头像居中裁会切掉头发只剩下巴和胸口，所以取景框要往上挪到头部。

let a = CommandLine.arguments
let circle = a.count == 8 && a[7] == "circle"
guard a.count == 7 || circle else {
    FileHandle.standardError.write(Data("用法: crop.swift 源 目标 cx cy 边长 输出像素\n".utf8))
    exit(2)
}
guard let src = CGImageSourceCreateWithURL(URL(fileURLWithPath: a[1]) as CFURL, nil),
      let img = CGImageSourceCreateImageAtIndex(src, 0, nil) else {
    FileHandle.standardError.write(Data("读不了 \(a[1])\n".utf8))
    exit(1)
}
let w = Double(img.width), h = Double(img.height)
let side = Double(a[5])! * w
let rect = CGRect(x: Double(a[3])! * w - side / 2,   // CGImage 裁剪原点在左上
                  y: Double(a[4])! * h - side / 2,
                  width: side, height: side)
guard let cropped = img.cropping(to: rect) else {
    FileHandle.standardError.write(Data("裁不出来\n".utf8))
    exit(1)
}

let out = Int(a[6])!
// 圆形那条路要留透明边，别的不带 alpha——app 图标不许透明。
let ctx = CGContext(data: nil, width: out, height: out, bitsPerComponent: 8,
                    bytesPerRow: 0, space: CGColorSpaceCreateDeviceRGB(),
                    bitmapInfo: (circle ? CGImageAlphaInfo.premultipliedLast
                                        : CGImageAlphaInfo.noneSkipLast).rawValue)!
ctx.interpolationQuality = .high
let box = CGRect(x: 0, y: 0, width: Double(out), height: Double(out))
if circle {
    ctx.addEllipse(in: box)
    ctx.clip()
}
ctx.draw(cropped, in: box)

let dest = CGImageDestinationCreateWithURL(
    URL(fileURLWithPath: a[2]) as CFURL, UTType.png.identifier as CFString, 1, nil)!
CGImageDestinationAddImage(dest, ctx.makeImage()!, nil)
guard CGImageDestinationFinalize(dest) else { exit(1) }
