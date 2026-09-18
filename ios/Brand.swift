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

    /// 主色到亮蓝的渐变。用在最显眼的那一两处（发送、今天），
    /// 别到处铺——满屏渐变会把层次全抹平。
    static var sheen: LinearGradient {
        LinearGradient(colors: [primary, accent],
                       startPoint: .topLeading, endPoint: .bottomTrailing)
    }

    /// 一句话说清这是什么。收在一处，免得欢迎页、空状态、设置页各写一句，
    /// 三个地方三种说法。
    ///
    /// 选这句是因为它同时说清了两件事：怎么用（说一句），以及凭什么敢
    /// 交给它（剩下的它办）。数据在谁手里那件事是第二位的，单独一行讲。
    static let slogan = "说一句，剩下的交给我"

    /// 副标题：数据归属。这是跟别的日程 app 最大的差别，值得单独说。
    static let subtitle = "日程、账本、邮件都是你自己的文件，手机和电脑同一份"

    /// 版本号，给设置页最底下那行用。
    ///
    /// 免费 Apple ID 签的包七天就过期、要重装，手机上到底是哪一版说不清，
    /// 有这一行就对得上。
    static var version: String {
        let info = Bundle.main.infoDictionary
        let v = info?["CFBundleShortVersionString"] as? String ?? "1.0"
        let b = info?["CFBundleVersion"] as? String ?? ""
        return b.isEmpty ? v : "\(v)（\(b)）"
    }

    /// 全 app 的圆角。3D 那张头像是圆润的，界面跟着圆一点才是一家人。
    static let radius: CGFloat = 18
    static let smallRadius: CGFloat = 12

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

/// 手感。
///
/// 触觉跟配色、圆角一样是品牌的一部分——「说一句它就办了」这件事，
/// 眼睛看到的是气泡，指头感到的就是这一下。收在一处是因为：散着写的话，
/// 每处各挑一种强度，同一个 app 里轻一下重一下，人会觉得它没准头。
///
/// 五档，按**这件事要不要紧**分，不按它是什么操作分：
/// - `tap` 按下去了。发送、停下、复制——最常见，最轻。
/// - `done` 落地了。勾掉一条、记一笔、归档一封。比 tap 厚一点，
///   因为它确认的是「文件真的改了」，不只是「你点到了」。
/// - `success` 一整趟办完了。导入几千条、测通一个邮箱。
/// - `warning` 做了件收不回来的事。清空对话、确认发出。
/// - `failure` 没办成。这一档要留着——手指离开屏幕之后人就不看了，
///   失败光靠一行红字，很容易当成办成了。
///
/// **系统开关自己会管**：用户在「设置 → 声音与触感」里关掉之后，
/// 这些调用就是空转，不必在这儿判。
enum Haptics {
    static func tap() {
        UIImpactFeedbackGenerator(style: .light).impactOccurred()
    }

    static func done() {
        UIImpactFeedbackGenerator(style: .soft).impactOccurred()
    }

    static func success() {
        UINotificationFeedbackGenerator().notificationOccurred(.success)
    }

    static func warning() {
        UINotificationFeedbackGenerator().notificationOccurred(.warning)
    }

    static func failure() {
        UINotificationFeedbackGenerator().notificationOccurred(.error)
    }
}

/// 忙着的时候的头像：轻轻喘气。
///
/// 裸一个转圈哪个 app 都长一样，那几秒钟界面就不是「他在给你办」，
/// 而是「系统在忙」。原先只有首屏读 iCloud 那儿这么做，对话里等回复
/// 反倒是个静止的头像——同一件事两种样子。收在这儿，两处用同一个。
///
/// 幅度压得很小（±5%）：它要的是「活着」，不是「在动」；在对话里
/// 每来一条回复就蹦一下会很吵。关了动效的人直接拿到一个静的。
struct BusyAvatar: View {
    var size: CGFloat = 56

    @State private var breathe = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        Avatar(size: size)
            .scaleEffect(breathe ? 1.05 : 0.95)
            .opacity(breathe ? 1 : 0.7)
            .onAppear { breathe = !reduceMotion }
            .animation(reduceMotion ? nil
                       : .easeInOut(duration: 1.1).repeatForever(autoreverses: true),
                       value: breathe)
    }
}

/// 字标。
///
/// 「Jarvis」这四个字原先在欢迎页、设置页、对话空状态各写了一遍，
/// 字重字距各不相同——同一个牌子在自己 app 里出现三种写法。
/// 收到一处，顺手加一点字距：字标要比正文站得开一些才立得住。
struct Wordmark: View {
    var font: Font = .headline

    var body: some View {
        Text("Jarvis")
            .font(font.weight(.bold))
            .tracking(0.6)
    }
}

/// 头像 + 字标 + 一句话。设置页顶上和对话空页面用的是同一块东西，
/// 各写一遍就会慢慢走散。
struct BrandHeader: View {
    var size: CGFloat = 52

    /// 头像跟着字号长。写死的话，字放大到无障碍档位就成了一个小圆点
    /// 配两行大字，而且那两行会绕到头像身上去。
    @ScaledMetric(relativeTo: .headline) private var scale: CGFloat = 1
    @Environment(\.dynamicTypeSize) private var typeSize

    var body: some View {
        // 字大到无障碍档位时改竖排。横着摆的话，一行字标加一句 slogan
        // 要占掉半屏宽，slogan 折成两三行、头像孤零零吊在左边。
        let stacked = typeSize.isAccessibilitySize
        return AnyLayout(stacked ? AnyLayout(VStackLayout(alignment: .leading, spacing: 8))
                                 : AnyLayout(HStackLayout(spacing: 12))) {
            // 跟着长，但封顶两倍。头像是装饰——字调到最大那几档时它能
            // 涨到一百多点，占掉三分之一屏，真正要读的字反被挤下去。
            Avatar(size: min(size * scale, size * 2))
            VStack(alignment: .leading, spacing: 2) {
                Wordmark()
                Text(Brand.slogan)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
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
            // 它是装饰，不是信息。每条回复前面都挂一个的话，VoiceOver
            // 会在每句话之前先念一遍「Avatar」——谁说的那件事由气泡
            // 自己的标签讲（见 Bubble）。
            .accessibilityHidden(true)
    }
}
