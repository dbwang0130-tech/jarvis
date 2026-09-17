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
    static func reschedule(from snapshot: Snapshot, leadMinutes: Int, enabled: Bool) async {
        let center = UNUserNotificationCenter.current()
        center.removeAllPendingNotificationRequests()
        guard enabled, await isAuthorized() else { return }

        let now = Date()
        var count = 0
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
