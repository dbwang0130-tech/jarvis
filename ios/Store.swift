import Foundation
import SwiftUI
import WidgetKit

/// Store 是界面唯一的数据来源：从 iCloud 读文件、交给 Go 解析、
/// 把结果摆出来；对话产生的改动也从这儿写回去。
///
/// 文件读写和 Go 的解析都不轻，全放到后台线程；界面只读 @Published。
@MainActor
final class Store: ObservableObject, AskEvents {
    /// 正等着用户拿主意的那件事。发信这类收不回来的动作会走到这儿。
    @Published var pending: PendingAsk?

    @Published var snapshot = Snapshot.empty {
        didSet { overdueCount = snapshot.todos.filter { $0.overdue }.count }
    }
    @Published var loading = false

    /// 从别的 app 分享进来的图，等着界面来取。
    ///
    /// 跟通知那个标记一个路子：图是系统投递给 app 的（onOpenURL），
    /// 而那会儿界面可能还没建出来，没法直接递给谁。
    @Published var sharedShots: [Data] = []

    /// 点通知进来要办的事。
    ///
    /// 界面看到它就打开邮件页并立刻收一次——通知里说的是「点一下，我这就
    /// 去收」，那就得真去收，而不是把人扔在对话页自己找。办完清空，
    /// 免得下次切回前台又触发一遍。
    @Published var wantsMailCheck = false
    @Published var lastError: String? {
        // 新的一条盖上来时，旧的那个重试办法就不作数了——照着它再试一次，
        // 试的是上一件事。谁报错谁顺手设 retry（`fail(_:retry:)`）。
        didSet { if lastError == nil { retry = nil } }
    }

    /// 出错了怎么再试一次。有的话错误条上会多一颗「重试」。
    ///
    /// 以前那条红条子只能关掉：读 iCloud 超时、写回撞上冲突、收信断在
    /// 半路——看完那句话，人能做的只有关掉它，然后自己找个地方下拉刷新，
    /// 而错误条出现的地方未必有下拉（比如刚进 app 那一屏）。
    @Published var retry: (() async -> Void)?

    /// 报个错，顺带说清这事怎么再试一次。
    func fail(_ message: String, retry again: (() async -> Void)? = nil) {
        lastError = message
        retry = again
    }

    /// 对话记录。界面显示用，同时原样带给 Go 做多轮上下文。
    @Published var messages: [Message] = []
    @Published var steps: [Step] = []
    @Published var thinking = false

    @Published var settings = Settings.load()

    let vault = Vault()

    var isConfigured: Bool { vault.isConfigured }

    init() {
        // 打开就接着上次那段对话。
        messages = SessionStore.load()
    }

    // 手上这份文件内容。对话要用它做入参，也用来判断哪些真的变了。
    private var files: [String: String] = [:]
    private var ledger = ""
    private var memory = ""
    private var health = ""

    /// 工具条上那两个数字。
    ///
    /// 没了标签栏，就没有「扫一眼就知道有没有事」的地方了——这两个数是
    /// 那一眼的替代品。逾期是从快照里现算的；未读要读一次本地邮件索引，
    /// 不连网，跟打开邮件页是同一条路。
    @Published var mailUnread = 0
    /// 逾期几条。
    ///
    /// 存起来而不是每次现算：ToolStrip 坐在 ChatView 的 body 里，
    /// 秘书干活时每来一步都会带着它重画，而待办上限是一千条。
    @Published var overdueCount = 0

    /// 正在跑的那次刷新。下拉、切回前台、首次打开三条路会撞上，
    /// 撞上就跟着等前一个，而不是各读各的文件最后谁写谁赢。
    private var refreshing: Task<Void, Never>?

    /// 秘书干活期间来过刷新请求，等它收工再补一次。
    private var refreshWanted = false

    // MARK: - 读

    func reload() async {
        guard vault.isConfigured else { return }

        // 秘书正拿着改之前的内容在干活。这会儿把文件重读进来，等它回话时
        // 写回的还是基于旧内容算出来的，会把刚同步下来的改动冲掉。
        // 记一笔，等它收工再刷。
        if thinking {
            refreshWanted = true
            return
        }

        if let t = refreshing {
            await t.value
            return
        }
        let t = Task { await doReload() }
        refreshing = t
        await t.value
        refreshing = nil
    }

