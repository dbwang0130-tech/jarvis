package mailbox

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"github.com/dabao/jarvis/internal/config"
)

// Draft 是一封待发送的邮件。它先被完整展示给用户，确认后才会发出去。
type Draft struct {
	ID      string // 会话内引用用的短 id
	Account string
	To      []string
	Cc      []string
	Subject string
	Body    string

	// 回信时带上，保证邮件客户端能把它挂回原来的会话。
	InReplyTo  string
	References string

	// 这封草稿是在回哪封信，仅用于展示。
	ReplyToSubject string
}

// Recipients 汇总所有实际收件地址。
func (d Draft) Recipients() []string {
	return append(append([]string{}, d.To...), d.Cc...)
}

// Validate 在发送前做基本检查，避免把空信或地址错误的信发出去。
func (d Draft) Validate() error {
	if len(d.To) == 0 {
		return fmt.Errorf("草稿没有收件人")
	}
	for _, addr := range d.Recipients() {
		if _, err := mail.ParseAddress(addr); err != nil {
			return fmt.Errorf("收件地址 %q 不合法: %w", addr, err)
		}
	}
	if strings.TrimSpace(d.Subject) == "" {
		return fmt.Errorf("草稿没有主题")
	}
	if strings.TrimSpace(d.Body) == "" {
		return fmt.Errorf("草稿正文是空的")
	}
	return nil
}

// Build 生成符合 RFC 5322 的邮件字节流。
// 主题按 RFC 2047 编码，正文用 quoted-printable，中文不会乱码。
func (d Draft) Build(a config.MailAccount) ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	from := mail.Address{Name: a.From, Address: a.User}

	var h bytes.Buffer
	fmt.Fprintf(&h, "From: %s\r\n", from.String())
	fmt.Fprintf(&h, "To: %s\r\n", strings.Join(d.To, ", "))
	if len(d.Cc) > 0 {
		fmt.Fprintf(&h, "Cc: %s\r\n", strings.Join(d.Cc, ", "))
	}
	fmt.Fprintf(&h, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", d.Subject))
	fmt.Fprintf(&h, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	fmt.Fprintf(&h, "Message-ID: <%d.%s@%s>\r\n", time.Now().UnixNano(), d.ID, hostOf(a.User))
	if d.InReplyTo != "" {
		fmt.Fprintf(&h, "In-Reply-To: %s\r\n", d.InReplyTo)
		refs := d.References
		if refs == "" {
			refs = d.InReplyTo
		}
		fmt.Fprintf(&h, "References: %s\r\n", refs)
	}
	h.WriteString("MIME-Version: 1.0\r\n")
	h.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	h.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
	h.WriteString("\r\n")

	body := strings.ReplaceAll(d.Body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\n", "\r\n")
	qp := quotedprintable.NewWriter(&h)
	if _, err := qp.Write([]byte(body)); err != nil {
		return nil, err
	}
	if err := qp.Close(); err != nil {
		return nil, err
	}
	return h.Bytes(), nil
}

// Send 通过 SMTP 发出草稿。465 走隐式 TLS，其余端口走 STARTTLS。
func Send(a config.MailAccount, d Draft) error {
	raw, err := d.Build(a)
	if err != nil {
		return err
	}
	pw, err := a.Secret()
	if err != nil {
		return err
	}

	c, err := dialSMTP(a)
	if err != nil {
		return err
	}
	defer c.Close()

	auth := smtp.PlainAuth("", a.SMTPUser, pw, a.SMTPHost)
	if err := c.Auth(auth); err != nil {
		return fmt.Errorf("SMTP 认证失败（%s 通常要用授权码）: %w", a.Name, err)
	}
	if err := c.Mail(a.User); err != nil {
		return fmt.Errorf("设置发件人失败: %w", err)
	}
	for _, to := range d.Recipients() {
		addr, err := mail.ParseAddress(to)
		if err != nil {
			return err
		}
		if err := c.Rcpt(addr.Address); err != nil {
			return fmt.Errorf("服务器拒收 %s: %w", addr.Address, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(raw); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("提交邮件正文失败: %w", err)
	}
	return c.Quit()
}

func dialSMTP(a config.MailAccount) (*smtp.Client, error) {
	tlsCfg := &tls.Config{ServerName: a.SMTPHost, MinVersion: tls.VersionTLS12}

	if a.SMTPPort == 465 {
		conn, err := tls.Dial("tcp", a.SMTPAddr(), tlsCfg)
		if err != nil {
			return nil, fmt.Errorf("连接 %s 失败: %w", a.SMTPAddr(), err)
		}
		c, err := smtp.NewClient(conn, a.SMTPHost)
		if err != nil {
			conn.Close()
			return nil, err
		}
		return c, nil
	}

	c, err := smtp.Dial(a.SMTPAddr())
	if err != nil {
		return nil, fmt.Errorf("连接 %s 失败: %w", a.SMTPAddr(), err)
	}
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(tlsCfg); err != nil {
			c.Close()
			return nil, fmt.Errorf("STARTTLS 失败: %w", err)
		}
	}
	return c, nil
}

func hostOf(addr string) string {
	if i := strings.LastIndexByte(addr, '@'); i >= 0 {
		return addr[i+1:]
	}
	return "localhost"
}
