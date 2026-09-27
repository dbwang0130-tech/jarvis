import SwiftUI
import UniformTypeIdentifiers

// 界面分四块：日程、待办、财务、对话。
// 手机屏幕小，每块只放最要紧的东西，详情靠点进去看。

/// 模拟器里查界面时别弹系统权限框。
///
/// **那个框是点不掉的**：simctl 没法点屏幕，也没有给通知权限的 privacy 子命令，
/// 于是它盖在要看的那一页上，截出来的图永远是它。跟 JARVIS_UI_VAULT /
/// JARVIS_UI_SCREEN 同一个口子，只在 Debug 里存在，真机上照常要权限。
private var skipPermissionPrompt: Bool {
#if DEBUG
    return ProcessInfo.processInfo.environment["JARVIS_UI_SCREEN"] != nil
#else
    return false
#endif
}

struct RootView: View {
    @StateObject private var store = Store()
    @Environment(\.scenePhase) private var phase

    var body: some View {
        Group {
            if store.isConfigured {
                MainShell()
            } else {
                WelcomeView()
            }
        }
        .environmentObject(store)
        // 通知点击要能找到 store。AppDelegate 那边挂 delegate 时 store
        // 还没建出来，所以反过来由这儿交过去。
        .onAppear {
            NotificationRouter.store = store
            // 冷启动时 onOpenURL 比这儿还早，攒下的图这会儿交出来。
            SharedImages.flush(to: store)
        }
        .tint(Brand.primary)
        // 图标那个人是圆润的软塑感，字跟着圆一点才像一家人。
        .fontDesign(.rounded)
        .task {
            guard store.isConfigured else { return }
            // 提醒默认开着，但权限得当面要一次——不要的话开关开着也白搭，
            // 而且用户会以为是 app 坏了。
            //
            // **不能等它**：requestAuthorization 会一直挂到用户点了为止，
            // 写成 await 的话，弹窗没点之前一个字都不会加载，界面就那么空着。
            if store.settings.remind, !skipPermissionPrompt {
                Task { _ = await Notifier.requestPermission() }
            }
            await store.reload()
        }
        // 切回前台重读一次。「跟电脑上是同一份」这句话得落到实处——
        // 在 Mac 上改完、拿起手机看到的还是旧的，那就不算同一份。
        // reload 内部会合并并发请求，跟首次进来那次撞上也不会读两遍。
        .onChange(of: phase) { _, now in
            guard store.isConfigured else { return }
            switch now {
            case .active:
                Task { await store.reload() }
            case .background:
                // 退到后台时把手头这段落进金库。**对话放 iCloud 是用户要的，
                // 不能只在他点「新建」时才生效**——很久不开新对话，那一段就
                // 一直只在本机。这里是低频时刻，也正是该落盘的时候。
                store.archiveCurrent()
            default:
                break
            }
        }
    }
}

// MARK: - 还没选目录

struct WelcomeView: View {
    @EnvironmentObject var store: Store
    @State private var picking = false
    @State private var entered = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        VStack(spacing: 20) {
            Spacer()
            Avatar(size: 140)
                .shadow(color: Brand.primary.opacity(0.25), radius: 18, y: 8)
                // 启动屏上那个头像是 120pt、正中、浅蓝底。这一屏接着它演：
                // 同一个人往前凑近一点，底色从上面那片蓝里化开。
                // 不做的话，蓝底「啪」地变白屏，像是换了个 app。
                .scaleEffect(entered || reduceMotion ? 1 : 0.86)
                .opacity(entered || reduceMotion ? 1 : 0)
            VStack(spacing: 8) {
                Wordmark(font: .largeTitle)
                Text(Brand.slogan)
                    .font(.title3)
                    .foregroundStyle(Brand.primary)
            }
            Text(Brand.subtitle)
                .font(.footnote)
                .multilineTextAlignment(.center)
                .foregroundStyle(.secondary)
                .padding(.horizontal, 32)

            Button("选 iCloud 里的 Jarvis 目录") { picking = true }
                .buttonStyle(.glassProminent)

            Text("iCloud Drive → Jarvis")
                .font(.footnote)
                .foregroundStyle(.tertiary)
            Spacer()
        }
        .padding()
        // 顶上这片蓝就是启动屏的底色（LaunchBackground 跟 Brand.soft
        // 是同一个色值），往下化掉。两屏之间因此没有硬边。
        .background(alignment: .top) {
            LinearGradient(colors: [Brand.soft, Color(.systemBackground)],
                           startPoint: .top, endPoint: .center)
                .ignoresSafeArea()
        }
        .onAppear {
            guard !reduceMotion else { return }
            withAnimation(.smooth(duration: 0.55)) { entered = true }
        }
        .fileImporter(isPresented: $picking, allowedContentTypes: [.folder]) { result in
            if case .success(let url) = result {
                Task { await store.chooseVault(url) }
            }
        }
    }
}

// MARK: - 主界面

/// 根视图：**一整屏对话，只有对话**。
///
/// 三轮下来是这样的：先是四个并排的标签页（Jarvis 占第一个），然后标签栏
/// 换成输入框上面一排四个胶囊，点开盖一张卡片；现在连那一排也没了。
///
/// 每一轮砍掉的都是同一件东西——**一个「去翻一翻」的地方**。而它们回答的
/// 问题（今天什么安排、还剩多少钱、有什么要紧的信、体重多少）问一句话就有
/// 答案，而且答案是挑过的、说人话的，比一屏列表有用。留着那一排的代价不只是
/// 屏幕，是它一直在暗示「真东西在里面，聊天只是个入口」。
///
/// 该看细节的时候照样看：聊天里提到的条目和邮件是可以点的，点开还是原来
/// 那几张卡片（ItemDetail、MailDetail）——**从话里点进去**，不是先进一个
/// 列表再找。桌面端早走完了这一步，见 CLAUDE.md 里删掉五视图那一段。
struct MainShell: View {
    var body: some View {
        NavigationStack { ChatView() }
    }
}

