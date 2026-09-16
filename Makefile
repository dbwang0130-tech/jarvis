# 可执行文件叫 jarvis，装到 ~/.local/bin 之后可以直接敲 jarvis。
BIN    := jarvis
PREFIX ?= $(HOME)/.local

.PHONY: build install uninstall install-remind uninstall-remind test fmt clean

build:
	go build -ldflags="-s -w" -o $(BIN) .

install: build
	install -d $(PREFIX)/bin
	install -m 755 $(BIN) $(PREFIX)/bin/$(BIN)
	@echo "已安装到 $(PREFIX)/bin/$(BIN)"

uninstall:
	rm -f $(PREFIX)/bin/$(BIN)

# 安装 launchd 定时任务：每 5 分钟跑一次 jarvis remind 检查日程提醒。
install-remind:
	mkdir -p $(HOME)/Library/LaunchAgents
	cp launchd/com.dabao.jarvis-remind.plist $(HOME)/Library/LaunchAgents/
	launchctl unload $(HOME)/Library/LaunchAgents/com.dabao.jarvis-remind.plist 2>/dev/null || true
	launchctl load $(HOME)/Library/LaunchAgents/com.dabao.jarvis-remind.plist
	@echo "已安装提醒任务（每 5 分钟检查一次），可用 make uninstall-remind 移除"

uninstall-remind:
	launchctl unload $(HOME)/Library/LaunchAgents/com.dabao.jarvis-remind.plist 2>/dev/null || true
	rm -f $(HOME)/Library/LaunchAgents/com.dabao.jarvis-remind.plist
	@echo "已移除提醒任务"

test:
	go test ./...

fmt:
	gofmt -l -w .

clean:
	rm -f $(BIN)
