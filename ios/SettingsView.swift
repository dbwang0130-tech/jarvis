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
    /// models 也要推到模型页——那一层再往里推挑模型那一页。
    @State private var debugToModel =
        ["model", "models"].contains(ProcessInfo.processInfo.environment["JARVIS_UI_SCREEN"] ?? "")
    @State private var debugToMail =
        ProcessInfo.processInfo.environment["JARVIS_UI_SCREEN"] == "mail"
    @State private var debugToPrivacy =
        ProcessInfo.processInfo.environment["JARVIS_UI_SCREEN"] == "privacy"
    @State private var debugToSessions =
        ProcessInfo.processInfo.environment["JARVIS_UI_SCREEN"] == "sessions"
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
                        SessionsView()
                    } label: {
                        SettingRow(icon: "clock.arrow.circlepath", tint: .indigo,
                                   title: "历史对话")
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
                    // 内置一份，离线也看得到。**这不能替代 App Store Connect 里
                    // 那个必填的隐私政策网址**，但那句「我们没有服务器」正是这个
                    // app 的立场，该在手边随时翻得到，而不是只挂在商店页面上。
                    NavigationLink {
                        PrivacyView()
                    } label: {
                        SettingRow(icon: "hand.raised", tint: .gray, title: "隐私政策")
                    }
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
            .navigationDestination(isPresented: $debugToMail) { MailAccountsView() }
            .navigationDestination(isPresented: $debugToPrivacy) { PrivacyView() }
            .navigationDestination(isPresented: $debugToSessions) { SessionsView() }
#endif
            .toolbar {
                // **只有「完成」，没有「存」。** 这一页每一项都是改完就生效的，
                // 再摆一个保存按钮就是在暗示「不按就不算数」。
                ToolbarItem(placement: .confirmationAction) {
                    Button("完成") { dismiss() }.fontWeight(.semibold)
                }
            }
            // 从「历史对话」里读回一段之后，整张设置都得收掉——不然人读完
            // 还盯着设置页，看不见自己刚读回来的那段对话。
            .onReceive(NotificationCenter.default.publisher(for: .jarvisCloseSettings)) { _ in
                dismiss()
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

/// 模型配置清单。
///
/// **跟邮箱那一页同一个形状**：列出来、点加号添一条、点一条改、左滑删。
/// 原先是「顶上编辑一份匿名的当前配置，底下再起个名字存下来」——两套心智
/// 模型搅在一起，那个「存」按钮尤其没道理：人刚填完就在用了，为什么还要
/// 再存一次？现在只有一件事：清单里的一条。用哪条，点一下。
struct ModelSettings: View {
    @EnvironmentObject var store: Store
    /// 上一层传下来的服务商清单。**空的时候自己再取一次**——这一页离了它
    /// 什么都选不了，而调用方可能还没取到（深链直接推进来就是这种）。
    let passedProviders: [ProviderInfo]
    @State private var fetched: [ProviderInfo] = []
    @State private var editing: ModelProfile?

    private var providers: [ProviderInfo] {
        passedProviders.isEmpty ? fetched : passedProviders
    }

    init(providers: [ProviderInfo]) { passedProviders = providers }

    var body: some View {
        List {
            Section {
                ForEach(store.settings.profiles) { p in
                    Button { editing = p } label: { row(p) }
                }
                .onDelete(perform: remove)

                Button { editing = ModelProfile() } label: {
                    Label("加一个模型", systemImage: "plus")
                }
            } footer: {
                Text(store.settings.profiles.isEmpty
                     ? "先加一个。填完服务商和密钥就能用了。"
                     : "点一下切换到那一套，左滑删除。干活用贵的、闲聊用便宜的，随手切。")
            }
        }
        .navigationTitle("模型")
        .navigationBarTitleDisplayMode(.inline)
        .sheet(item: $editing) { ModelEditor(profile: $0, providers: providers) }
        .onAppear {
            if passedProviders.isEmpty, fetched.isEmpty { fetched = Bridge.providers() }
#if DEBUG
            // 跟别的 JARVIS_UI_* 同一个口子：直接把编辑器开出来，
            // 好在模拟器里一路看到挑模型那一页（simctl 点不了屏幕）。
            if ProcessInfo.processInfo.environment["JARVIS_UI_SCREEN"] == "models",
               editing == nil {
                editing = store.settings.profiles.first ?? ModelProfile()
            }
#endif
            // 老版本里那份「游离的当前配置」收进清单，免得一进来看见空列表——
            // 而他的 app 明明正常用着。幂等，重复进不会补出第二条。
            var s = store.settings
            let before = s.profiles.count
            s.adoptCurrentModel()
            if s.profiles.count != before {
                s.save()
                store.settings = s
            }
        }
    }

    /// 清单里的一行：名字、用的是哪家的哪个模型、在用的打勾。
    @ViewBuilder
    private func row(_ p: ModelProfile) -> some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(p.name.isEmpty ? p.provider : p.name).foregroundStyle(.primary)
                Text(desc(p)).font(.caption).foregroundStyle(.secondary)
            }
            Spacer()
            if p.apiKey.isEmpty, !isLocal(p.provider) {
                Image(systemName: "key.slash")
                    .foregroundStyle(.orange)
                    .accessibilityLabel("还没填密钥")
            }
            if store.settings.isActive(p) {
                Image(systemName: "checkmark").foregroundStyle(Brand.accent)
            }
        }
    }

    /// 一行小字：哪家、哪个模型、认不认得图片。
    ///
    /// 只在用着预设默认模型时才敢说死视觉——自己挑了模型名就无从判断了
    /// （那一页有服务商现报的标记，这儿没有）。
    private func desc(_ p: ModelProfile) -> String {
        let info = providers.first { $0.name == p.provider }
        let name = p.model.isEmpty ? (info?.model ?? "默认") : p.model
        var out = p.provider + " · " + name
        if p.model.isEmpty, let info, info.vision { out += " · 视觉" }
        return out
    }

    private func isLocal(_ provider: String) -> Bool {
        providers.first { $0.name == provider }?.local ?? false
    }

    private func remove(_ offsets: IndexSet) {
        var s = store.settings
        s.removeProfiles(at: offsets)
        s.save()
        store.settings = s
    }
}

