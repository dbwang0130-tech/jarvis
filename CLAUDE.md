# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目

jarvis 是一个终端个人助理（Go，无框架），把日程（org-mode）、邮件（IMAP/SMTP）、记账（ledger-cli）、浏览器（CDP）合并到一个由大模型驱动的工具调用循环里。代码注释、提交信息、面向用户的文案一律中文。

## iOS 端

`mobile/` 是给 iOS 用的桥接层，`ios/` 是 Swift 源码（见 `ios/README.md`）。

**桥接层是纯函数**：`Snapshot`/`Ask` 收 JSON 吐 JSON，Go 一个文件都不碰——
iOS 那边文件访问的坑（沙箱授权、iCloud 占位符、`NSFileCoordinator` 协调）
全留在 Swift 的 `Vault` 里。`Ask` 内部把内容落到临时目录让现成的 orgtools
去改，改完读回来**只交出真变过的那几个文件**。

两条硬规矩：

- **不许 panic**。手机上崩一次就是整个 app 挂掉，出错一律填在返回值的
  `error` 字段里。`mobile` 的测试里有一组专喂烂输入的用例守着。
- **`mobile` 的 JSON 字段就是契约**，改了要同步改 `ios/Models.swift`。
  gomobile 只认得基本类型，map 和结构体切片都过不去，所以只能走 JSON。
  Swift 那边数组字段是非可选的，Go 的 nil slice 序列化成 `null` 会让整个
  出参解码失败、界面一片空白，所以 `marshal` 出口统一把数组补齐
  （`normalizer` 接口）。`TestNoNullArrays` 守着，包括出错那几条路。

改完跑这两个（Go 自己的测试对字段改名和可选性变化是无感的）：

```sh
make ios-contract   # 拿 ios/Models.swift 去解 Go 真实出参，只要 swiftc
make ios-ui         # 界面那半纯逻辑（链接解析、markdown 切块），只要 swiftc
make ios-check      # Swift 在真实 iOS SDK 下类型检查，要完整 Xcode
```

**markdown 那块拆成两半**：`MarkdownParse.swift` 只管「怎么看懂」（`JarvisLink`、
`Block`、`Markdown.blocks`，只 import Foundation），`Markdown.swift` 只管「怎么画」。
拆的理由是测试——合在一起的时候要测解析就得把 `ChartCard` 连带 `Store`、HealthKit
一路拖进来编，于是 iOS 那边一条测试都没有。现在 `ios/uitest` 拿纯 swiftc 编一个文件
加一份断言就能跑，不需要模拟器、也不需要在工程里挂测试目标。解析器本来就不该知道
SwiftUI 的事，所以这个拆分本身也是对的。

画那一半（`MarkdownText` / `ChartCard` / 输入框）**只能真跑起来看**。模拟器就够用，
不用每次装真机：

```sh
SIM=$(xcrun simctl list devices booted | awk -F'[()]' '/iPhone/{print $2; exit}')
xcodebuild -project ios/Jarvis.xcodeproj -scheme Jarvis -configuration Debug     -destination "id=$SIM" build
APP=$(xcodebuild ... -showBuildSettings | awk -F' = ' '/ BUILT_PRODUCTS_DIR/{print $2;exit}')/Jarvis.app
xcrun simctl install "$SIM" "$APP"
C=$(xcrun simctl get_app_container "$SIM" com.dabao.jarvis data)
# 预置金库和一段假对话，然后带着 JARVIS_UI_VAULT 启动
SIMCTL_CHILD_JARVIS_UI_VAULT="$C/Documents/Jarvis"     xcrun simctl launch --terminate-running-process "$SIM" com.dabao.jarvis
xcrun simctl io "$SIM" screenshot /tmp/ui.png
```

`JARVIS_UI_SCREEN=settings|model` 让那一屏直接出来（设置要点右上角才开得了，子页还要再点一下）。
`JARVIS_UI_VAULT` 是 `Vault.init` 里一个 **`#if DEBUG` 的口子**：给一个目录就直接用，
跳过文件选择器。选择器没法从命令行点（simctl 点不了屏幕），而要看的恰恰是选完之后
那几屏。Release 构建里这几行不存在。对话本身不用改代码就能预置——`SessionStore` 存的是
`Library/Application Support/session.json`，从宿主机写进去即可（模拟器里路径就是真路径）。

**这条路抓到过两个别的层面都抓不到的问题**：`Sparkline` 里 `yDomain` 那段（代码、注释、
意图全对，只有画出来的东西不对），以及设置页那条「去领密钥」的链接没出来——因为
**手动跑 `xcodebuild` 不会重编 Go 的 framework**。`ios/install-device.sh` 里专门写过这条
（「别自己判新旧，问 make」），而绕开脚本直接 `xcodebuild` 正好踩进去：Swift 编过、装上、
跑起来，只是桥那头还是旧的，新加的字段拿到的是空值。改了 `mobile/` 之后要么走
`make ios-install`，要么先 `make ios/Mobile.xcframework`。

iOS 端带日程、记账、健康、记忆四项（都是 RiskNone、改的都是本地文件，所以
不会冒出确认框），配了邮箱再加上邮件——发信那类收不回来的动作靠
`Host.Confirm` 问用户。浏览器只在桌面端。

