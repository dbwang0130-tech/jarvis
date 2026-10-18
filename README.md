# Jarvis

Jarvis 是一个运行在终端里的个人助理。你用自然语言交代事情，它负责查资料、调用工具、修改本地文件，并在需要时停下来向你确认。

它把日程、邮件、记账、健康记录和长期记忆放进同一个对话里，适合希望把工作流留在本地、又不想维护一套复杂自动化脚本的人。

> 项目目前处于早期阶段。数据格式、配置项和工具行为仍可能变化，重要数据请先做好备份。

> Jarvis 不提供 iOS App。想在 iPhone 上用，可以通过 [UULink](https://uulink.pro/) 连回 Mac 上的 Jarvis，见下文「在 iPhone 上使用」。

## 能做什么

- **日程与待办**：读取 org-mode 文件，查日程、待办和空档，创建和修改条目，设置截止日期、优先级和循环任务。
- **邮件**：通过 IMAP/SMTP 增量同步、整理、标记、起草和发送邮件。
- **记账**：读写 ledger-cli 风格的纯文本账本，查询账户余额和交易流水。
- **健康记录**：用 NDJSON 文件记录体征和饮食。
- **长期记忆**：保存你主动告诉 Jarvis 的偏好和背景，在后续会话中继续使用。

Jarvis 按风险级别执行操作。查询和本地可逆整理可以自动完成；发信、删除等无法轻易收回的动作会先请求确认。你可以用 `/auto` 调整自动化级别，也可以随时中断当前任务。

## 快速开始

### 环境要求

- macOS 或其他支持 Go 终端程序的 Unix 系统
- Go **1.26.4** 或兼容版本
- 一个兼容 OpenAI Chat Completions API 的模型服务商及 API 密钥
- 需要邮件功能时，准备 IMAP/SMTP 账号

### 安装

从源码编译并安装到 `~/.local/bin/jarvis`：

```sh
git clone https://github.com/dabao/jarvis.git
cd jarvis
make install
```

也可以只编译当前目录的可执行文件：

```sh
make build
./jarvis
```

### 第一次运行

```sh
jarvis
```

进入界面后运行配置向导：

```text
/setup
```

向导会依次设置模型、日程文件、邮箱、账本、自动化级别和定时提醒。已经配置过的项目可以直接回车保留原值。

常用单项命令：

```text
/login <密钥>             设置模型 API 密钥
/provider <服务商>       切换模型服务商
/files <路径...>         设置 org-mode 文件或目录
/mail                    添加邮箱账号
/ledger [路径]           设置账本文件
/auto <off|clean|all>    设置自动化级别
/mailcheck [时间...]     设置自动收信时间，例如 08:30 18:00
```

默认情况下，日程文件使用 `~/org`，账本使用 `~/finance/ledger.dat`。不存在的目录会在需要时创建。

## 在 iPhone 上使用

Jarvis 不提供 iOS App。需要在手机上用时，可以安装 [UULink](https://uulink.pro/)（[App Store](https://apps.apple.com/app/id6819126745)），
由它经 ACP 反向连接到 Mac 上运行的 Jarvis：

1. 在 Mac 上安装 Jarvis（见上文「安装」），并在终端里完成 `/setup`；
2. 在 Mac 上安装并打开 UULink，用 iPhone 上的 UULink 扫码配对；
3. 在 Mac 的 UULink「设置 › Agent」中点按「+」添加 Jarvis。

之后在 iPhone 上新建 Jarvis 会话即可。对话、日程、邮件、账本等数据仍在 Mac 本地，发信、清理邮件这类需要确认的动作会在 iPhone 上弹出审批。

## 用其他客户端驱动（ACP）

```sh
jarvis acp
```

经 stdio 提供 [Agent Client Protocol](https://agentclientprotocol.com)（换行分隔的 JSON-RPC 2.0），
UULink、Zed 等 ACP 客户端可以直接把 Jarvis 当作一个 agent 接入。支持：

- `session/new`、`session/load`（回放历史）、`session/list`、`session/close`、`session/delete`；
- `session/prompt` 流式推送正文、思考和工具卡片，可带图片；`session/cancel` 中断；
- 有风险的动作用 `session/request_permission` 向客户端确认，「以后不再问」会把本会话的自动化级别抬到正好放行这一级；
- 自动化级别是会话模式（`off` / `clean` / `all`，`session/set_mode`）；配置里存了多套模型时，以 `model` 选项出现（`session/set_config_option`，只影响当前会话）。

会话和终端界面存在同一个目录里，`jarvis -c` 也能接着 ACP 里聊过的会话。ACP 会话额外记下所在目录和标题
（`sessions/meta/`），终端里开的会话归在主目录下。`jarvis --version` 显示版本号。

## 配置与数据

配置文件默认位于：

```text
~/.config/jarvis/config.json
```

配置文件只保存路径、偏好和服务商设置。模型密钥和邮箱密码应使用系统钥匙串，或通过 `api_key_cmd`、`password_cmd` 等外部命令读取；不要把明文密钥提交到仓库或写进共享配置。

主要数据文件包括：

| 数据 | 默认位置 | 格式 |
| --- | --- | --- |
| 日程和待办 | `~/org` | org-mode |
| 账本 | `~/finance/ledger.dat` | ledger-cli 文本格式 |
| 健康记录 | `~/health/health.ndjson` | NDJSON |
| 长期记忆 | 配置目录下的 `memory.md` | Markdown |
| 会话历史 | 配置目录下的 session 文件 | JSON |

Jarvis 不提供云端数据同步服务。模型请求会把当前对话和必要的工具结果发送给你配置的模型服务商；请根据服务商的隐私政策选择模型。

## 终端操作

- `Enter`：提交输入
- `Alt-Enter`：换行
- `Tab`：补全 `/` 命令
- `Esc` / `Ctrl-C`：中断当前任务或取消确认
- `PageUp` / `PageDown`：翻看历史
- `Ctrl-R`：重新读取日程文件
- `Ctrl-L`：清除可翻看的历史
- `Ctrl-D`：空输入行退出

回复支持 Markdown、代码块、表格和链接。高风险操作会在界面中显示确认选项。

## 开发

项目使用标准 Go 目录布局：

```text
cmd/jarvis/       可执行程序入口
internal/agent/   模型客户端、工具调用循环、风险控制和上下文压缩
internal/chat/    终端界面和交互命令
internal/*        日程、邮件、记账、健康、记忆等领域模块
launchd/          macOS 定时提醒任务
website/          项目官网静态页面
testdata/         固定测试数据
```

常用命令：

```sh
make test         # 运行全部测试
make lint         # go vet；安装 deadcode 后也会检查死代码
make fmt          # 格式化 Go 源码
make build        # 编译桌面端
make eval         # 真实模型行为评测，需要网络和费用
make install-remind
make uninstall-remind
```

开发时如果网络代理影响 Go 依赖下载，可以使用：

```sh
unset HTTP_PROXY HTTPS_PROXY http_proxy https_proxy
export GOPROXY=https://goproxy.cn,direct GOSUMDB=off
go mod download
```

新增能力时，实现 `agent.Skill` 并在 `internal/skillset` 中装配；新增不可逆动作时，必须明确声明风险级别并补充确认路径测试。涉及提示词、工具声明或模型行为的改动，应同时更新固定测试和 `internal/eval` 场景。

## 贡献

欢迎提交 Issue 和 Pull Request。建议先说明：

1. 要解决的问题和可复现步骤；
2. 预期行为与实际行为；
3. 运行环境、Go 版本和模型服务商；
4. 相关测试结果。

提交代码前请运行：

```sh
make fmt
make test
make lint
```

## 许可

项目许可证尚未确定。正式发布前请补充根目录 `LICENSE` 文件，并在这里替换本节内容。
