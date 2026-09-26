import SwiftUI
import UniformTypeIdentifiers

// 设置页重排过一次。原先是一整张 Form 平铺九个 section（品牌头、模型、存着的几套、
// 关于你、提醒、端上模型、邮箱、数据、现在读到的、版本），有三个毛病：
//
// **一、两套规矩混在一页里。** 右上角有个 ✓「存」，可提醒开关、切模型套、选目录
// 都是改完立刻生效的。左上那个 ✕ 写着「取消」，却撤销不了已经生效的那些——它在
// 骗人。现在全页只有一条规矩：**改完就生效**，右上只剩「完成」（关掉而已）。
// iOS 自家「设置」就是这么做的，没有保存按钮。
//
// **二、第一次用的人会卡住。** 唯一的真门槛是模型密钥，而界面上它只是一个空的
// 密码框，没有任何地方说它从哪儿来。现在顶上一行直说还差什么、点一下就到；模型
// 那页给出「去 xxx 领密钥」的链接（地址跟着 Go 那份服务商表走）。
//
// **三、没有层级。** 九个 section 一样重，而其中两个是诊断信息（端上模型可不可用、
// 现在读到几条日程）。现在顶层只有「一行状态 + 五个入口 + 关于」，诊断收进「数据」
// 那一页——它们回答的是「这台设备上装成什么样了」，不是每天要调的东西。

struct SettingsView: View {
    @EnvironmentObject var store: Store
    @Environment(\.dismiss) private var dismiss

    /// 服务商清单从 Go 那边拿（`Bridge.providers()`），不在这儿抄一份。
    /// 抄的那份不会跟着桌面端更新——上一版就留着一个早已删掉的服务商。
    @State private var providers: [ProviderInfo] = []

#if DEBUG
    /// 跟 JARVIS_UI_VAULT 同一个口子：让某一层直接推出来，好在模拟器里看。
    /// 设置的子页要点一下才进得去，而 simctl 点不了屏幕。
    @State private var debugToModel =
        ProcessInfo.processInfo.environment["JARVIS_UI_SCREEN"] == "model"
#endif

    var body: some View {
        NavigationStack {
            List {
                Section { readyRow }

                Section {
                    NavigationLink {
                        ModelSettings(providers: providers)
                    } label: {
                        SettingRow(icon: "sparkles", tint: Brand.primary,
                                   title: "模型", value: modelSummary)
                    }
                    NavigationLink {
                        MailAccountsView()
                    } label: {
                        SettingRow(icon: "envelope", tint: .orange, title: "邮箱",
                                   value: store.settings.mail.isEmpty
                                        ? "没配" : "\(store.settings.mail.count) 个")
                    }
                    NavigationLink {
                        RemindSettings()
                    } label: {
                        SettingRow(icon: "bell", tint: .red, title: "提醒",
                                   value: remindSummary)
                    }
                    NavigationLink {
                        AboutYouSettings()
                    } label: {
                        SettingRow(icon: "person.text.rectangle", tint: .teal,
                                   title: "关于你",
                                   value: store.settings.profile.isEmpty ? "没写" : "已写")
                    }
                    NavigationLink {
                        DataSettings()
                    } label: {
                        SettingRow(icon: "folder", tint: .blue, title: "数据",
                                   value: store.vault.root?.lastPathComponent ?? "没选")
                    }
                }

                // 最底下这一行是**承诺**，不是版权声明：欢迎页那句话用户只看得到
                // 一次，可「文件是你自己的」正是这个 app 跟别家最不一样的地方，
                // 得留个随时看得见的地方。
                Section {
                    LabeledContent("版本", value: Brand.version)
                } footer: {
                    Text(Brand.subtitle)
                }
            }
            .navigationTitle("设置")
            .navigationBarTitleDisplayMode(.inline)
#if DEBUG
            .navigationDestination(isPresented: $debugToModel) {
                ModelSettings(providers: providers)
            }
#endif
            .toolbar {
                // **只有「完成」，没有「存」。** 这一页每一项都是改完就生效的，
                // 再摆一个保存按钮就是在暗示「不按就不算数」。
                ToolbarItem(placement: .confirmationAction) {
                    Button("完成") { dismiss() }.fontWeight(.semibold)
                }
            }
        }
        .onAppear { if providers.isEmpty { providers = Bridge.providers() } }
    }