**界面只有一整屏对话。** 早先是四个并排的标签页，后来换成输入框上面一排
四个胶囊（点开盖一张卡片），现在连那一排也没了：它们回答的问题（今天什么
安排、还剩多少、有什么要紧的信、体重多少）问一句话就有答案，而且答案是
挑过的。桌面端更早走完了同一步，见下面「界面」那一节。

该看细节的时候照样看，**从话里点进去**：秘书提到条目和邮件时写成
`[标题](jarvis://item/work.org:12)`、`[主题](jarvis://mail/<编号>)`，
`ChatView` 拦下 `jarvis://`（`OpenURLAction`）弹原来那几张卡片
（`ItemDetail` / `MailDetail`）。体征则是另起一行写
`[体重趋势](jarvis://chart/体重)`，`Markdown.swift` 把这一行识别成一种**块**，
画成挂在回答底下的趋势图（`ChartCard`）——「最近往上还是往下」一句话说不清，
而它一眼就给。三条要点：①编号只在这一轮会话里有意义，而界面那份清单来自
本地索引（标识是「账号:UID」），所以 `mobile/maillink.go` 在出口把编号改写成
key，**翻不出来的退化成纯文本**——点了没反应的蓝字比不可点更糟；②邮件 key
要 `url.PathEscape`（账号名可能是中文，否则 Swift 那边 URL 直接解不出来）；
③提示两端共用，所以 `internal/chat/markdown.go` 给 `jarvis://` 开了例外：
不发 OSC 8 死链接，只留下划线。

## 常用命令

测试覆盖（`go test ./... -cover`）：核心链路都有测——`agent` 的工具调用循环用注入 `http.RoundTripper` 的假模型跑脚本，不碰网络；`orgtools` 直接建临时 org 文件、调工具、把文件读回来比对，重点盯快照/撤销和删除的边界。

`mailbox` 那层**在进程里起一个假 IMAP** 测（`imap_test.go`）：`go-imap/v2` 自带
`imapserver` + `imapmemserver`，而它已经是直接依赖，起在本机不联网、不加依赖、不碰
任何真邮件。生产代码一行没改——`Connect` 走 TLS 拨号，而 `Client` 的字段是包内可见的，
测试在同一个包里自己拨号自己组装，比在生产路径上留一个注入点干净。这套测试一上来
就抓到一个真 bug，见下面 `FetchNew` 那条。`browser` 要真 Chrome，仍然没测。

```sh
make build        # go build -ldflags="-s -w" -o jarvis .
make install      # 编译并装到 ~/.local/bin/jarvis
make test         # go test ./...
make fmt          # gofmt -l -w .

go test ./internal/org/                 # 单个包
go test ./internal/org/ -run TestParse   # 单个测试
go test ./internal/org/ -run TestParse -v
go vet ./...

make install-remind   # 装 launchd 定时任务（每 5 分钟 jarvis remind）

make lint             # go vet + 死代码（deadcode，没装会说明怎么装）
make eval             # 拿真模型跑固定场景，断言它实际调了哪些工具（要钱要网）
```

**行为只有两层覆盖，别把它们搞混。**

① 确定性的、进 `make test` 的两份金标：`internal/chat/testdata/tools.json`（全部工具声明）
和 `testdata/prompt.txt`（系统提示 + 【当下】，带字节数）。它们能证明「提示/声明变了」，
**证明不了「变得更好还是更糟」**。改动是有意的就 `-update`，顺手在 diff 里看见自己让
每轮的常驻开销长了多少。金标类的测试必须钉住时钟（`testSkills` 里的 `testClock`）——
日程的 `Describe` 会印当天日期，不钉的话金标每天都不一样，而且**长度往往相同**，
报出来只是「内容不同」，最难猜。

② `make eval`（两组）：拿真模型跑固定场景。`internal/eval` 验 agent 循环和提示
（两端共用的那部分），`mobile` 那组验**只有手机那条桥才会坏的东西**——技能装配
（`Deps` 传错某项能力就凭空消失，编译和契约都不会响）、只回传真变过的文件、人设
出参的指针语义（没改是 nil，改成空串是「恢复内置」）、扩展技能原文过桥后在 Go 侧
解析并真被用上、对话历史原样往返。那一组故意绕一整圈 JSON（`ask` 辅助函数），
因为字段名写错、数组是 null 这类毛病只在这一圈里暴露；那个辅助函数顺手守住
「出参里不许有 null」，每条走它的测试都免费带上。
断言它**实际调了哪些工具**。
十一条场景，覆盖到人设真写进文件、扩展技能真被翻开并照着办（那份技能里的归账规矩
故意写成跟常识相反的，没读正文就会归错）、一句话三件事三件都落盘。
别的测试都给模型写好台词（假 `RoundTripper` 放脚本），于是提示词改一个字都不会有任何
测试响——而这个 app 的行为几乎全长在提示里。断言写成**性质**（「问安排必须先查、不许
直接新建」「记账要从已有账户里挑」「不做了要标取消、不许删」），不是逐字对照。所有文件
都在临时目录，一个字都不碰 iCloud 上真的日程和账本。一条失败不一定是回归——先看它到底
调了什么（失败时全部打印），再判断是模型抖动还是提示真写坏了。

