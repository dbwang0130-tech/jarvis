import SwiftUI

/// 历史对话。
///
/// 存档在金库的 `sessions/` 下，一段对话一个 `.jsonl` 文件——**跟日程、
/// 账本一样是用户自己的文件**，可以自己打开看、自己拷走、自己删。
///
/// 点一条就读回来接着聊；当前这段会先被归档，不会被冲掉。
struct SessionsView: View {
    @EnvironmentObject var store: Store

    @State private var entries: [SessionArchive.Entry] = []
    @State private var loading = true

    var body: some View {
        List {
            if loading {
                HStack(spacing: 10) {
                    ProgressView().controlSize(.small)
                    Text("正在读…").foregroundStyle(.secondary)
                }
            } else if entries.isEmpty {
                // 空状态要说清**什么时候会有东西**，否则人会以为功能坏了。
                VStack(alignment: .leading, spacing: 6) {
                    Text("还没有存档").font(.headline)
                    Text("点对话右上角那支笔开始新的一段时，上一段就会存到这里。")
                        .font(.caption).foregroundStyle(.secondary)
                }
                .padding(.vertical, 6)
            } else {
                Section {
                    ForEach(entries) { e in
                        Button { open(e) } label: {
                            VStack(alignment: .leading, spacing: 2) {
                                Text(e.title)
                                    .foregroundStyle(.primary)
                                    .lineLimit(2)
                                Text(e.subtitle)
                                    .font(.caption).foregroundStyle(.secondary)
                            }
                            .frame(maxWidth: .infinity, alignment: .leading)
                        }
                    }
                    .onDelete(perform: remove)
                } footer: {
                    Text("点一条接着聊，手头这段会先存起来。左滑删除。\n"
                         + "这些是 sessions/ 下的 .jsonl 文件，跟日程账本放在一起。")
                }
            }
        }
        .navigationTitle("历史对话")
        .navigationBarTitleDisplayMode(.inline)
        .onAppear(perform: reload)
    }

    private func reload() {
        loading = true
        let vault = store.vault
        Task {
            // 列表要读每个文件的头几行，文件多了别卡住主线程。
            let list = await Task.detached(priority: .userInitiated) {
                SessionArchive.list(from: vault)
            }.value
            entries = list
            loading = false
        }
    }

    private func open(_ e: SessionArchive.Entry) {
        guard store.loadArchived(e.id) else { return }
        Haptics.success()
        // 读回来了就该看见它。这一页在设置里，得把设置整个收掉——
        // 只发通知，由 SettingsView 关掉整张 sheet（连带把这一页 pop 走）。
        // 这儿再 dismiss 一次自己，两段动画会打架。
        NotificationCenter.default.post(name: .jarvisCloseSettings, object: nil)
    }

    private func remove(_ offsets: IndexSet) {
        for i in offsets { SessionArchive.delete(entries[i].id, from: store.vault) }
        entries.remove(atOffsets: offsets)
    }
}

extension Notification.Name {
    /// 从设置深处要求把整个设置界面关掉——读回一段对话之后得让人看见它。
    static let jarvisCloseSettings = Notification.Name("jarvis.closeSettings")
}