    private func doReload() async {
        loading = true
        lastError = nil
        defer { loading = false }

        do {
            // 四个文件之间没有任何依赖，各读各的。
            // 串着读的话最坏情况是四份 iCloud 下载等待相加——
            // ensureDownloaded 对没下下来的文件最多干等十秒，四个就是四十秒。
            async let orgs = Task.detached(priority: .userInitiated) { [vault] in
                try vault.readOrgFiles()
            }.value
            async let led = Task.detached(priority: .userInitiated) { [vault] in
                try vault.readLedger()
            }.value
            async let mem = Task.detached(priority: .userInitiated) { [vault] in
                try vault.readMemory()
            }.value
            async let hea = Task.detached(priority: .userInitiated) { [vault] in
                try vault.readHealth()
            }.value
            let (f, l, m, h) = try await (orgs, led, mem, hea)
            files = f
            ledger = l
            memory = m
            health = h

            let snap = await Task.detached(priority: .userInitiated) { [f, l, h] in
                Bridge.snapshot(files: f, ledger: l, health: h)
            }.value
            snapshot = snap
            if let err = snap.error, !err.isEmpty {
                lastError = err
            }
            await refreshReminders()
            publishWidget(snap)
            // 徽标不参与首屏，别让加载指示器等这趟盘读。
            Task { await refreshMailBadge() }
        } catch {
            fail("读不到 iCloud 里的内容：\(error.localizedDescription)") { [weak self] in
                await self?.reload()
            }
        }
    }

    // MARK: - 对话

    /// 让秘书把刚才那句重答一遍。
    ///
    /// 答偏了、答得太啰嗦、或者中途被打断只答了一半——以前唯一的办法是
    /// 把原话重新打一遍（「再问一遍」只在自己说的那条上有）。而这一轮
    /// 的问题就在上面摆着。
    ///
    /// **先把这一轮掐掉再重来**：不摘掉的话，那个答坏了的回答会留在
    /// 上下文里，模型多半会顺着它再说一遍。
    func retryLast() async {
        guard !thinking else { return }
        // 从后往前找最后一次提问，它之后的全扔掉（回答、还有我们自己
        // 插的那些提示）。
        guard let idx = messages.lastIndex(where: { $0.role == "user" }) else { return }
        let question = messages[idx].content
        messages = Array(messages.prefix(idx))
        persist()
        await send(question)
    }

    func send(_ question: String, images: [Data] = []) async {
        let text = question.trimmingCharacters(in: .whitespacesAndNewlines)
        // 光发图不说话也行——「这是什么」有时候不必打字。
        guard !text.isEmpty || !images.isEmpty, !thinking else { return }
        guard settings.model.provider.isEmpty == false,
              settings.apiKey.isEmpty == false else {
            lastError = "还没配模型，去设置里填一下"
            return
        }

        // 这条只为让界面立刻有反应。传给 Go 的 history 必须是**这轮之前**的——
        // agent.Ask 自己会把提问追加进去，把它一起传过去就成了两条。
        let history = messages.filter { $0.role != "__local" }
        // 存档里只留一句「[图片]」，不留图本身：会话存档会反复读写，
        // 塞几张几百 KB 的图进去，它就不再是个能随手读的小文件了。
        let shown = images.isEmpty ? text
                                   : (text.isEmpty ? "[图片]" : "[图片] " + text)
        messages.append(Message(role: "user", content: shown))
        steps = []
        thinking = true
        defer { thinking = false }

        var model = settings.model
        model.apiKey = settings.apiKey

        var input = AskInput(files: files, ledger: ledger, question: text,
                             history: history, model: model, profile: settings.profile,
                             memory: memory, health: health,
                             accounts: settings.mailAccounts())
        input.images = images.map { "data:image/jpeg;base64," + $0.base64EncodedString() }

        let out = await Task.detached(priority: .userInitiated) { [weak self] in
            Bridge.ask(input, events: self)
        }.value

        if let err = out.error, !err.isEmpty {
            lastError = err
            messages.append(Message(role: "__local", content: "没办成：\(err)"))
            persist()
            await refreshIfMissed()
            return
        }

        messages = out.history
        steps = out.steps ?? []
        persist()

        // 秘书改了东西就写回 iCloud，然后重新解析一遍。
        if (out.files?.isEmpty == false) || (out.ledger?.isEmpty == false)
            || (out.memory?.isEmpty == false) || (out.health?.isEmpty == false) {
            await writeBack(org: out.files ?? [:], ledger: out.ledger,
                            memory: out.memory, health: out.health)
        }

        await refreshIfMissed()
    }

