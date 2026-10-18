# jarvis 开发说明

jarvis 是一个 Go 终端个人助理，支持日程、邮件、记账、健康记录和长期记忆。代码注释、提交信息和用户文案使用中文。

## 目录约定

- `cmd/jarvis/`：桌面端可执行程序入口。
- `internal/agent/`：模型客户端、工具调用循环、风险确认和上下文压缩。
- `internal/chat/`：终端界面和交互命令；`acphost.go` 给 ACP 装配秘书。
- `internal/acp/`：`jarvis acp` 的协议层（Agent Client Protocol，stdio JSON-RPC），只管协议不管装配。改了它的行为就涨 `cmd/jarvis` 里的 `version`，UULink 按版本判断兼容。
- `internal/orgtools/`、`internal/mail/`、`internal/ledger/`、`internal/health/`、`internal/memory/`：业务技能。
- `internal/org/`、`internal/mailbox/`、`internal/mailstore/`：领域解析、邮件协议和本地存储。
- `internal/config/`、`internal/state/`、`internal/session/`：配置、状态和会话持久化。
- `internal/eval/`：需要真实模型的行为评测，不属于默认测试。
- `testdata/`：测试用 org 文件和其他固定输入。
- `launchd/`：macOS 定时提醒任务。

新功能优先放进对应的 `internal` 包；只有程序启动装配放在 `cmd/jarvis`。跨业务的能力通过 `agent.Skill` 接入，并在 `internal/skillset` 统一装配。

## 常用命令

```sh
make build        # 编译桌面端
make install      # 安装到 ~/.local/bin/jarvis
make test         # 运行全部 Go 测试
make lint         # go vet 和 deadcode（已安装时）
make fmt          # gofmt
make eval         # 真实模型行为评测，需要网络和费用
make install-remind
```

配置默认位于 `~/.config/jarvis/config.json`，密钥使用 macOS 钥匙串或外部命令读取，不写入配置文件。

提交改动前至少运行 `make test` 和 `make fmt`；涉及提示词、工具声明或行为规则时，补跑对应的 `internal/eval` 场景。