`make lint` 里那个死代码检查有一处不显然：**gomobile 那道边界 deadcode 看不过去**。
`mobile` 包的导出函数是给 Swift 调的，Go 这边一个调用点都没有，于是 Ask、AskLive、
Snapshot、Providers 全被报成不可达——十几条噪音，人看两次就再也不看这个检查了。
所以豁免名单**从 Swift 的实际调用点现取**（gomobile 把 `AskLive` 导成 `MobileAskLive`，
在 `ios/*.swift` 里搜 `Mobile<名字>` 就知道那一侧还用不用它），不手写。手写的话，
哪天 Swift 不用了这儿也不会响，而那正是最该报出来的时候。

拉依赖要绕开代理：

```sh
unset HTTP_PROXY HTTPS_PROXY http_proxy https_proxy
export GOPROXY=https://goproxy.cn,direct GOSUMDB=off
```

运行时：`jarvis` 新会话，`jarvis -c` 继续上次会话，`jarvis remind` 跑一次提醒检查后退出。

## 架构

### 核心循环（internal/agent）

`Agent.Ask` 是一切的中心：拼系统提示 → 调模型（默认流式）→ 执行返回的工具调用 → 把结果回灌进 History → 直到模型不再调工具或超过 `MaxSteps`。它对业务零感知，所有能力通过 `Skill` 插进来。

- `tool.go` 定义 `Tool` / `Skill` / `Risk` / `AutoLevel`，是扩展的唯一入口。
- `Risk` 三级（`RiskNone` / `RiskReversible` / `RiskIrreversible`）配合 `AutoLevel` 三档（逐项确认 / 自动整理 / 全自动）决定某个动作是直接跑，还是发 `EventPending` 挂起等界面回 `Pending.Reply`。
- 可选接口 `ReadOnlyTool`：一批工具调用全部只读时，`executeParallel` 并发执行；只要有一个不是，整批串行。所有 skill 的工具适配器都有 `readonly` 字段，新工具别忘了设。
- 进度通过 `emit(Event)` 回调外抛（`EventDelta` / `EventToolCall` / `EventToolResult` / `EventPending` / `EventAuto` / `EventCompact`…），界面层只消费事件，不碰循环内部。
- 内置工具现在只有两个，而且都是**按条件注入**的：人设可写才有 `persona`（见下），
  有扩展技能才有 `skill`。早先还有 `plan` / `step_done`，删了——`Agent.Plan` 除了
  `plan.go` 自己没有任何读点，界面、桥、iOS 一个都不读，它们喂的那个进度展示压根不
  存在。代价却是每轮 239 token 的声明、提示里一条规则，以及模型照做时一个三步任务要
  多四次完整往返（每次都带着整份提示和全部历史）；更糟的是 `plan` 返回给模型的
  「已展示给用户」是假话，而提示里写着「绝不说自己做了没做的事」。干活进度界面一直
  有：`EventToolCall` 那串事件（桌面端状态行、iOS 的 ThinkingRow「正在查日程（第 2 步）」）。
  **发现它的是 `make eval`**：一条断言「三件事要先列计划」的场景红了，查下去才发现
  规则要求用的东西没人用——`deadcode` 抓不到这种（它只管函数，不管结构体字段）。
- `compact.go` 在估算 token 超过 `ContextLimit`（默认 32000）的七成时把早期对话压成一份
  「交接说明」。**切点只落在 `user` 消息上**，所以工具调用组永远切不散——切散了的后果
  不是答得差一点，而是下一轮请求被服务端直接拒（`tool` 消息必须紧跟在声明了对应
  `tool_calls` 的 assistant 之后）。压缩失败时历史一个字都不动：先问模型要摘要、拿到了
  才换掉旧对话，失败还把旧的删了的话，用户说过的话就凭空没了而他完全不知道为什么。
  这几条不变量由 `internal/agent/compact_test.go` 守着（其中「配对没切散」那条验过会
  失败：把切点改成随便切一刀，它当场红）。
  「摘要里有没有该有的内容」钉不到不变量上，那条在 `internal/eval/compact_test.go`：
  真模型跑几轮、逼出压缩、再问一件只有用户说过的事。**逼压缩不能靠随便填个小预算**
  ——`contextLimit()` 里那条「旧口径自愈」会把小于固定开销两倍的值兜回默认 32000，
  而 `fixed` 自己还会随环境快照浮动，所以要留余量（`(fixed+600)*2`）。
  **预算算的是一次请求的全部**：系统提示 + 工具声明 + 对话历史。早先只数 History，
  固定开销一分不算，于是工具表一路长到五十几个、每轮白发两万字节也没人发现——
  长的那部分根本不在账上。`fixedTokens()` 把这笔算进来，**但不缓存**——这两样里只有
  工具声明是真不变的，系统提示带着各技能的环境快照（余额、未读数、当前时间），
  `/auto` 换档也会换掉一段文案，按技能集合缓存一次就等于整个会话都用开头那一版，
  漏算的恰好是会长的那部分。
  （删掉 configskill / workflow、`update_item` 五合一之后是 36 个；加上后来的
  `persona` 和 `skill` 共 39，声明 14KB 上下、行为规则 10KB。）
