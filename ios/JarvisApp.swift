import SwiftUI

@main
struct JarvisApp: App {
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
