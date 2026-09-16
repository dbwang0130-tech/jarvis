// Package browser 通过 Chrome DevTools Protocol 驱动 Chrome，让秘书完成常规网页自动化。
//
// 连接策略：优先连接已运行、带调试端口的 Chrome；连不上时自动启动一个
// 独立配置目录的 Chrome（不和你日常的 Chrome 冲突）。
package browser

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/chromedp/chromedp"
)

// Client 是连到 Chrome DevTools 的会话，按需建立并复用。
type Client struct {
	Endpoint   string
	ProfileDir string

	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
}

func (c *Client) connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ctx != nil {
		return nil
	}

	// 1) 优先连接已运行的调试 Chrome。
	if err := c.connectRemote(); err == nil {
		return nil
	}
	// 2) 连不上就自动启动一个。
	if err := c.launchAndConnect(); err != nil {
		return fmt.Errorf(
			"无法连接或启动 Chrome（端点 %s）。请确认已安装 Google Chrome：%w",
			c.Endpoint, err)
	}
	return nil
}

func (c *Client) connectRemote() error {
	allocCtx, cancelAlloc := chromedp.NewRemoteAllocator(context.Background(), c.Endpoint)
	ctx, cancelCtx := chromedp.NewContext(allocCtx)
	if err := chromedp.Run(ctx); err != nil {
		cancelCtx()
		cancelAlloc()
		return err
	}
	c.ctx = ctx
	c.cancel = func() { cancelCtx(); cancelAlloc() }
	return nil
}

func (c *Client) launchAndConnect() error {
	port := endpointPort(c.Endpoint)
	opts := []chromedp.ExecAllocatorOption{
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("remote-debugging-port", port),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("no-default-browser-check", true),
		chromedp.Flag("disable-background-networking", true),
		chromedp.Flag("disable-default-apps", true),
		chromedp.Flag("disable-sync", true),
	}
	if c.ProfileDir != "" {
		opts = append(opts, chromedp.UserDataDir(c.ProfileDir))
	}
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, cancelCtx := chromedp.NewContext(allocCtx)
	if err := chromedp.Run(ctx); err != nil {
		cancelCtx()
		cancelAlloc()
		return err
	}
	c.ctx = ctx
	c.cancel = func() { cancelCtx(); cancelAlloc() }
	return nil
}

func (c *Client) run(actions ...chromedp.Action) error {
	if err := c.connect(); err != nil {
		return err
	}
	if err := chromedp.Run(c.ctx, actions...); err != nil {
		return fmt.Errorf("浏览器操作失败: %w", err)
	}
	return nil
}

// Close 断开连接。
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
	c.ctx = nil
	c.cancel = nil
}

// endpointPort 从 ws://127.0.0.1:9222 取出端口号。
func endpointPort(endpoint string) string {
	if i := strings.LastIndex(endpoint, ":"); i >= 0 {
		return endpoint[i+1:]
	}
	return "9222"
}
