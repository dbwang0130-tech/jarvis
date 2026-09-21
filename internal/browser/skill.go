package browser

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/chromedp/chromedp"

	"github.com/dabao/jarvis/internal/agent"
	"github.com/dabao/jarvis/internal/config"
)

// Skill 把浏览器自动化作为秘书的一项技能接入。
type Skill struct {
	Cfg    config.Config
	Client *Client
}

// New 构造浏览器技能。
func New(cfg config.Config) *Skill {
	return &Skill{
		Cfg: cfg,
		Client: &Client{
			Endpoint:   cfg.Browser.Endpoint,
			ProfileDir: filepath.Join(config.Dir(), "chrome-profile"),
		},
	}
}

// Close 关闭浏览器会话；自动启动的 Chrome 会被关闭，远程连接只断开不杀浏览器。
func (s *Skill) Close() { s.Client.Close() }

func (s *Skill) Name() string { return "浏览器" }

func (s *Skill) Describe(b *strings.Builder) {
	fmt.Fprintf(b, "Chrome 调试端点：%s（连不上时会自动启动一个独立配置的 Chrome）\n", s.Cfg.Browser.Endpoint)
}

func (s *Skill) Instructions() string {
	return `浏览器自动化：
1. 打开页面用 browser_navigate；读页面内容用 browser_read（给 CSS 选择器，
   要链接地址就把 links 设为真）。它是只读的，能跟别的查询并发，优先用它。
   browser_read 表达不了的才用 browser_exec_js 写表达式，结果 JSON.stringify 返回。
2. 结果保持精简：只取用户要的字段，不整页拉取，省 token。不截图。
3. 点击、输入、提交会真的在浏览器里产生作用，按风险档位执行；涉及付款、删除、发送时先确认。
4. 不确定自己在哪个页面，用 browser_exec_js 取 location.href 和 document.title；
   browser_navigate 本来就会把这两样返回给你。
5. 如果 browser_navigate 返回「需要登录」，立刻停下，请用户登录，等他说「登录好了」再继续；绝不要自己填密码、验证码或扫码。用户说登录不了时，按他的指示换方式、换站点或跳过。`
}

func (s *Skill) Tools() []agent.Tool {
	return []agent.Tool{
		s.toolNavigate(), s.toolRead(),
		s.toolClick(), s.toolType(), s.toolSubmit(), s.toolEval(),
	}
}

type tool struct {
	spec     agent.ToolSpec
	risk     agent.Risk
	readonly bool
	preview  func(args string) (string, string, error)
	run      func(args string) (string, bool, error)
}

func (t tool) Spec() agent.ToolSpec { return t.spec }
func (t tool) Risk() agent.Risk     { return t.risk }
func (t tool) ReadOnly() bool       { return t.readonly }
func (t tool) Preview(args string) (string, string, error) {
	if t.preview == nil {
		return "", "", nil
	}
	return t.preview(args)
}
func (t tool) Run(args string) (string, bool, error) { return t.run(args) }

func obj(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }

func flag(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}

// jsStr 把 Go 字符串安全地转成 JS 字符串字面量，避免选择器里的引号破坏脚本。
func jsStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// evalString 执行一段返回字符串的 JS。
func (s *Skill) evalString(expr string) (string, error) {
	var out string
	if err := s.Client.run(chromedp.Evaluate(expr, &out)); err != nil {
		return "", err
	}
	return out, nil
}