- **提示的顺序是按「变不变」排的，不是按「重不重要」排的。** 请求的前缀是
  「工具声明 + system + 对话历史…」，那些兼容 OpenAI 协议的家伙都按「跟上一次请求的
  最长公共前缀」计费，命中的那段便宜一个数量级。所以：`systemPrompt()` 里**一个会变的
  字都没有**（人设、能力清单、各技能的 `Instructions`、通用规则、档位、用户自述），
  每轮都变的东西（几点几号、各技能 `Describe` 出来的环境快照）收进 `stateNote()`，
  作为**消息列表的最后一条**（role 用 system，不是 user——它不是人说的话），
  而且**不进 History**（进了的话旧快照永远留着，几轮之后就是一串互相矛盾的余额）。
  原先「现在是 … 14:21」排在系统提示第二行，每分钟一变，能缓存的只有人设那 360 字节；
  现在 14.9KB 的系统提示整段稳定、连同历史一起命中，每轮按原价的只剩末尾那 500 多字节。
  **往提示里加东西之前先问一句：它会不会每轮都变？** 会变的只能往 `stateNote` 里加，
  加在前面等于把它后面所有东西的缓存一起废掉。`internal/agent/prompt_test.go` 的
  `TestSystemPromptIsByteStableAcrossTurns` 守着这条，`internal/chat/cacheratio_test.go`
  拿真技能量实际数字（`go test ./internal/chat/ -run TestCachePrefixRatio -v`）。
  技能的规则里要指回快照，说「【当下】里那张…」，**不许说「上面那张」**——它在最后面；
  `internal/chat/label_test.go` 的 `TestNoSkillPointsUpwards` 守着。
- `client.go` 只讲 OpenAI 兼容协议，`Presets` 里列了 deepseek/qwen/kimi/minimax/zhipu/doubao/spark/baichuan/ollama。
- `build.go` 的 `agent.Build(cfg, skills...)` 是唯一的装配点；未配 provider 时返回 `(nil, err)`，调用方降级而不是崩。

### 技能（Skill）

一个 Skill = `Name()` + `Describe()`（往系统提示里塞环境快照）+ `Instructions()`（行为规则）+ `Tools()`。现有实现：

| 包 | 职责 |
|---|---|
| `internal/orgtools` | 日程：`Workspace` 包住 org 文件集合，带快照/撤销/改动记录 |
| `internal/mail` | 邮件：`Session` 持有连接、编号索引、triage 结论、草稿 |
| `internal/ledger` | 记账：ledger-cli 纯文本解析与追加 |
| `internal/health` | 健康：体征与饮食记录。NDJSON 一行一条 FHIR 资源，只追加——手机和电脑两头都在写，整文件重写会互相吃改动。**没有 HealthKit 导入，别加**，理由见下 |
| `internal/browser` | Chrome DevTools 自动化，连不上已有 Chrome 就自己起一个独立 profile |
| `internal/memory` | 跨会话长期记忆（`memory.md`），`/dream` 整理 |
| `internal/persona` | 人设文件（`persona.md`）的读写，两端共用一份规则 |
| `internal/skills` | 用户放在 `skills/` 下的扩展技能（SKILL.md），只解析不执行 |
| `internal/skillset` | **谁该在场**：两端共用的那一份技能装配表 |

**健康那块不许碰 HealthKit。** 曾经有过「从系统『健康』导入体重、步数、睡眠」
这个功能，整个删掉了：App Store 审核指南 5.1.3 明文规定，通过 HealthKit API 收集的
健康信息**不得存储在 iCloud**，而 `health.ndjson` 就在用户的 iCloud 云盘目录里。
两者不可兼得，而「手机和电脑同一份文件」是这个 app 的立身之本，先走的只能是 HealthKit。
规则只管「通过 HealthKit API 收集的」，用户自己说给 Jarvis 记的体征不受限，所以功能
本身没丢，丢的只是那条导入捷径。**这个功能看起来无害又顺手，最容易被后来人捡回去**，
捡回去就上不了架。连带删掉的还有 `mobile.ImportHealth`、`health.ImportSamples` /
`PrepareImport` / `AppendTo`——它们只为那条路存在，留着就是死代码。
上架的其余事项见 `APPSTORE.md`，隐私政策正文见 `PRIVACY.md`。

**隐私政策只有一份文件**：仓库根的 `PRIVACY.md`。它既要发成网页填进 App Store
Connect（那是必填项，app 内置的替代不了），又要在「设置 → 隐私政策」里看得到，
所以 `ios/genproject.py` 把它从上一层引进来打进 bundle，`PrivacyView` 读的就是
那一份。两处各抄一份迟早会漂，而这偏偏是一份声称「我们不收集数据」的文件，
说法不一致比没有更糟。两条写的时候要记得：

- **段落一律不手工折行。** `MarkdownText` 是为聊天气泡写的，逐行走，源码里
  折一行界面上就真断一行。markdown 的段落本来也该是一整行。
- 剥 HTML 注释的正则**必须带 `(?s)`**。`.` 默认不匹配换行，而那些给开发者看的
  注释全是跨行的——少了它一条都剥不掉，整段开发笔记原样印在隐私政策最上面。
  编译、类型检查、契约全不会响，只有真跑起来看才看得见（栽过）。