/// 列表里的金额：千分位分组、两位小数。
/// v == 0 那下是把 -0.00 拍平成 0.00。
func money(_ v: Double) -> String {
    let n = v == 0 ? 0 : v
    return Formatter.money2.string(from: NSNumber(value: n))
        ?? String(format: "%.2f", n)
}

/// 两位小数，末尾的零去掉：1250.46 / 3.5 / 12（而不是 12.00）。
func trimZeros(_ v: Double) -> String {
    var s = String(format: "%.2f", v)
    if s.contains(".") {
        while s.hasSuffix("0") { s.removeLast() }
        if s.hasSuffix(".") { s.removeLast() }
    }
    return s
}

extension Formatter {
    static let money2 = decimal(fraction: 2)

    private static func decimal(fraction: Int) -> NumberFormatter {
        let f = NumberFormatter()
        f.numberStyle = .decimal
        f.minimumFractionDigits = fraction
        f.maximumFractionDigits = fraction
        f.groupingSeparator = ","
        return f
    }
}

// MARK: - 对话

struct ChatView: View {
    @EnvironmentObject var store: Store
    @State private var draft = ""
    @State private var openSettings = false
    /// 记一笔账的表单。Siri 说「记一笔 35」时直接摆开它。
    @State private var spending = false
    /// Siri 说「记一笔账」时带来的金额，直接摆开记账表单。
    @State private var spendAmount: Double?
    /// 这一轮要一起发出去的图。
    @State private var shots: [Data] = []
    /// 点开的那条日程：通知点进来的，或者从话里点链接点进来的。
    @State private var notified: Item?
    /// 从话里点链接点开的那封邮件。
    @State private var openMail: MailItem?
    /// 点了句子里的体征链接（独占一行的那种是直接画成卡片的）。
    @State private var openVital: HealthVital?
    /// 点了链接但那条已经找不到了，说一句——比默默没反应好。
    @State private var linkMiss: String?
    /// 听写。摆在这一层是为了让发送那一下够得着它。
    @StateObject private var dictation = Dictation()
    @State private var entered = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.scenePhase) private var phase
    @FocusState private var focused: Bool

    var body: some View {
#if DEBUG
        // 跟 JARVIS_UI_VAULT 同一个口子：让某一屏直接出来，好在模拟器里看。
        // 设置页平时要点右上角才开得了，而 simctl 点不了屏幕。
        let _ = {
            let want = ProcessInfo.processInfo.environment["JARVIS_UI_SCREEN"] ?? ""
            if ["settings", "model", "models", "mail", "privacy", "sessions"].contains(want), !openSettings {
                DispatchQueue.main.async { openSettings = true }
            }
        }()
#endif
        // 输入区用 safeAreaInset 浮在内容之上，不是拿 VStack 摞在下面。
        // 摞着的话玻璃背后什么都没有，只能是一块白板——跟别的页面里
        // 内容从底部穿过去的样子对不上，看着很突兀。
        transcript
            .safeAreaInset(edge: .bottom, spacing: 0) { inputBar }
            // **读失败得说出来。** 这两条原来挂在日程/财务那几页上，页删掉
            // 之后就没了家——而聊天现在是唯一的屏。
            //
            // 发消息那条路出错会在对话里留一句「没办成」，但 lastError 还有
            // 别的来源：读 iCloud 超时、写回时跟电脑那头撞车、点邮件链接
            // 找不到那封。那些原来靠这条横幅显示，不接回来就是**静默失败**
            // ——界面上什么都不变，人以为点坏了。
            .safeAreaInset(edge: .top, spacing: 0) { ErrorBar() }
            // 第一次进来要读 iCloud、下载占位符，可能要等几秒。不给指示的话
            // 就是一屏例句，看着像「本来就没数据」。
            .overlay { LoadingHint(isEmpty: shownMessages.isEmpty) }
            // 标题由 ChatTitle 画在左上角（头像 + 字标 + 状态），
            // 所以这儿不能再给 navigationTitle——inline 标题会居中再画
            // 一遍「Jarvis」，一个标题栏上两个名字。
            .navigationBarTitleDisplayMode(.inline)
            // 秘书要做收不回来的事（比如发信）会停在这儿等。
            .sheet(item: $store.pending) { ConfirmSheet(ask: $0) }
            .sheet(isPresented: $spending) {
                SpendView(accounts: store.snapshot.ledger?.accounts ?? [],
                          preset: spendAmount)
            }
            // 点了「该收信了」那条通知：交给秘书去收。
            //
            // 早先是开邮件页、界面自己拉一遍，拿到的是一列标题——而这条通知
            // 问的是「有什么要紧的吗」。秘书收完会分拣、会挑出该回的那几封
            // 说人话，这才是点它的理由。要看某一封的原文，简报里那个主题
            // 点得开（见 openMailByKey）。
            .onChange(of: store.wantsMailCheck) { _, want in
                guard want else { return }
                store.wantsMailCheck = false
                if store.settings.mail.isEmpty {
                    // 原来是开邮件页，那一页的空状态会说「去设置里加一个」。
                    // 页没了就直接开设置——少绕一道。
                    openSettings = true
                    return
                }
                Task { await store.send(Store.mailCheckPrompt) }
            }
            // 点了到点提醒：直接把那一条打开。
            .onChange(of: store.wantsItem) { _, ref in
                guard let ref, let item = findItem(ref) else { return }
                store.wantsItem = nil
                notified = item
            }
            // 点了早上那条简报：**问它一句**，不是打开一个列表。
            //
            // 原来这儿开日程页。而那条通知问的是「今天要干什么」——秘书会
            // 挑出该先做的、说清哪几件撞了钟点，这比一屏按日期排好的条目
            // 有用。想看全的，话里那些条目点得开。
            .onChange(of: store.wantsPlanner) { _, want in
                guard want else { return }
                store.wantsPlanner = false
                Task { await store.send(Store.plannerPrompt) }
            }
            .sheet(item: $notified) { ItemDetail(item: $0) }
            .sheet(item: $openMail) { MailDetail(mail: $0) }
            .sheet(item: $openVital) { VitalDetail(vital: $0) }
            // **话里的链接自己接，不交给系统。** 秘书提到某条日程或某封邮件
            // 时会写成 [标题](jarvis://item/work.org:12)，点一下就在这儿展开
            // 原来那几张卡片。不拦的话系统会去找 jarvis:// 的处理程序，
            // 找不到——点下去毫无反应。
            //
            // 别的链接（http 之类）照旧交给系统：秘书引一个网址，人当然
            // 是想在浏览器里打开。
            .environment(\.openURL, OpenURLAction { url in
                guard url.scheme == "jarvis" else { return .systemAction }
                open(url)
                return .handled
            })
            .alert("找不到了", isPresented: .constant(linkMiss != nil)) {
                Button("好") { linkMiss = nil }
            } message: {
                Text(linkMiss ?? "")
            }
            // Siri / 快捷指令带进来的那一件事。起来时取一次；切回前台也
            // 取一次——app 已经开着的时候喊 Siri，走的是后面那条。
            .task { await takeIntent() }
            // 别的 app 分享进来的图：直接交给秘书看，它自己判断该干什么。
            //
            // 从相册或别的 app 特地分享一张图过来，多半已经想好了要拿它
            // 做什么（小票记账、会议截图排期）。原先落进输入框等人再补一
            // 句话，那句话十有八九也就是「记一下」——不如让它先看一眼，
            // 判断错了再说也来得及。
            //
            // **模型看不见图片就别发**：那一轮除了「我看不到图片」什么都
            // 拿不到，钱照花。退回老路子，图留在输入框里，人还能改用别的
            // 说法（比如自己念一遍金额）。
            .onChange(of: store.sharedShots) { _, incoming in
                guard !incoming.isEmpty else { return }
                store.sharedShots = []
                guard store.modelSeesImages else {
                    for data in incoming where shots.count < PhotoButton.maxShots {
                        shots.append(data)
                    }
                    focused = true
                    Haptics.done()
                    return
                }
                Haptics.done()
                Task { await store.send(Store.sharedImagePrompt, images: incoming) }
            }
            .onChange(of: phase) { _, now in
                if now == .active { Task { await takeIntent() } }
            }
            .toolbar {
                ToolbarItem(placement: .topBarLeading) { ChatTitle() }
                    // 标题不是按钮。不撤掉这层玻璃底的话，头像自己那个圆
                    // 外面再套一个圆形按钮，两层同心圆，而且内圈看着偏心。
                    .sharedBackgroundVisibility(.hidden)
                // 「新建」跟设置排一起靠右。左上角让给标题——那儿是聊天软件
                // 放「跟谁在聊」的位置。
                //
                // 图标从垃圾桶换成笔：**做的是同一件事，说的不是同一件事**。
                // 这个 app 里只有一段对话，开新的一段就等于把旧的清掉；但
                // 「清空」让人盯着自己要损失什么，「新建」说的是他真正想干的
                // 那件事——换个话题重新开始。iMessage、微信的「发起新对话」
                // 都是这支笔。
                ToolbarItem(placement: .topBarTrailing) {
                    // **不问了。** 原先这儿挂着确认框，因为按下去这一段就
                    // 真的没了。现在 clearChat 会先把它归档进金库的
                    // sessions/，「设置 → 历史对话」里随时读得回来——后果
                    // 变轻了，那一问也就没必要。
                    //
                    // 注意这是靠**让动作可撤销**来省掉确认，不是靠把一个
                    // 危险动作做得更顺手。收不回来的事（发信、删邮件）照旧要问。
                    Button {
                        store.clearChat()
                        Haptics.tap()
                    } label: {
                        Image(systemName: "square.and.pencil")
                    }
                    .disabled(store.messages.isEmpty)
                    .accessibilityLabel("新建对话")
                }
                SettingsLink()
            }
    }

    // MARK: - 对话记录

    private var transcript: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 12) {
                    if shownMessages.isEmpty && !store.thinking {
                        VStack(alignment: .leading, spacing: 16) {
                            // **这儿不再摆 BrandHeader。** 标题栏上就挂着同一张
                            // 脸、同一个名字，隔两厘米再来一份，第一眼会被当成
                            // 一条消息。招牌话留着——它不重复，标题栏那行写的是
                            // 状态（「随时待命」），而这一句是告诉人这东西怎么用。
                            Text(Brand.slogan)
                                .font(.callout)
                                .foregroundStyle(.secondary)
                            Starters(pick: startWith)
                                // 例句是**可以点的样例**，不是要逐字读的正文。
                                // 一路放大的话四句话就占满两屏，人得先滚过
                                // 它们才够得着输入框——而这一屏的正事是打字。
                                .dynamicTypeSize(...DynamicTypeSize.accessibility2)
                        }
                        .padding(.top, 20)
                        // 接着启动屏往上浮一点。头像现在停在标题栏上不动，
                        // 浮的是这段招牌话和例句——硬糊上去的话，启动屏
                        // 那一下就白铺垫了。
                        .opacity(entered || reduceMotion ? 1 : 0)
                        .offset(y: entered || reduceMotion ? 0 : 10)
                        .onAppear {
                            guard !reduceMotion else { return }
                            withAnimation(.smooth(duration: 0.45)) { entered = true }
                        }
                    }
                    ForEach(Array(shownMessages.enumerated()), id: \.offset) { i, m in
                        Bubble(message: m, isLast: i == shownMessages.count - 1)
                    }
                    if store.thinking {
                        ThinkingRow(steps: store.steps)
                    }
                    Color.clear.frame(height: 1).id("bottom")
                }
                .padding()
            }
            // 一开始滚就收键盘。.interactively 要手指往下拖着才收，
            // 而这里想要的是「去看上面说了什么」，滚动本身就是意思。
            .scrollDismissesKeyboard(.immediately)
            // 内容滚到输入区底下时柔和淡出，不然是硬生生被玻璃切一刀。
            .scrollEdgeEffectStyle(.soft, for: .bottom)
            // 恢复出来的对话落在底部，不然打开看到的是最早那几句。
            // 但空着的时候要顶到上面——否则那几句例句会被压到屏幕最下方，
            // 上面留一大片空白。
            .defaultScrollAnchor(shownMessages.isEmpty ? .top : .bottom)
            .onChange(of: store.messages.count) { _, _ in toBottom(proxy) }
            .onChange(of: store.steps.count) { _, count in
                // 开始动手了就把键盘收掉——这会儿是要看它干了什么，
                // 而不是接着打字。只在第一步冒出来时收，之后不打扰。
                if count == 1 { focused = false }
                toBottom(proxy)
            }
            .onChange(of: store.thinking) { _, _ in toBottom(proxy) }
        }
    }

    private func toBottom(_ proxy: ScrollViewProxy) {
        withAnimation { proxy.scrollTo("bottom", anchor: .bottom) }
    }

    // MARK: - 输入区

    private var inputBar: some View {
        VStack(spacing: 0) {
            if needsKey {
                Button {
                    openSettings = true
                } label: {
                    Label("还没填模型密钥，点这里去设置", systemImage: "key")
                        .font(.footnote)
                        .padding(.horizontal, 14).padding(.vertical, 8)
                }
                .buttonStyle(.plain)
                .foregroundStyle(Brand.primary)
                // 底是**实的**，不是玻璃。这是一条横幅，压在对话之上；
                // 玻璃会把底下滚过去的气泡透上来，两层蓝叠在一起谁都读不清
                // ——字号调大之后尤其明显。别处用玻璃是因为底下没有字。
                .background(Brand.soft, in: .capsule)
                .overlay(Capsule().strokeBorder(Brand.primary.opacity(0.15)))
                .padding(.horizontal, 12).padding(.bottom, 6)
            }
            // 输入区浮在内容之上，不再用一条实心底栏压着——下面的话能透出来，
            // 屏幕显得高一些。GlassEffectContainer 让挨着的两块玻璃算作一体，
            // 各自算各自的会在缝隙处叠出一道亮边。
            // 挑好的图摆在输入框上面：发出去之前得看得见自己挑了什么，
            // 也得能反悔。
            if !shots.isEmpty {
                ShotStrip(shots: $shots)
                    // **得有自己的底。** 这一条跟输入框一样浮在对话之上，
                    // 没有底的话下面滚过去的气泡会从图的缝隙里透上来，
                    // 看着像是图片糊了——跟「还没填模型密钥」那条当初
                    // 踩的是同一个坑。
                    .background(.bar, in: .rect(cornerRadius: Brand.radius))
                    .padding(.horizontal, 12)
                    .padding(.bottom, 6)
            }

            GlassEffectContainer(spacing: 8) {
                HStack(alignment: .bottom, spacing: 8) {
                    // 想说的话可能不止一行，让它自己长，最多六行。
                    // 相机在左、麦克风在右，**分开站**。
                    //
                    // 三颗都堆在右边（相机、麦克风，外面还有发送）的话，
                    // 指头落下去那一片全是按钮，想发图点成了发送、想说话
                    // 点成了发图。左右分开，误点就没了。
                    //
                    // 用 HStack 而不是 overlay：overlay 叠在 padding 之后的
                    // 整块区域上，靠 alignment 加 offset 去凑位置，多行输入
                    // 长高之后就会飘。摆进 HStack，位置是算出来的。
                    //
                    // 垂直方向按**底部**对齐，但两颗按钮各往上提 5pt：
                    // TextField 自带 11pt 的上下留白，纯按底边对齐的话，
                    // 按钮会沉到那片留白里，看着比文字低一截。
                    HStack(alignment: .bottom, spacing: 2) {
                        PhotoButton(shots: $shots)
                            .padding(.bottom, 5)
                        TextField(shots.isEmpty ? "说点什么" : "问问这张图",
                                  text: $draft, axis: .vertical)
                            .lineLimit(1...6)
                            .padding(.vertical, 11)
                            .focused($focused)
                        // 输入框右边那一格：**麦克风和发送轮着占**，不同时出现。
                        //
                        // 这是 iOS 消息的做法，也正好躲开原来那个取舍：原先
                        // 发送是框外单独一颗，注释里写着「相机在左、麦克风在右
                        // 分开站，三颗堆右边会误点」。挪进框里就又挤到一起了，
                        // 除非——让它们轮流上。没话可发时那一格是麦克风（要说
                        // 就说），一有字就换成蓝箭头（要发就发）。**同一时刻
                        // 那个位置只有一个意思**，误点从根上没了。
                        if canSend {
                            SendButton(action: submit)
                        } else {
                            MicButton(draft: $draft, dictation: dictation)
                        }
                    }
                    .padding(.horizontal, 6)
                    .glassEffect(.regular, in: .capsule)
                    // 这一下换的是「那一格是什么」，不是位置，所以只淡入淡出，
                    // 不要滑动——滑动会让输入框在打第一个字时抖一下。
                    .animation(.easeInOut(duration: 0.18), value: canSend)
                }
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
        }
        .animation(.easeOut(duration: 0.2), value: focused)
        .sheet(isPresented: $openSettings) { SettingsView() }
    }

    private var needsKey: Bool { store.settings.apiKey.isEmpty }

    private var canSend: Bool {
        // 光发图不说话也行——「这是什么」有时候不必打字。
        let hasText = !draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        return (hasText || !shots.isEmpty) && !store.thinking
    }

    private func submit() {
        // 人说完顺手就点发送，这会儿麦克风还开着是说不过去的——红点亮着，
        // 而且尾音还会继续往输入框里写字。
        dictation.stop()

        let q = draft
        let imgs = shots
        draft = ""
        shots = []
        Haptics.tap()
        // 不在这儿收键盘：刚发完还想接着补一句是常事。
        // 等它真动起手来（操作列表冒出来）再收，那会儿是要看它干什么。
        Task { await store.send(q, images: imgs) }
    }

    private func startWith(_ text: String) {
        draft = text
        focused = true
    }

    /// 按 ref 找那条日程。
    ///
    /// 日程和待办两摊都要找：到点提醒排的是有钟点的事（在 days 里），
    /// 而人从别处点进来的可能是任何一条。找不到就不开——文件在电脑上
    /// 被改过之后，行号可能已经指向别处了，宁可不开也别开错一条。
    /// 点了话里的一个 jarvis:// 链接。
    ///
    /// `jarvis://item/work.org:12` → host 是 item，path 是 /work.org:12。
    /// 邮件的 key 在 Go 那边转义过（账号名可能是中文），`url.path` 拿到的
    /// 已经是解码后的原样，不用自己再解一遍。
    private func open(_ url: URL) {
        // 解析归 JarvisLink 一处（见 Markdown.swift）：**这儿认得的种类必须跟
        // 块切分认得的是同一张表**，否则某一种链接就成了点了没反应的蓝字。
        //
        // 连它都认不出来的话也得说一声。默默 return 的话人点下去毫无反应，
        // 会以为 app 卡了——而真模型偶尔真会写出认不得的链接。
        guard let link = JarvisLink(url) else {
            linkMiss = "这个链接我认不出来（\(url.absoluteString)）。再问一次一般就好了。"
            return
        }
        Haptics.tap()
        switch link.kind {
        case .item:
            // 找不到多半是这条已经改过（行号变了）或者做完删掉了。
            // 说一句，别让人以为点坏了。
            if let it = findItem(link.id) {
                notified = it
            } else {
                linkMiss = "这条日程已经变了或者不在了。再问一句「今天什么安排」就能拿到最新的。"
            }
        case .mail:
            Task { await openMailByKey(link.id) }
        case .chart:
            // 独占一行的图表会直接画成卡片（点卡片进详情）；夹在句子里的那种
            // 就靠这条路——同一个链接两种摆法，都得能用。
            if let v = store.snapshot.health?.vitals.first(where: { $0.kind == link.id }) {
                openVital = v
            } else {
                linkMiss = "还没有「\(link.id)」的记录。"
            }
        }
    }

    /// 按 key 取那封邮件。
    ///
    /// 清单从本地索引读（不连服务器）——点一下要立刻有反应，而这一封
    /// 刚才收信时就已经存进索引了。
    private func openMailByKey(_ key: String) async {
        let r = await store.mailAsync(op: "list")
        if let err = r.error, !err.isEmpty {
            store.lastError = err
            return
        }
        if let m = r.items.first(where: { $0.key == key }) {
            openMail = m
        } else {
            linkMiss = "这封邮件在本地找不到了，可能已经清理掉。"
        }
    }

    private func findItem(_ ref: String) -> Item? {
        store.snapshot.days.flatMap(\.items).first { $0.ref == ref }
            ?? store.snapshot.todos.first { $0.ref == ref }
    }

    /// 取走 Siri / 快捷指令留下的那件事。
    ///
    /// 说的话直接发出去，不填进输入框等人再按一下——人已经开口说过了，
    /// 再让他确认一遍等于白说。记账则是把表单摆好：科目得挑，而挑科目
    /// 要看到账本里有哪些，Siri 那头给不了。
    private func takeIntent() async {
        guard let task = IntentInbox.take() else { return }
        switch task {
        case .say(let text):
            let q = text.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !q.isEmpty else { return }
            await store.send(q)
        case .spend(let amount):
            // 人已经开口说过「记一笔 35」，再让他自己点一下「+」等于白说。
            spendAmount = amount
            spending = true
        }
    }

    // store.messages 本来就只有一问一答（工具调用在 steps 里单独显示），
    // 不必再滤一遍。空文本的那条留着——只发图不说话时它顶着位置。
    var shownMessages: [ChatMessage] { store.messages }
}

