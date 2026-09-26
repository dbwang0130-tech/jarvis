import SwiftUI
import WebKit

/// 邮件原文。HTML 交给 WebView 渲染，纯文本直接显示。
///
/// **默认拦掉远程内容**。营销邮件里那些 1×1 的图片是跟踪像素，一加载就等于
/// 告诉发件人「这封信被打开了、什么时候、大概在哪儿」。所有正经邮件客户端
/// 都是默认不加载、给个按钮让人自己决定。内联图片（随信一起发来的那些）
/// 已经在 Go 那边嵌成 data URI，不受影响。
struct MailBodyView: View {
    /// 叫 content 不叫 body——View 协议自己有个 body，撞名字编不过。
    let content: MailBody
    @State private var allowRemote = false
    @State private var height: CGFloat = 200

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            if !content.html.isEmpty {
                if !allowRemote {
                    Button {
                        allowRemote = true
                    } label: {
                        Label("显示远程图片", systemImage: "photo")
                            .font(.caption)
                    }
                    .buttonStyle(.plain)
                    .foregroundStyle(Brand.primary)
                }
                HTMLView(html: content.html, allowRemote: allowRemote, height: $height)
                    .frame(height: height)
            } else if !content.text.isEmpty {
                Text(content.text)
                    .font(.footnote)
                    .textSelection(.enabled)
            }

            if content.inlineDropped > 0 {
                Label("有 \(content.inlineDropped) 张图片过大，未显示",
                      systemImage: "exclamationmark.circle")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }
        }
    }
}

/// 把 HTML 渲染出来，并把高度报回去——邮件长短不一，写死高度不是滚不完
/// 就是留一大块空白。
struct HTMLView: UIViewRepresentable {
    let html: String
    let allowRemote: Bool
    @Binding var height: CGFloat

    func makeCoordinator() -> Coordinator { Coordinator(height: $height) }

    func makeUIView(context: Context) -> WKWebView {
        let config = WKWebViewConfiguration()
        // 邮件里的脚本一概不跑。正常邮件用不着它，会用的多半没安好心。
        let prefs = WKWebpagePreferences()
        prefs.allowsContentJavaScript = false
        config.defaultWebpagePreferences = prefs

        let web = WKWebView(frame: .zero, configuration: config)
        web.navigationDelegate = context.coordinator
        web.scrollView.isScrollEnabled = false   // 让外面那层列表统一滚
        web.isOpaque = false
        web.backgroundColor = .clear
        return web
    }

    func updateUIView(_ web: WKWebView, context: Context) {
        // 先比输入，再决定要不要拼。邮件的 HTML 加上内联图的 data URI 能到
        // 好几 MB，而 body 每次重算都会走到这儿——拿拼好的结果去比，等于
        // 每次白拼一次几 MB 的字符串。
        let key = Coordinator.Loaded(html: html, allowRemote: allowRemote)
        guard context.coordinator.loaded != key else { return }
        context.coordinator.loaded = key
        web.loadHTMLString(Self.wrap(html, allowRemote: allowRemote), baseURL: nil)
    }

    /// 套一层样式：按屏幕宽度缩放、跟随深浅色、图片不许撑破边界。
    /// 邮件的 HTML 是按桌面宽度写的，原样放进手机就是要左右拖着看。
    private static func wrap(_ html: String, allowRemote: Bool) -> String {
        // 不允许远程内容时，用 CSP 拦住——比自己拿正则去改 src 靠谱，
        // 邮件里那些 HTML 千奇百怪，正则改不干净。
        let csp = allowRemote
            ? ""
            : """
              <meta http-equiv="Content-Security-Policy" \
              content="default-src 'none'; img-src data:; style-src 'unsafe-inline' data:; font-src data:;">
              """
        return """
        <!DOCTYPE html><html><head>
        <meta name="viewport" content="width=device-width, initial-scale=1">
        \(csp)
        <style>
          :root { color-scheme: light dark; }
          body { margin: 0; font: -apple-system-body;
                 word-break: break-word; -webkit-text-size-adjust: 100%; }
          img, table, pre { max-width: 100% !important; height: auto; }
          table { display: block; overflow-x: auto; }
          a { color: -apple-system-blue; }
        </style></head><body>\(html)</body></html>
        """
    }

    /// 只拿它真正需要的东西，不持有整个 HTMLView。
    ///
    /// 持有 HTMLView 的话，里面那个可能几 MB 的 html 字符串会跟 WKWebView
    /// 同寿——详情页开着的整段时间都钉在内存里，而它其实只需要一个回调。
    final class Coordinator: NSObject, WKNavigationDelegate {
        struct Loaded: Equatable {
            let html: String
            let allowRemote: Bool
        }

        private let height: Binding<CGFloat>
        var loaded: Loaded?

        init(height: Binding<CGFloat>) { self.height = height }

        func webView(_ web: WKWebView, didFinish _: WKNavigation!) {
            web.evaluateJavaScript("document.body.scrollHeight") { [height] value, _ in
                guard let h = value as? CGFloat, h > 0 else { return }
                // 太长的信截一截，外面那层列表照样能滚到底。
                height.wrappedValue = min(h, 6000)
            }
        }

        /// 邮件里的链接一律用 Safari 打开，不在这个小 WebView 里跳转——
        /// 在里面跳的话，用户会以为还在看邮件，其实已经到了别人的网页上。
        func webView(_ web: WKWebView,
                     decidePolicyFor action: WKNavigationAction,
                     decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
            if action.navigationType == .linkActivated, let url = action.request.url {
                UIApplication.shared.open(url)
                decisionHandler(.cancel)
                return
            }
            decisionHandler(.allow)
        }
    }
}
