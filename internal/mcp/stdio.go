package mcp

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// closeGrace 是关 stdin 之后等它自己退多久。
const closeGrace = 2 * time.Second

// timeAfter 收在一处，测试要缩短它。
var timeAfter = time.After

// Stdio 起一个子进程，拿它的 stdin/stdout 当管子。
//
// **stderr 不丢掉，攒在内存里**：一个 server 起不来的时候，唯一说得清原因的东西
// 就在那儿（「找不到 node」「缺环境变量」），而 stdout 上只会是一片安静。
// 攒的量有上限——server 可能一直往 stderr 打日志，无上限地留着就是内存漏。
type Stdio struct {
	cmd  *exec.Cmd
	in   io.WriteCloser
	out  io.ReadCloser
	errs *tailBuf

	once sync.Once
}

// Spawn 起一个 server 进程。
//
// env 是**额外**的环境变量（`KEY=值`），在当前环境之上叠加——多数 server 要靠
// 环境变量拿密钥，而完全不继承当前环境的话连 PATH 都没有，连 node 都找不到。
func Spawn(ctx context.Context, command string, args, env []string) (*Stdio, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Env = append(os.Environ(), env...)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	errs := &tailBuf{limit: 4 << 10}
	cmd.Stderr = errs
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("起不来（%s）：%w", command, err)
	}
	return &Stdio{cmd: cmd, in: in, out: out, errs: errs}, nil
}

func (s *Stdio) Read(p []byte) (int, error) {
	n, err := s.out.Read(p)
	if err != nil && err != io.EOF {
		return n, err
	}
	if err == io.EOF {
		// **把 stderr 接到错误里。** server 悄悄退了的话，stdout 上只有 EOF，
		// 而人要看的原因全在 stderr。
		if msg := s.errs.String(); msg != "" {
			return n, fmt.Errorf("server 退出了，它说：%s", msg)
		}
	}
	return n, err
}

func (s *Stdio) Write(p []byte) (int, error) { return s.in.Write(p) }

// Close 收掉进程。
//
// 先关 stdin——按协议这是「你可以退了」的信号，多数 server 会自己干净地退出；
// 等一会儿还不退才杀。直接 Kill 的话，有状态的 server（正在写文件的那种）可能
// 留下半截东西。
func (s *Stdio) Close() error {
	var err error
	s.once.Do(func() {
		_ = s.in.Close()
		done := make(chan struct{})
		go func() { _, _ = s.cmd.Process.Wait(); close(done) }()
		select {
		case <-done:
		case <-timeAfter(closeGrace):
			_ = s.cmd.Process.Kill()
		}
		_ = s.out.Close()
	})
	return err
}

// Stderr 是 server 打在 stderr 上的最后一段话，排查用。
func (s *Stdio) Stderr() string { return s.errs.String() }

// tailBuf 只留最后 limit 个字节。
//
// server 可能一直往 stderr 打日志，无上限地攒着就是内存漏；而排查要看的几乎总是
// **最后**那几行（崩在哪儿），不是启动横幅。
type tailBuf struct {
	mu    sync.Mutex
	buf   []byte
	limit int
}

func (t *tailBuf) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.limit {
		t.buf = t.buf[len(t.buf)-t.limit:]
	}
	return len(p), nil
}

func (t *tailBuf) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(t.buf))
}
