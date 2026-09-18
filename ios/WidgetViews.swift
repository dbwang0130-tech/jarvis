import SwiftUI
import WidgetKit

// 小组件的画面。
//
// **放在主 app 这边、由小组件共用**（见 genproject.py 的 WIDGET_SHARED），
// 不是放在 Widget/ 下：小组件是个独立进程，跑起来才看得见，改一版样式要
// 装一次、加一次小组件、等系统刷一遍。放在这儿，主 app 里就能把它摆出来
// 看——这些视图只吃一个 WidgetSnapshot，跟数据从哪儿来无关。
//
// 品牌那套（头像、主色、圆角）跟 app 里用的是同一份 Brand，小组件才不会
// 看着像另一个 app 的东西。

// MARK: - 画面
struct TodayView: View {
    let snap: WidgetSnapshot
    /// 摆多大。小组件里不传，由系统给（`widgetFamily` 是只读的环境值）；
    /// 在 app 里把它摆出来看时直接传——那种场合环境里没有真的尺寸。
    var forced: WidgetFamily?

    @Environment(\.widgetFamily) private var family

    private var size: WidgetFamily { forced ?? family }

    var body: some View {
        switch size {
        case .systemSmall: small
        default:           medium
        }
    }

    // MARK: 小号

    /// 小号只放一件事：**下一件要做的**。
    ///
    /// 挤进三行标题的话，每行都短得看不出是什么。一件事加上它什么时候，
    /// 剩下的用「还有 N 件」一句话带过。
    private var small: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 6) {
                WidgetAvatar(size: 22)
                Text(headline)
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(.secondary)
                Spacer(minLength: 0)
                Ring(done: snap.doneToday, total: snap.totalToday)
                    .frame(width: 18, height: 18)
            }

            Spacer(minLength: 6)

            if let run = snap.running {
                Running(item: run, compact: true)
            } else if let first = snap.items.first {
                VStack(alignment: .leading, spacing: 3) {
                    if !first.time.isEmpty {
                        Text(first.time)
                            .font(.caption2.monospacedDigit().weight(.semibold))
                            .foregroundStyle(first.urgent ? Color.red : Brand.primary)
                    }
                    Text(first.title)
                        .font(.callout.weight(.medium))
                        .lineLimit(3)
                        .minimumScaleFactor(0.85)
                }
            } else {
                Text(snap.emptyHint)
                    .font(.callout)
                    .foregroundStyle(.secondary)
            }

            Spacer(minLength: 4)

            if snap.items.count > 1 {
                Text("还有 \(snap.items.count - 1) 件")
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
            }
        }
    }

    // MARK: 中号

    private var medium: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 8) {
                WidgetAvatar(size: 26)
                Text(headline)
                    .font(.subheadline.weight(.semibold))
                Spacer(minLength: 0)
                if snap.totalToday > 0 {
                    HStack(spacing: 5) {
                        Ring(done: snap.doneToday, total: snap.totalToday)
                            .frame(width: 16, height: 16)
                        Text("\(snap.doneToday)/\(snap.totalToday)")
                            .font(.caption2.monospacedDigit())
                            .foregroundStyle(.secondary)
                    }
                }
            }

            if let run = snap.running {
                Running(item: run, compact: false)
            }

            if snap.items.isEmpty && snap.running == nil {
                Spacer(minLength: 0)
                Text(snap.emptyHint)
                    .font(.callout)
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, alignment: .leading)
                Spacer(minLength: 0)
            } else {
                // 正在计时那条已经在上面单独摆过了，列表里不再重复一遍。
                let rest = snap.items.filter { $0.ref != snap.running?.ref }
                let shown = snap.running == nil ? 4 : 2
                VStack(alignment: .leading, spacing: 8) {
                    ForEach(rest.prefix(shown)) { Row(item: $0) }
                }
                Spacer(minLength: 0)
                // 摆不下的那些也得交代一句，否则「今天还剩 6 件」下面只列了
                // 四条，对不上。
                if rest.count > shown {
                    Text("还有 \(rest.count - shown) 件")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }
            }
        }
    }

    private var headline: String {
        if snap.running != nil { return "正在做" }
        let left = snap.items.count
        return left > 0 ? "今天还剩 \(left) 件" : "今天"
    }
}

