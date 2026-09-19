import AVFoundation
import Speech
import SwiftUI

/// 对着手机说话，边说边出字。
///
/// 这个 app 的 slogan 是「说一句，剩下的交给我」，可在这之前只能打字——
/// 「下周三下午把跟老张的方案会排上，顺便提醒我提前半小时准备」这种话，
/// 打出来要半分钟，说出来五秒。
///
/// **本机识别，一个字不上传。** `requiresOnDeviceRecognition = true` 强制
/// 走端上模型：这个 app 的全部立场就是「数据是你自己的文件」，为了省事把
/// 用户说的每句话送到苹果服务器去转文字，跟这个立场直接冲突。
///
/// 代价是端上模型不一定装了（要在「设置 → 通用 → 键盘 → 听写」里下过对应
/// 语言），装了才给用。没有就把麦克风按钮藏起来——给一个点了不出字的按钮，
/// 比没有这个按钮更糟。
@MainActor
final class Dictation: ObservableObject {
    /// 正在听。
    @Published private(set) var listening = false
    /// 边说边更新的那段文字。
    @Published private(set) var text = ""
    /// 出了什么事（没权限、端上模型没装）。
    @Published var problem: String?

    private let recognizer = SFSpeechRecognizer(locale: Locale(identifier: "zh_CN"))
    private var task: SFSpeechRecognitionTask?
    private var request: SFSpeechAudioBufferRecognitionRequest?
    private let engine = AVAudioEngine()

    /// 这台机器能不能本机识别中文。
    ///
    /// 静态属性而不是每次问一遍：界面要拿它决定显不显示麦克风按钮，
    /// 每帧问一次太浪费，而这件事在 app 一次运行里不会变。
    static let available: Bool = {
        guard let r = SFSpeechRecognizer(locale: Locale(identifier: "zh_CN")) else {
            return false
        }
        return r.isAvailable && r.supportsOnDeviceRecognition
    }()

    func start() async {
        guard !listening else { return }
        text = ""
        problem = nil

        guard await authorize() else { return }
        guard let recognizer, recognizer.isAvailable else {
            problem = "这会儿用不了听写"
            return
        }
        guard recognizer.supportsOnDeviceRecognition else {
            // 不退而求其次走云端：宁可不给这个功能，也不把用户说的话送出去。
            problem = "本机中文听写没装。到「设置 → 通用 → 键盘 → 听写」里开一下。"
            return
        }

        do {
            try beginAudio()
        } catch {
            problem = "麦克风开不了：\(error.localizedDescription)"
            return
        }

        let req = SFSpeechAudioBufferRecognitionRequest()
        req.shouldReportPartialResults = true   // 边说边出字，不然像卡住了
        req.requiresOnDeviceRecognition = true  // 一个字不上传
        request = req

        task = recognizer.recognitionTask(with: req) { [weak self] result, error in
            guard let self else { return }
            Task { @MainActor in
                if let result {
                    self.text = result.bestTranscription.formattedString
                }
                // 说完了或者出错了都收摊。不收的话麦克风一直开着，
                // 状态栏那个红点会一直亮，人会以为被偷听。
                if error != nil || result?.isFinal == true {
                    self.finish()
                }
            }
        }
        listening = true
    }

    /// 停下，把听到的交出去。
    @discardableResult
    func stop() -> String {
        let said = text
        finish()
        return said
    }

    private func finish() {
        guard listening || engine.isRunning else { return }
        engine.stop()
        engine.inputNode.removeTap(onBus: 0)
        request?.endAudio()
        task?.cancel()
        request = nil
        task = nil
        listening = false
        // 让开音频会话：不还回去的话，退出之后别的 app 放音乐会小一截。
        try? AVAudioSession.sharedInstance().setActive(
            false, options: .notifyOthersOnDeactivation)
    }

    private func beginAudio() throws {
        let session = AVAudioSession.sharedInstance()
        // .measurement 关掉系统的语音增强处理：那套是给通话调的，
        // 对识别反而有损。
        try session.setCategory(.record, mode: .measurement, options: .duckOthers)
        try session.setActive(true, options: .notifyOthersOnDeactivation)

        let input = engine.inputNode
        let format = input.outputFormat(forBus: 0)
        input.removeTap(onBus: 0)   // 上一轮没摘干净的话，这儿会直接崩
        input.installTap(onBus: 0, bufferSize: 1024, format: format) { [weak self] buf, _ in
            self?.request?.append(buf)
        }
        engine.prepare()
        try engine.start()
    }

    /// 要两样权限：识别和麦克风。缺哪样都说清是哪样——
    /// 笼统一句「没权限」，人不知道去设置里开哪个开关。
    private func authorize() async -> Bool {
        let speech = await withCheckedContinuation { c in
            SFSpeechRecognizer.requestAuthorization { c.resume(returning: $0) }
        }
        guard speech == .authorized else {
            problem = "没有语音识别权限。设置 → Jarvis → 语音识别。"
            return false
        }
        let mic = await AVAudioApplication.requestRecordPermission()
        guard mic else {
            problem = "没有麦克风权限。设置 → Jarvis → 麦克风。"
            return false
        }
        return true
    }
}

/// 输入框旁边那颗麦克风。
///
/// 按住说、松手停——不做「点一下开始再点一下结束」：说完还要再找一次按钮，
/// 而那会儿手多半已经放下了。按住的这段时间里字是实时出来的，说错了松手
/// 改字就行。
struct MicButton: View {
    @Binding var draft: String
    @StateObject private var dictation = Dictation()
    @State private var before = ""

    var body: some View {
        if Dictation.available {
            Image(systemName: dictation.listening ? "waveform" : "mic.fill")
                .font(.system(.headline, weight: .semibold))
                .foregroundStyle(dictation.listening ? Color.white : Color.secondary)
                .frame(width: 38, height: 38)
                .background {
                    if dictation.listening {
                        Circle().fill(Brand.primary)
                    }
                }
                .contentShape(Circle())
                .symbolEffect(.variableColor.iterative, isActive: dictation.listening)
                .gesture(
                    DragGesture(minimumDistance: 0)
                        .onChanged { _ in
                            guard !dictation.listening else { return }
                            before = draft
                            Haptics.tap()
                            Task { await dictation.start() }
                        }
                        .onEnded { _ in
                            guard dictation.listening else { return }
                            dictation.stop()
                            Haptics.done()
                        })
                .onChange(of: dictation.text) { _, said in
                    guard !said.isEmpty else { return }
                    // 接在原来打了一半的字后面，别把它冲掉。
                    draft = before.isEmpty ? said : before + " " + said
                }
                .alert("听不了", isPresented: problemShown) {
                    Button("知道了") { dictation.problem = nil }
                } message: {
                    Text(dictation.problem ?? "")
                }
                .accessibilityLabel("按住说话")
        }
    }

    private var problemShown: Binding<Bool> {
        Binding(get: { dictation.problem != nil },
                set: { if !$0 { dictation.problem = nil } })
    }
}
