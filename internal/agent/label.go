package agent

// 工具名翻成中文标签。
//
// **这张表只有一份。** 原先桌面端（internal/chat）和 iOS（ios/Views.swift）
// 各写了一份，无人对账，结果 iOS 那份里还留着早就删掉的 read_mail、
// 挂着五个根本不存在的工具，而新加的健康那四个一个都没补——界面上直接
// 显示 record_vital 这种英文。
//
// 放在 agent 包里是因为工具本来就定义在这儿（ToolSpec）。桌面端直接查，
// 手机端由 mobile 查好了跟着 Step 一起过桥，Swift 拿现成的字符串显示。
var toolLabels = map[string]string{
	// 内置
	"plan": "制定计划", "step_done": "标记完成",
	// 日程
	"list_agenda": "查看日程", "list_todos": "查看待办", "find_free_slots": "寻找空档",
	"get_item": "查看条目", "schedule_item": "安排时间", "set_deadline": "设置截止",
	"set_todo_state": "修改状态", "set_priority": "设置优先级", "create_item": "新建条目",
	"set_repeat": "设置循环", "delete_item": "删除条目", "clock_in": "开始计时",
	"clock_out": "结束计时", "add_note": "记笔记", "demote_item": "降级",
	"promote_item": "升级", "move_item": "移动条目",
	// 邮件
	"list_mail": "查看邮件", "triage": "整理邮件", "refresh_mail": "同步邮件",
	"load_more_mail": "加载更早邮件", "mark_read": "标记已读", "clean_mail": "清理邮件",
	"draft_reply": "起草回信", "draft_new": "起草邮件", "send_draft": "发送邮件",
	// 记账
	"list_accounts": "查余额", "list_transactions": "看流水", "add_transaction": "记账",
	// 健康
	"record_vital": "记体征", "record_meal": "记饮食",
	"set_health_profile": "填基础信息", "list_health": "查健康记录",
	// 浏览器
	"browser_navigate": "打开网页", "browser_text": "提取文本", "browser_links": "提取链接",
	"browser_click": "点击", "browser_type": "输入", "browser_submit": "提交",
	"browser_exec_js": "执行JS", "browser_title": "读标题", "browser_url": "读网址",
	// 配置、工作流、记忆
	"read_config": "读配置", "set_config": "改配置", "remove_mail_account": "删除邮箱",
	"list_workflows": "列出工作流", "save_workflow": "保存工作流",
	"run_workflow": "运行工作流", "delete_workflow": "删除工作流",
	"remember": "记住偏好",
}

// ToolLabel 把工具名翻成界面上显示的中文，没登记过就原样返回。
func ToolLabel(name string) string {
	if l, ok := toolLabels[name]; ok {
		return l
	}
	return name
}