/// 空着的时候给几句现成的，省得第一次打开不知道能说什么。
/// 点一下只是填进输入框，不直接发——让人有机会改。
struct Starters: View {
    let pick: (String) -> Void

    private let examples = [
        "今天还有什么要做的？",
        "把明天上午空出来",
        "这周花了多少钱",
        "记一笔：午饭 35 块",
    ]

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("试试这样说")
                .font(.footnote)
                .foregroundStyle(.secondary)
            ForEach(examples, id: \.self) { e in
                Button { Haptics.tap(); pick(e) } label: {
                    Text(e)
                        .font(.callout)
                        .foregroundStyle(Brand.onSoft)
                        .padding(.horizontal, 14).padding(.vertical, 9)
                }
                .buttonStyle(.plain)
                .glassEffect(.regular.tint(Brand.soft).interactive(), in: .capsule)
                // 点了只是填进输入框，不直接发。看得见的人从上下文能猜到，
                // 读屏的人猜不到——一句话说清就不用试。
                .accessibilityHint("填进输入框，可以改完再发")
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// 标题栏上的「跟谁在聊」：头像 + 字标 + 状态。
///
/// 头像原来挂在每条回复前面。一屏下来同一张脸出现四五次，而它每次只说
/// 同一句话——「这条是 Jarvis 说的」，而这件事气泡的颜色和靠哪边已经
/// 讲清楚了。挪到标题栏只出现一次，正文那栏还宽出 34 点（26 的头像
/// 加 8 的间距）。微信、WhatsApp 的单聊都是这么摆的。
///
/// 忙不忙也归它讲：状态那行在「正在输入…」和 slogan 之间换，两种情况
/// 都占一行，忙起来不会把字标顶上去半行。
struct ChatTitle: View {
    @EnvironmentObject var store: Store

    var body: some View {
        HStack(spacing: 8) {
            // 38 加玻璃那圈 2pt 是 42，inline 标题栏 44pt 高，上下各留 1pt。
            // 再往上加就顶边了——标题栏不会为它长高，只会把它切掉。
            // 它不动；忙不忙看下面那行字。
            GlassAvatar(size: 38)
            VStack(alignment: .leading, spacing: 1) {
                Wordmark(font: .subheadline)
                // **这行必须短。** 原来闲着时摆的是 Brand.slogan（「说一句，
                // 剩下的交给我」），十个字——leading 那一格的宽度还要跟右边
                // 两颗按钮分，那句话一定被切掉后半截。招牌话留在设置页顶上
                // 的 BrandHeader，标题栏只报状态。
                Text(status)
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }
            .lineLimit(1)
            // 两行字在 44pt 里没有富余。不锁住的话 SwiftUI 会压缩行高，
            // 表现就是下面那行被切掉一半——横竖都按自己该有的尺寸排。
            .fixedSize()
        }
        // 头像是装饰，VoiceOver 念一遍名字和这行状态就够。
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Jarvis，" + status)
    }

    /// 状态行。
    ///
    /// **未读数和逾期数原来是输入框上面那排胶囊的角标。** 那排删掉之后它们
    /// 没地方去了，而这两个数恰恰是「要不要现在开口问一句」的依据——摆在
    /// 名字底下比挂在一个要去点的按钮上更早被看到。
    ///
    /// 都是 0 就不写 0：常年挂着「0 封未读」等于让人从此不看这一行。
    private var status: String {
        if store.thinking { return "正在输入…" }
        var parts: [String] = []
        if store.mailUnread > 0 { parts.append("\(store.mailUnread) 封未读") }
        if store.overdueCount > 0 { parts.append("\(store.overdueCount) 条逾期") }
        return parts.isEmpty ? "随时待命" : parts.joined(separator: " · ")
    }
}

struct Bubble: View {
    @EnvironmentObject var store: Store
    let message: ChatMessage
    /// 是不是最后一条。只有最后那条秘书的回答能重答——中间某条重答了，
    /// 后面的对话就接不上了。
    var isLast = false

    var isUser: Bool { message.isUser }

    /// 配图是不是真读得回来。存档还在、图被清掉了的情况下，
    /// 「[图片]」那句话就是唯一剩下的线索，不能连它也藏了。
    private var hasVisibleImage: Bool {
        message.imageRefs.contains { ImageStore.load($0) != nil }
    }

    /// 四个角不一样圆：靠说话人那一侧的下角收紧。
    ///
    /// 四角同圆的话，一串气泡看着像并排的卡片，得靠颜色和左右位置才分得出
    /// 谁在说；收一个角，形状本身就指向说话的人——头像在左，左下角就尖，
    /// 话是从那儿出来的。iMessage、微信都是这个道理。
    static let bigRadius: CGFloat = 16
    static let tightRadius: CGFloat = 5

    /// 回答那一侧的气泡形状。**「正在输入」那颗点气泡用的是同一个**
    /// （见 ThinkingRow）——它们本来就是同一颗气泡的两个时刻，各写一遍
    /// 的话圆角一改就接不上，点变成字的那一下会跳一下形状。
    static let replyShape = UnevenRoundedRectangle(
        topLeadingRadius: Bubble.bigRadius,
        bottomLeadingRadius: Bubble.tightRadius,
        bottomTrailingRadius: Bubble.bigRadius,
        topTrailingRadius: Bubble.bigRadius)

    private var shape: UnevenRoundedRectangle {
        guard isUser else { return Bubble.replyShape }
        return UnevenRoundedRectangle(
            topLeadingRadius: Bubble.bigRadius,
            bottomLeadingRadius: Bubble.bigRadius,
            bottomTrailingRadius: Bubble.tightRadius,
            topTrailingRadius: Bubble.bigRadius)
    }

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            // 回答这一侧不再挂头像——它挪到标题栏了，见 ChatTitle。
            if isUser { Spacer(minLength: 40) }
            Group {
                if isUser {
                    VStack(alignment: .trailing, spacing: 6) {
                        // 发过的图摆在话的上面，跟拍照那一刻的顺序一致。
                        if !message.imageRefs.isEmpty {
                            ForEach(message.imageRefs, id: \.self) { ref in
                                if let img = ImageStore.load(ref) {
                                    Image(uiImage: img)
                                        .resizable()
                                        .scaledToFill()
                                        .frame(maxWidth: 200, maxHeight: 200)
                                        .clipShape(RoundedRectangle(cornerRadius: 10))
                                }
                            }
                        }
                        // 只发了图没说话时 content 是「[图片]」，图既然画出来了
                        // 就不必再写一遍；图读不回来的话这句话就是唯一的线索。
                        if !(message.text == "[图片]" && hasVisibleImage) {
                            Text(message.text)
                        }
                    }
                } else {
                    MarkdownText(text: message.text)
                }
            }
                .padding(10)
                .background(isUser ? Brand.primary : Brand.soft, in: shape)
                .foregroundStyle(isUser ? Color.white : Brand.onSoft)
                .textSelection(.enabled)
                // 左右对齐和颜色是给眼睛分的，VoiceOver 只听得到一串
                // 文字，一问一答连在一起读完全分不出谁在说话。
                .accessibilityLabel(isUser ? "你说：\(message.text)"
                                           : "Jarvis 说：\(message.text)")
                // 长按：抄走，或者换个说法再问一遍。重发对用户自己那句
                // 尤其有用——秘书会错意时，改一个词比重打一整句快得多。
                .contextMenu {
                    Button {
                        UIPasteboard.general.string = message.text
                        Haptics.tap()
                    } label: {
                        Label("复制", systemImage: "doc.on.doc")
                    }
                    if isUser {
                        Button {
                            Task { await store.send(message.text) }
                        } label: {
                            Label("再问一遍", systemImage: "arrow.clockwise")
                        }
                        .disabled(store.thinking)
                    } else if isLast {
                        // 答偏了、答得太啰嗦、或者中途被打断只答了一半——
                        // 以前唯一的办法是把原话重新打一遍。只给最后那条：
                        // 中间某条重答了，后面的对话就接不上了。
                        Button {
                            Task { await store.retryLast() }
                        } label: {
                            Label("重新生成", systemImage: "arrow.clockwise")
                        }
                        .disabled(store.thinking)
                    }
                }
            if !isUser { Spacer(minLength: 40) }
        }
    }
}

