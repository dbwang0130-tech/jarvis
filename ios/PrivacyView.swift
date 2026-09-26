import SwiftUI

/// 隐私政策。
///
/// **正文是仓库根的 `PRIVACY.md`，打包进 bundle 的就是那一份文件。**
/// 它同时还要发成网页填进 App Store Connect（隐私政策网址是必填项，
/// app 里内置替代不了它）。两处各抄一份迟早会漂——改了网页忘了 app，
/// 或者反过来，而这偏偏是一份声称「我们不收集数据」的文件，说法不一致
/// 比没有更糟。所以只留一份，`ios/genproject.py` 从上一层把它引进来。
///
/// 渲染用的是聊天气泡那套 `MarkdownText`：这份文件里只有标题、段落、
/// 列表和一张表，它都认得，不必为一页静态文字另写一个渲染器。
struct PrivacyView: View {
    var body: some View {
        ScrollView {
            if let text = Self.text {
                MarkdownText(text: text)
                    .padding(.horizontal, 20)
                    .padding(.vertical, 16)
                    .frame(maxWidth: .infinity, alignment: .leading)
            } else {
                // 读不出来只可能是打包漏了。**说清楚去哪儿看**，
                // 别留一页空白让人以为这个 app 根本没有隐私政策。
                VStack(alignment: .leading, spacing: 8) {
                    Text("这一份没能读出来。")
                    Text("完整正文在项目仓库的 PRIVACY.md，网页版见 App Store 页面上的隐私政策链接。")
                        .foregroundStyle(.secondary)
                }
                .font(.callout)
                .padding(20)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .navigationTitle("隐私政策")
        .navigationBarTitleDisplayMode(.inline)
    }

    /// bundle 里那份正文。文件名是 `PRIVACY.md`——打包时只留 basename，
    /// 上一层那个 `../` 不会跟进来。
    static var text: String? {
        guard let url = Bundle.main.url(forResource: "PRIVACY", withExtension: "md"),
              let raw = try? String(contentsOf: url, encoding: .utf8) else { return nil }
        // 开头那些给读源码的人看的 HTML 注释（比如「上线前填真邮箱」那条）
        // 不该出现在界面上。
        //
        // **`(?s)` 不能少**：`.` 默认不匹配换行符，而那些注释全是跨行的，
        // 少了它一条都剥不掉——整段开发者笔记会原样印在隐私政策最上面。
        // 这一条编译、类型检查、契约都不会响，只有真跑起来看才看得见（栽过）。
        return raw.replacingOccurrences(
            of: "(?s)<!--.*?-->", with: "", options: [.regularExpression])
            .trimmingCharacters(in: .whitespacesAndNewlines)
    }
}