    // MARK: - 顶上那一行

    /// 能不能用，差什么，点一下去哪儿补。
    ///
    /// **这是整页最要紧的一行。** 第一次进来的人不需要知道有几个 section，他只需要
    /// 知道「现在能用吗、不能的话我该干什么」。差东西的时候它是一条可点的提示，
    /// 齐了的时候它退成一行安静的状态。
    @ViewBuilder
    private var readyRow: some View {
        if store.vault.root == nil {
            NavigationLink {
                DataSettings()
            } label: {
                StatusRow(ok: false, title: "还没选存放目录",
                          detail: "日程和账本放在 iCloud 里，两端共用一份")
            }
        } else if store.settings.apiKey.isEmpty && !isLocalProvider {
            NavigationLink {
                ModelSettings(providers: providers)
            } label: {
                StatusRow(ok: false, title: "还差一个模型密钥",
                          detail: "填上就能开口说话了，点这里去填")
            }
        } else {
            StatusRow(ok: true, title: "一切就绪", detail: modelSummary)
        }
    }

    private var isLocalProvider: Bool {
        providers.first { $0.name == store.settings.model.provider }?.local ?? false
    }

    private var modelSummary: String {
        let m = store.settings.model
        let info = providers.first { $0.name == m.provider }
        let name = m.model.isEmpty ? (info?.model ?? "预设") : m.model
        return m.provider.isEmpty ? "没选" : "\(m.provider) · \(name)"
    }

    private var remindSummary: String {
        let s = store.settings
        if !s.remind && !s.brief { return "关" }
        var parts: [String] = []
        if s.remind { parts.append(s.remindLead == 0 ? "准点" : "提前 \(s.remindLead) 分") }
        if s.brief { parts.append("早上 \(s.briefHour) 点") }
        return parts.joined(separator: " · ")
    }
}

// MARK: - 两种行

/// 顶上那一行的样子。差东西时是橙的、可点；齐了是绿的、安静。
struct StatusRow: View {
    let ok: Bool
    let title: String
    let detail: String

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: ok ? "checkmark.circle.fill" : "exclamationmark.circle.fill")
                .font(.title2)
                .foregroundStyle(ok ? Color.green : Color.orange)
            VStack(alignment: .leading, spacing: 2) {
                Text(title).font(.headline)
                Text(detail).font(.caption).foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, 4)
        // 读屏软件听到的是一句话，不是「对勾、一切就绪、deepseek」三段。
        .accessibilityElement(children: .combine)
        .accessibilityLabel("\(title)，\(detail)")
    }
}

/// 一个入口：左边图标、中间名字、右边当前值。
///
/// 图标用 iOS 设置那种**带底色的圆角方块**，不是光秃秃的符号——一列七八行时
/// 颜色是最快的定位手段，而纯符号扫过去都一样。
struct SettingRow: View {
    let icon: String
    let tint: Color
    let title: String
    var value: String = ""

    var body: some View {
        HStack {
            Image(systemName: icon)
                .font(.system(size: 14, weight: .semibold))
                .foregroundStyle(.white)
                .frame(width: 28, height: 28)
                .background(tint, in: RoundedRectangle(cornerRadius: 7))
            Text(title)
            Spacer()
            if !value.isEmpty {
                Text(value)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .truncationMode(.middle)
            }
        }
    }
}

// MARK: - 模型

struct ModelSettings: View {
    @EnvironmentObject var store: Store
    /// 上一层传下来的服务商清单。**空的时候自己再取一次**——这一页离了它
    /// 什么都选不了，而调用方可能还没取到（深链直接推进来就是这种）。
    let passedProviders: [ProviderInfo]
    @State private var fetched: [ProviderInfo] = []

    private var providers: [ProviderInfo] {
        passedProviders.isEmpty ? fetched : passedProviders
    }

    init(providers: [ProviderInfo]) { passedProviders = providers }

