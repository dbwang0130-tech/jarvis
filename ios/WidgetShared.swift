import Foundation

/// 小组件要显示的那一小份东西。
///
/// **为什么要单独存一份**：小组件是独立进程，够不着 iCloud Drive 里那些
/// org 文件——用户授权是给主 app 的（安全作用域书签存在主 app 自己的
/// UserDefaults 里），换个进程就不作数了。所以由主 app 每次读完 org
/// 顺手写一份快照到共享容器，小组件只读这一份，不碰文件、不调 Go。
///
/// 只放**今天要看的那几条**：小组件屏幕就那么大，而且这份数据每次
/// reload 都要写一遍，塞整份日程是白费功夫。
struct WidgetSnapshot: Codable {
    /// 这份数据是什么时候攒的。小组件上要显示「刚刚更新」之类的话，
    /// 也用来判断手上这份是不是太旧了。
    var updated = Date()

    /// 今天剩下要做的，已经按时间排好，最多 `maxItems` 条。
    var items: [WidgetItem] = []

    /// 今天一共几条、做完几条。进度环用这两个数——只给「还剩 3 件」的话，
    /// 看不出是「一上午干掉了 7 件还剩 3 件」还是「一件没动」。
    var doneToday = 0
    var totalToday = 0

    /// 正在计时的那条（org 里有一条没收尾的 CLOCK）。
    ///
    /// 小组件上让它自己走秒，见 `WidgetItem.startedAt`——这是整个小组件里
    /// 唯一真正「动」起来的东西，而且不花刷新预算。
    var running: WidgetItem?

    /// 一条都没有时说什么。空着的原因不止一种：今天真没事，或者还没选目录。
    var emptyHint = ""

    /// 小组件最多显示几条。多了也摆不下，中号能看到三四条。
    static let maxItems = 4

    static let empty = WidgetSnapshot(emptyHint: "还没选目录")
}

/// 小组件上的一条。字段只留摆得下的那几样。
struct WidgetItem: Codable, Identifiable, Hashable {
    /// org 里的 ref（`work.org:12`）。点它能直接跳回 app 里那一条。
    var ref = ""
    var title = ""
    /// `09:30`，没排时间就空着。
    var time = ""
    /// 逾期、截止当天这类要紧的，界面上标红。
    var urgent = false
    var done = false
    /// 这条正在计时的话，是它开始的时刻。小组件拿它做一个自己走的秒表。
    var startedAt: Date?

    var id: String { ref }
}

/// 主 app 和小组件之间那只信箱。
///
/// 走 App Group 的共享 UserDefaults 而不是共享容器里的文件：这份数据
/// 只有几百字节，一次写一整份、一次读一整份，没有并发写的问题，
/// 用不着为它张罗一套文件协调。
///
/// **App Group 没配好时一切照旧**：`defaults` 拿不到就返回 nil，
/// 主 app 那边写不进去也只是白写一趟，不影响别的。小组件会显示
/// 「打开 Jarvis 看看」——总好过整个 app 起不来。
enum WidgetBox {
    /// 跟 entitlements 里那个 App Group 保持一致，改一处就得改两处。
    static let group = "group.com.dabao.jarvis"

    private static let key = "jarvis.widget.snapshot"

    private static var defaults: UserDefaults? {
        UserDefaults(suiteName: group)
    }

    static func save(_ snap: WidgetSnapshot) {
        guard let d = defaults, let data = try? JSONEncoder().encode(snap) else { return }
        d.set(data, forKey: key)
    }

    static func load() -> WidgetSnapshot? {
        guard let d = defaults, let data = d.data(forKey: key) else { return nil }
        return try? JSONDecoder().decode(WidgetSnapshot.self, from: data)
    }
}