**人设不在代码里**：`internal/agent/persona.go` 有 `DefaultPersona` 兜底，真正用的那份在
iCloud 的 `persona.md`（`config.PersonaPath()`）。`Agent.SetPersona(text, save)` 把文本和
落盘方式一起装上，`save` 为 nil 就不注入那个工具——注入了却存不下来更糟，模型会回一句
「已经改好了」而下一轮人设原样，这恰恰是最不该骗人的地方。读文件留在各自那一端
（桌面 `chat.buildAgent`，手机由 Swift 读了过桥），agent 不碰文件。
`persona` 工具**一个兼读写**，靠 `text` 给没给分：没给是读，给了是覆盖，给空串是恢复
内置。参数是 `*string`，否则「恢复默认」和「只想看看」挤在同一个零值上——跟
`orgtools/args.go` 是同一笔账。出参里 `AskOutput.Persona` 同理必须是指针。

**用户可以自己加技能**：`skills/` 下一个子目录一份 `SKILL.md`（格式跟 Claude Code 的一样，
frontmatter 里 `name` + `description`）。**只有名字和用途常驻系统提示**，正文等模型调
`skill` 工具才读进来——全都常驻的话，放十份进去就是每轮白发几万字节，而其中九份跟当下
无关。它们**不带工具**：一份 markdown 讲的是「这件事该怎么办」，动作还是骑在现有工具上；
想加真正的新动作得写 Go。格式解析只有 `skills.Parse` 一份，手机端由 Swift 送原文过来
（`AskInput.Skills`），**不在 Swift 那边再写一个 frontmatter 解析器**——两边判据迟早不一样，
表现出来是同一份技能电脑上认、手机上不认。

**技能清单只有一份**：`internal/skillset` 的 `Build(Deps)`。原先桌面端（`chat.buildSkills`）和
手机端（`mobile/ask.go`）各写一份手抄清单——两端带的东西确实不一样（手机上不带浏览器，
邮件要配了邮箱才有），但这个差异写成了「另一份清单里恰好没写」，谁都看不出差在哪。
后果不是假想的：这份文档里那句「iOS 端只带日程和记账两项技能」**错了整整两项**，而要
发现它得把两个包里两份分散的清单并排比一遍。现在 `Deps` 里 **nil 表示不带**，两端的差异
是调用处明写的一行（`Browser: nil` 并说明为什么）。`internal/skillset/skillset_test.go`
把三种组合（桌面 / 手机配了邮箱 / 手机没配）钉住，改任何一端都会在 diff 里看得见。
注意别把 nil 的具体指针直接塞进 `[]agent.Skill`——装进接口之后接口本身不是 nil，
一调 `Name()` 就 panic，而手机上 panic 一次就是整个 app 挂掉。

**新增一项能力**：在 `internal/xxx/` 实现 `agent.Skill`，然后在 `internal/skillset` 的
`Deps` 里加一个字段、`Build` 里加一行，两端各自决定传不传。同时在 `internal/agent/label.go` 的 `toolLabels` 表里补上工具名到中文标签的映射，否则界面直接显示英文工具名——那张表桌面端和 iOS 共用一份，`internal/chat/label_test.go` 会守着，漏了就红。

**JSON Schema 那点词汇只有一份**：`internal/agent/schema.go` 的 `Obj` / `ObjAnyOf` /
`Str` / `Int` / `Num` / `Flag` / `Arr`，各技能包里留的是一行别名（`var obj = agent.Obj`），
不是第二份实现。原先是六个包各写一份函数体，而**已经漂出两处**：同一个 `num` 在
ledger/health 里生成 `"number"`、在 mail/orgtools/plan 里生成 `"integer"`；orgtools 那份
`obj`（当时叫 `schema`）还会多发一个 `"required": []`。照着邻居的写法抄一行，抄来的是
另一种类型，而模型读的正是这个 schema，运行期也不会报错。所以整数和浮点分成两个名字，
选哪个在调用处写明白。收拢那次靠 `internal/chat/testdata/tools.json` 这份金标对前后，
35 个声明只允许那两处 `"required": []` 消失（33 个工具本来就没这个字段，一直正常工作）。

**工具声明有金标**：`internal/chat/schemadump_test.go` 把全部工具的名字、风险、只读标记
和参数 schema 倒成 `testdata/tools.json` 对比。工具表是常驻开销，让它的任何变化都在 diff
里看得见；改动是有意的就 `go test ./internal/chat/ -run TestToolSchemas -update`，顺手也
就看见自己让声明长了多少字节。它还兜住改共用辅助函数那类静默事故——编译和别的测试都
不会响，只有这儿会。

**工具表是要花钱的**：每个工具的声明每一轮都重发一遍，描述写长一点、参数多列一个，都是常驻开销。同一个对象的多项修改合成一个工具（`update_item` 就是 `schedule_item`/`set_deadline`/`set_todo_state`/`set_priority`/`set_repeat` 五个合来的），比摊成五个各带一遍 `ref`+`title` 省得多。约束也要写进 schema（`schemaOneOf` 生成 `anyOf`）而不只写在描述的散文里——模型读的是 schema，靠运行期报错纠正要白费一个来回。

**「没给」和「给了空串」是两回事**，这条是 `internal/orgtools/args.go` 的存在理由。`args` 类型把这件事做成方法（`Has` / `OptStr`），`itemPatch` 让 `create_item` 和 `update_item` 读同一份结构：**没提**状态才落默认的 TODO，**给了空串**是要一条不带状态的条目；改的那边同理，没提就不动、给空串才清除。早先两个工具用 `""` 同时兼任两种意思，各记各的约定，`mobile/act.go` 也跟着分裂成两套写法。判据也要一致——`move_item` 的两条分支一度一条按「键在不在」、一条按「值是不是零」。`update_test.go` 里有一组专钉这套语义的用例。

