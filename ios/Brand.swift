import SwiftUI

/// 全 app 的配色，取自图标那张头像。
///
/// 只管「品牌」这一路——按钮、气泡、强调。**功能色不归它管**：日程的蓝、
/// 截止的橙、逾期的红是拿来区分状态的，跟着品牌走就失去辨识度了。
///
/// 深浅两套都定死。只写一套的话，深色模式下要么刺眼要么糊成一团。
enum Brand {
    /// 主色，取自制服。按钮、用户气泡、选中态。
    static let primary = adaptive(light: 0x1A5FC8, dark: 0x4D93E8)

    /// 亮蓝，取自制服上那几道发光的线。用在需要再亮一档的地方。
    static let accent = adaptive(light: 0x4499EE, dark: 0x6FB4F5)

    /// 背景浅蓝，取自头像底色。秘书的气泡、空状态。
    static let soft = adaptive(light: 0xE3F1FB, dark: 0x16263D)

    /// 深蓝黑，取自头发。深色模式下当底。
    static let deep = adaptive(light: 0x0F1A33, dark: 0x0B1220)

    /// 秘书气泡里的字。浅色下是深蓝而不是纯黑——纯黑压在浅蓝上太硬。
    static let onSoft = adaptive(light: 0x14294A, dark: 0xDCE8F7)

    private static func adaptive(light: UInt32, dark: UInt32) -> Color {
        Color(uiColor: UIColor { traits in
            traits.userInterfaceStyle == .dark ? ui(dark) : ui(light)
        })
    }

    private static func ui(_ hex: UInt32) -> UIColor {
        UIColor(red: CGFloat((hex >> 16) & 0xFF) / 255,
                green: CGFloat((hex >> 8) & 0xFF) / 255,
                blue: CGFloat(hex & 0xFF) / 255, alpha: 1)
    }
}

/// 头像。空状态、欢迎页用它，让 app 里和桌面上那个图标是同一个人。
///
/// 资源是**按头部单独裁过**的，不是图标那张满幅图——圆形裁剪居中切下去
/// 只会剩下巴和胸口，头发全没了。
struct Avatar: View {
    var size: CGFloat = 64

    var body: some View {
        Image("Avatar")
            .resizable()
            .aspectRatio(contentMode: .fill)
            .frame(width: size, height: size)
            .clipShape(Circle())
            // 浅蓝底配浅蓝边会糊在一起，描一圈更亮的
            .overlay(Circle().strokeBorder(Brand.accent.opacity(0.35),
                                           lineWidth: max(1, size / 48)))
    }
}