/// 列表里的一行。时间用等宽数字，几行叠下来能对齐成一列。
struct Row: View {
    let item: WidgetItem

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 7) {
            if item.time.isEmpty {
                // 没排时间的用一个点占住那一列，不然标题会参差不齐。
                Circle()
                    .fill(Color.secondary.opacity(0.4))
                    .frame(width: 4, height: 4)
                    .frame(width: 42, alignment: .trailing)
            } else {
                Text(item.time)
                    .font(.caption.monospacedDigit().weight(.medium))
                    .foregroundStyle(item.urgent ? Color.red : Brand.primary)
                    .frame(width: 42, alignment: .trailing)
            }
            Text(item.title)
                .font(.footnote)
                .lineLimit(1)
            Spacer(minLength: 0)
        }
    }
}

/// 正在做的那条。
///
/// **这是整块小组件里唯一真的在动的东西**：`Text(style: .timer)` 的秒表
/// 由系统自己走，不用重画、不花刷新预算。一眼看过去它在跳，就知道这件事
/// 还开着——比任何静态的「进行中」三个字都有说服力。
struct Running: View {
    let item: WidgetItem
    let compact: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack(spacing: 5) {
                Image(systemName: "stopwatch")
                    .font(.caption2)
                if let at = item.startedAt {
                    Text(at, style: .timer)
                        .font(.caption.monospacedDigit().weight(.semibold))
                } else {
                    Text("进行中").font(.caption.weight(.semibold))
                }
            }
            .foregroundStyle(.orange)

            Text(item.title)
                .font(compact ? .callout.weight(.medium) : .subheadline.weight(.medium))
                .lineLimit(compact ? 2 : 1)
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 6)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color.orange.opacity(0.12), in: .rect(cornerRadius: 10))
    }
}

/// 今天做完了多少。
///
/// 只写「还剩 3 件」的话，看不出是「一上午干掉七件还剩三件」还是
/// 「一件没动」——同样是 3，心情差很远。
struct Ring: View {
    let done: Int
    let total: Int

    private var ratio: Double {
        total > 0 ? min(1, Double(done) / Double(total)) : 0
    }

    var body: some View {
        ZStack {
            Circle().stroke(Color.secondary.opacity(0.25), lineWidth: 3)
            Circle()
                .trim(from: 0, to: ratio)
                .stroke(Brand.primary, style: StrokeStyle(lineWidth: 3, lineCap: .round))
                .rotationEffect(.degrees(-90))
        }
        .accessibilityLabel("今天做完 \(done) 件，共 \(total) 件")
    }
}

/// 小组件上的头像。
///
/// 锁屏和待机显示会把整块小组件抹成单色（accented / vibrant），那种时候
/// 一张彩色 3D 头像会被糊成一团看不出是谁。`.fullColor` 让它在这些模式下
/// 保持原样——这张脸是这个 app 的招牌，糊掉就白放了。
struct WidgetAvatar: View {
    var size: CGFloat = 24

    var body: some View {
        Image("Avatar")
            .resizable()
            // 得挂在 Image 上，不能等它变成 some View 之后再说。
            .widgetAccentedRenderingMode(.fullColor)
            .aspectRatio(contentMode: .fill)
            .frame(width: size, height: size)
            .clipShape(Circle())
            .accessibilityHidden(true)
    }
}

extension WidgetSnapshot {
    /// 小组件库里挑选时看到的样子。真数据还没写进来时也用它——
    /// 一块空白会让人以为这个小组件坏了。
    static let preview = WidgetSnapshot(
        items: [
            WidgetItem(ref: "a:1", title: "跟老张过一遍方案", time: "10:00"),
            WidgetItem(ref: "a:2", title: "写周报", time: "14:30"),
            WidgetItem(ref: "a:3", title: "买牛奶"),
        ],
        doneToday: 2, totalToday: 5)

    /// 正在计时的样子。秒表那一条是这块小组件的招牌，预览里得看得见。
    static let running: WidgetSnapshot = {
        var s = preview
        s.running = WidgetItem(ref: "a:1", title: "跟老张过一遍方案", time: "10:00",
                               startedAt: Date().addingTimeInterval(-1500))
        return s
    }()
}

extension WidgetItem {
    /// 把 `09:30` 这种排在今天的钟点还原成一个时刻，用来排时间线。
    /// 形如 `10:00-11:30` 的取开头。
    func clockTime(on day: Date, cal: Calendar) -> Date? {
        let hhmm = time.split(separator: "-").first.map(String.init) ?? time
        let parts = hhmm.split(separator: ":")
        guard parts.count == 2, let h = Int(parts[0]), let m = Int(parts[1]) else { return nil }
        return cal.date(bySettingHour: h, minute: m, second: 0, of: day)
    }
}
