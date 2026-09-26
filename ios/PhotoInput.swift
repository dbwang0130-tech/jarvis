import PhotosUI
import SwiftUI

/// 输入框里那颗相机。
///
/// 拍一张或从相册挑，图跟着这句话一起发给秘书——「这张小票记一下」
/// 「这个菜单有什么推荐」「这是我的体检报告」。
///
/// **一次最多三张。** 图按 token 算贵得多，一次发五六张，一轮对话的钱
/// 够问几十句话；而真要一次问这么多图的事，多半分几次问更清楚。
struct PhotoButton: View {
    @Binding var shots: [Data]

    @State private var picking: PhotosPickerItem?
    @State private var camera = false
    @State private var menu = false
    @State private var showPicker = false

    static let maxShots = 3

    var body: some View {
        Button { menu = true } label: {
            Image(systemName: "camera.fill")
                .font(.system(.subheadline, weight: .semibold))
                .foregroundStyle(.secondary)
                .frame(width: 34, height: 34)
                .contentShape(Circle())
        }
        .buttonStyle(.plain)
        .disabled(shots.count >= Self.maxShots)
        .accessibilityLabel("发送图片")
        .confirmationDialog("发送图片", isPresented: $menu, titleVisibility: .hidden) {
            Button("拍一张") { camera = true }
            Button("从相册选") { showPicker = true }
            Button("算了", role: .cancel) {}
        }
        .photosPicker(isPresented: $showPicker, selection: $picking, matching: .images)
        .onChange(of: picking) { _, item in
            guard let item else { return }
            Task {
                // 相册里的原图动辄四五 MB，跟拍照那条路走同一个压缩：
                // 传不动（有的服务商直接 413），也不该为一句话花那么多钱。
                // 解不出图就什么都不做——HEIC 之类偶尔会栽在这儿，
                // 硬塞一张空图给模型只会换回一句莫名其妙的回答。
                if let data = try? await item.loadTransferable(type: Data.self),
                   let image = UIImage(data: data),
                   let shrunk = CameraShot.Coordinator.jpeg(image) {
                    add(shrunk)
                }
                picking = nil
            }
        }
        .fullScreenCover(isPresented: $camera) {
            CameraShot { data in
                camera = false
                if let data { add(data) }
            }
            .ignoresSafeArea()
        }
    }

    private func add(_ data: Data) {
        guard shots.count < Self.maxShots else { return }
        shots.append(data)
        Haptics.done()
    }
}

/// 输入框上面那排待发的图。
///
/// 发出去之前得看得见自己挑了什么，也得能反悔——挑错一张就发出去，
/// 那一轮的钱和等待都白花了。
struct ShotStrip: View {
    @Binding var shots: [Data]

    var body: some View {
        ScrollView(.horizontal) {
            HStack(spacing: 8) {
                ForEach(Array(shots.enumerated()), id: \.offset) { i, data in
                    if let img = UIImage(data: data) {
                        Image(uiImage: img)
                            .resizable()
                            .scaledToFill()
                            .frame(width: 56, height: 56)
                            .clipShape(.rect(cornerRadius: 10))
                            .overlay(alignment: .topTrailing) {
                                Button {
                                    shots.remove(at: i)
                                    Haptics.tap()
                                } label: {
                                    Image(systemName: "xmark.circle.fill")
                                        .font(.caption)
                                        .foregroundStyle(.white, .black.opacity(0.55))
                                        .padding(3)
                                }
                                .buttonStyle(.plain)
                                .accessibilityLabel("去掉这张")
                            }
                    }
                }
            }
            .padding(.horizontal, 14)
        }
        .scrollIndicators(.hidden)
        .frame(height: 64)
    }
}