// MARK: - 改一条模型配置

/// 加一条或改一条模型配置。
///
/// 跟 `MailAccountEditor` 同一个形状：一张表，左上角取消、右上角保存。
/// **没有「离开即生效」那一套**——清单式的页面里，改了不存就该不算数。
struct ModelEditor: View {
    @EnvironmentObject var store: Store
    @Environment(\.dismiss) private var dismiss

    let profile: ModelProfile
    let providers: [ProviderInfo]

    @State private var name = ""
    @State private var provider = ""
    @State private var model = ""
    @State private var baseURL = ""
    @State private var apiKey = ""
    @State private var checking = false
    @State private var check: ModelCheck?

    private var info: ProviderInfo? { providers.first { $0.name == provider } }
    private var isNew: Bool { profile.provider.isEmpty }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Picker("服务商", selection: $provider) {
                        ForEach(providers) { p in
                            Text(p.vision ? "\(p.name) · 视觉" : p.name).tag(p.name)
                        }
                    }
                    .onChange(of: provider) { _, _ in
                        // 换了一家就把上一家的模型名和地址清掉——留着的话会拿
                        // 豆包的模型名去问 deepseek，报一个看不懂的 400。
                        model = ""
                        baseURL = ""
                        check = nil
                    }
                    if info?.local != true {
                        SecureField("密钥", text: $apiKey)
                            .autocorrectionDisabled()
                            .textInputAutocapitalization(.never)
                    }
                } footer: {
                    // **整个配置流程只有这一个真门槛。** 第一次用的人问的不是
                    // 「安不安全」，而是「密钥从哪儿来」。
                    if info?.local == true {
                        Text("本地模型不需要密钥。请先确保 Ollama 已在本机运行。")
                    } else if let url = info?.console, let link = URL(string: url) {
                        VStack(alignment: .leading, spacing: 4) {
                            Text("还没有密钥？在 \(provider) 的控制台创建一个，复制后粘贴到上方。")
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
                        Text("填入密钥后才能测试连接。")
                    } else {
                        Text("会真实发起一次请求，确认密钥和地址无误。不测也能用，只是填错要到第一次提问时才会发现。")
                    }
                }

                Section {
                    // 模型名是挑的，不是打的：下一层问服务商现要一份，
                    // 里面标着哪个支持图像输入。
                    NavigationLink {
                        ModelPicker(config: configForQuery, preset: info?.model ?? "",
                                    picked: $model)
                    } label: {
                        HStack {
                            Text("模型名")
                            Spacer()
                            Text(model.isEmpty ? "用默认" : model)
                                .foregroundStyle(.secondary)
                                .lineLimit(1)
                                .truncationMode(.middle)
                        }
                    }
                    TextField("接口地址", text: $baseURL, prompt: Text(info?.baseURL ?? "用默认"))
                        .autocorrectionDisabled()
                        .textInputAutocapitalization(.never)
                        .keyboardType(.URL)
                } header: {
                    Text("进阶")
                } footer: {
                    Text("两项都留空就行，按服务商的默认来。只有要指定某个特定模型、或者走自建网关时才填。")
                }

                Section("备注") {
                    TextField("留空就用服务商名", text: $name)
                        .autocorrectionDisabled()
                }
            }
            .navigationTitle(isNew ? "加模型" : "改模型")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button { dismiss() } label: { Image(systemName: "xmark") }
                        .accessibilityLabel("取消")
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button { save() } label: { Image(systemName: "checkmark") }
                        .accessibilityLabel("保存")
                        .disabled(provider.isEmpty)
                }
            }
        }
        .onAppear(perform: load)
    }

    /// 拉模型列表要用的那份配置：**界面上现填的**，不是存下来的那份。
    /// 人的习惯是填完密钥直接去挑模型，那会儿还没按保存。
    private func configForQuery() -> ModelConfig {
        var m = ModelConfig()
        m.provider = provider
        m.baseURL = baseURL.trimmed
        m.apiKey = apiKey.trimmed
        return m
    }

    private func load() {
        provider = profile.provider.isEmpty ? (providers.first?.name ?? "deepseek")
                                            : profile.provider
        name = profile.name
        model = profile.model
        baseURL = profile.baseURL
        apiKey = profile.apiKey
    }

    private func save() {
        var p = profile
        p.provider = provider
        p.name = name.trimmed.isEmpty ? provider : name.trimmed
        p.model = model.trimmed
        p.baseURL = baseURL.trimmed
        p.apiKey = apiKey.trimmed   // 存钥匙串，不进 UserDefaults

        var s = store.settings
        s.upsert(p)
        s.save()
        store.settings = s
        Haptics.success()
        dismiss()
    }

    private func runCheck() {
        var m = configForQuery()
        m.model = model.trimmed
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

// MARK: - 挑模型

/// 问服务商它今天有哪些模型，让人挑一个。
///
/// **这一页存在的理由是「别让人打字」。** 模型名打错一个字，换来的是一个看不懂的
/// 404；而第一次配的人压根不知道该填什么，预设表里那个默认名还会过时——到那天
/// 同样是 404。现问一份，挑就是了。
///
/// 三条都要顾着：①**拿不到不是错**，有的服务商没实现这个接口，那就退回手填，
/// 不能把人卡在这儿；②「用预设」永远排第一，它是绝大多数人该选的那个；
/// ③「认不认图片」用厂商报的，不用我们预设表里猜的——手机这端会拍照记账。
struct ModelPicker: View {
    /// 拉列表用的配置。**是个闭包，不是一份快照**：界面上那几个框还在改，
    /// 而这一页是从上一层的 body 里构造出来的——按值传的话，传进来的是
    /// 构造那一刻的样子，人刚粘进去的密钥还没算数，于是一进来就「获取失败」。
    let config: () -> ModelConfig
    /// 这家服务商的预设模型名，用来说清「用预设」到底是哪个。
    let preset: String
    @Binding var picked: String

    @Environment(\.dismiss) private var dismiss
    @State private var list: [ModelEntry] = []
    @State private var loading = false
    @State private var failed = ""
    @State private var typed = ""

    var body: some View {
        List {
            Section {
                row(title: presetTitle, detail: "不指定模型名时使用", on: picked.isEmpty) {
                    choose("")
                }
            } header: {
                Text("默认")
            }

            if loading {
                Section {
                    HStack(spacing: 10) {
                        ProgressView().controlSize(.small)
                        Text("正在获取 \(providerName) 的模型列表…")
                            .foregroundStyle(.secondary)
                    }
                }
            } else if !list.isEmpty {
                Section {
                    ForEach(list) { m in
                        row(title: m.name, detail: detail(m), on: picked == m.id) {
                            choose(m.id)
                        }
                    }
                } header: {
                    Text("\(providerName) 可用模型")
                } footer: {
                    Text("列表由服务商接口实时返回，不是内置的。标注「视觉」的支持图像输入，拍照记账需要选这类模型。")
                }
            }

            // 拿不到就退回手填，别把配置卡死在这一步。
            if !failed.isEmpty {
                Section {
                    TextField("手动填写模型名", text: $typed, prompt: Text(preset))
                        .autocorrectionDisabled()
                        .textInputAutocapitalization(.never)
                        .onSubmit { choose(typed.trimmed) }
                    Button("重试") { fetch() }
                } header: {
                    Text("获取失败")
                } footer: {
                    Text("\(failed)\n该服务商可能未提供模型列表接口。保留「默认」即可正常使用，或在其文档中查到模型名后填入。")
                }
            }
        }
        .navigationTitle("模型名")
        .navigationBarTitleDisplayMode(.inline)
        .onAppear {
            typed = picked
            if list.isEmpty, failed.isEmpty { fetch() }
        }
    }

    /// 一行：左边名字、下面一行小字、选中的打勾。
    @ViewBuilder
    private func row(title: String, detail: String, on: Bool,
                     action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack {
                VStack(alignment: .leading, spacing: 2) {
                    Text(title).foregroundStyle(.primary)
                    if !detail.isEmpty {
                        Text(detail).font(.caption).foregroundStyle(.secondary)
                    }
                }
                Spacer()
                if on { Image(systemName: "checkmark").foregroundStyle(Brand.accent) }
            }
        }
    }

    /// 「默认」那一行的标题。
    ///
    /// 列表拿到之后用服务商给的**显示名**——否则上面写着 deepseek-flash、
    /// 下面写着 DeepSeek-V4.1-Flash，是同一个模型却看着像两个。
    private var presetTitle: String {
        if let m = list.first(where: { $0.id == preset }) { return m.name }
        return preset.isEmpty ? "服务商的默认模型" : preset
    }

    /// 一个模型的那行小字：是不是默认那个、支不支持图像输入、上下文多大、模型 ID。
    ///
    /// 只说服务商真报了的东西——没报窗口就不显示，别为了版面整齐编一个出来。
    private func detail(_ m: ModelEntry) -> String {
        var parts: [String] = []
        // 不点明的话，「默认」那一行跟这一行看着是两个模型，而它们是同一个。
        if m.id == preset { parts.append("默认") }
        if m.vision { parts.append("视觉") }
        if let w = window(m.context) { parts.append("上下文 " + w) }
        if m.name != m.id { parts.append(m.id) }
        return parts.joined(separator: " · ")
    }

    /// 上下文窗口写成人念得出来的样子。
    ///
    /// 按 1024 进位，不按 1000：服务商报的 1048576 正是 1M，除以 1000 得出的
    /// 「1048k」既不是它说的数，也不是任何人会说的话。
    private func window(_ n: Int) -> String? {
        switch n {
        case (1 << 20)...: return "\(n >> 20)M"
        case (1 << 10)...: return "\(n >> 10)k"
        default: return nil
        }
    }

    private func choose(_ id: String) {
        picked = id
        Haptics.success()
        dismiss()
    }

    private var providerName: String { config().provider }

    private func fetch() {
        loading = true
        failed = ""
        let cfg = config()
        Task {
            let r = await Task.detached(priority: .userInitiated) {
                Bridge.listModels(cfg)
            }.value
            list = r.models
            // 空列表跟出错一样处理：都是「这儿挑不出来」，都该露出手填那条路。
            failed = r.models.isEmpty ? (r.message.isEmpty ? "未能获取模型列表。" : r.message) : ""
            loading = false
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
                        Label("几点发", systemImage: "alarm")
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

    var body: some View {
        List {
            Section {
                LabeledContent("存放目录", value: store.vault.root?.lastPathComponent ?? "没选")
                Button(store.vault.root == nil ? "选一个目录" : "换一个目录") { picking = true }
            } footer: {
                Text("日程、账本、健康记录都放在这儿。跟电脑上是同一份，两头都能改。")
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
                    Text("设备端模型")
                    Spacer()
                    if LocalModel.available {
                        Label("可用", systemImage: "checkmark.circle.fill")
                            .foregroundStyle(.green).font(.caption).labelStyle(.titleAndIcon)
                    } else {
                        Text(LocalModel.unavailableReason ?? "不可用")
                            .font(.caption).foregroundStyle(.secondary)
                            .multilineTextAlignment(.trailing)
                    }
                }
            } header: {
                Text("本机状态")
            } footer: {
                Text("设备端模型把「明天三点跟老张开会」拆成表单字段，在本机运行，不联网。")
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
