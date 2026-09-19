import Foundation
import UserNotifications

/// 手机上的提醒。
///
/// 桌面端靠 launchd 每五分钟跑一次 `jarvis remind`，手机上没这条路——
/// iOS 不会让一个 app 在后台一直醒着。所以走本地通知：**打开 app 时把
/// 未来几天的提醒一次性排好**，之后不管 app 在不在跑，系统到点自己弹。
///
/// 代价要说清楚：在电脑上新加了一件事，手机上得打开一次 app 才会排上
/// 它的提醒。这是 iOS 的规矩，不是偷懒。
enum Notifier {
    /// iOS 对每个 app 最多只保留 64 条待发通知，超了默默丢掉最靠后的。
    /// 留点余量，别正好卡在边界上。
    private static let maxPending = 56

    static func requestPermission() async -> Bool {
        let center = UNUserNotificationCenter.current()
        let settings = await center.notificationSettings()
        switch settings.authorizationStatus {
        case .authorized, .provisional, .ephemeral:
            return true
        case .denied:
            return false
        default:
            return (try? await center.requestAuthorization(options: [.alert, .sound, .badge]))
                ?? false
        }
    }

    static func isAuthorized() async -> Bool {
        let s = await UNUserNotificationCenter.current().notificationSettings()
        return [.authorized, .provisional, .ephemeral].contains(s.authorizationStatus)
    }

    /// 按当前这份日程重排全部提醒。
    ///
    /// 每次都先清空再排，而不是增量更新——条目改了时间、标完成了、被删了，
    /// 挨个去对哪条该撤哪条该改，比重排一遍复杂得多也更容易错。
    static func reschedule(from snapshot: Snapshot, leadMinutes: Int, enabled: Bool,
                           brief: Bool = false, briefHour: Int = 8) async {
        let center = UNUserNotificationCenter.current()
        center.removeAllPendingNotificationRequests()
        guard enabled, await isAuthorized() else { return }

        let now = Date()
        var count = 0

        // 每天一条简报排在最前面：它只占几条名额，而「到点提醒」多到
        // 撑满 64 条上限时，被挤掉的应该是靠后那些单项，不是简报。
        if brief {
            count += await scheduleBriefs(snapshot, hour: briefHour, now: now)
        }
        for day in snapshot.days {
            for item in day.items {
                guard count < maxPending else { return }
                guard !item.isDone, !item.time.isEmpty,
                      let at = fireDate(day: day.date, time: item.time,
                                        lead: leadMinutes), at > now
                else { continue }

                let content = UNMutableNotificationContent()
                content.title = item.title
                content.body = body(for: item, lead: leadMinutes)
                content.sound = .default
                content.userInfo = ["ref": item.ref]

                let comps = Calendar.current.dateComponents(
                    [.year, .month, .day, .hour, .minute], from: at)
                let request = UNNotificationRequest(
                    // 同一条目同一天只排一次；id 带上日期，循环任务的下一次
                    // 才不会把这一次顶掉。
                    identifier: "jarvis.\(item.ref).\(day.date)",
                    content: content,
                    trigger: UNCalendarNotificationTrigger(dateMatching: comps, repeats: false))
                try? await center.add(request)
                count += 1
            }
        }
    }

    // MARK: - 每天早上那一条

