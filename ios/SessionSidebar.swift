import SwiftUI

/// 会话侧栏：Mac 和 iPad 横屏上常驻在左边的那一列。
///
/// **它只放会话，不放别的。** 早先有过四个并排的标签页（日程/待办/邮件/财务），
/// 删掉的理由是它们跟「问一句」完全重叠——而那一句才是这东西的立身之本。
/// 宽屏上有空地不等于要把它们请回来：会话列表不重叠，它是**导航**，回答的是
/// 「我刚才那段聊到哪了」，问一句问不出来。所有聊天工具都是这个形状。
///
/// 窄屏（iPhone）上根本不出现，见 `MainShell`。
struct SessionSidebar: View {
    @EnvironmentObject var store: Store

    var body: some View {
        List {
            Section {
                // 还没归档的那一段（刚开口、后台还没落过盘）在清单里没有
                // 对应文件，可它正是人眼下在聊的——不摆出来的话，侧栏上
                // 一条高亮都没有，看着像谁都没选中。
                if store.archiveName == nil, !store.messages.isEmpty {
                    row(title: current, subtitle: "进行中", on: true) {}
                }
                ForEach(store.archives) { e in
                    row(title: e.title, subtitle: e.subtitle,
                        on: store.archiveName == e.id) {
                        guard store.archiveName != e.id else { return }
                        store.loadArchived(e.id)
                    }
                    .contextMenu {
                        Button("删掉这段", role: .destructive) {
                            store.deleteArchive(e.id)
                        }
                    }
                }
                .onDelete(perform: remove)
            } header: {
                Text("对话")
            } footer: {
                if store.archives.isEmpty && store.messages.isEmpty {
                    Text("聊过的会存在这儿，换台设备也还在。")
                }
            }
        }
        // **不设标题。** 窗口标题栏已经写着 Jarvis，右栏顶上还有一张同样的脸
        // 和同一个名字——侧栏再来一遍就是一屏三个 Jarvis。那个 Section
        // header「对话」已经说清这一列是什么了。
        .navigationTitle("")
        .toolbar {
            ToolbarItem {
                Button {
                    store.clearChat()
                    Haptics.tap()
                } label: {
                    Label("新建对话", systemImage: "square.and.pencil")
                }
                .disabled(store.messages.isEmpty)
                // Mac 上这是肌肉记忆：⌘N 开新的一段。
                .keyboardShortcut("n", modifiers: .command)
            }
        }
        .onAppear { store.refreshArchives() }
    }

    /// 手头这段还没有标题（没归档就没有 meta 行），拿第一句话顶上。
    private var current: String {
        let first = store.messages.first { $0.role == "user" } ?? store.messages.first
        let raw = (first?.text ?? "新对话").trimmingCharacters(in: .whitespacesAndNewlines)
        let oneLine = raw.replacingOccurrences(of: "\n", with: " ")
        return oneLine.count > 40 ? String(oneLine.prefix(40)) + "…" : oneLine
    }

    @ViewBuilder
    private func row(title: String, subtitle: String, on: Bool,
                     action: @escaping () -> Void) -> some View {
        Button(action: action) {
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .foregroundStyle(on ? Brand.primary : .primary)
                    .fontWeight(on ? .semibold : .regular)
                    .lineLimit(2)
                Text(subtitle).font(.caption).foregroundStyle(.secondary)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
    }

    private func remove(_ offsets: IndexSet) {
        for i in offsets { store.deleteArchive(store.archives[i].id) }
    }
}
