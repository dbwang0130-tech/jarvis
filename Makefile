# 可执行文件叫 jarvis，装到 ~/.local/bin 之后可以直接敲 jarvis。
BIN    := jarvis
PREFIX ?= $(HOME)/.local

.PHONY: build install uninstall install-remind uninstall-remind test fmt clean ios-framework ios-contract

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

# 把 Go 侧编译成 iOS 用的 framework。需要先装 Xcode，
# 第一次还要 go install golang.org/x/mobile/cmd/gomobile@latest && gomobile init。
ios-framework:
	@command -v gomobile >/dev/null || { \
		echo "先装 gomobile："; \
		echo "  go install golang.org/x/mobile/cmd/gomobile@latest"; \
		echo "  gomobile init"; \
		exit 1; }
	gomobile bind -target=ios -o ios/Mobile.xcframework ./mobile
	@echo "生成好了：ios/Mobile.xcframework，拖进 Xcode 项目即可"

# 验两端的 JSON 契约：Go 真实出参能不能被 iOS 真正在用的 Models.swift 解开。
# 只要 swiftc（命令行工具就有），不需要完整 Xcode。
ios-contract:
	@command -v swiftc >/dev/null || { echo "没有 swiftc，先 xcode-select --install"; exit 1; }
	@tmp=$$(mktemp -d) && \
	go run ./ios/contract $$tmp && \
	swiftc -swift-version 5 ios/Models.swift ios/contract/main.swift -o $$tmp/check && \
	$$tmp/check $$tmp; rc=$$?; rm -rf $$tmp; exit $$rc

clean:
	rm -f $(BIN)
	rm -rf ios/Mobile.xcframework