    /// 干活期间来过的刷新请求，收工后补上。
    /// 得先把 thinking 放开——defer 还没轮到，不放开 reload 会以为它还在忙。
    private func refreshIfMissed() async {
        guard refreshWanted else { return }
        refreshWanted = false
        thinking = false
        await reload()
    }

    /// 干完一步就报上来，不用等整轮结束。
    nonisolated func onStep(kind: String, name: String, label: String, text: String) {
        let step = Step(kind: kind, name: name, label: label, text: text)
        Task { @MainActor [weak self] in self?.steps.append(step) }
    }

    /// 秘书要做一件收不回来的事，问用户。
    ///
    /// **这个方法在后台线程上跑，而且会一直等**——Go 那边 emit 返回后立刻
    /// 就阻塞着收答复，这里不给答案就是整轮卡死。所以无论如何都要 signal：
    /// 弹窗挂了、Store 没了，都算不批。
    nonisolated func onConfirm(tool: String, title: String, detail: String,
                               irreversible: Bool) -> Bool {
        let sem = DispatchSemaphore(value: 0)
        let box = AnswerBox()
        Task { @MainActor [weak self] in
            guard let self else { box.set(false); sem.signal(); return }
            self.pending = PendingAsk(tool: tool, title: title, detail: detail,
                                      irreversible: irreversible) { ok in
                box.set(ok)
                sem.signal()
            }
        }
        sem.wait()
        return box.get()
    }

    /// 用户点了。收起弹窗，把答复送回还在等着的那个后台线程。
    func answerPending(_ ok: Bool) {
        guard let p = pending else { return }
        pending = nil
        p.answer(ok)
    }

    func clearChat() {
        messages = []
        steps = []
        persist()
    }

    /// 存档可能有几百 KB，别占着主线程。
    private func persist() {
        let now = messages
        Task.detached(priority: .utility) { SessionStore.save(now) }
    }

    // MARK: - 从「健康」App 导入

    /// 正在导。
    @Published var importingHealth = false

    /// 把系统「健康」里最近的数据并进来。
    ///
    /// 重复导是安全的：每条带着 HealthKit 的 UUID，Go 那边认得出已经有的。
    /// 所以不记「上次导到哪儿」——那种游标一旦跟真实情况错位（换机、
    /// 恢复备份、事后补录旧数据），就会漏掉一整段而没人发现。
    @discardableResult
    func importFromHealthKit() async -> String {
        guard HealthKitImporter.isAvailable else {
            return "这台设备上没有「健康」"
        }
        guard !importingHealth else { return "" }
        importingHealth = true
        defer { importingHealth = false }

        guard await HealthKitImporter.requestPermission() else {
            return "没能取得「健康」的授权"
        }
        let samples = await HealthKitImporter.read()
        if samples.isEmpty {
            // iOS 有意不告诉 app 读权限被拒了，所以这里分不清是拒了还是
            // 本来就没数据。两种都说到，别让人以为 app 坏了。
            return "没读到数据。要么这段时间「健康」里就没有记录，"
                + "要么没给读的权限——在「设置 → 隐私与安全性 → 健康 → Jarvis」里看一下。"
        }

        let current = health
        let r = await Task.detached(priority: .userInitiated) {
            Bridge.importHealth(health: current, samples: samples)
        }.value

        if let err = r.error, !err.isEmpty {
            lastError = err
            return err
        }
        guard let merged = r.health, !merged.isEmpty else {
            return r.message   // 一条都没新增，不用碰文件
        }
        guard await writeBack(health: merged) else { return lastError ?? "" }
        return r.message
    }