**只读标记是按批生效的**：`executeParallel` 要求整批调用全只读才并发，所以一个技能把 `readonly` 丢光，拖慢的不止它自己——同一轮里本该并发的日程、邮件查询会被一起拉回串行。浏览器技能做工具合并时就这么丢过一次（四个只读工具全并进能改 DOM 的 `browser_exec_js`），编译测试全绿，没有任何地方会响；现在 `label_test.go` 的 `TestEverySkillKeepsAReadOnlyTool` 守着。

### 数据层

- `internal/org`：org-mode 解析与**行级写回**。`Document` 持有 `Lines`，改完 `Reparse()` 再 `Save()`；改动前先 `Workspace.Snapshot()` 留底，这是 `Undo` 的基础。不要重新生成整个文件，保留用户原有格式。
- `internal/agenda`：把 org 条目投影到日期上（计划/截止/事件/逾期）、找空档。
- `internal/mailbox`：IMAP/SMTP 底层（拉取、解析、垃圾箱探测、发信）。
- `internal/mailbox` 的 `FetchNew` 里有一处**协议怪癖的补丁**：IMAP 的 `n:*` 范围
  永远至少匹配最后一封，就算 `n` 比所有 UID 都大（RFC 3501 明写的）。所以「从
  sinceUID+1 到 *」在没有新信时会把最新那封重新拉回来，而调用方看到的是「有 1 封
  新的」。现在按 UID 再滤一遍，`Matched`/`Fetched` 也跟着减。这条是拿假 IMAP 跑
  测试时撞上的——真邮箱上它不会报错，只是每次同步白取一封正文，而下一个相信
  「空就是没新的」的调用方会直接被坑。
- `internal/state`：每个账号的同步进度。**按 UID 而不是时间划增量**——IMAP 的 SINCE 只比日期，按时间会重复拉当天邮件；`UIDValidity` 变了要重新全量。
- `internal/config`：单份 JSON 配置 `~/.config/jarvis/config.json`（权限 600）。改配置只有 `/` 命令一条路（`/setup` `/login` `/provider` `/model` `/files` `/ledger` `/theme` `/mailcheck` `/accounts` `/mailrm`）——原先还有个 `configskill` 让模型自己读改，两条路改同一份配置，`deadline_warn_days=0` 这类「传了零值」的意图在 `config.fill()` 那儿被当成「没这项」补回默认。技能删掉之后这个冲突自然没了；真要让模型改配置，得先让 `fill` 按「字段在 JSON 里出现过没有」判断。

配置里的路径（`files` / `inbox` / `ledger_file`）在 `Load` 末尾由 `normalize()` 统一展开成绝对路径（**包括配置文件不存在、直接返回默认值那条路径**），调用方一律假定拿到的就是绝对路径，不必各自记得调 `Expand`——漏掉就会在当前工作目录下造出一个名叫 `~` 的目录。密码和 API key 走 macOS 钥匙串（`KeychainGet`/`KeychainSet`，底层是 `security` 命令），也支持 `password_cmd` / `api_key_cmd` 从外部取。配置里不落明文。
- `internal/session`：对话历史持久化到 `~/.config/jarvis/sessions/`，时间戳做 id。

### 界面（internal/chat）

`internal/chat` 是真实入口，一个 raw 模式下的流式终端，不依赖 bubbletea：

- `render.go` — `screen` 是整屏渲染器，跟 Claude Code 同一个路子（那边用 Ink，这边手写）：进 alt screen 之后整块屏幕归自己画，**内容区**（历史 + 正在流的正文，可往回翻）在上，**输入框钉在最底下**。终端原生的 scrollback 不再参与，历史攒在 `history` 里自己管，滚动就是挪 `offset` 再重画。
  三条要害：①**只画变化的行**——每帧先拼出整屏该长什么样，跟 `prev` 逐行比，只重画不同的。少了这一步，流式正文每来一个字就刷几 KB 转义序列，终端跟不上，管道写满还会把自己卡死（踩过）。②每帧包在同步输出 `?2026h/l` 里，终端攒齐一帧再刷，不会看到画到一半。③进 alt screen、清屏之后必须把 `prev` 置空，否则 diff 会以为屏幕还是老样子。
  内容区不拼成新切片（那是每帧两次全量复制，而要画的只有一屏），按下标取；底部区一帧只算一次，滚动指示由 screen 自己接在状态栏那行后面——底部区的**行数**跟滚动无关，没必要为一行文字重算整块。
  退出时 `leave()` 会把 `history` 回放到主屏幕——这一趟的内容不会白跑，还留在终端里可翻可复制（这点比 Claude Code 强，它退出后屏幕就恢复原样了）。
