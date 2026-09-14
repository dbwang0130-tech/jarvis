//go:build diag

package mailbox

import (
	"testing"
	"time"

	"github.com/dabao/jarvis/internal/config"
	"github.com/emersion/go-imap/v2"
)

// 只看 MIME 结构，不打印任何邮件内容。
func TestDiagSnippets(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range cfg.Enabled() {
		c, err := Connect(a)
		if err != nil {
			t.Logf("%s 连接失败: %v", a.Name, err)
			continue
		}
		if _, err := c.c.Select("INBOX", nil).Wait(); err != nil {
			c.Close()
			continue
		}
		res, err := c.c.UIDSearch(&imap.SearchCriteria{
			Since: time.Now().AddDate(0, 0, -a.Days),
		}, nil).Wait()
		if err != nil {
			c.Close()
			continue
		}
		uids := res.AllUIDs()
		if len(uids) > a.Limit {
			uids = uids[len(uids)-a.Limit:]
		}
		msgs, structs, err := c.fetchMeta(uids)
		if err != nil {
			t.Logf("%s fetchMeta 失败: %v", a.Name, err)
			c.Close()
			continue
		}
		c.fetchSnippets(msgs, structs)

		empty := 0
		for _, m := range msgs {
			if m.Snippet != "" {
				continue
			}
			empty++
			bs, ok := structs[m.UID]
			if !ok {
				t.Logf("[%s] UID=%d 没有 BodyStructure", a.Name, m.UID)
				continue
			}
			part, path := pickTextPart(bs)
			if part == nil {
				t.Logf("[%s] UID=%d 顶层=%s 未选出文本段", a.Name, m.UID, bs.MediaType())
				continue
			}
			t.Logf("[%s] UID=%d 顶层=%s 选中=%s 编码=%s 声明大小=%d 路径=%v",
				a.Name, m.UID, bs.MediaType(), part.MediaType(), part.Encoding, part.Size, path)
		}
		t.Logf("=== %s: %d 封中 %d 封无正文 ===", a.Name, len(msgs), empty)
		c.Close()
	}
}