// evalRaw 执行一段返回任意值的 JS，结果转成紧凑 JSON 文本。
func (s *Skill) evalRaw(expr string) (string, error) {
	var out json.RawMessage
	if err := s.Client.run(chromedp.Evaluate(expr, &out)); err != nil {
		return "", err
	}
	return string(out), nil
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// checkLogin 判断当前页面是不是登录页：URL 命中登录关键词，或页面里有密码输入框。
func (s *Skill) checkLogin(url, title string) (bool, string) {
	u := strings.ToLower(url)
	for _, k := range []string{"login", "signin", "sign-in", "sign_in", "auth", "oauth", "passport", "sso", "logon"} {
		if strings.Contains(u, k) {
			return true, "URL 命中登录关键词"
		}
	}
	if v, err := s.evalString(`document.querySelector('input[type=password]') ? '1' : ''`); err == nil && strings.TrimSpace(v) == "1" {
		return true, "页面包含密码输入框"
	}
	return false, ""
}

func (s *Skill) toolNavigate() agent.Tool {
	return tool{
		spec: agent.ToolSpec{
			Name:        "browser_navigate",
			Description: "打开一个网址，返回最终地址和页面标题。",
			Parameters:  obj(map[string]any{"url": str("网址，如 https://example.com")}, "url"),
		},
		run: func(raw string) (string, bool, error) {
			var a struct {
				URL string `json:"url"`
			}
			if err := json.Unmarshal([]byte(raw), &a); err != nil {
				return "", false, err
			}
			url := strings.TrimSpace(a.URL)
			if url == "" {
				return "", false, fmt.Errorf("必须提供 url")
			}
			if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
				url = "https://" + url
			}
			var title, loc string
			if err := s.Client.run(chromedp.Navigate(url), chromedp.Title(&title), chromedp.Location(&loc)); err != nil {
				return "", false, fmt.Errorf("打开失败: %w", err)
			}
			if need, why := s.checkLogin(loc, title); need {
				return fmt.Sprintf("⚠ 页面需要登录（%s）。请立刻停下，等用户登录完成后再继续；不要自己填密码。", why), false, nil
			}
			return fmt.Sprintf("已打开：%s\n标题：%s", loc, title), false, nil
		},
	}
}

// toolRead 是唯一的只读抓取入口。
//
// 它一个顶掉了原先的 browser_text 和 browser_links，但**必须留着**：
// 九合五那一刀把四个 readonly 工具全并进了 browser_exec_js，而 exec_js
// 能改 DOM、能点按钮，没法标只读。结果整个技能一个只读工具都不剩——
// executeParallel 要求整批调用全只读才并发，于是一轮里只要捎带一次
// 网页读取，同轮的日程、邮件查询也跟着退回串行。
func (s *Skill) toolRead() agent.Tool {
	return tool{
		spec: agent.ToolSpec{
			Name: "browser_read",
			Description: "读取匹配 CSS 选择器的元素文本，最多约 2000 字，用于抓标题、价格、列表。" +
				"只读不改页面，能跟别的查询并发；要执行有副作用的脚本用 browser_exec_js。",
			Parameters: obj(map[string]any{
				"selector": str("CSS 选择器，如 .price、#result、a.title"),
				"links":    flag("为真时每行附上链接地址，用于抓链接列表"),
			}, "selector"),
		},
		readonly: true,
		run: func(raw string) (string, bool, error) {
			var a struct {
				Selector string `json:"selector"`
				Links    bool   `json:"links"`
			}
			if err := json.Unmarshal([]byte(raw), &a); err != nil {
				return "", false, err
			}
			sel := strings.TrimSpace(a.Selector)
			if sel == "" {
				return "", false, fmt.Errorf("必须提供 selector")
			}
			pick := `e=>(e.innerText||e.textContent||'').trim()`
			if a.Links {
				pick = `e=>{const t=(e.innerText||e.textContent||'').trim();return e.href?t+' → '+e.href:t}`
			}
			expr := fmt.Sprintf(
				`Array.from(document.querySelectorAll(%s)).map(%s).filter(Boolean).join('\n')`,
				jsStr(sel), pick)
			out, err := s.evalString(expr)
			if err != nil {
				return "", false, err
			}
			return truncate(out, 2000), false, nil
		},
	}
}

