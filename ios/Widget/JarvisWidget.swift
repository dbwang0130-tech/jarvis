import SwiftUI
import WidgetKit

/// 主屏上的 Jarvis：今天还剩什么。
///
/// 这块小组件要回答的只有一个问题——**现在该干什么**。所以正在计时的那条
/// 排最前（还带个自己走的秒表），其次是最近要发生的那几件；「今天一共
/// 几件」这种总账退到角落里的进度环上。
///
/// 数据全部来自主 app 写进共享容器的那份快照（见 WidgetShared.swift）。
/// 这儿不读文件、不调 Go、不联网——小组件的进程随时会被系统掐掉，
/// 它只该做「把手上这份画出来」这一件事。
@main
struct JarvisWidgetBundle: WidgetBundle {
    var body: some Widget {
        TodayWidget()
    }
}

struct TodayWidget: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: "JarvisToday", provider: Provider()) { entry in
            TodayView(snap: entry.snap)
                // 背景交给系统：这样它在主屏、在锁屏、在待机显示里能各按
                // 各的规矩来（比如锁屏上会被抹成单色），不必自己判。
                .containerBackground(for: .widget) { Brand.widgetBackground }
        }
        .configurationDisplayName("今天")
        .description("今天还剩什么，正在做的那件会自己走秒。")
        .supportedFamilies([.systemSmall, .systemMedium])
    }
}

// MARK: - 时间线

struct Entry: TimelineEntry {
    let date: Date
    let snap: WidgetSnapshot
}

/// 刷新策略：**能不刷就不刷**。
///
/// 系统给每个小组件的刷新次数是有限的（一天几十次），花光了它就停在
/// 那儿不动了。所以这块小组件把「会变的东西」尽量交给系统自己算：
/// 正在计时那条用 `Text(style: .timer)`，秒表是系统在走，不算刷新。
///
/// 真需要重画的只有两种时候：数据变了（主 app 那边写完快照会喊
/// `reloadAllTimelines`），以及某一条的时间点过去了——所以下面按今天
/// 剩下那几条的时间点排 entry，没有的话一小时看一次。
struct Provider: TimelineProvider {
    func placeholder(in context: Context) -> Entry {
        Entry(date: Date(), snap: .preview)
    }

    func getSnapshot(in context: Context, completion: @escaping (Entry) -> Void) {
        // 小组件库里预览用的那一份：没有真数据时给个像样的样子，
        // 不然用户在挑选界面上看到的是一块空白。
        let snap = context.isPreview ? .preview : (WidgetBox.load() ?? .empty)
        completion(Entry(date: Date(), snap: snap))
    }

    func getTimeline(in context: Context, completion: @escaping (Timeline<Entry>) -> Void) {
        let snap = WidgetBox.load() ?? .empty
        let now = Date()

        // 今天剩下那几条的时间点，各排一个 entry：到点了那条就该从
        // 「接下来」变成「刚过去」。
        var dates = [now]
        let cal = Calendar.current
        for it in snap.items {
            guard let at = it.clockTime(on: now, cal: cal), at > now else { continue }
            dates.append(at)
        }
        // 兜底：什么都没排时也一小时醒一次，免得「还剩几件」停在昨天。
        dates.append(now.addingTimeInterval(3600))
        dates = Array(Set(dates)).sorted().prefix(8).map { $0 }

        let entries = dates.map { Entry(date: $0, snap: snap) }
        completion(Timeline(entries: entries, policy: .atEnd))
    }
}
