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

    /// 把一句话交给秘书，返回答复和要写回的文件。
    static func ask(_ input: AskInput) -> AskOutput {
        guard let data = try? JSONEncoder().encode(input),
              let json = String(data: data, encoding: .utf8) else {
            return AskOutput(reply: "", files: nil, ledger: nil, history: input.history,
                             steps: nil, error: "入参编码失败")
        }
        let out = callAsk(json)
        do {
            return try JSONDecoder().decode(AskOutput.self, from: Data(out.utf8))
        } catch {
            return AskOutput(reply: "", files: nil, ledger: nil, history: input.history,
                             steps: nil, error: "出参解不开：\(error.localizedDescription)")
        }
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

    private static func callDetail(_ json: String) -> String {
        #if canImport(Mobile)
        return MobileDetail(json)
        #else
        return #"{"error":"Mobile framework 没连上"}"#
        #endif
    }

    private static func callAsk(_ json: String) -> String {
        #if canImport(Mobile)
        return MobileAsk(json)
        #else
        return #"{"reply":"","history":[],"error":"Mobile framework 没连上"}"#
        #endif
    }

    private static func encode(_ v: [String: Any]) -> String? {
        guard let data = try? JSONSerialization.data(withJSONObject: v) else { return nil }
        return String(data: data, encoding: .utf8)
    }
}
