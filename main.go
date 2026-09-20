// jarvis 是个人助理：日程 + 邮箱 + 记账 + 浏览器，一个流式终端搞定。
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dabao/jarvis/internal/chat"
	"github.com/dabao/jarvis/internal/config"
	"github.com/dabao/jarvis/internal/mail"
	"github.com/dabao/jarvis/internal/remind"
)

func main() {
	// 子命令：jarvis remind 由 launchd 定时调用，检查一次日程提醒后退出。
	if len(os.Args) > 1 && os.Args[1] == "remind" {
		if err := runRemind(); err != nil {
			fmt.Fprintln(os.Stderr, "jarvis remind: "+err.Error())
			os.Exit(1)
		}
		return
	}

	cont := flag.Bool("c", false, "继续上次的会话")
	flag.Usage = usage
	flag.Parse()
	if err := chat.Run(*cont); err != nil {
		fmt.Fprintln(os.Stderr, "jarvis: "+err.Error())
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `jarvis — 个人助理：日程 + 邮箱 + 记账 + 浏览器

用法：
  jarvis         开一个全新会话
  jarvis -c      继续上次的会话
  jarvis remind  检查一次日程提醒（供 launchd 定时调用）

进去之后：直接说话，滚轮或 PageUp/PageDown 往回翻历史，
tab 补全 / 命令，/help 看全部命令。
第一次用先敲 /setup，跟着走一遍就配好了。
`)
}

func runRemind() error {
	cfg, err := config.Load(config.Path())
	if err != nil {
		return err
	}
	coll, err := chat.LoadOrg(cfg)
	if err != nil {
		return err
	}
	store := &remind.Store{Path: filepath.Join(config.Dir(), "reminded.json")}
	reminded := store.Load()
	now := time.Now()

	// 每天早上那一句。Check 只认排了具体钟点的事，而截止和逾期本来就没有
	// 钟点——「周五前交报告」不会响，「昨天就该做完的」也不会响，
	// 而那两样恰恰是最容易忘的。
	if text := remind.Brief(coll, now, reminded); text != "" {
		if err := remind.Notify(text); err != nil {
			fmt.Fprintf(os.Stderr, "发通知失败：%v\n", err)
		}
	}

	// 到点收信。这是**桌面端独有**的：launchd 每五分钟给一次运行机会，
	// 拿它真的连一趟 IMAP。手机上办不到——iOS 不让 app 在后台按点醒来连网。
	if slot, due := remind.MailDue(cfg.MailCheckTimes, now, reminded); due {
		if text := checkMail(cfg, slot); text != "" {
			if err := remind.Notify(text); err != nil {
				fmt.Fprintf(os.Stderr, "发通知失败：%v\n", err)
			}
		}
	}

	for _, text := range remind.Check(coll, now, reminded) {
		if err := remind.Notify(text); err != nil {
			// launchd 把 stdout/stderr 收进 /tmp/jarvis-remind.log，
			// 发不出去的时候至少在那儿留个痕迹。
			fmt.Fprintf(os.Stderr, "发通知失败：%v\n", err)
		}
	}
	return store.Save(reminded)
}

// checkMail 收一趟信，把「有几封新的、都是谁来的」攒成一句话。
//
// **不自动分拣。** 判断要紧不要紧要调模型，而这是个在后台自己跑的任务：
// 人可能不在电脑前，一天三趟悄悄花钱不合适；何况看一眼发件人多半就知道
// 要不要紧了。想让秘书过一遍，打开 app 说一句就是。
//
// 一封新的都没有就不吭声——「没有新邮件」也是一次打扰。
func checkMail(cfg config.Config, slot string) string {
	if len(cfg.Enabled()) == 0 {
		return ""
	}
	sess := mail.NewSession(cfg)
	defer sess.Close()

	r := sess.Sync()
	if len(r.New) == 0 {
		// 连不上也不吵人：网不好、密码过期这类事，留在日志里就行，
		// 没必要每两小时弹一次「收信失败」。
		for name, err := range r.Errors {
			fmt.Fprintf(os.Stderr, "%s 收信失败：%v\n", name, err)
		}
		return ""
	}

	// 列几个发件人，不列主题：通知栏就那么宽，主题多半被截断，
	// 而「谁来的信」才是决定现在要不要去看的那个信息。
	var who []string
	seen := map[string]bool{}
	for _, it := range r.New {
		name := it.Msg.FromName
		if name == "" {
			name = it.Msg.FromAddr
		}
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		if len(who) < 3 {
			who = append(who, name)
		}
	}

	text := fmt.Sprintf("%s 收信：%d 封新的", slot, len(r.New))
	if len(who) > 0 {
		text += "，来自 " + strings.Join(who, "、")
		if len(seen) > len(who) {
			text += " 等"
		}
	}
	return text
}