/// 干活过程。原来只显示最后一步，清理邮件那种「一口气动了好几封」的事
/// 结束后完全看不出它到底动了什么。现在整条轨迹都留着，点一下展开。
struct ThinkingRow: View {
    @EnvironmentObject var store: Store
    let steps: [Step]
    @State private var expanded = false

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            VStack(alignment: .leading, spacing: 6) {
                // **这颗气泡就是待会儿装回答的那颗**：同一个 replyShape、
                // 同一个底色、同样贴着左边。回答到了就是点换成字，气泡
                // 本身不动——WhatsApp 的「对方正在输入」就是这么接上的。
                // 原先这儿是头像加转圈，回答出来时那一行整块消失、
                // 气泡在下面另起一行，等于告诉人「刚才那个不算」。
                Button { expanded.toggle() } label: {
                    TypingDots()
                        .padding(.horizontal, 14)
                        .padding(.vertical, 13)
                        .background(Brand.soft, in: Bubble.replyShape)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("正在办：" + headline)

                // **点在跳就不再写一遍「想想…」**：那句话跟动画说的是同一件事，
                // 占一行只为了重复一遍。真拿到了步骤名（「正在查日程」）才值得
                // 显示——那是动画说不出来的信息。
                if let step = stepLabel {
                    HStack(spacing: 6) {
                        Text(step).font(.caption).foregroundStyle(.secondary)
                        if steps.count > 1 {
                            Image(systemName: expanded ? "chevron.up" : "chevron.down")
                                .font(.caption2).foregroundStyle(.tertiary)
                        }
                    }
                    .accessibilityHidden(true)
                }

                if expanded {
                    VStack(alignment: .leading, spacing: 4) {
                        ForEach(Array(steps.enumerated()), id: \.offset) { StepLine(step: $1) }
                    }
                    .padding(.leading, 2)
                }
            }

            Spacer(minLength: 8)

            // 一轮可能要跑一分钟。中途改主意总得能喊停——桌面端按 esc。
            Button { store.stopThinking(); Haptics.tap() } label: {
                Image(systemName: "stop.fill")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .frame(width: 30, height: 30)
            }
            .buttonStyle(.plain)
            .glassEffect(.regular.interactive(), in: .circle)
            // 玻璃圆还是 30，但**能按到的地方**撑到 44——这是 HIG 的下限，
            // 而这颗按钮偏偏是着急的时候要点的那颗。
            .frame(width: 44, height: 44)
            .contentShape(Circle())
            .accessibilityLabel("停下")
        }
    }

    /// 显示用：没有步骤就不显示。返回 nil 那一路交给跳动的点去说。
    private var stepLabel: String? {
        guard let last = steps.last else { return nil }
        let n = steps.count > 1 ? "（第 \(steps.count) 步）" : ""
        return label(for: last) + n
    }

    /// 念给 VoiceOver 听的。这儿反倒需要那句兜底——屏幕上是三个跳动的点，
    /// 读屏软件什么也读不到，「想想…」是它唯一能听见的状态。
    private var headline: String {
        stepLabel ?? "想想…"
    }

    func label(for step: Step) -> String {
        step.kind == "result" ? step.text : step.display
    }
}

