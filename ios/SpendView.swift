import SwiftUI

/// 记一笔账。
///
/// 财务页一直是只读的：余额、账户、流水都看得见，可「刚花了 35 块」这件事
/// 只能开口跟秘书说一句——一个记账 app 在手机上记不了账。
///
/// **账户不让人手打**。ledger 的科目名是层级字符串（`Expenses:餐饮`），
/// 手打错一个字就会凭空多出一个账户来，而且要过很久才被发现——那会儿
/// 余额已经分到两个名字底下去了。所以只能从账本里已有的科目里挑；
/// 真要开新科目，那是件该想清楚的事，跟 Jarvis 说一句。
struct SpendView: View {
    @EnvironmentObject var store: Store
    @Environment(\.dismiss) private var dismiss

    /// 账本里已有的科目，按大类分好。
    let accounts: [Account]

    @State private var payee = ""
    @State private var amount = ""
    @State private var from = ""
    @State private var to = ""
    @State private var date = Date()
    @State private var saving = false
    @FocusState private var focused: Bool

    /// 钱从哪儿出：资产和负债（刷信用卡也是花钱）。
    private var sources: [Account] {
        accounts.filter { $0.kind == "asset" || $0.kind == "debt" }
    }

    /// 花在什么上：支出科目。收入不在这儿记——那是另一个方向的事，
    /// 硬塞进同一个表单只会让两边都别扭。
    private var targets: [Account] {
        accounts.filter { $0.kind == "expense" }
    }

    private var money: Double? {
        Double(amount.replacingOccurrences(of: "，", with: "").trimmed)
    }

    private var ready: Bool {
        (money ?? 0) > 0 && !from.isEmpty && !to.isEmpty
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    HStack {
                        Text("金额").foregroundStyle(.secondary)
                        Spacer()
                        TextField("0.00", text: $amount)
                            .keyboardType(.decimalPad)
                            .multilineTextAlignment(.trailing)
                            .font(.title3.monospacedDigit().weight(.medium))
                            .focused($focused)
                    }
                    TextField("买的什么（留空就记科目名）", text: $payee)
                }

                Section("从哪儿出") {
                    picker(sources, selection: $from, empty: "账本里还没有资产账户")
                }

                Section("花在什么上") {
                    picker(targets, selection: $to, empty: "账本里还没有支出科目")
                }

                Section {
                    DatePicker("哪天", selection: $date, displayedComponents: [.date])
                } footer: {
                    Text("记进账本末尾，跟电脑上是同一份。")
                }
            }
            .navigationTitle("记一笔")
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
                    .disabled(saving || !ready)
                }
            }
            .onAppear {
                focused = true
                // 上次用的那两个先填上——连着记几笔多半是同一张卡。
                if from.isEmpty { from = sources.first?.name ?? "" }
                if to.isEmpty { to = targets.first?.name ?? "" }
            }
        }
    }

    /// 科目选择器。一个都没有时说清楚为什么空着，别摆个点不动的控件。
    @ViewBuilder
    private func picker(_ items: [Account], selection: Binding<String>,
                        empty: String) -> some View {
        if items.isEmpty {
            Text(empty).font(.footnote).foregroundStyle(.secondary)
        } else {
            Picker("", selection: selection) {
                ForEach(items) { a in
                    Text(a.leaf).tag(a.name)
                }
            }
            .labelsHidden()
            .pickerStyle(.menu)
        }
    }

    private func save() {
        guard let value = money, value > 0, !saving else { return }
        saving = true
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        f.dateFormat = "yyyy-MM-dd"
        Task {
            let ok = await store.spend(payee: payee.trimmed, amount: value,
                                       from: from, to: to, date: f.string(from: date))
            saving = false
            if ok {
                Haptics.done()
                dismiss()
            } else {
                Haptics.failure()
            }
        }
    }
}
