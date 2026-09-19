import SwiftUI
import UIKit

/// 拍一张。
///
/// 用系统相机（`UIImagePickerController`）而不是自己拿 AVFoundation 搭一个：
/// 对焦、闪光灯、HDR、拍完的确认页，系统那套都调好了，自己写一遍只会更差。
/// 代价是得包一层 UIKit——SwiftUI 至今没有拍照的原生入口。
struct CameraShot: UIViewControllerRepresentable {
    /// 拍完给出 JPEG；取消了给 nil。
    let done: (Data?) -> Void

    func makeUIViewController(context: Context) -> UIImagePickerController {
        let picker = UIImagePickerController()
        // 模拟器没有相机，退回相册——不然点了是一片黑，看着像坏了。
        picker.sourceType = UIImagePickerController.isSourceTypeAvailable(.camera)
            ? .camera : .photoLibrary
        picker.delegate = context.coordinator
        return picker
    }

    func updateUIViewController(_ picker: UIImagePickerController, context: Context) {}

    func makeCoordinator() -> Coordinator { Coordinator(done: done) }

    final class Coordinator: NSObject, UIImagePickerControllerDelegate,
                             UINavigationControllerDelegate {
        private let done: (Data?) -> Void

        init(done: @escaping (Data?) -> Void) { self.done = done }

        func imagePickerController(
            _ picker: UIImagePickerController,
            didFinishPickingMediaWithInfo info: [UIImagePickerController.InfoKey: Any]
        ) {
            guard let image = info[.originalImage] as? UIImage else {
                done(nil)
                return
            }
            done(Self.jpeg(image))
        }

        func imagePickerControllerDidCancel(_ picker: UIImagePickerController) {
            done(nil)
        }

        /// 压到能看清字、又不至于传不动的大小。
        ///
        /// 原图有四五 MB，而这张图要做两件事：发给模型认（太大传不动，
        /// 有的服务商直接 413）、存进 iCloud（几百张小票不该占掉几个 G）。
        /// 长边 1600 足够认清小票上的字，质量 0.7 之后通常落在两三百 KB。
        static func jpeg(_ image: UIImage) -> Data? {
            let maxSide: CGFloat = 1600
            let side = max(image.size.width, image.size.height)
            guard side > maxSide else { return image.jpegData(compressionQuality: 0.7) }

            let scale = maxSide / side
            let size = CGSize(width: image.size.width * scale,
                              height: image.size.height * scale)
            let renderer = UIGraphicsImageRenderer(size: size)
            let smaller = renderer.image { _ in
                image.draw(in: CGRect(origin: .zero, size: size))
            }
            return smaller.jpegData(compressionQuality: 0.7)
        }
    }
}