    @State private var provider = ""
    @State private var model = ""
    @State private var baseURL = ""
    @State private var apiKey = ""
    @State private var newName = ""
    @State private var checking = false
    @State private var check: ModelCheck?

    private var info: ProviderInfo? { providers.first { $0.name == provider } }

    var body: some View {
        List {
            Section {
                Picker("服务商", selection: $provider) {
                    ForEach(providers) { p in
                        Text(p.vision ? "\(p.name) · 看图" : p.name).tag(p.name)
                    }
                }
                .onChange(of: provider) { _, name in
                    // 换服务商就把上一家的模型名和地址清掉——留着的话会拿豆包的
                    // 模型名去问 deepseek，报一个看不懂的 400。
                    guard let p = providers.first(where: { $0.name == name }) else { return }
                    model = ""
                    baseURL = ""
                    check = p.vision ? nil : ModelCheck(ok: false, message: "这个模型看不了图片")
                    save()
                }
                if info?.local != true {
                    SecureField("密钥", text: $apiKey)
                        .autocorrectionDisabled()
                        .textInputAutocapitalization(.never)
                        .onSubmit(save)
                }
            } header: {
                Text("用哪个模型")
            } footer: {
                // **整个配置流程只有这一个真门槛。** 原先这儿只有一句「密钥存在
                // 本机钥匙串」——那回答的是「安不安全」，而第一次用的人问的是
                // 「它从哪儿来」。地址跟着 Go 那份服务商表走，不在这边另抄。
                if info?.local == true {
                    Text("本地跑的，不用密钥。先确保 Ollama 已经在这台机器上跑着。")
                } else if let url = info?.console, let link = URL(string: url) {
                    VStack(alignment: .leading, spacing: 4) {
                        Text("还没有密钥？去 \(provider) 的控制台领一个，复制回来粘在上面。")
                        Link("打开 \(provider) 控制台", destination: link)
                            .font(.footnote.weight(.medium))
                    }
                } else {
                    Text("密钥存在本机钥匙串，不进 iCloud。")
                }
            }

            Section {
                Button(action: runCheck) {
                    HStack(spacing: 8) {
                        if checking { ProgressView().controlSize(.small) }
                        Text(checking ? "连接中…" : "测试连接")
                        Spacer()
                        if let c = check, !checking {
                            Label(c.message,
                                  systemImage: c.ok ? "checkmark.circle.fill"
                                                    : "exclamationmark.circle.fill")
                                .font(.caption)
                                .foregroundStyle(c.ok ? Color.green : Color.orange)
                                .labelStyle(.tight)
                                .lineLimit(2)
                                .multilineTextAlignment(.trailing)
                        }
                    }
                }
                .disabled(checking || (apiKey.isEmpty && info?.local != true))
            } footer: {
                // 按钮灰着的时候要说明为什么灰——不说的话人只会以为它坏了。
                if apiKey.isEmpty && info?.local != true {
                    Text("先填密钥，才能试着连一次。")
                } else {
                    Text("真发一次请求，确认密钥和地址都对。不测也能用，只是第一次提问时才会发现填错。")
                }
            }

            // 进阶：绝大多数人一辈子不用动这两项，所以收在后面、说清「留空就行」。
            Section {
                TextField("模型名", text: $model, prompt: Text(info?.model ?? "用预设"))
                    .autocorrectionDisabled()
                    .textInputAutocapitalization(.never)
                    .onSubmit(save)
                TextField("接口地址", text: $baseURL, prompt: Text(info?.baseURL ?? "用预设"))
                    .autocorrectionDisabled()
                    .textInputAutocapitalization(.never)
                    .keyboardType(.URL)
                    .onSubmit(save)
            } header: {
                Text("进阶")
            } footer: {
                Text("两项都留空就行，按服务商的预设来。只有要指定某个特定模型、或者走自建网关时才填。")
            }

            // 配好几套，随手切：干活用贵的，闲聊用便宜的，出门用本地的。
            Section {
                ForEach(store.settings.profiles) { p in
                    Button {
                        useProfile(p)
                    } label: {
                        HStack {
                            VStack(alignment: .leading, spacing: 2) {
                                Text(p.name).foregroundStyle(.primary)
                                Text(profileDesc(p)).font(.caption).foregroundStyle(.secondary)
                            }
                            Spacer()
                            if store.settings.isActive(p) {
                                Image(systemName: "checkmark").foregroundStyle(Brand.accent)
                            }
                        }
                    }
                }
                .onDelete(perform: deleteProfiles)

                HStack {
                    TextField("起个名字，存下现在这套", text: $newName)
                        .autocorrectionDisabled()
                    Button("存") { saveProfile() }
                        .disabled(newName.trimmed.isEmpty || apiKey.trimmed.isEmpty)
                }
            } header: {
                Text("存着的几套")
            } footer: {
                Text(store.settings.profiles.isEmpty
                     ? "干活用贵的、闲聊用便宜的，存下来就能随手切。"
                     : "点一下切换，左滑删除。")
            }
        }
        .navigationTitle("模型")
        .navigationBarTitleDisplayMode(.inline)
        .onAppear {
            if passedProviders.isEmpty, fetched.isEmpty { fetched = Bridge.providers() }
            load()
        }
        // **离开这一页就生效**，全页没有保存按钮。输入框在 onSubmit 时也存一次，
        // 免得有人填完直接去点「测试连接」。
        .onDisappear(perform: save)
    }