    /// 数一下本地存着几封没读的。只读盘不连网，跟打开邮件页是同一条路。
    /// 读完一封信就把它标成已读。
    ///
    /// 以前没有这一步：正文读出来了，那封信还是未读，工具条上的角标
    /// 一直挂着——人已经读过了，数字却不降。
    ///
    /// 服务器那一趟由 Go 那边尽力而为，这儿不等它：连不上最坏也就是
    /// 下次同步又变回未读，重看一眼而已。
    func markRead(_ key: String) async {
        let r = await mailAsync(op: "mark_read", keys: [key])
        if let err = r.error, !err.isEmpty {
            fail(err) { [weak self] in await self?.markRead(key) }
            return
        }
        mailUnread = r.items.filter { $0.unread }.count
    }

    private func refreshMailBadge() async {
        guard !settings.mail.isEmpty else {
            mailUnread = 0
            return
        }
        let input = mailInput(op: "list", key: "", keys: [])
        let r = await Task.detached(priority: .utility) { Bridge.mail(input) }.value
        mailUnread = r.items.filter { $0.unread }.count
    }

    /// 手上的内容变了，重新解析一遍给界面。
    private func reparse() async {
        let f = files, l = ledger, h = health
        snapshot = await Task.detached(priority: .userInitiated) {
            Bridge.snapshot(files: f, ledger: l, health: h)
        }.value
    }

    /// 按当前这份日程重排提醒。每次读完文件都重排一遍——条目的时间变了、
    /// 标完成了，提醒也得跟着变。
    func refreshReminders() async {
        let s = settings
        await Notifier.reschedule(from: snapshot,
                                  leadMinutes: s.remindLead, enabled: s.remind,
                                  brief: s.brief, briefHour: s.briefHour)
        // 收信提醒单独排：它是每天重复的，不跟着日程走，reschedule 里那趟
        // 全清也不该把它带走。
        await Notifier.scheduleMailChecks(s.mailCheckTimes,
                                          enabled: !s.mailCheckTimes.isEmpty && !s.mail.isEmpty)
    }

    /// 叫停正在跑的那一轮。已经做完的改动会照常写回来，不会白干。
    /// 把今天那几条抄一份给小组件。
    ///
    /// 小组件是独立进程，够不着 iCloud 里的 org 文件——授权是给主 app 的。
    /// 所以每次读完顺手写一份到共享容器，它只读这一份。
    ///
    /// **App Group 没配好也不会出事**：写不进去就是白写一趟（`WidgetBox`
    /// 那边拿不到 defaults 会直接返回），主 app 该怎么跑还怎么跑。
    private func publishWidget(_ snap: Snapshot) {
        var out = WidgetSnapshot()

        // 今天那一天里的条目，加上已经逾期的——逾期的本来就该今天处理，
        // 单看「今天」会漏掉昨天没做完的那些。
        let today = snap.days.first { $0.today }?.items ?? []
        let overdue = snap.todos.filter { $0.overdue && !$0.isDone }
        var seen = Set<String>()
        let all = (overdue + today).filter { seen.insert($0.ref).inserted }

        out.totalToday = all.count
        out.doneToday = all.filter(\.isDone).count
        out.items = all.filter { !$0.isDone }
            .prefix(WidgetSnapshot.maxItems)
            .map { it in
                WidgetItem(ref: it.ref, title: it.title, time: it.time,
                           urgent: it.overdue, done: false, startedAt: it.startedAt)
            }

        // 正在计时的那条不限于今天——真在走的事就该摆在最显眼处。
        if let run = (snap.todos + snap.days.flatMap(\.items)).first(where: { $0.running }) {
            out.running = WidgetItem(ref: run.ref, title: run.title, time: run.time,
                                     urgent: false, done: false, startedAt: run.startedAt)
        }

        if out.items.isEmpty {
            out.emptyHint = out.totalToday > 0 ? "今天的都做完了" : "今天没安排"
        }

        WidgetBox.save(out)
        WidgetCenter.shared.reloadAllTimelines()
    }

    func stopThinking() {
        guard thinking else { return }
        Task.detached(priority: .userInitiated) { Bridge.cancel() }
    }

    /// 手上这些 org 文件的名字，排好序。记一条时可以挑写进哪个。
    var orgFiles: [String] { files.keys.sorted() }