- `input.go` — 按键先经 **`readKey` 解成 `key`**（kRune / kCtrl / kEsc / kUp / kWheelUp / …），再由 `handleKey` 按模式分派。这一层是有意抽出来的：「ESC 后面跟没跟字节」的消歧之前在打断、确认框、行编辑三处各判一遍，已经开始各走各的（确认框认不得 shift+tab、Home/End）。现在只在解码器里判一次，翻历史的按键在所有模式下统一先处理。`reader` 只管读键和报出输入框要占哪几行（`inputLines`），位置由 screen 安排。开了 `?1000h`+`?1006h` 收鼠标按键上报，滚轮是按键 64/65；没开 `?1002h/?1003h`（拖动/移动上报），事件量小些。**滚轮和终端原生拖选是互斥的**——开了上报，鼠标按下就归应用了。默认给滚轮（按住 option/shift 拖可绕过），`/mouse off` 换成拖选优先并记进配置；`screen.setMouseLocked` 是唯一的开关点，`leave()` 里务必撤干净，否则退出后终端还开着上报。这一层不依赖真终端，`keys_test.go` 直接喂字节测。
  开了 `?2004h`（bracketed paste），粘进来的内容被 `ESC[200~`/`ESC[201~` 包着，`pasting` 期间所有字符原样入缓冲（换行也留着），不然一段多行文本会被拆成好几条提交。输入框随内容长高，最多 `maxInputRows` 行。
  秘书干活时输入框照常留着，可以接着打字，回车把这句话排进 `Session.queued`，等这一轮忙完 `loop` 自动接着办；只有 esc / ctrl+c 是打断。
  **别让主程序碰 lipgloss/termenv**：它们会向终端查背景色（OSC 11）并直接从 tty 读应答，把用户按键一起吃掉，表现为「键盘完全没反应」。`jarvis remind` 那条路因此走 `chat.LoadOrg`。
- `live.go` + `markdown.go` — 正文走**重画模型**，画在内容区末尾（跟 Claude Code 一个路子）：正在流的那段回复放在屏幕末尾一块可反复重画的区域里，每来一点增量就把攒到现在的整段文本重新渲染、整块重画。于是「要看到后面才知道怎么画」的结构可以先画成半成品再改对——表格先显示原始管道符，等分隔行到了自己变成对齐的表格；`**粗体` 没闭合就先原样摆着。`renderMarkdown` 因此是个纯函数，不需要任何流式状态。
  整屏模型下它不用管光标也不用固化，只要交出「当前该显示哪些行」。渲染一次要把全文过一遍正则，所以做了两层：整段结果按「文本长度 + 宽度」缓存（挡住转圈、滚动、确认框上下挪），以及**定期切一刀把前面的定稿**（`lineCut`），往后每帧只渲染还没定稿的那一小段。切点可以落在任意行边界——`renderMarkdown` 逐行驱动，真正跨行的只有代码围栏和表格，躲开这两样即可；早先只认空行，遇到一大段不带空行的正文就完全退回每帧重渲全文（8k 要 116ms / 49MB，现在 1.4ms / 0.5MB）。切点只设在围栏之外，所以下一段总是从干净状态起步，不必跨帧带状态。切的时候记得排掉分界那个换行符，否则两段各自多一个空行；`isFence` 跟渲染器共用一份定义，免得哪天认了新的围栏写法两边走散；渲染出的每行仍必须用 `wrapANSI` 折到终端宽度以内，否则终端自动折行会让行数对不上。插工具调用、挂确认框、一轮结束前都要 `freeze()`，把这段定格进 `history`。
  改这块——以及任何碰光标的地方——务必跑 `screensim_test.go`——它拿一个迷你 ANSI 终端把逐字流的输出真画一遍，断言屏幕最终内容与整段渲染结果逐字节相同。
- `confirm.go` — 风险动作的确认框，也是底部区的一块（`show` 挂上、`done` 定格）。它亮着时输入框收起来只留状态栏，因为那会儿按键是挑选项不是打字。方向键/数字选择、回车确认。选项由 `agent.Pending.Risk` 决定：可逆动作多一项「以后不用问」（提到 `AutoClean`），不可逆的提到 `AutoAll`。

终端输出都经过 `termOut`（默认 `os.Stdout`），测试里换成缓冲区就能断言绘制序列，见 `screen_test.go`。

打断（esc / ctrl+c）时那条还没得到答复的消息会退回输入框（`reader.insert`），同时 `agent.NoteInterrupted()` 往上下文里记一笔，免得模型把没做完的当成做完了。

按键分发抽在 `reader.handleKey` 里（不依赖真终端，`keys_test.go` 直接喂按键测），`reader.loop` 只管读和分三种模式：`busy`（秘书干活，只认打断）、`picking`（确认框，键全给 `decide`）、正常行编辑。

`render.go` 的 `init` 把 `runewidth.DefaultCondition.EastAsianWidth` 关掉：框线、`❯`、`·` 这些 Ambiguous 字符在中文 locale 下会被算成两列，而终端按一列画，不关掉框就会散架。算可见宽度一律用 `runewidth`，带颜色的串先过 `stripANSI`。

`chat.go` 只留 Session 生命周期和 `loop`/`ask`/`emit`；`/` 命令在 `command.go`，launchd 提醒在 `remind.go`，配置向导在 `setup.go`（`setupSteps` 是步骤表，每步返回「改没改」和「要不要中止」，加一步就往表里加一行）。**向导必须幂等**：每步都先显示当前值、回车即跳过，填的还是原值就什么都不做——重复跑一遍不该改变任何已配好的东西（`sameStrings` 那类比较就是为这个）。