    private func load() {
        let s = store.settings
        provider = s.model.provider.isEmpty ? "deepseek" : s.model.provider
        model = s.model.model
        baseURL = s.model.baseURL
        apiKey = s.apiKey
    }

    private func save() {
        var s = store.settings
        s.model.provider = provider
        s.model.model = model.trimmed
        s.model.baseURL = baseURL.trimmed
        s.apiKey = apiKey.trimmed
        s.save()
        store.settings = s
    }

    /// 一套模型显示成一行小字：用哪个服务商的哪个模型。
    private func profileDesc(_ p: ModelProfile) -> String {
        let i = providers.first { $0.name == p.provider }
        let name = p.model.isEmpty ? (i?.model ?? "") + "（预设）" : p.model
        var desc = p.provider + " · " + name
        // 只在用着预设默认模型时才敢说死——自己填了模型名就无从判断了。
        if p.model.isEmpty, let i, i.vision { desc += " · 认得图片" }
        return desc
    }

    /// 切到某一套：整套换过去，界面上那几个框也跟着变。
    private func useProfile(_ p: ModelProfile) {
        var s = store.settings
        s.use(p)
        s.save()
        store.settings = s
        provider = p.provider
        model = p.model
        baseURL = p.baseURL
        apiKey = p.apiKey
        check = nil
        Haptics.success()
    }

    private func saveProfile() {
        save()
        var s = store.settings
        s.saveProfile(named: newName.trimmed)
        s.save()
        store.settings = s
        newName = ""
        Haptics.success()
    }

    private func deleteProfiles(at offsets: IndexSet) {
        var s = store.settings
        s.profiles.remove(atOffsets: offsets)
        s.save()
        store.settings = s
    }

    private func runCheck() {
        save()   // 拿刚填的那份去试，不是上次存下的
        var m = store.settings.model
        m.apiKey = apiKey.trimmed
        checking = true
        check = nil
        Task {
            let r = await Task.detached(priority: .userInitiated) {
                Bridge.checkModel(m)
            }.value
            check = r
            checking = false
            r.ok ? Haptics.success() : Haptics.warning()
        }
    }
}

// MARK: - 提醒

struct RemindSettings: View {
    @EnvironmentObject var store: Store

    @State private var remind = true
    @State private var remindLead = 10
    @State private var brief = true
    @State private var briefHour = 8
    @State private var mailCheckTimes: [String] = []