func (s *Skill) toolClick() agent.Tool {
	return tool{
		spec: agent.ToolSpec{
			Name:        "browser_click",
			Description: "点击匹配 CSS 选择器的第一个元素。",
			Parameters:  obj(map[string]any{"selector": str("CSS 选择器")}, "selector"),
		},
		risk: agent.RiskReversible,
		preview: func(raw string) (string, string, error) {
			var a struct {
				Selector string `json:"selector"`
			}
			_ = json.Unmarshal([]byte(raw), &a)
			return "点击页面元素", "选择器：" + a.Selector, nil
		},
		run: func(raw string) (string, bool, error) {
			var a struct {
				Selector string `json:"selector"`
			}
			if err := json.Unmarshal([]byte(raw), &a); err != nil {
				return "", false, err
			}
			if strings.TrimSpace(a.Selector) == "" {
				return "", false, fmt.Errorf("必须提供 selector")
			}
			if err := s.Client.run(chromedp.Click(a.Selector)); err != nil {
				return "", false, err
			}
			return "已点击 " + a.Selector, false, nil
		},
	}
}

func (s *Skill) toolType() agent.Tool {
	return tool{
		spec: agent.ToolSpec{
			Name:        "browser_type",
			Description: "向匹配选择器的输入框输入文字。",
			Parameters: obj(map[string]any{
				"selector": str("CSS 选择器，如 input[name=q]"),
				"text":     str("要输入的文字"),
			}, "selector", "text"),
		},
		risk: agent.RiskReversible,
		preview: func(raw string) (string, string, error) {
			var a struct {
				Selector string `json:"selector"`
				Text     string `json:"text"`
			}
			_ = json.Unmarshal([]byte(raw), &a)
			return "在页面输入文字", fmt.Sprintf("选择器：%s\n内容：%s", a.Selector, truncate(a.Text, 200)), nil
		},
		run: func(raw string) (string, bool, error) {
			var a struct {
				Selector string `json:"selector"`
				Text     string `json:"text"`
			}
			if err := json.Unmarshal([]byte(raw), &a); err != nil {
				return "", false, err
			}
			if strings.TrimSpace(a.Selector) == "" {
				return "", false, fmt.Errorf("必须提供 selector")
			}
			if err := s.Client.run(chromedp.SendKeys(a.Selector, a.Text)); err != nil {
				return "", false, err
			}
			return "已输入到 " + a.Selector, false, nil
		},
	}
}

func (s *Skill) toolSubmit() agent.Tool {
	return tool{
		spec: agent.ToolSpec{
			Name:        "browser_submit",
			Description: "提交匹配选择器的表单。这是会产生后果的操作，谨慎使用。",
			Parameters:  obj(map[string]any{"selector": str("表单选择器，如 form")}, "selector"),
		},
		risk: agent.RiskIrreversible,
		preview: func(raw string) (string, string, error) {
			var a struct {
				Selector string `json:"selector"`
			}
			_ = json.Unmarshal([]byte(raw), &a)
			return "提交页面表单", "选择器：" + a.Selector, nil
		},
		run: func(raw string) (string, bool, error) {
			var a struct {
				Selector string `json:"selector"`
			}
			if err := json.Unmarshal([]byte(raw), &a); err != nil {
				return "", false, err
			}
			if strings.TrimSpace(a.Selector) == "" {
				return "", false, fmt.Errorf("必须提供 selector")
			}
			if err := s.Client.run(chromedp.Submit(a.Selector)); err != nil {
				return "", false, err
			}
			return "已提交表单 " + a.Selector, false, nil
		},
	}
}

func (s *Skill) toolEval() agent.Tool {
	return tool{
		spec: agent.ToolSpec{
			Name:        "browser_exec_js",
			Description: "在页面里执行一段 JS 表达式并返回结果（建议用 JSON.stringify 返回紧凑结果）。用于精确抓取，比整页提取省 token。",
			Parameters:  obj(map[string]any{"script": str("JS 表达式，返回字符串或可 JSON 化的值")}, "script"),
		},
		risk: agent.RiskNone,
		run: func(raw string) (string, bool, error) {
			var a struct {
				Script string `json:"script"`
			}
			if err := json.Unmarshal([]byte(raw), &a); err != nil {
				return "", false, err
			}
			if strings.TrimSpace(a.Script) == "" {
				return "", false, fmt.Errorf("必须提供 script")
			}
			out, err := s.evalRaw(a.Script)
			if err != nil {
				return "", false, err
			}
			return truncate(out, 3000), false, nil
		},
	}
}