/// 过程里的一行。做了什么用主色，做完的结果用次要色，出错标红，
/// 自动做掉的（清理邮件那类可逆动作）单独标出来——那些没问过你。
struct StepLine: View {
    let step: Step

    var body: some View {
        HStack(alignment: .top, spacing: 5) {
            Image(systemName: icon)
                .font(.system(size: 9))
                .foregroundStyle(color)
                .padding(.top, 3)
                .accessibilityHidden(true)
            Text(text)
                .font(.caption2)
                .foregroundStyle(step.kind == "error" ? Color.red : .secondary)
                .lineLimit(2)
        }
        .accessibilityElement(children: .combine)
    }

    private var text: String {
        switch step.kind {
        case "tool":  return step.display
        case "auto":  return step.display + "：" + step.text + "（没问你，可逆）"
        default:      return step.text.isEmpty ? step.display : step.text
        }
    }

    private var icon: String {
        switch step.kind {
        case "tool":   return "circle.fill"
        case "auto":   return "bolt.fill"
        case "error":  return "exclamationmark.triangle.fill"
        default:       return "checkmark"
        }
    }

    private var color: Color {
        switch step.kind {
        case "tool":  return Brand.primary
        case "auto":  return .orange
        case "error": return .red
        default:      return .green
        }
    }
}