    var body: some View {
        List {
            Section {
                Toggle(isOn: $remind) {
                    Label("到点提醒", systemImage: "bell")
                }
                .onChange(of: remind) { _, on in
                    if on { Task { _ = await Notifier.requestPermission(); save() } } else { save() }
                }
                if remind {
                    Picker(selection: $remindLead) {
                        ForEach([0, 5, 10, 15, 30, 60], id: \.self) { m in
                            Text(m == 0 ? "准点" : "提前 \(m) 分钟").tag(m)
                        }
                    } label: {
                        Label("什么时候响", systemImage: "clock")
                    }
                    .onChange(of: remindLead) { _, _ in save() }
                }
            } footer: {
                Text("只管排了钟点的事。截止和逾期靠早上那一句。")
            }

            Section {
                // 这是整个 app 唯一一次主动开口。
                Toggle(isOn: $brief) {
                    Label("每天早上说一句", systemImage: "sun.horizon")
                }
                .onChange(of: brief) { _, on in
                    if on { Task { _ = await Notifier.requestPermission(); save() } } else { save() }
                }
                if brief {
                    Picker(selection: $briefHour) {
                        ForEach([6, 7, 8, 9, 10], id: \.self) { h in Text("\(h):00").tag(h) }
                    } label: {
                        Label("几点说", systemImage: "alarm")
                    }
                    .onChange(of: briefHour) { _, _ in save() }
                }
            } footer: {
                Text("今天有什么、哪几件撞了钟点、哪几件逾期了。")
            }

            // 到点提醒收信。手机上只能提醒，不能自己去收——点一下通知 app 起来，
            // 那会儿在前台才真的能连。
            if !store.settings.mail.isEmpty {
                Section {
                    NavigationLink {
                        MailCheckTimes(times: $mailCheckTimes, onChange: save)
                    } label: {
                        HStack {
                            Label("提醒收信", systemImage: "envelope.badge.clock")
                            Spacer()
                            Text(mailCheckTimes.isEmpty ? "关" : mailCheckTimes.joined(separator: " "))
                                .font(.caption.monospacedDigit())
                                .foregroundStyle(.secondary)
                        }
                    }
                } footer: {
                    Text("到点只提醒，点通知打开 app 才会真去收——iOS 不让 app 在后台按点醒来连网。")
                }
            }
        }
        .navigationTitle("提醒")
        .navigationBarTitleDisplayMode(.inline)
        .onAppear {
            let s = store.settings
            remind = s.remind
            remindLead = s.remindLead
            brief = s.brief
            briefHour = s.briefHour
            mailCheckTimes = s.mailCheckTimes
        }
    }

    private func save() {
        var s = store.settings
        s.remind = remind
        s.remindLead = remindLead
        s.brief = brief
        s.briefHour = briefHour
        s.mailCheckTimes = mailCheckTimes
        s.save()
        store.settings = s
        Task { await store.refreshReminders() }
    }
}

// MARK: - 关于你

struct AboutYouSettings: View {
    @EnvironmentObject var store: Store
    @State private var profile = ""

    var body: some View {
        List {
            Section {
                TextField("比如：我是产品经理，最在意面试邀约和账单，不想被促销打扰",
                          text: $profile, axis: .vertical)
                    .lineLimit(4...12)
            } footer: {
                Text("Jarvis 据此判断什么算「要紧」。不写也能用，写了分拣会准很多。")
            }
        }
        .navigationTitle("关于你")
        .navigationBarTitleDisplayMode(.inline)
        .onAppear { profile = store.settings.profile }
        .onDisappear {
            var s = store.settings
            s.profile = profile
            s.save()
            store.settings = s
        }
    }
}

// MARK: - 数据

struct DataSettings: View {
    @EnvironmentObject var store: Store
    @State private var picking = false
    /// 导入之后那一句「导进来多少条」。
    @State private var healthNote = ""

