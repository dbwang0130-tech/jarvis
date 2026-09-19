import SwiftUI

/// 直接记一条，不经过对话。
///
/// 「买牛奶」这种一句话的事，打字进对话、等模型跑一趟，比直接记还慢。
/// 需要判断的（「下周找个空档把论文提纲写了」）才该交给秘书。
struct QuickAddView: View {
    @EnvironmentObject var store: Store
    @Environment(\.dismiss) private var dismiss

    @State private var title = ""
    @State private var when: When = .none
    @State private var at = Date()
    @State private var priority = ""
    @State private var file = ""
    @State private var saving = false
    @State private var parsing = false
    /// 这一页自己的听写。记一条这儿没有「发送」那一下，但存下来时
    /// 同样该把麦克风收掉。
    @StateObject private var dictation = Dictation()
    /// 已经认过一次了，别一直挂着那颗按钮——认出来的东西就在下面摆着，
    /// 人改完之后再让他「重认」只会把改动冲掉。
    @State private var parsed = false
    @FocusState private var focused: Bool

    enum When: String, CaseIterable {
        case none = "不排期"
        case today = "今天"
        case tomorrow = "明天"
        case pick = "选个时间"
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("要做什么", text: $title, axis: .vertical)
                        .lineLimit(1...3)
                        .focused($focused)
                        .onSubmit(save)
                        // 说一句，下面的字段自己填好。
                        //
                        // 端上模型只做**提取**，填进表单让人过目，从不直接
                        // 落盘——它把「三点」听成「三十」的时候，那一眼是
                        // 唯一的拦截点。
                        .overlay(alignment: .trailingLastTextBaseline) {
                            MicButton(draft: $title, dictation: dictation).offset(y: 6)
                        }
                }

                if LocalModel.available && !title.trimmed.isEmpty && !parsed {
                    Section {
                        Button {
                            Task { await autofill() }
                        } label: {
                            HStack(spacing: 8) {
                                if parsing {
                                    ProgressView().controlSize(.small)
                                } else {
                                    Image(systemName: "wand.and.sparkles")
                                }
                                Text(parsing ? "读一下…" : "从这句话里认出时间")
                                Spacer(minLength: 0)
                            }
                            .font(.subheadline)
                        }
                        .disabled(parsing)
                    } footer: {
                        Text("在这台设备上认，不联网、不花钱。认完你再看一眼。")
                    }
                }

                Section("什么时候") {
                    Picker("", selection: $when) {
                        ForEach(When.allCases, id: \.self) { Text($0.rawValue).tag($0) }
                    }
                    .pickerStyle(.segmented)
                    .labelsHidden()

                    if when == .pick {
                        DatePicker("时间", selection: $at)
                            .datePickerStyle(.compact)
                    }
                }

                // 有好几个 org 文件时才问——只有一个的话，问「记到哪儿」
                // 是明知故问。Act 那边一直收 file，只是界面没给出来，
                // 于是所有东西都往收件箱里堆。
                if store.orgFiles.count > 1 {
                    Section("记到哪儿") {
                        Picker("", selection: $file) {
                            Text("收件箱").tag("")
                            ForEach(store.orgFiles, id: \.self) { f in
                                Text(f.replacingOccurrences(of: ".org", with: "")).tag(f)
                            }
                        }
                        .labelsHidden()
                    }
                }

                Section("急不急") {
                    Picker("", selection: $priority) {
                        Text("不标").tag("")
                        Text("A 最急").tag("A")
                        Text("B").tag("B")
                        Text("C").tag("C")
                    }
                    .pickerStyle(.segmented)
                    .labelsHidden()
                }
            }
            .navigationTitle("记一条")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button { dismiss() } label: { Image(systemName: "xmark") }
                        .accessibilityLabel("取消")
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button(action: save) {
                        if saving { ProgressView().controlSize(.small) }
                        else { Image(systemName: "checkmark") }
                    }
                    .accessibilityLabel("记下")
                    .disabled(saving || title.trimmed.isEmpty)
                }
            }
        }
        .onAppear { focused = true }
    }

    /// 让端上模型把这句话拆开，填进下面的字段。
    ///
    /// **只填空着的**：人已经挑了日期又去认一遍，认出来的把他挑的冲掉，
    /// 那比不认还糟。
    private func autofill() async {
        let said = title.trimmed
        guard !said.isEmpty, !parsing else { return }
        parsing = true
        defer { parsing = false }

        guard let got = await LocalModel.parseTask(said) else {
            parsed = true   // 认不出来就别再摆那颗按钮了
            return
        }
        parsed = true

        if !got.title.trimmed.isEmpty {
            title = got.title.trimmed
        }
        guard when == .none, let day = parseDay(got.date) else { return }

        // 只说了日期没说钟点，就别自作主张定个时刻——不带时刻的条目
        // 是整天的事，不会到点弹提醒，这对「买牛奶」正合适。
        if let clock = parseClock(got.time) {
            at = Calendar.current.date(bySettingHour: clock.0, minute: clock.1,
                                       second: 0, of: day) ?? day
            when = .pick
        } else {
            at = day
            when = .pick
            // 正好是今天或明天的话，用那两个现成的档，比摆个日期选择器清楚。
            if Calendar.current.isDateInToday(day) { when = .today }
            if Calendar.current.isDateInTomorrow(day) { when = .tomorrow }
        }
        Haptics.done()
    }

    private func parseDay(_ s: String) -> Date? {
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        f.dateFormat = "yyyy-MM-dd"
        return f.date(from: s.trimmed)
    }

    private func parseClock(_ s: String) -> (Int, Int)? {
        let parts = s.trimmed.split(separator: ":")
        guard parts.count == 2, let h = Int(parts[0]), let m = Int(parts[1]),
              (0...23).contains(h), (0...59).contains(m) else { return nil }
        return (h, m)
    }

    private func save() {
        dictation.stop()   // 说完顺手点「记下」，别让麦克风还开着
        let text = title.trimmed
        guard !text.isEmpty, !saving else { return }
        saving = true
        Task {
            let ok = await store.act("create", on: "", title: text,
                                     scheduled: scheduledText, priority: priority,
                                     file: file)
            saving = false
            if ok {
                Haptics.done()
                dismiss()
            } else {
                Haptics.failure()
            }
        }
    }

    /// 排期写成 org 认得的样子。只选了天就不带时刻——
    /// 不带时刻的条目在日程里是整天的事，不会到点弹提醒，这是对的：
    /// 「买牛奶」不需要在某个精确时刻打断你。
    private var scheduledText: String {
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        switch when {
        case .none:
            return ""
        case .today:
            f.dateFormat = "yyyy-MM-dd"
            return f.string(from: Date())
        case .tomorrow:
            f.dateFormat = "yyyy-MM-dd"
            return f.string(from: Date().addingTimeInterval(86400))
        case .pick:
            f.dateFormat = "yyyy-MM-dd HH:mm"
            return f.string(from: at)
        }
    }
}
