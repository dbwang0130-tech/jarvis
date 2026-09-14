1、初始化向导：进入 jarvis 后自动创建 org 文件 ✅（internal/ui/load.go）
2、邮件/日程两个 agent 融合成 jarvis ✅
3、内核按 Skill 解耦，后续新场景实现 agent.Skill 即可接入 ✅
4、CLI 只保留 jarvis 一个入口，配置全部走界面 / 命令 ✅
5、记账技能（ledger-cli 纯文本格式）：记一笔、查余额、看流水 ✅
6、鼠标滚轮滚动 + macOS 自然滚动方向 ✅

后续可做：
- 邮件视图支持回车看全文、就地标记/清理
- /profile 命令编辑用户自述
- 更多 Skill：日历、浏览器……
