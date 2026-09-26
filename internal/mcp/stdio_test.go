package mcp

import (
	"context"
	"strings"
	"testing"
	"time"
)

// 命令根本不存在时要说得清。
func TestSpawnMissingCommand(t *testing.T) {
	_, err := Spawn(context.Background(), "jarvis-没有这个命令", nil, nil)
	if err == nil {
		t.Fatal("该起不来")
	}
	if !strings.Contains(err.Error(), "起不来") ||
		!strings.Contains(err.Error(), "jarvis-没有这个命令") {
		t.Errorf("错误里没说是哪个命令起不来：%v", err)
	}
}

// **server 起来就退，而原因只在 stderr 上。**
//
// 这是配 MCP 时最常见的失败（缺环境变量、node 版本不对），而 stdout 上只有一个
// EOF——一片安静。原因不接进错误里的话，人看到的就是「连不上」三个字。
func TestSpawnSurfacesStderr(t *testing.T) {
	tr, err := Spawn(context.Background(), "sh",
		[]string{"-c", "echo 'MCP_TOKEN 没设' >&2; exit 1"}, nil)
	if err != nil {
		t.Fatalf("sh 都起不来：%v", err)
	}
	defer tr.Close()

	// 等它退，然后读——EOF 那一下要把 stderr 带出来。
	deadline := time.Now().Add(3 * time.Second)
	var readErr error
	for time.Now().Before(deadline) {
		buf := make([]byte, 64)
		if _, readErr = tr.Read(buf); readErr != nil {
			break
		}
	}
	if readErr == nil {
		t.Fatal("没读到错误")
	}
	if !strings.Contains(readErr.Error(), "MCP_TOKEN 没设") {
		t.Errorf("stderr 没接进错误里，人就只能看到「连不上」：%v", readErr)
	}
	if !strings.Contains(tr.Stderr(), "MCP_TOKEN 没设") {
		t.Errorf("Stderr() 里也该有：%q", tr.Stderr())
	}
}

// 环境变量是**叠加**在当前环境之上的，不是替换。
//
// 完全不继承的话连 PATH 都没有，连 node 都找不到——而多数 server 是 npx 起的。
func TestSpawnInheritsEnv(t *testing.T) {
	tr, err := Spawn(context.Background(), "sh",
		[]string{"-c", `test -n "$PATH" && echo ok || echo 没有PATH; echo "$JARVIS_TEST_VAR"`},
		[]string{"JARVIS_TEST_VAR=叠加进来了"})
	if err != nil {
		t.Fatalf("起不来：%v", err)
	}
	defer tr.Close()

	buf := make([]byte, 256)
	n, _ := tr.Read(buf)
	got := string(buf[:n])
	if !strings.Contains(got, "ok") {
		t.Errorf("没继承当前环境（PATH 丢了，npx 那类 server 会起不来）：%q", got)
	}
	if !strings.Contains(got, "叠加进来了") {
		t.Errorf("额外的环境变量没进去：%q", got)
	}
}

// 只留最后一段：server 可能一直往 stderr 打日志，无上限地攒着就是内存漏，
// 而排查要看的几乎总是崩在哪儿，不是启动横幅。
func TestTailBufKeepsTail(t *testing.T) {
	b := &tailBuf{limit: 16}
	b.Write([]byte(strings.Repeat("旧", 100)))
	b.Write([]byte("最后这句要紧"))
	got := b.String()
	if len(got) > 16+8 {
		t.Errorf("没截住：%d 字节", len(got))
	}
	if !strings.Contains(got, "要紧") {
		t.Errorf("留的是开头而不是结尾：%q", got)
	}
}