    /// 每天早上一条简报：今天几件事、哪些今天截止、有几件逾期。
    ///
    /// **为什么是一条而不是一堆**：截止项一直没有任何提醒（配置里那个
    /// 「提前几天提醒」只影响日程视图显示的范围，从不发通知），逾期的
    /// 也只在列表里标红。但给每一条都排个通知，一天弹七八次，人会把
    /// 通知整个关掉——那就一条都收不到了。汇成一条，一天只响一次。
    ///
    /// **只有今天那条的内容是准的。** 明天及以后的简报是今天排好的，
    /// 用的是今天这份快照——「有几件逾期」到那天早就变了。所以往后
    /// 那几天只说「今天排了 N 件事」（这个由日程算得出，不会变），
    /// 不说逾期数，免得报一个假数字。
    private static func scheduleBriefs(_ snapshot: Snapshot, hour: Int,
                                       now: Date) async -> Int {
        let cal = Calendar.current
        let center = UNUserNotificationCenter.current()
        var made = 0

        for (i, day) in snapshot.days.enumerated() where i < briefDays {
            guard let at = fireTime(day: day.date, hour: hour), at > now else { continue }

            let today = cal.isDateInToday(at)
            guard let text = briefText(day, snapshot: snapshot, isToday: today) else {
                continue   // 那天什么都没有，别为「今天没事」响一声
            }

            let content = UNMutableNotificationContent()
            content.title = today ? "今天" : dayTitleCN(at)
            content.body = text
            content.sound = .default

            let comps = cal.dateComponents([.year, .month, .day, .hour, .minute], from: at)
            let request = UNNotificationRequest(
                identifier: "jarvis.brief.\(day.date)",
                content: content,
                trigger: UNCalendarNotificationTrigger(dateMatching: comps, repeats: false))
            try? await center.add(request)
            made += 1
        }
        return made
    }

    /// 简报排几天。排太多没意义——内容是今天算的，越往后越不准。
    private static let briefDays = 5

    private static func briefText(_ day: Day, snapshot: Snapshot, isToday: Bool) -> String? {
        let todo = day.items.filter { !$0.isDone }
        let due = todo.filter { $0.dueKind == "deadline" }

        var parts: [String] = []
        if !todo.isEmpty { parts.append("\(todo.count) 件事") }
        if !due.isEmpty { parts.append("\(due.count) 件今天截止") }

        if isToday {
            // 逾期只在今天这条里说：往后那几天的数字是今天算的，到那天
            // 早就不是这个数了。
            let overdue = snapshot.todos.filter { $0.overdue && !$0.isDone }.count
            if overdue > 0 { parts.append("\(overdue) 件逾期") }
        }
        guard !parts.isEmpty else { return nil }

        // 第一件事的标题带上——只给数字的话，还得点开才知道是什么。
        var text = parts.joined(separator: " · ")
        if let first = todo.first {
            let when = first.time.isEmpty ? "" : first.time + " "
            text += "\n" + when + first.title
        }
        return text
    }

    private static func fireTime(day: String, hour: Int) -> Date? {
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        f.dateFormat = "yyyy-MM-dd"
        guard let d = f.date(from: day) else { return nil }
        return Calendar.current.date(bySettingHour: hour, minute: 0, second: 0, of: d)
    }

    private static func dayTitleCN(_ at: Date) -> String {
        let f = DateFormatter()
        f.locale = Locale(identifier: "zh_Hans_CN")
        f.dateFormat = "M 月 d 日 EEEE"
        return f.string(from: at)
    }

    private static func body(for item: Item, lead: Int) -> String {
        var parts = [item.time]
        if !item.parents.isEmpty { parts.append(item.parents) }
        parts.append(lead > 0 ? "还有 \(lead) 分钟" : "现在开始")
        return parts.joined(separator: " · ")
    }

    /// 把「2026-09-17」和「10:00-11:30」拼成该响铃的那一刻。
    /// 取的是开始时间，往前挪 lead 分钟。
    private static func fireDate(day: String, time: String, lead: Int) -> Date? {
        let start = time.split(separator: "-").first.map(String.init) ?? time
        let f = DateFormatter()
        f.calendar = Calendar.current
        f.timeZone = .current
        f.locale = Locale(identifier: "en_US_POSIX")
        f.dateFormat = "yyyy-MM-dd HH:mm"
        guard let at = f.date(from: "\(day) \(start)") else { return nil }
        return at.addingTimeInterval(-Double(lead) * 60)
    }
}
