import Foundation

// iOS 那边原先一条测试都没有：只有 ios-contract（拿真出参验字段）和 ios-check
// （在真 iOS SDK 下类型检查）。而界面里有真逻辑——链接怎么解、哪一行算一张图表、
// markdown 怎么切块——**解错了的表现是「点了没反应」或者「排版塌了」，编译一声不响。**
//
// 这份跑的是 MarkdownParse.swift 里那一半纯逻辑，用纯 swiftc 编（不需要模拟器、
// 不需要完整 Xcode 的测试目标），跟 ios-contract 同一个路子。
//
// 画那一半（MarkdownText / ChartCard / 输入框）没在这儿——它要真跑起来才看得出，
// 靠的是装到手机上看。

var failed = 0

func check(_ name: String, _ body: () throws -> String) {
    do { print("  ok   \(name)：\(try body())") }
    catch { print("  FAIL \(name)：\(error)"); failed += 1 }
}

struct Err: Error, CustomStringConvertible { let description: String
    init(_ s: String) { description = s } }

func link(_ s: String) -> JarvisLink? {
    guard let u = URL(string: s) else { return nil }
    return JarvisLink(u)
}

// MARK: - 链接解析

check("认得三种链接") {
    guard let item = link("jarvis://item/work.org:12"),
          item.kind == .item, item.id == "work.org:12" else {
        throw Err("日程链接解错了：\(String(describing: link("jarvis://item/work.org:12")))")
    }
    guard let chart = link("jarvis://chart/%E4%BD%93%E9%87%8D"),
          chart.kind == .chart, chart.id == "体重" else {
        throw Err("图表链接解错了（percent 转义没还原？）")
    }
    // 邮件 key 是「账号:UID」，账号名可能是中文，Go 那边做过转义。
    guard let mail = link("jarvis://mail/%E5%B7%A5%E4%BD%9C:42"),
          mail.kind == .mail, mail.id == "工作:42" else {
        throw Err("邮件链接解错了：\(String(describing: link("jarvis://mail/%E5%B7%A5%E4%BD%9C:42")))")
    }
    return "item / chart / mail 都对，中文 id 也还原了"
}

check("漏了 host 的也认") {
    // 真模型评测里抓到过一次：它把 jarvis://item/work.org:10 写成了
    // jarvis://work.org:10。不认的话那一下就是个点了没反应的蓝字。
    guard let l = link("jarvis://work.org:10"), l.kind == .item, l.id == "work.org:10" else {
        throw Err("没兜住漏 host 的写法：\(String(describing: link("jarvis://work.org:10")))")
    }
    return "jarvis://work.org:10 → 当条目用"
}

check("不该认的一个都不认") {
    let bad = [
        "https://example.com/a",        // 普通网址交给系统
        "jarvis://chart/",              // 没有 id
        "jarvis://没这种/x",             // 不认识的 kind
        "jarvis://随便一段话",            // 不像 ref
    ]
    for s in bad {
        if let l = link(s) {
            throw Err("\(s) 不该认，却解成了 \(l.kind)/\(l.id)")
        }
    }
    return "\(bad.count) 种都拒了"
}

// MARK: - 整行图表链接

check("整行只有图表链接才算一张图") {
    guard let sole = Markdown.soleLink("[体重趋势](jarvis://chart/体重)"),
          sole.link.kind == .chart, sole.link.id == "体重", sole.label == "体重趋势" else {
        throw Err("整行的图表链接没认出来")
    }
    // **夹在句子里的不算**：图是横着占满一行的东西，塞在半句话里排不出来。
    // 它走行内链接那条路（点一下弹详情），所以这儿必须返回 nil。
    if Markdown.soleLink("最近 [体重趋势](jarvis://chart/体重) 在往下走") != nil {
        throw Err("句子里的链接被当成整行的了，那句话会被拆成三块")
    }
    if Markdown.soleLink("[](jarvis://chart/体重)") != nil {
        throw Err("空标签的也认了——桌面端只剩这几个字，空的等于什么都没有")
    }
    return "整行认、句中不认、空标签不认"
}

// MARK: - 切块

func blocks(_ s: String) -> [Block] { Markdown.blocks(s) }

check("图表独占一行变成块，句中的留在段里") {
    let b = blocks("今天 77.5 公斤，比上周轻了 0.4。\n\n[体重趋势](jarvis://chart/体重)\n")
    guard b.count == 2 else { throw Err("该切成 2 块，实际 \(b.count)") }
    guard case .paragraph = b[0] else { throw Err("第一块该是段落") }
    guard case .chart(let kind, let label) = b[1] else { throw Err("第二块该是图表") }
    guard kind == "体重", label == "体重趋势" else { throw Err("图表参数不对：\(kind)/\(label)") }

    // 同一个链接夹在句子里时，整段还是一个段落。
    let inline = blocks("最近 [体重趋势](jarvis://chart/体重) 在往下走。")
    guard inline.count == 1, case .paragraph = inline[0] else {
        throw Err("句中的图表链接把段落切开了：\(inline)")
    }
    return "独占一行 → 块；夹在句中 → 段落"
}

check("代码围栏里的东西原样留着") {
    let b = blocks("说明：\n```\n# 这不是标题\n- 这不是列表\n```\n收尾。")
    guard b.count == 3 else { throw Err("该切成 3 块，实际 \(b.count)：\(b)") }
    guard case .code(let body) = b[1] else { throw Err("中间那块该是代码") }
    guard body.contains("# 这不是标题"), body.contains("- 这不是列表") else {
        throw Err("围栏里的标记被当语法处理了：\(body)")
    }
    return "围栏里 \(body.split(separator: "\n").count) 行原样"
}

check("表格要有分隔行才算表格") {
    let real = blocks("| 项 | 值 |\n|---|---|\n| 体重 | 77.5 |")
    guard real.count == 1, case .table(let rows) = real[0], rows.count == 2 else {
        throw Err("正常表格没认出来：\(real)")
    }
    // 没有 |---| 那行的，只是一段带竖线的文字——认成表格会把它排得很怪。
    let fake = blocks("| 这只是 | 一句话 |")
    if case .table = fake.first { throw Err("没有分隔行却认成了表格") }
    return "有分隔行认、没有不认"
}

check("标题、列表、分隔线") {
    guard case .heading(let lv, let t) = blocks("## 今天").first, lv == 2, t == "今天" else {
        throw Err("二级标题没认出来")
    }
    guard case .list(let items) = blocks("- 甲\n- 乙").first, items.count == 2 else {
        throw Err("列表没认出来")
    }
    guard case .rule = blocks("---").first else { throw Err("分隔线没认出来") }
    return "都对"
}

check("空输入不炸") {
    for s in ["", "\n\n\n", "   ", "```", "|"] {
        _ = blocks(s)   // 不崩就算过——手机上崩一次是整个 app 挂掉
    }
    return "5 种边角输入都没崩"
}

if failed > 0 {
    print("\n\(failed) 条没过。")
    exit(1)
}
print("\n界面那半纯逻辑都对得上。")