    /// 把一条健康记录标成「录错了」。
    ///
    /// 不是删——医疗记录跟账本一样不改历史。那条留在文件里，只是往后
    /// 不再参与趋势和「最近一次」。
    @discardableResult
    func voidHealth(kind: String, at: Date) async -> Bool {
        guard !thinking else { return false }
        let iso = ISO8601DateFormatter()
        iso.formatOptions = [.withInternetDateTime]
        let stamp = iso.string(from: at)
        let current = health
        let r = await Task.detached(priority: .userInitiated) {
            Bridge.voidHealth(health: current, kind: kind, at: stamp)
        }.value

        if let err = r.error, !err.isEmpty {
            fail(err)
            return false
        }
        guard let updated = r.health, !updated.isEmpty else { return true }
        return await writeBack(health: updated)
    }

    /// 冲掉记错的那一笔。
    ///
    /// 不是删——账本是流水账，记错了要记一笔反向的把它抵掉，原始那笔
    /// 留着。这是 ledger 的规矩，也是复式记账本来的样子。
    @discardableResult
    func reverse(_ txn: TxnBrief) async -> Bool {
        guard !thinking else { return false }
        let book = ledger
        let r = await Task.detached(priority: .userInitiated) {
            Bridge.reverse(ledger: book, index: txn.index,
                           date: txn.date, payee: txn.payee)
        }.value

        if let err = r.error, !err.isEmpty {
            fail(err)
            return false
        }
        guard let updated = r.ledger, !updated.isEmpty else { return true }
        return await writeBack(ledger: updated)
    }

    /// 存一张小票，返回它相对 Jarvis 目录的路径。
    ///
    /// **图片先落盘，再谈识别。** 识别可能失败（模型不支持看图、没网、
    /// 密钥过期），但那张图是用户拍下来的资产，不该因为认不出来就丢掉。
    func keepReceipt(_ data: Data, payee: String, at when: Date) async -> String? {
        do {
            return try await Task.detached(priority: .userInitiated) { [vault] in
                try vault.saveReceipt(data, payee: payee, at: when)
            }.value
        } catch {
            fail("小票存不下来：\(error.localizedDescription)")
            return nil
        }
    }

    /// 让模型认一张小票。
    func readReceipt(_ data: Data) async -> ReceiptRead {
        let model = modelWithKey
        // 只给支出科目：认小票认的是花钱，给一堆资产账户只会让它挑错。
        let accounts = (snapshot.ledger?.accounts ?? [])
            .filter { $0.kind == "expense" }
            .map(\.name)
        return await Task.detached(priority: .userInitiated) {
            Bridge.readReceipt(image: data, model: model, accounts: accounts)
        }.value
    }

    /// 记一笔账。
    ///
    /// 财务页一直是只读的：余额、账户、流水都看得见，可「刚花了 35 块」
    /// 只能开口跟秘书说一句——一个记账 app 在手机上记不了账。
    @discardableResult
    func spend(payee: String, amount: Double, from: String, to: String,
               date: String = "", receipt: String = "") async -> Bool {
        guard !thinking else { return false }   // 秘书正拿着旧账本干活，别插队
        let book = ledger
        let r = await Task.detached(priority: .userInitiated) {
            Bridge.spend(ledger: book, date: date, payee: payee,
                         amount: amount, from: from, to: to, receipt: receipt)
        }.value

        if let err = r.error, !err.isEmpty {
            fail(err)
            return false
        }
        guard let updated = r.ledger, !updated.isEmpty else { return true }
        guard await writeBack(ledger: updated) else { return false }
        return true
    }

    /// 对一个条目直接做一件事（完成/开始/停止/推迟/取消），不经过模型。
    ///
    /// 走的是跟秘书同一套 orgtools 工具，所以改动的语义、撤销点、循环任务
    /// 的处理都跟对话里说一句完全一致，不会出现两套写回逻辑各走各的。
    @discardableResult
    func act(_ op: String, on ref: String, days: Int = 0, title: String = "",
             scheduled: String = "", priority: String = "",
             index: Int = 0, file: String = "", state: String = "",
             text: String = "", refs: [String] = []) async -> Bool {
        guard !thinking else { return false }   // 秘书正拿着旧内容干活，别插队
        let f = files
        let r = await Task.detached(priority: .userInitiated) {
            Bridge.act(files: f, ref: ref, op: op, days: days,
                       title: title, scheduled: scheduled, priority: priority,
                       index: index, file: file, state: state, text: text,
                       refs: refs)
        }.value

        if let err = r.error, !err.isEmpty {
            lastError = err
            return false
        }
        guard let changed = r.files, !changed.isEmpty else { return true }
        guard await writeBack(org: changed) else { return false }
        await refreshReminders()
        return true
    }

