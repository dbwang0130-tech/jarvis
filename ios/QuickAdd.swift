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

    private func save() {
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