// MARK: - 零碎

/// 收不回来的动作停在这儿等用户点头。
///
/// 可逆的动作（清理邮件之类）走不到这里——自动整理档下它们直接就做了，
/// 只会在过程里留一条记录。
struct ConfirmSheet: View {
    @EnvironmentObject var store: Store
    let ask: PendingAsk

    var body: some View {
        NavigationStack {
            VStack(alignment: .leading, spacing: 16) {
                HStack(spacing: 10) {
                    Image(systemName: ask.irreversible
                          ? "exclamationmark.triangle.fill" : "questionmark.circle.fill")
                        .font(.title2)
                        .foregroundStyle(ask.irreversible ? Color.orange : Brand.primary)
                    Text(ask.title).font(.headline)
                }

                if !ask.detail.isEmpty {
                    ScrollView {
                        Text(ask.detail)
                            .font(.callout)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .textSelection(.enabled)
                    }
                }

                if ask.irreversible {
                    Label("这一步做了收不回来", systemImage: "arrow.uturn.backward.slash")
                        .font(.footnote)
                        .foregroundStyle(.orange)
                }

                Spacer(minLength: 0)

                HStack(spacing: 12) {
                    Button("算了") { store.answerPending(false) }
                        .buttonStyle(.glass)
                        .controlSize(.large)
                        .frame(maxWidth: .infinity)
                    Button(ask.irreversible ? "确认发出" : "去吧") {
                        // 收不回来的事给一下更重的——手指离开屏幕的瞬间
                        // 就该知道「这下真发出去了」。
                        ask.irreversible ? Haptics.warning() : Haptics.tap()
                        store.answerPending(true)
                    }
                    .buttonStyle(.glassProminent)
                    .controlSize(.large)
                    .tint(ask.irreversible ? .orange : Brand.primary)
                    .frame(maxWidth: .infinity)
                }
            }
            .padding()
            .navigationTitle(ask.tool)
            .navigationBarTitleDisplayMode(.inline)
            .interactiveDismissDisabled()
        }
        .presentationDetents([.medium])
    }
}

