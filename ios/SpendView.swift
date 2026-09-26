import PhotosUI
import SwiftUI

/// 记一笔账：花钱或者进账。
///
/// 平时记账就是跟秘书说一句（「打车 26 块」），它自己配平了写进账本。
/// 这张表单是给两种情况留的：Siri 说了金额直接摆开（「记一笔 35」——人已经
/// 开口了，再让他打一遍字等于白说），以及要贴小票的时候。
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
    /// 从 Siri 带进来的金额，进来就填好。
    var preset: Double?

    @State private var payee = ""
    @State private var amount = ""
    @State private var from = ""
    @State private var to = ""
    @State private var date = Date()
    @State private var kind = Kind.expense
    @State private var saving = false
    @FocusState private var focused: Bool

    // 小票那一摊。
    @State private var picking: PhotosPickerItem?
    @State private var shot: Data?          // 手上这张图
    @State private var shotPath = ""        // 存进 iCloud 之后的相对路径
    @State private var reading = false
    @State private var readNote = ""        // 认出来什么 / 为什么没认出来
    @State private var camera = false

    /// 花钱还是进账。
    ///
    /// **两边共用一套 from/to，方向天生对称**，Go 那边一个字都不用改：
    /// `to` 那侧记正数、`from` 那侧配平成负数。花钱是「支出科目增加、
    /// 银行卡减少」，进账是「银行卡增加、收入科目减少」——收入科目在
    /// 复式记账里本来就记负数。
    enum Kind: String, CaseIterable, Identifiable {
        case expense = "花钱"
        case income = "进账"
        var id: String { rawValue }
    }

    private var assets: [Account] {
        accounts.filter { $0.kind == "asset" || $0.kind == "debt" }
    }

    /// 钱从哪儿来：花钱时是资产和负债（刷信用卡也是花钱），
    /// 进账时是收入科目。
    private var sources: [Account] {
        kind == .expense ? assets : accounts.filter { $0.kind == "income" }
    }

    /// 钱到哪儿去：花钱时是支出科目，进账时是落到哪张卡上。
    private var targets: [Account] {
        kind == .expense ? accounts.filter { $0.kind == "expense" } : assets
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
                    TextField(kind == .expense ? "买的什么（留空就记科目名）"
                                               : "哪来的（留空就记科目名）",
                              text: $payee)
                }

                Section {
                    Picker("", selection: $kind) {
                        ForEach(Kind.allCases) { Text($0.rawValue).tag($0) }
                    }
                    .pickerStyle(.segmented)
                    .labelsHidden()
                }

                Section(kind == .expense ? "从哪儿出" : "哪来的") {
                    picker(sources, selection: $from,
                           empty: kind == .expense ? "账本里还没有资产账户"
                                                   : "账本里还没有收入科目")
                }

                Section(kind == .expense ? "花在什么上" : "进到哪儿") {
                    picker(targets, selection: $to,
                           empty: kind == .expense ? "账本里还没有支出科目"
                                                   : "账本里还没有资产账户")
                }

                Section {
                    DatePicker("哪天", selection: $date, displayedComponents: [.date])
                } footer: {
                    Text("记进账本末尾，跟电脑上是同一份。")
                }

                // 小票。**图片是资产，不是一次性的识别输入**——拍下来就
                // 存进 iCloud 里按年月分好，账本里记下它的相对路径，
                // 几年后翻出那笔账还能找到这张图。
                if kind == .expense {
                    Section("小票") {
                        if let shot, let img = UIImage(data: shot) {
                            Image(uiImage: img)
                                .resizable()
                                .scaledToFit()
                                .frame(maxHeight: 180)
                                .clipShape(.rect(cornerRadius: Brand.smallRadius))
                                .overlay(alignment: .topTrailing) { removeShot }
                        } else {
                            HStack(spacing: 16) {
                                Button { camera = true } label: {
                                    Label("拍一张", systemImage: "camera")
                                }
                                PhotosPicker(selection: $picking, matching: .images) {
                                    Label("从相册选", systemImage: "photo")
                                }
                            }
                            .buttonStyle(.plain)
                            .foregroundStyle(Brand.primary)
                        }

                        if reading {
                            HStack(spacing: 8) {
                                ProgressView().controlSize(.small)
                                Text("认一下…").foregroundStyle(.secondary)
                            }
                            .font(.footnote)
                        } else if !readNote.isEmpty {
                            Text(readNote)
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                        }
                    }
                }
            }
            // 换方向之后原来选的科目多半不对了（「餐饮」不能收钱），
            // 重挑一遍默认值。
            .onChange(of: kind) { _, _ in
                from = sources.first?.name ?? ""
                to = targets.first?.name ?? ""
            }
            .onChange(of: picking) { _, item in
                guard let item else { return }
                Task {
                    if let data = try? await item.loadTransferable(type: Data.self) {
                        await took(data)
                    }
                    picking = nil
                }
            }
            .fullScreenCover(isPresented: $camera) {
                CameraShot { data in
                    camera = false
                    if let data { Task { await took(data) } }
                }
                .ignoresSafeArea()
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
                if let a = preset, amount.isEmpty {
                    // 金额已经说过了，光标就别再往那儿跳——该填的是
                    // 「买的什么」。
                    amount = trimZeros(a)
                } else {
                    focused = true
                }
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

    private var removeShot: some View {
        Button {
            // 只从表单上撤掉，**不删文件**：图已经在 iCloud 里了，
            // 那是用户的东西；他要删自己去「文件」里删。
            shot = nil
            shotPath = ""
            readNote = ""
        } label: {
            Image(systemName: "xmark.circle.fill")
                .font(.title3)
                .foregroundStyle(.white, .black.opacity(0.5))
                .padding(6)
        }
        .buttonStyle(.plain)
        .accessibilityLabel("不要这张小票")
    }

    /// 拿到一张图之后：先存，再认。
    ///
    /// **顺序是有讲究的。** 识别可能失败（模型不支持看图、没网、密钥过期），
    /// 但那张图是用户刚拍下来的，不该因为认不出来就丢。所以先落盘，
    /// 认不出来最多是手动填几个字。
    private func took(_ data: Data) async {
        shot = data
        readNote = ""

        // 存的时候还不知道商家是谁（正要去认），先用当前填的那个；
        // 认出来之后文件名已经定了，不为一个名字再挪一次文件。
        if let path = await store.keepReceipt(data, payee: payee.trimmed, at: date) {
            shotPath = path
        }

        reading = true
        defer { reading = false }
        let got = await store.readReceipt(data)

        if let err = got.error, !err.isEmpty {
            // 认不出来不是事故：图已经存好了，字自己填就是。
            readNote = err
            return
        }
        fill(from: got)
    }

    /// 把认出来的填进表单。**只填空着的**——人已经改过的不能冲掉。
    private func fill(from got: ReceiptRead) {
        var filled: [String] = []
        if got.amount > 0, money == nil || money == 0 {
            amount = trimZeros(got.amount)
            filled.append("金额")
        }
        if !got.payee.isEmpty, payee.trimmed.isEmpty {
            payee = got.payee
            filled.append("商家")
        }
        if !got.account.isEmpty, targets.contains(where: { $0.name == got.account }) {
            to = got.account
            filled.append("科目")
        }
        if !got.date.isEmpty {
            let f = DateFormatter()
            f.locale = Locale(identifier: "en_US_POSIX")
            f.dateFormat = "yyyy-MM-dd"
            if let d = f.date(from: got.date) {
                date = d
                filled.append("日期")
            }
        }
        readNote = filled.isEmpty ? "没认出什么，手填吧"
                                  : "认出了" + filled.joined(separator: "、") + "，核对一下"
        Haptics.done()
    }

    private func save() {
        guard let value = money, value > 0, !saving else { return }
        saving = true
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        f.dateFormat = "yyyy-MM-dd"
        Task {
            let ok = await store.spend(payee: payee.trimmed, amount: value,
                                       from: from, to: to, date: f.string(from: date),
                                       receipt: shotPath)
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
