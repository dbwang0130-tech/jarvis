import Foundation

#if canImport(Mobile)
import Mobile
#endif

/// Bridge 是调 Go 那边 mobile 包的唯一入口。
///
/// gomobile 只认得基本类型，所以进出都是 JSON 字符串；这一层负责编解码，
/// 上面的界面代码看到的是正经的 Swift 结构。
///
/// Go 那边所有函数都保证不 panic、出错填在返回值的 error 字段里——
/// 手机上崩一次就是整个 app 挂掉。
enum Bridge {
    /// 告诉 Go 这台设备在哪个时区。**必须在任何别的调用之前调一次。**
    ///
    /// Go 那边改的是 time.Local 这个全局变量，只认第一次。放在别处调的话，
    /// 第一次改写会跟已经在跑的 Snapshot/Detail 撞上——那是数据竞争。
    static func setTimeZone() {
        #if canImport(Mobile)
        MobileSetTimeZone(TimeZone.current.identifier)
        #endif
    }

    /// 告诉 Go 能往哪儿写自己的东西（邮件同步进度这类）。
    ///
    /// 不设的话它按 ~/.config 算路径，而 iOS 上容器根目录不可写——
    /// 报的就是 operation not permitted，而且是静默的：写失败只返回个错误，
    /// 没人看，表现为「每次收信都像第一次」。
    static func setDataDir(_ path: String) {
        #if canImport(Mobile)
        MobileSetDataDir(path)
        #endif
    }

    /// 把 org 文件和账本解析成界面要显示的内容。
    static func snapshot(files: [String: String], ledger: String, days: Int = 7) -> Snapshot {
        let input: [String: Any] = [
            "files": files,
            "ledger": ledger,
            "days": days,
            // Go 在 iOS 上读不到系统时区库，不带过去就按 UTC 算日期。
            "tz": TimeZone.current.identifier,
        ]
        guard let json = encode(input) else {
            return Snapshot(days: [], todos: [], ledger: nil, error: "入参编码失败")
        }
        let out = callSnapshot(json)
        do {
            return try JSONDecoder().decode(Snapshot.self, from: Data(out.utf8))
        } catch {
            return Snapshot(days: [], todos: [], ledger: nil,
                            error: "出参解不开：\(error.localizedDescription)")
        }
    }

    /// 取一条条目的详情。列表里只有轻量字段，正文和勾选项点开才要。
    static func detail(files: [String: String], ref: String) -> DetailData {
        let input: [String: Any] = [
            "files": files,
            "ref": ref,
            "tz": TimeZone.current.identifier,
        ]
        guard let json = encode(input) else {
            return DetailData(error: "入参编码失败")
        }
        let out = callDetail(json)
        do {
            return try JSONDecoder().decode(DetailData.self, from: Data(out.utf8))
        } catch {
            return DetailData(error: "出参解不开：\(error.localizedDescription)")
        }
    }

    /// 直接对一个条目做一件事，不经过模型。标完成这种事界面已经握着 ref 了，
    /// 让模型跑一趟要好几秒和一次 API 调用。
    static func act(files: [String: String], ref: String, op: String,
                    days: Int = 0, title: String = "",
                    scheduled: String = "", priority: String = "") -> ActResult {
        let input: [String: Any] = [
            "files": files, "ref": ref, "op": op, "days": days,
            "title": title, "scheduled": scheduled, "priority": priority,
            "tz": TimeZone.current.identifier,
        ]
        guard let json = encode(input) else {
            return ActResult(error: "入参编码失败")
        }
        let out = callAct(json)
        do {
            return try JSONDecoder().decode(ActResult.self, from: Data(out.utf8))
        } catch {
            return ActResult(error: "出参解不开：\(error.localizedDescription)")
        }
    }

    /// 把一句话交给秘书，返回答复和要写回的文件。
    ///
    /// events 不为空时，干活过程会实时报上去，碰到要拿主意的事会回头问——
    /// 那个回调**会阻塞调用它的线程**，所以这个方法别在主线程上调。
    static func ask(_ input: AskInput, events: AskEvents? = nil) -> AskOutput {
        guard let data = try? JSONEncoder().encode(input),
              let json = String(data: data, encoding: .utf8) else {
            return AskOutput(reply: "", files: nil, ledger: nil, history: input.history,
                             steps: nil, error: "入参编码失败")
        }
        let out = callAsk(json, events)
        do {
            return try JSONDecoder().decode(AskOutput.self, from: Data(out.utf8))
        } catch {
            return AskOutput(reply: "", files: nil, ledger: nil, history: input.history,
                             steps: nil, error: "出参解不开：\(error.localizedDescription)")
        }
    }