/// 小标题。健康那几块在用；别的页面走 List 的原生 Section。
///
/// 放这儿是因为 LoadingHint / ErrorBar 这些共享组件都在这个
/// 文件里——收在某个功能页里的话，下一个要用的人找不到，就又写一个。
struct SectionLabel: View {
    let text: String
    var icon: String?

    var body: some View {
        HStack(spacing: 6) {
            if let icon {
                Image(systemName: icon).font(.caption2)
            }
            Text(text).font(.subheadline.weight(.semibold))
        }
        .foregroundStyle(.secondary)
        .padding(.horizontal, 4)
    }
}

/// 读失败得说出来。原来只有设置页和秘书页显示 lastError，日程/待办/财务
/// 下拉刷新失败什么都不显示——转圈停了、数字没变，看着就像「本来就没变化」。
struct ErrorBar: View {
    @EnvironmentObject var store: Store

    var body: some View {
        if let err = store.lastError, !err.isEmpty {
            HStack(alignment: .top, spacing: 6) {
                Image(systemName: "exclamationmark.triangle.fill")
                    .accessibilityHidden(true)
                Text(err).lineLimit(3)
                Spacer(minLength: 0)
                // 能再试一次的就给一颗。以前这条子只能关掉——读 iCloud
                // 超时、收信断在半路，看完那句话人能做的只有把它关了，
                // 然后自己找个地方下拉刷新，而出错的地方未必有下拉。
                if let again = store.retry {
                    Button {
                        store.lastError = nil
                        Task { await again() }
                    } label: {
                        Text("重试")
                            .font(.caption.weight(.semibold))
                            .padding(.horizontal, 10)
                            .frame(height: 30)
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                }
                Button { store.lastError = nil } label: {
                    Image(systemName: "xmark")
                        .frame(width: 30, height: 30)
                        .contentShape(Rectangle())
                }
                .accessibilityLabel("关掉")
            }
            .font(.caption)
            .foregroundStyle(.white)
            .padding(.horizontal, 14).padding(.vertical, 10)
            .glassEffect(.regular.tint(.red), in: .rect(cornerRadius: Brand.smallRadius))
            .padding(.horizontal, 12)
            .padding(.top, 6)
        }
    }
}

/// 第一次进来要读 iCloud、下载占位符，可能要等几秒。
/// 不给指示的话就是一屏空白，像是数据没了。
struct LoadingHint: View {
    @EnvironmentObject var store: Store
    let isEmpty: Bool
    var text = "正在读 iCloud…"

