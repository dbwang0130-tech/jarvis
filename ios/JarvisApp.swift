import SwiftUI
import UserNotifications

@main
struct JarvisApp: App {
    /// 接住通知点击。**必须在 app 起来的那一刻就挂上**——用户是点着通知
    /// 把 app 叫醒的，delegate 晚一步设，那一下点击就没人接了。
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var delegate

    init() {
        // 这两件必须在任何后台活儿开始之前办，而且只办一次。
        //
        // 时区：Go 那边改的是 time.Local 这个全局变量。晚一步的话，第一次
        // 改写会跟已经在跑的解析撞上——那是数据竞争，不是「顶多算错一次」。
        //
        // 数据目录：不告诉它的话，Go 会按 ~/.config 算路径，而 iOS 上容器
        // 根目录不可写，报的就是 operation not permitted。邮件同步进度因此
        // 一直存不下来，每次收信都当成第一次全量重拉。
        Bridge.setTimeZone()
        Bridge.setDataDir(Paths.support.path)
    }

    var body: some Scene {
        WindowGroup {
            RootView()
        }
    }
}

/// 只为一件事存在：在 app 起来的第一时间把通知 delegate 挂上。
///
/// 用户点通知把 app 叫醒时，系统会在启动完成后立刻投递那一下点击——
/// 等到某个 View 的 onAppear 再设 delegate 就晚了，点击已经没人接。
final class AppDelegate: NSObject, UIApplicationDelegate {
    private let router = NotificationRouter()

    func application(_ application: UIApplication,
                     didFinishLaunchingWithOptions options:
                     [UIApplication.LaunchOptionsKey: Any]? = nil) -> Bool {
        UNUserNotificationCenter.current().delegate = router
        return true
    }
}
