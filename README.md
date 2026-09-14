# jarvis — 个人助理

一个终端同时管理 **日程（org-mode）** 和 **邮箱**，内置一个能直接动手改文件和收发邮件的私人助理。

```
 jarvis
──────────────────────────────────────────────────────────────────────────
今天 2026-09-14 周一 · 3 件已排期 · 1 项今天到期
  工作:      14:00-16:00 NEXT [#A] 把 jarvis 用起来  :jarvis:
──────────────────────────────────────────────────────────────────────────
> 帮我整理今天的邮件，并把里面提到的会议排进日程
● 同步邮件 {}
● 整理邮件 {}
  ⎿  已标注 12 封…
● 新建条目 {title=…}
● 我整理完了：3 封要紧，5 封广告可以清掉…
```

## 安装

```sh
make install    # 编译出 jarvis 并装到 ~/.local/bin/
```

之后直接敲 `jarvis`。配置不用离开界面——所有配置都在里面用 `/` 命令完成。

## 快速开始

```sh
jarvis
```

启动就是助理，输入框已聚焦。第一次用先做两件事：

```
/login sk-...        # 设置模型密钥（不回显）
/provider qwen       # 换服务商，支持 deepseek / qwen / kimi / minimax / zhipu / doubao / spark / baichuan / ollama
/mail                # 交互式添加邮箱账号（账号名 → 邮箱 → IMAP 服务器 → 端口 → 密码/授权码）
/files ~/org         # 设置 org 文件或目录（目录不存在会自动创建）
```

没配置 org 文件时，会自动创建 `~/org/inbox.org`。

## 秘书能做什么

一个秘书，三套技能（Skill），还可以继续加：

- **日程**：查日程/待办/空档，安排时间、改截止、改状态和优先级、设循环任务、新建/删除条目
- **邮件**：增量同步（只拉新邮件）、triage 分类、写简报、把邮件里的时间点排进日程、清理广告、起草回信/新邮件
- **记账**：ledger-cli 纯文本格式，复式记账、查余额、看流水（`/ledger` 设置账本文件，未配置时默认用 `~/finance/ledger.dat` 并自动创建）

有风险的动作会停下来等你确认（`y` 执行 / `n` 取消）。自动档用 `ctrl+a` 切换：

| 档位 | 清理邮件 | 发信 |
|---|---|---|
| 逐项确认 | 问 | 问 |
| 自动整理 | 直接做 | 问 |
| 全自动 | 直接做 | 直接做 |

发出去的信收不回来，开「全自动」前想清楚。

## 界面

| 视图 | 键 | 说明 |
|---|---|---|
| 秘书 | `1` / `a` | 主视图，直接打字说话 |
| 日程 | `2` | 按天展开的计划/截止/事件，`d`/`w`/`m` 切日周月 |
| 待办 | `3` | 全部未完成条目，`/` 过滤 |
| 邮件 | `4` | 本次同步到的邮件清单，回车看摘要 |
| 财务 | `5` | 净资产/资产/负债概览、分类账户表、最近流水 |

`tab` 依次轮换四个视图。日程/待办里的操作键和 org-agenda 习惯一致：`t` 状态、`s` 计划、`D` 截止、`p` 优先级、`R` 循环、`c` 新建、`X` 删除、`I`/`O` 计时、`enter` 详情、`u` 撤销秘书改动。

秘书面板里：

| 键/命令 | 作用 |
|---|---|
| 直接打字 + 回车 | 对秘书说话 |
| `esc` | 秘书说话时打断；否则松开输入框 |
| `i` | 回到输入框 |
| `ctrl+r` | 同步新邮件 |
| `ctrl+n` | 往前加载更早的邮件 |
| `ctrl+a` | 切换邮件自动档 |
| `/login` `/provider` `/model` | 配置模型 |
| `/mail` `/accounts` `/mailrm` | 配置邮箱 |
| `/ledger [路径]` | 设置账本文件（ledger-cli 格式） |
| `/files` `/inbox` | 配置 org 文件 |
| `/clear` `/compact` | 清空/压缩上下文 |
| `/help` | 全部命令 |

## 配置

配置文件在 `~/.config/jarvis/config.json`（权限 `600`），一般用 `/` 命令改就行，也可以直接编辑：

```json
{
  "files": ["~/org"],
  "inbox": "~/org/inbox.org",
  "work_hours": { "start": 9, "end": 22 },
  "agent": { "provider": "deepseek", "temperature": 0.3, "auto_compact": true },
  "ledger_file": "~/finance/ledger.dat",
  "profile": { "interests": "我是研究生，导师是王老师，最近在写开题报告……" },
  "behavior": { "auto_confirm": "clean" },
  "accounts": [
    { "name": "gmail", "host": "imap.gmail.com", "user": "you@gmail.com" }
  ]
}
```

邮箱密码和模型密钥一律存 macOS 钥匙串，配置文件里不落明文（也可用 `password_cmd` / `api_key_cmd` 从 1Password、pass 等取）。

## 开发

```sh
make test     # go test ./...
make fmt      # gofmt 整个项目
make build    # 只编译不安装
```

架构上，秘书内核与业务解耦：

```
internal/agent/    秘书内核：模型客户端 + 工具循环 + 风险确认 + 上下文压缩
  tool.go          Tool / Skill 接口，新增场景实现 Skill 即可接入
internal/orgtools/ 日程技能（org 解析、agenda、改写工具）
internal/mail/     邮件技能（会话、同步、triage、起草、清理、发送）
internal/ledger/   记账技能（ledger-cli 纯文本解析、余额、追加交易）
internal/org/      org-mode 解析与写回
internal/agenda/   把条目投影到日期上的 agenda 引擎
internal/mailbox/  IMAP/SMTP
internal/config/   统一配置 + 钥匙串
internal/ui/       界面（四个视图）
```

拉依赖要绕开代理：

```sh
unset HTTP_PROXY HTTPS_PROXY http_proxy https_proxy
export GOPROXY=https://goproxy.cn,direct GOSUMDB=off
```