    var body: some View {
        // 原来还有个 busy 参数，让邮件页传自己的「正在收信」进来。那一页
        // 没了之后只剩读 iCloud 这一条路，参数也就跟着去掉了。
        if store.loading && isEmpty {
            VStack(spacing: 12) {
                // 静的。旁边那行「正在读 iCloud…」已经说清在等什么了，
                // 头像不必再动一遍。
                Avatar(size: 56)
                Text(text).font(.footnote).foregroundStyle(.secondary)
            }
            // 「正在读」要念出来，不然屏幕上什么都没有，读屏的人不知道
            // 是在等还是空了。
            .accessibilityElement(children: .combine)
            .accessibilityLabel(text)
        }
    }
}

/// 设置的入口。**只挂在对话那一页上。**
///
/// 日程、邮件、财务、健康原先各挂了一个——那会儿它们是并排的标签页，
/// 不各带一个就没别的路进设置。现在它们是从对话里掀起来的卡片，右上角
/// 已经有「关掉」了，再摆个齿轮就是两个按钮挤在一起，而且点进去还得
/// 关两层才回得到刚才那一页。
struct SettingsLink: ToolbarContent {
    @State private var open = false

    var body: some ToolbarContent {
        ToolbarItem(placement: .topBarTrailing) {
            Button { open = true } label: { Image(systemName: "gearshape") }
                .accessibilityLabel("设置")
                .sheet(isPresented: $open) { SettingsView() }
        }
    }
}

/// 发送键：实心圆加一支白箭头，照 iOS 消息那颗。
///
/// **实心，不是玻璃。** 它现在坐在输入框（本身就是一块玻璃胶囊）里面，
/// 玻璃套玻璃会在边缘叠出一道亮边，而且透过去的是输入框的底不是页面的底，
/// 蓝色出不来。跟标题栏那个「头像的圆套在玻璃圆里」是同一类毛病。
///
/// 尺寸跟着字号走：写死的话，输入框在无障碍档位下长高了，这颗还是原样，
/// 看着像掉在坑里。
struct SendButton: View {
    let action: () -> Void

    /// **占位跟 MicButton 必须一模一样**，所以基数取 `Brand.inputSlot`、字号档
    /// 也取同一个（`.headline`），底距两边都是 5pt 且都自带。
    ///
    /// 这两颗轮着占同一格。占位不一样的话，打第一个字那一下输入框会跟着长高或
    /// 矮一截——手指还在键盘上，整条输入区先抖一下。原先这儿跟着字号缩放、那边
    /// 写死 34，而这段注释当时就已经在说「必须一模一样」了。
    @ScaledMetric(relativeTo: .headline) private var box: CGFloat = Brand.inputSlot

    var body: some View {
        Button(action: action) {
            Image(systemName: "arrow.up")
                .font(.system(size: box * 0.44, weight: .bold))
                .foregroundStyle(.white)
                // 蓝圆比方格小一圈，留出呼吸；能按到的是整个 34 的方格。
                .frame(width: box * 0.88, height: box * 0.88)
                .background(Brand.primary, in: .circle)
                .frame(width: box, height: box)
                .contentShape(Circle())
        }
        .buttonStyle(.plain)
        .padding(.bottom, 5)
        .accessibilityLabel("发送")
    }
}