    /// 邮件库放本机，**不进 iCloud**。
    ///
    /// 试过把索引放进 iCloud 目录（想跟电脑共用分拣结论），结果重开 app
    /// 邮件全空了——iCloud 那个目录是**安全作用域 URL**，只有在
    /// startAccessingSecurityScopedResource 里面才能碰，而 Go 是直接按
    /// 路径写的，一律被拒。
    ///
    /// 要做也不是不行（把每次调用包进作用域里），但今天没有收益：电脑那边
    /// 还没用这套存储，共用不起来；而一个每次同步都重写的文件放进 iCloud，
    /// 不走 NSFileCoordinator 就是在等冲突副本。等桌面端也接上再说。
    ///
    /// 重复拉取的真正原因是 ~/.config 在 iOS 上写不了，同步进度一直没存下来——
    /// 那个已经修了。
    nonisolated var mailIndexDir: String {
        Paths.support.appendingPathComponent("mail").path
    }

    nonisolated var mailBodyDir: String {
        Paths.support.appendingPathComponent("mail-body").path
    }

    /// 只读本地那一下，不连网，所以可以同步调——打开页面要秒开。
    func mail(op: String, key: String = "", keys: [String] = []) -> MailResult {
        Bridge.mail(mailInput(op: op, key: key, keys: keys))
    }

    /// 要连服务器的那几下，挪到后台。
    func mailAsync(op: String, key: String = "", keys: [String] = []) async -> MailResult {
        let input = mailInput(op: op, key: key, keys: keys)
        return await Task.detached(priority: .userInitiated) {
            Bridge.mail(input)
        }.value
    }

    /// 入参装配只有这一处。同步版和异步版各拼一遍的话，将来加个字段
    /// 要改两处，漏一处就是「同步能用、异步不能用」这种难查的毛病。
    private func mailInput(op: String, key: String, keys: [String]) -> MailInput {
        MailInput(op: op, indexDir: mailIndexDir, bodyDir: mailBodyDir,
                  accounts: settings.mailAccounts(), model: modelWithKey,
                  profile: settings.profile, key: key, keys: keys)
    }

    /// 配置里的模型，密钥现从钥匙串取。
    private var modelWithKey: ModelConfig {
        var m = settings.model
        m.apiKey = settings.apiKey
        return m
    }

    /// 撞车了要说清楚发生了什么、接下来该怎么办。
    ///
    /// 说「保存失败」是不够的——人会以为再点一次就好，而再点一次还是撞。
    /// 得让他知道电脑那头改过、这边的改动没保存、刷新之后重来一次。

    /// 把改动写回 iCloud，写之前先对一遍盘上那份。
    ///
    /// 乐观写回是这个 app 里唯一防「电脑那头同时也改了」的机制，原先在
    /// 三个地方各写了一遍（对话、动作、从「健康」导入），三份已经开始
    /// 不一样了——错误话不同，有的漏了重排提醒。收成一处。
    ///
    /// 返回真表示写成功了；撞上冲突或出错返回假，`lastError` 里有话说。
    private func writeBack(org: [String: String] = [:],
                           ledger newLedger: String? = nil,
                           memory newMemory: String? = nil,
                           health newHealth: String? = nil) async -> Bool {
        // 动手前那份一起交上去。盘上的要是已经变了，说明另一头也改过，
        // 整文件覆盖会把人家的改动抹掉。
        var before = files
        before["ledger.dat"] = ledger
        before["memory.md"] = memory
        before["health.ndjson"] = health
        let expecting = before
        do {
            let conflicts = try await Task.detached(priority: .userInitiated) { [vault] in
                try vault.write(orgFiles: org, ledger: newLedger,
                                memory: newMemory, health: newHealth,
                                expecting: expecting)
            }.value
            if !conflicts.isEmpty {
                lastError = conflictNote(conflicts)
                await reload()
                return false
            }
        } catch {
            fail("改动没写回去：\(error.localizedDescription)")
            return false
        }
        for (name, content) in org { files[name] = content }
        if let newLedger { ledger = newLedger }
        if let newMemory { memory = newMemory }
        if let newHealth { health = newHealth }
        await reparse()
        return true
    }