    /// 邮件。list 只读本地不连网，sync 才去拉。
    static func mail(op: String, indexDir: String, bodyDir: String,
                     accounts: [MailAccountInput],
                     model: ModelConfig? = nil, profile: String = "",
                     key: String = "", keys: [String] = []) -> MailResult {
        var input: [String: Any] = [
            "op": op, "index_dir": indexDir, "body_dir": bodyDir,
            "key": key, "keys": keys,
            "profile": profile, "tz": TimeZone.current.identifier,
        ]
        if let data = try? JSONEncoder().encode(accounts),
           let arr = try? JSONSerialization.jsonObject(with: data) {
            input["accounts"] = arr
        }
        if let model, let data = try? JSONEncoder().encode(model),
           let obj = try? JSONSerialization.jsonObject(with: data) {
            input["model"] = obj
        }
        guard let json = encode(input) else {
            return MailResult(error: "入参编码失败")
        }
        let out = callMail(json)
        do {
            return try JSONDecoder().decode(MailResult.self, from: Data(out.utf8))
        } catch {
            return MailResult(error: "出参解不开：\(error.localizedDescription)")
        }
    }

    /// 拿当前配置真发一次请求，看通不通。密钥填错不该等到第一次认真提问
    /// 才暴露——那会儿人已经打完一整句话、等了十几秒。
    static func checkModel(_ model: ModelConfig) -> ModelCheck {
        guard let data = try? JSONEncoder().encode(["model": model]),
              let json = String(data: data, encoding: .utf8) else {
            return ModelCheck(message: "入参编码失败")
        }
        let out = callTestModel(json)
        do {
            return try JSONDecoder().decode(ModelCheck.self, from: Data(out.utf8))
        } catch {
            return ModelCheck(message: "出参解不开：\(error.localizedDescription)")
        }
    }

    /// 叫停正在跑的那一轮。已经做完的改动会照常交回来。
    static func cancel() {
        #if canImport(Mobile)
        MobileCancel()
        #endif
    }

    // MARK: - 跟 Go 打交道的那几下

    // gomobile 生成的符号是「包名首字母大写 + 函数名」：mobile.Snapshot → MobileSnapshot。
    // 没连上 framework 时（比如在 macOS 上做语法检查）走下面的桩，好歹能编过。

    private static func callSnapshot(_ json: String) -> String {
        #if canImport(Mobile)
        return MobileSnapshot(json)
        #else
        return #"{"days":[],"todos":[],"error":"Mobile framework 没连上"}"#
        #endif
    }

    private static func callMail(_ json: String) -> String {
        #if canImport(Mobile)
        return MobileMail(json)
        #else
        return #"{"items":[],"error":"Mobile framework 没连上"}"#
        #endif
    }

    private static func callTestModel(_ json: String) -> String {
        #if canImport(Mobile)
        return MobileTestModel(json)
        #else
        return #"{"ok":false,"message":"Mobile framework 没连上"}"#
        #endif
    }

    private static func callAct(_ json: String) -> String {
        #if canImport(Mobile)
        return MobileAct(json)
        #else
        return #"{"error":"Mobile framework 没连上"}"#
        #endif
    }

    private static func callDetail(_ json: String) -> String {
        #if canImport(Mobile)
        return MobileDetail(json)
        #else
        return #"{"error":"Mobile framework 没连上"}"#
        #endif
    }

    private static func callAsk(_ json: String, _ events: AskEvents?) -> String {
        #if canImport(Mobile)
        guard let events else { return MobileAsk(json) }
        return MobileAskLive(json, MobileHostAdapter(events))
        #else
        return #"{"reply":"","history":[],"error":"Mobile framework 没连上"}"#
        #endif
    }

    private static func encode(_ v: [String: Any]) -> String? {
        guard let data = try? JSONSerialization.data(withJSONObject: v) else { return nil }
        return String(data: data, encoding: .utf8)
    }
}
