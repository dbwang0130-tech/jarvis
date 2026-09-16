# iOS 版 jarvis

手机上看日程、待办、账本，也能跟秘书说话让它改。数据是 iCloud 里那一份，
跟电脑上的 jarvis 读写同一批文件。

```
iPhone (Swift)                 Mac (jarvis)
     │                              │
     └──────┬───────────────────────┘
            ▼
   iCloud Drive/Jarvis/
     org/*.org
     ledger.dat
```

## 为什么这么分工

Go 那边（`mobile` 包）**一个文件都不碰**，只收内容、吐内容。iOS 的文件访问
有一串坑——沙箱要用户亲自授权、iCloud 文件可能只是个没下载的占位符、读写
要经 `NSFileCoordinator` 协调，不然跟 Mac 那头同时改会互相踩。这些全关在
Swift 的 `Vault` 里，Go 只管解析和算。

好处是 **org 解析两端共用一套**。你的 org 是真实数据，两边各写一个解析器，
迟早在某个边界上解出不同结果。

## 第一次搭起来

### 1. 装 Xcode

App Store 搜 Xcode，10+ GB。装完打开一次，让它装完附加组件，然后把命令行
指到完整的 Xcode（默认还指着精简版的 Command Line Tools）：

```sh
sudo xcode-select -s /Applications/Xcode.app/Contents/Developer
sudo xcodebuild -license accept
xcodebuild -version                                     # 确认
```

### 2. 生成 Go framework

```sh
go install golang.org/x/mobile/cmd/gomobile@latest
gomobile init
make ios-framework     # 产出 ios/Mobile.xcframework
```

### 3. 打开工程

```sh
open ios/Jarvis.xcodeproj
```

工程文件已经在仓库里了，源文件、framework 嵌入、Scheme 都配好了，不用手动拖。

### 4. 签名

Target → Signing & Capabilities：

- Team 选你的 Apple ID（没有就 Xcode → Settings → Accounts 加一个）
- Bundle Identifier 默认 `com.dabao.jarvis`，被占用的话改一个

免费账号签的 app **7 天过期**，到期插上电脑重新 Run 一次就行。
付费开发者账号（$99/年）签的管一年。

### 5. 装到手机

手机插电脑，Xcode 顶部选你的设备，按 ⌘R。

第一次装完手机上会提示「不受信任的开发者」——
设置 → 通用 → VPN 与设备管理 → 点你的 Apple ID → 信任。

### 6. 在 app 里选目录

第一次打开会让你选 iCloud 目录：**iCloud Drive → Jarvis**。
选的是那个**文件夹**，不是里面的文件。选完它会记住（存的是安全作用域书签），
以后不用再选。

然后进设置填模型密钥（存在本机钥匙串里，不跟着 iCloud 走）。

## 代码怎么分的

| 文件 | 干什么 |
|---|---|
| `JarvisApp.swift` | 入口 |
| `Vault.swift` | iCloud 目录的读写：授权、书签、下载、协调 |
| `Bridge.swift` | 调 Go，JSON 进出 |
| `Store.swift` | 界面的数据来源，串起读文件 → Go 解析 → 写回 |
| `Models.swift` | 跟 Go 那边 JSON 一一对应的结构 |
| `Views.swift` | 日程 / 待办 / 财务 / 对话四个页面 |
| `SettingsView.swift` | 模型、目录、自述 |
| `Keychain.swift` | 密钥存取 |
| `Jarvis.xcodeproj` | Xcode 工程，源文件和 framework 都配好了 |

Go 那边只有两个入口，都在 `mobile/`：

- `Snapshot(json) -> json` 把文件解析成界面要显示的内容
- `Ask(json) -> json` 把一句话交给秘书，返回答复和**要写回的文件**

两个都保证不 panic、出错填在返回值的 `error` 里——手机上崩一次就是整个
app 挂掉。

## 手机上能干什么，不能干什么

**能**：看日程待办、看账本、跟秘书说话改日程、记账。

**不能**：收发邮件、浏览器自动化。那两样留在桌面端——它们有不可逆的动作
（信发出去收不回来），手机上弹确认框很别扭，而且要把半途的状态存下来等
用户回话。日程和记账改的都是本地文件、都可逆，所以 iOS 端固定跑在
「自动整理」档，不会中途拦着问你。

## 改代码之后

```sh
make ios-check      # Swift 在 iOS SDK 下类型检查（需要完整 Xcode）
make ios-contract   # 两端 JSON 字段名和可选性对不对得上（只要 swiftc）
```

`ios-contract` 值得每次动 `mobile/` 的结构体或 `Models.swift` 时跑一遍：
Go 改了字段名、或让某个数组变回 nil，Go 自己的测试照样绿，装到手机上才
发现界面一片空白。

## 改了 Go 代码之后

```sh
make ios-framework     # 重新生成 framework
```

Xcode 里 ⌘R 重新跑。`mobile` 包的函数签名改了的话，`Bridge.swift` 那两个
`callSnapshot` / `callAsk` 也要跟着改。

## 两边同时改同一个文件会怎样

iCloud 自己会处理冲突，但它的办法是留下一个「冲突副本」。所以：

- 手机上改动是**整文件写回**，写的是 Go 刚改完的那一版
- 只写真的变过的文件——多写一次就多一次同步、多一次撞车的机会
- 电脑那头正在编辑时，别在手机上改同一个文件

真撞上了，iCloud 会在目录里留一个带「冲突」字样的副本，手动合一下。