**启动路径不许把人挡在门外**：配置文件坏了、日程路径不可用，都只记一条 `startupWarn` 然后照常进界面——进去了才能用 `/setup` 修。`config.Load` 出错时返回的也是一份能直接用的默认配置（路径已展开）。新增启动步骤时守住这条：能降级就降级，别 `return err`。**命令表 `commands` 是补全和 `/help` 的单一数据源**——之前两处各写各的，`usage` 里一度挂着个根本不存在的 `/inbox`；新增命令只加表里一行，候选列表和帮助一起更新。

**新增工具后**除了在 `buildSkills()` 注册，还要在 `internal/agent/label.go` 的 `toolLabels` 表里补中文标签，否则界面直接显示英文工具名。

早先那版 bubbletea 四视图 TUI（`internal/ui`，约 4800 行）已经删掉了——`internal/chat` 是它的完整替代。连带摘掉了 bubbletea / bubbles / lipgloss / termenv 四个直接依赖，直接依赖从 8 个降到 5 个。想翻旧实现的话在 git 历史里（初始提交 `7febf4b`）。

后来 `chat` 里又长出过一套 tab 轮换的五视图（日程/待办/邮件/财务，`view.go` 约 350 行），现在也删了：它们做的事跟问一句「今天什么安排」完全重叠，而后者才是这东西的立身之本。tab 从此专职补全命令，`reader` 也不必再管输入框该不该画（原来那个 `boxed` 开关没了；确认框收起输入框走的是 `bottomLocked` 里 `confirmLines != nil` 那条路，跟视图无关）。**别把 lipgloss/termenv 再引回主程序**：它们会向终端查背景色（OSC 11）并直接从 tty 读应答，把用户按键一起吃掉，表现为「键盘完全没反应」。

删掉的东西里有一个 `chat` 没有对应实现的功能：`editor.go` 的「用 $EDITOR 打开条目编辑」。需要的话得重新写。

## 模型名是挑的，不是打的

`Client.Models(ctx)` 问服务商 `GET /models`，界面照着列，人挑一个。做它的理由是
**手打一个模型名换来的是一个看不懂的 404**，而第一次配的人根本不知道该填什么；
`Presets` 里那个默认名也会过时，到那天同样是 404。

- 管道是现成的：`Verify()` 本来就在 GET `/models`，只是把返回的内容整个丢掉。
  两者现在共用 `getModels()`——分成两趟的话，设置页里「测试连接」和「拉模型列表」
  就是两次网络，而它们问的是同一个问题。
- **只有 `id` 是协议规定的**，`name` / `context_window` / `input_modalities` 都是
  各家的扩展。缺了不能让整张列表作废，也不能猜一个出来。其中 `input_modalities`
  正是我们原先硬编码在 `Preset.Vision` 里的那件事——厂商自己就说了。
- **拿不到不是错**：有的服务商没实现这个接口。iOS 那页退回手填，桌面端照旧
  `/model <名字>`。别把配置流程卡在这一步（跟启动路径「能降级就降级」同一条规矩）。
- 入口：iOS 是 `ModelPicker`（设置 → 模型 → 模型名），桌面端是 `/model list`。
  `ModelPicker` 的 `config` 是**闭包不是快照**——界面上那几个框还在改，按值传进去的
  是构造那一刻的样子，人刚粘进去的密钥还没算数，一进来就「没问到」（踩过）。

**邮箱的服务器和端口不该让人填**：`internal/config/mailproviders.go` 按域名查出
IMAP/SMTP 参数，两端共用（桌面 `/mail` 向导、iOS 加邮箱页的 `autofill`，桥是
`mobile.MailProvider`）。这几项是固定的公开常识（imap.qq.com:993 十年没变过），
却要用户自己去搜，搜错一个字母换来的是一句 IMAP 的英文超时。三条讲究：

- **「认得」和「猜的」要分开**。没收录的域名照样给一份按 `imap.<域名>:993` 猜的
  （自建和企业邮箱绝大多数守这个约定），但 `known` 是 false，界面才好说实话——
  认得的说「已识别为 QQ 邮箱」，猜的说「按常见约定填的，连不上就自己改」。
- **不守约定的那几家才是表存在的理由**：苹果是 `imap.mail.me.com`（SMTP 还在 587，
  交给 `fill()` 推导会落到 465，收信正常、发信一直失败），讯飞收发都在
  `mail.iflytek.com`。猜得出来的那些，列进表里只是顺手。
- 只收**密码/授权码能登进去**的。微软个人版 Outlook 从 2024 年起停了 IMAP 的基本
  认证，只剩 OAuth，列进来等于给一份必然失败的配置，比不列更糟。
- iOS 的 `autofill` **只填空着的框**，判据是「跟上一份预设一模一样」——那说明这几项
  是我们填的，不是人填的。少了这条，人改一个字就被弹回预设值。

模拟器里查界面的四个口子（都只在 Debug 里存在）：`JARVIS_UI_VAULT` 指定数据目录、
`JARVIS_UI_SCREEN` 直接推到某一屏（`settings` / `model` / `models` / `mail`）、
`JARVIS_UI_MAIL` 给个地址直接开加邮箱那张表（好看自动填充的样子）、`JARVIS_UI_KEY`
顶替钥匙串里的密钥——模拟器的钥匙串从外面写不进去（simctl 只管证书），没有它就
永远看不到「密钥齐了」之后的那几屏。设了 `JARVIS_UI_SCREEN` 就不弹通知权限框：
那个框 simctl 点不掉，会盖在要看的那一页上，截出来的图永远是它。