    private func conflictNote(_ files: [String]) -> String {
        "电脑那头也改了 \(files.joined(separator: "、"))，"
            + "这次的改动没有保存——已经重新读了一遍，请再操作一次。"
    }

    /// 取一条条目的详情。列表里没有正文，点开才去问 Go。
    func detail(of ref: String) async -> DetailData {
        let f = files
        return await Task.detached(priority: .userInitiated) {
            Bridge.detail(files: f, ref: ref)
        }.value
    }

    // MARK: - 目录

    func chooseVault(_ url: URL) async {
        do {
            try vault.remember(url)
            await reload()
        } catch {
            lastError = "目录记不住：\(error.localizedDescription)"
        }
    }
}

/// Settings 是本机的设置。密钥单独存钥匙串，不进 UserDefaults。
struct Settings: Codable {
    var model = ModelConfig()
    var profile = ""
    /// 邮箱账号。密码不在这儿，单独进钥匙串。
    var mail: [MailAccount] = []
    /// 到点提醒，以及提前几分钟。
    var remind = true
    var remindLead = 10
    /// 每天几点提醒收信，形如 ["08:30", "18:00"]。
    ///
    /// 手机上到点只能**提醒**，不能自己去收——iOS 不让 app 在后台按点
    /// 醒来连网。点一下通知 app 起来，那会儿在前台，才真的能连。
    var mailCheckTimes: [String] = []

    /// 每天早上那一条简报，以及几点发。
    ///
    /// 默认开着：这是这个 app 唯一一次**主动开口**。不开的话它只在被问到
    /// 时才说话，而「今天有什么」恰恰是最不该等人问的。
    var brief = true
    var briefHour = 8

    var apiKey: String {
        get { Keychain.read(key: "jarvis.apiKey") ?? "" }
        nonmutating set { Keychain.write(key: "jarvis.apiKey", value: newValue) }
    }

    private static let storeKey = "jarvis.settings"

    enum CodingKeys: String, CodingKey {
        case model, profile, mail, remind, remindLead, brief, briefHour
        case mailCheckTimes = "mail_check_times"
    }

    /// 传给 Go 的那份：现从钥匙串取密码填上，只带配全了的账号。
    func mailAccounts() -> [MailAccountInput] {
        mail.compactMap { a in
            let pw = a.password
            guard !a.host.isEmpty, !a.user.isEmpty, !pw.isEmpty else { return nil }
            return MailAccountInput(
                name: a.name.isEmpty ? a.user : a.name,
                host: a.host, port: a.port, user: a.user, password: pw,
                smtpHost: a.smtpHost, smtpPort: a.smtpPort, from: a.from)
        }
    }

    init() {}

    // 老存档里没有 mail 这一项，缺了就当没配，别让整份设置解不开。
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        model = try c.decodeIfPresent(ModelConfig.self, forKey: .model) ?? ModelConfig()
        profile = try c.decodeIfPresent(String.self, forKey: .profile) ?? ""
        mail = try c.decodeIfPresent([MailAccount].self, forKey: .mail) ?? []
        remind = try c.decodeIfPresent(Bool.self, forKey: .remind) ?? true
        remindLead = try c.decodeIfPresent(Int.self, forKey: .remindLead) ?? 10
        brief = try c.decodeIfPresent(Bool.self, forKey: .brief) ?? true
        briefHour = try c.decodeIfPresent(Int.self, forKey: .briefHour) ?? 8
        mailCheckTimes = try c.decodeIfPresent([String].self, forKey: .mailCheckTimes) ?? []
    }

    static func load() -> Settings {
        guard let data = UserDefaults.standard.data(forKey: storeKey),
              let s = try? JSONDecoder().decode(Settings.self, from: data) else {
            return Settings()
        }
        return s
    }

    func save() {
        guard let data = try? JSONEncoder().encode(self) else { return }
        UserDefaults.standard.set(data, forKey: Self.storeKey)
    }
}
