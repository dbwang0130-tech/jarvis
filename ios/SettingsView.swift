import SwiftUI
import UniformTypeIdentifiers

struct SettingsView: View {
    @EnvironmentObject var store: Store
    @Environment(\.dismiss) private var dismiss

    @State private var provider = ""
    @State private var model = ""
    @State private var baseURL = ""
    @State private var apiKey = ""
    @State private var profile = ""
    @State private var picking = false
    @State private var checking = false
    @State private var remind = true
    @State private var remindLead = 10
    @State private var brief = true
    @State private var briefHour = 8
    @State private var mailCheckTimes: [String] = []
    @State private var check: ModelCheck?

    /// 跟桌面端那份预设表一致。
    private let providers = ["deepseek", "qwen", "kimi", "minimax", "zhipu",
                             "doubao", "spark", "baichuan", "ollama"]

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    BrandHeader()
                        .padding(.vertical, 4)
                    .listRowBackground(Brand.soft)
                }

                Section {
                    Picker("服务商", selection: $provider) {
                        ForEach(providers, id: \.self) { Text($0).tag($0) }
                    }
                    TextField("模型名（留空用预设）", text: $model)
                        .autocorrectionDisabled()
                        .textInputAutocapitalization(.never)
                    SecureField("密钥", text: $apiKey)
                        .autocorrectionDisabled()
                        .textInputAutocapitalization(.never)
                    TextField("接口地址（留空用预设）", text: $baseURL)
                        .autocorrectionDisabled()
                        .textInputAutocapitalization(.never)
                        .keyboardType(.URL)
                    Button {
                        runCheck()
                    } label: {
                        HStack(spacing: 8) {
                            if checking {
                                ProgressView().controlSize(.small)
                            } else {
                                Image(systemName: "bolt.horizontal.circle")
                            }
                            Text(checking ? "连着看看…" : "测试连接")
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
                    .disabled(checking || apiKey.isEmpty)
                } header: {
                    Text("模型")
                } footer: {
                    Text("密钥存在本机钥匙串里，不会跟着 iCloud 走。\n"
                         + "填完点一下「测试连接」，别等到第一次提问才发现填错了。")
                }

                Section("关于你") {
                    TextField("一句话介绍自己，Jarvis 据此判断什么算要紧",
                              text: $profile, axis: .vertical)
                        .lineLimit(2...6)
                }

                Section {
                    Toggle(isOn: $remind) {
                        Label("到点提醒", systemImage: "bell")
                    }
                    .onChange(of: remind) { _, on in
                        if on { Task { _ = await Notifier.requestPermission(); saveRemind() } }
                        else { saveRemind() }
                    }

                    if remind {
                        Picker(selection: $remindLead) {
                            ForEach([0, 5, 10, 15, 30, 60], id: \.self) { m in
                                Text(m == 0 ? "准点" : "提前 \(m) 分钟").tag(m)
                            }
                        } label: {
                            Label("什么时候响", systemImage: "clock")
                        }
                        .onChange(of: remindLead) { _, _ in saveRemind() }
                    }

                    // 这是整个 app 唯一一次主动开口。
                    Toggle(isOn: $brief) {
                        Label("每天早上说一句", systemImage: "sun.horizon")
                    }
                    .onChange(of: brief) { _, on in
                        if on { Task { _ = await Notifier.requestPermission(); saveRemind() } }
                        else { saveRemind() }
                    }

                    if brief {
                        Picker(selection: $briefHour) {
                            ForEach([6, 7, 8, 9, 10], id: \.self) { h in
                                Text("\(h):00").tag(h)
                            }
                        } label: {
                            Label("几点说", systemImage: "alarm")
                        }
                        .onChange(of: briefHour) { _, _ in saveRemind() }
                    }
                    // 到点提醒收信。手机上只能提醒，不能自己去收——
                    // 点一下通知 app 起来，那会儿在前台才真的能连。
                    if !store.settings.mail.isEmpty {
                        NavigationLink {
                            MailCheckTimes(times: $mailCheckTimes, onChange: saveRemind)
                        } label: {
                            HStack {
                                Label("提醒收信", systemImage: "envelope.badge.clock")
                                Spacer()
                                Text(mailCheckTimes.isEmpty ? "关"
                                     : mailCheckTimes.joined(separator: " "))
                                    .font(.caption.monospacedDigit())
                                    .foregroundStyle(.secondary)
                            }
                        }
                    }
                } footer: {
                    Text("「到点提醒」只管排了具体钟点的事。"
                         + "**今天有几件、哪些今天截止、有几件逾期**，靠早上那一句——"
                         + "截止和逾期本来就没有钟点，却最容易忘。\n\n"
                         + "提醒是在打开 app 时一次性排好的——iOS 不让 app 在后台一直醒着。"
                         + "所以在电脑上新加的事，手机上得打开一次才会排上它的提醒。")
                }

                // 端上模型的状态。不摆出来的话，用不了的时候界面只是
                // 少一颗按钮，人不知道是没这功能还是坏了。
                Section {
                    HStack {
                        Label("端上模型", systemImage: "cpu")
                        Spacer()
                        if LocalModel.available {
                            Label("可用", systemImage: "checkmark.circle.fill")
                                .foregroundStyle(.green)
                                .labelStyle(.titleAndIcon)
                                .font(.caption)
                        } else {
                            Text(LocalModel.unavailableReason ?? "用不了")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                                .multilineTextAlignment(.trailing)
                        }
                    }
                } footer: {
                    Text("用它把「明天三点跟老张开会」这种话拆成字段填进表单："
                         + "在这台设备上算，不联网、不花钱、一个字不上传。"
                         + "用不了也不影响别的——那只是省几下打字。")
                }

                Section {
                    NavigationLink {
                        MailAccountsView()
                    } label: {
                        HStack {
                            Label("邮箱", systemImage: "envelope")
                            Spacer()
                            Text(store.settings.mail.isEmpty
                                 ? "没配" : "\(store.settings.mail.count) 个")
                                .foregroundStyle(.secondary)
                        }
                    }
                } footer: {
                    Text("配好之后邮件页就能收信，也能直接跟 Jarvis 说「看看新邮件」。")
                }

                Section("数据") {
                    HStack {
                        Text("iCloud 目录")
                        Spacer()
                        Text(store.vault.root?.lastPathComponent ?? "没选")
                            .foregroundStyle(.secondary)
                    }
                    Button("换一个目录") { picking = true }
                }

                Section {
                    HStack {
                        Text("日程")
                        Spacer()
                        Text("\(store.snapshot.days.reduce(0) { $0 + $1.items.count }) 条")
                            .foregroundStyle(.secondary)
                    }
                    HStack {
                        Text("待办")
                        Spacer()
                        Text("\(store.snapshot.todos.count) 条").foregroundStyle(.secondary)
                    }
                    if let l = store.snapshot.ledger {
                        HStack {
                            Text("账户")
                            Spacer()
                            Text("\(l.accounts.count) 个").foregroundStyle(.secondary)
                        }
                    }
                } header: {
                    Text("现在读到的")
                }

                if let err = store.lastError {
                    Section {
                        Text(err).font(.footnote).foregroundStyle(.red)
                    }
                }

                // 最底下这一行是**承诺**，不是版权声明：欢迎页那句话用户
                // 只看得到一次，可「文件是你自己的」正是这个 app 跟别家
                // 最不一样的地方，得留个随时看得见的地方。
                Section {
                    LabeledContent("版本", value: Brand.version)
                } footer: {
                    Text(Brand.subtitle)
                }
            }
            .navigationTitle("设置")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button { dismiss() } label: {
                        Image(systemName: "xmark")
                    }
                    .accessibilityLabel("取消")
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button { save() } label: {
                        Image(systemName: "checkmark")
                    }
                    .accessibilityLabel("存")
                }
            }
            .fileImporter(isPresented: $picking, allowedContentTypes: [.folder]) { result in
                if case .success(let url) = result {
                    Task { await store.chooseVault(url) }
                }
            }
        }
        .onAppear(perform: load)
    }

    /// 拿界面上当前填的值去试，不是存过的那份——填到一半就能验。
    private func runCheck() {
        var m = store.settings.model
        m.provider = provider
        m.model = model.trimmed
        m.baseURL = baseURL.trimmed
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

    /// 提醒这两项是开关，改完立刻生效，不等「存」那一下——
    /// 拨了开关却没反应会让人以为坏了。
    private func saveRemind() {
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

    private func load() {
        let s = store.settings
        provider = s.model.provider.isEmpty ? "deepseek" : s.model.provider
        model = s.model.model
        baseURL = s.model.baseURL
        profile = s.profile
        apiKey = s.apiKey
        remind = s.remind
        remindLead = s.remindLead
        brief = s.brief
        briefHour = s.briefHour
        mailCheckTimes = s.mailCheckTimes
    }

    private func save() {
        var s = store.settings
        s.model.provider = provider
        s.model.model = model.trimmingCharacters(in: .whitespaces)
        s.model.baseURL = baseURL.trimmingCharacters(in: .whitespaces)
        s.profile = profile
        s.apiKey = apiKey.trimmingCharacters(in: .whitespaces)
        s.save()
        store.settings = s
        dismiss()
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
                    Text("还没设。到点会提醒你，点一下就自动收。")
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
                Text("手机上到点只能**提醒**——iOS 不让 app 在后台按点醒来连网。"
                     + "点一下那条通知，app 会打开邮件页并立刻收一次。\n\n"
                     + "电脑上装了后台提醒的话（`/remind`），那边是真的到点自己收。")
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