    var body: some View {
        List {
            Section {
                LabeledContent("存放目录", value: store.vault.root?.lastPathComponent ?? "没选")
                Button(store.vault.root == nil ? "选一个目录" : "换一个目录") { picking = true }
            } footer: {
                Text("日程、账本、健康记录都放在这儿。跟电脑上是同一份，两头都能改。")
            }

            // 从「健康」导入。它本来就是**偶尔才干一次**的事（第一次用把几年的
            // 旧数据搬进来），往后每天都是跟 Jarvis 说一句记的，放这儿正合它的频率。
            if HealthKitImporter.isAvailable {
                Section {
                    Button {
                        Task { healthNote = await store.importFromHealthKit(); Haptics.success() }
                    } label: {
                        HStack(spacing: 8) {
                            if store.importingHealth { ProgressView().controlSize(.small) }
                            Text(store.importingHealth ? "正在读…" : "从「健康」导入")
                        }
                    }
                    .disabled(store.importingHealth)
                    if !healthNote.isEmpty {
                        Text(healthNote).font(.caption).foregroundStyle(.secondary)
                    }
                } footer: {
                    Text("把「健康」里已有的体重、步数、睡眠搬进来，只导一次就够。")
                }
            }

            // 诊断。原先这两块各占一个顶层 section，跟「提醒」「邮箱」一样重——
            // 而它们回答的是「这台设备上装成什么样了」，不是每天要调的东西。
            Section {
                LabeledContent("日程", value: "\(store.snapshot.days.reduce(0) { $0 + $1.items.count }) 条")
                LabeledContent("待办", value: "\(store.snapshot.todos.count) 条")
                if let l = store.snapshot.ledger {
                    LabeledContent("账户", value: "\(l.accounts.count) 个")
                }
                HStack {
                    Text("端上模型")
                    Spacer()
                    if LocalModel.available {
                        Label("可用", systemImage: "checkmark.circle.fill")
                            .foregroundStyle(.green).font(.caption).labelStyle(.titleAndIcon)
                    } else {
                        Text(LocalModel.unavailableReason ?? "用不了")
                            .font(.caption).foregroundStyle(.secondary)
                            .multilineTextAlignment(.trailing)
                    }
                }
            } header: {
                Text("现在读到的")
            } footer: {
                Text("端上模型把「明天三点跟老张开会」拆成表单字段，在本机算，不联网。")
            }

            if let err = store.lastError {
                Section {
                    Text(err).font(.footnote).foregroundStyle(.red)
                }
            }
        }
        .navigationTitle("数据")
        .navigationBarTitleDisplayMode(.inline)
        .fileImporter(isPresented: $picking, allowedContentTypes: [.folder]) { result in
            if case .success(let url) = result {
                Task { await store.chooseVault(url) }
            }
        }
    }
}

/// 每天几点提醒收信。可以设好几个。
///
/// **为什么是「好几个点」而不是「每隔 N 小时」**：固定间隔的话半夜也在响。
/// 收信的节奏因人而异，有人早上看一次就够，有人上午下午各一次。
struct MailCheckTimes: View {
    @Binding var times: [String]
    let onChange: () -> Void

    @State private var adding = false
    @State private var newTime = Date()

    var body: some View {
        List {
            Section {
                if times.isEmpty {
                    Text("还没设")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                ForEach(times, id: \.self) { t in
                    Text(t).font(.body.monospacedDigit())
                }
                .onDelete { idx in
                    times.remove(atOffsets: idx)
                    onChange()
                }

                Button {
                    newTime = Calendar.current.date(
                        bySettingHour: 9, minute: 0, second: 0, of: Date()) ?? Date()
                    adding = true
                } label: {
                    Label("加一个时间", systemImage: "plus")
                }
            } footer: {
                Text("到点只提醒，点通知才会收信。")
            }
        }
        .navigationTitle("提醒收信")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar { EditButton() }
        .sheet(isPresented: $adding) { picker }
    }

    private var picker: some View {
        NavigationStack {
            DatePicker("几点", selection: $newTime, displayedComponents: [.hourAndMinute])
                .datePickerStyle(.wheel)
                .labelsHidden()
                .padding()
                .navigationTitle("加一个时间")
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) {
                        Button("算了") { adding = false }
                    }
                    ToolbarItem(placement: .confirmationAction) {
                        Button("加上") { add() }
                    }
                }
        }
        .presentationDetents([.medium])
    }

    private func add() {
        adding = false
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        f.dateFormat = "HH:mm"
        let t = f.string(from: newTime)
        // 已经有了就不重复加——同一个点排两条通知，到点会响两声。
        guard !times.contains(t) else { return }
        times.append(t)
        times.sort()
        onChange()
        Haptics.done()
    }
}
