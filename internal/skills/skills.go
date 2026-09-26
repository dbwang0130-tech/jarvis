// Package skills 读用户放在 skills/ 目录下的扩展技能。
//
// **现有能力（日程、邮件、记账、健康、浏览器、记忆）不走这儿**，它们是编进
// 代码的 agent.Skill，带着自己的工具。这个包管的是用户自己加的那些：一份
// SKILL.md，讲清某件事该怎么办，骑在现有工具上跑。
//
// 格式跟 Claude Code 的 skill 一样，方便直接搬现成的：
//
//	skills/
//	  月度对账/
//	    SKILL.md
//
//	---
//	name: 月度对账
//	description: 每月核对信用卡账单。用户说「对账」「账单来了」时用。
//	---
//
//	正文……
//
// **只有 name 和 description 常驻系统提示**，正文要等模型调 skill 工具才读
// 进去。全都常驻的话，丢十个文件进来就是每一轮白发几万字节——而其中九个
// 跟这一轮要办的事无关。这也是 Claude Code 那边的做法。
package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// maxBody 是单份技能正文的上限。
//
// 有上限不是怕磁盘，是怕上下文：正文是模型主动读进去的，一份十万字的文件
// 一读就把这一轮的预算吃光，接着触发压缩，把真正在办的事挤出去。宁可截断
// 并在末尾说一句，也不能让它悄悄吃掉整个窗口。
const maxBody = 32 << 10

// Skill 是一份从文件读来的扩展技能。
type Skill struct {
	// Name 是模型调 skill 工具时用的名字，默认取目录名。
	Name string
	// Description 一句话说清「什么时候该用它」，常驻系统提示。
	Description string
	// Body 是正文，按需才交给模型。
	Body string
	// Path 是 SKILL.md 的位置，报错时说得出是哪一份。
	Path string
}

// Load 扫 dir 下每个子目录里的 SKILL.md。
//
// **读不出来不算错。** 目录不存在、某一份格式不对、iCloud 占位符还没下载，
// 都只是少一项扩展能力，不该让整个会话起不来——启动路径不许把人挡在门外。
// 出问题的那几份连同原因一起返回，界面可以提一句，但照常往下走。
func Load(dir string) (list []Skill, warns []string) {
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		// 没建过这个目录是常态，不值得报。
		if !os.IsNotExist(err) {
			warns = append(warns, fmt.Sprintf("扩展技能目录读不出来（%s）：%v", dir, err))
		}
		return nil, warns
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		path := filepath.Join(dir, e.Name(), "SKILL.md")
		raw, err := os.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				warns = append(warns, fmt.Sprintf("%s 读不出来：%v", path, err))
			}
			continue
		}
		s, warn, ok := Parse(e.Name(), string(raw))
		s.Path = path
		if warn != "" {
			warns = append(warns, path+"："+warn)
		}
		if !ok {
			continue
		}
		list = append(list, s)
	}
	return list, warns
}

// Parse 把一份 SKILL.md 的原文变成 Skill。dirName 是它所在目录的名字，
// frontmatter 里没写 name 就用它。
//
// **导出是为了让手机端共用这一份。** iOS 上 Go 不碰文件，技能原文由 Swift
// 读出来过桥送进来——格式解析必须还是这一份代码，Swift 那边再写一个的话，
// 「BOM 算不算」「frontmatter 没闭合怎么办」这类判据迟早两边不一样，而表现
// 出来是「同一份技能电脑上认、手机上不认」，最难猜。
//
// ok 为假表示这份不能用（正文空、frontmatter 没闭合）；warn 非空是有话要说，
// 跟 ok 无关——缺 description 照样能用，但值得提一句。
func Parse(dirName, content string) (s Skill, warn string, ok bool) {
	s = parse(content)
	if s.Name == "" {
		s.Name = dirName
	}
	if strings.TrimSpace(s.Body) == "" {
		return s, "正文是空的，跳过", false
	}
	if s.Description == "" {
		// 没有 description 模型就无从判断什么时候该用它，但也不至于
		// 丢掉——拿名字顶上，并提醒一句。
		s.Description = s.Name
		return s, "缺 description，模型可能不知道什么时候该用它", true
	}
	return s, "", true
}

// parse 拆出 frontmatter 和正文。
//
// 自己拆三行键值，不引 YAML 库：这个项目的直接依赖只有 5 个，为了读两个
// 字段搭一整个 YAML 解析器不划算，而 frontmatter 里真正用到的就这两行。
// 没有 frontmatter 的文件也收——整份当正文，名字取目录名。
func parse(text string) Skill {
	// 有些编辑器会在文件开头写一个 BOM，留着的话第一行就不等于 "---"，
	// 整份 frontmatter 会被当成正文念给模型。这儿按字节去掉，源码里不放
	// 那个字符本身——放了 Go 编译器自己就先报「invalid BOM」。
	text = strings.TrimPrefix(text, string([]byte{0xEF, 0xBB, 0xBF}))
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return Skill{Body: clamp(text)}
	}
	var s Skill
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			s.Body = clamp(strings.Join(lines[i+1:], "\n"))
			return s
		}
		key, val, ok := strings.Cut(lines[i], ":")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		// description 里常有冒号和引号，去引号但不动内部。
		val = strings.Trim(val, `"'`)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "name":
			s.Name = val
		case "description":
			s.Description = val
		}
	}
	// frontmatter 没闭合：别把整份文件当正文交出去，那样 name/description
	// 那几行会被当成正文念给模型。当成格式不对，正文留空，由 Load 跳过。
	return s
}

func clamp(body string) string {
	body = strings.TrimSpace(body)
	if len(body) <= maxBody {
		return body
	}
	// 按 rune 边界截，别把一个汉字劈成两半。
	//
	// **只剥续字节是不够的**：留在末尾的那个首字节自己也算「rune 开头」，
	// 循环当场就停，剩下一个不完整的序列——中文正文截断处会多一个乱码字符。
	// 直接按「这一整段还是不是合法 UTF-8」往回退，最多退三个字节。
	cut := body[:maxBody]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut + "\n\n（这份技能太长，后面截掉了。）"
}
