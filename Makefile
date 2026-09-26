# 可执行文件叫 jarvis，装到 ~/.local/bin 之后可以直接敲 jarvis。
BIN    := jarvis
PREFIX ?= $(HOME)/.local

.PHONY: build install uninstall install-remind uninstall-remind test lint eval fmt clean ios-framework ios-contract ios-check ios-install ios-project ios-assets

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

# 行为评测：**拿真模型跑固定场景，要钱要网**，所以不在 make test 里。
#
# 别的测试都给模型写好台词（假 RoundTripper 放脚本），于是提示词改一个字都不会
# 有任何测试响——而这个 app 的行为几乎全长在提示里。这个目标是唯一能发现
# 「提示改了、行为退了」的手段。场景和断言见 internal/eval。
# 两组：internal/eval 验 agent 循环和提示（两端共用的部分），
# mobile 验手机那条桥只有它才会坏的东西（技能装配、只回传变过的文件、
# 人设出参的指针语义、扩展技能原文过桥、历史往返）。
eval:
	@JARVIS_EVAL=1 go test ./internal/eval/ ./mobile/ -v -count=1 -timeout 25m

# 代码质量：go vet + 死代码。死代码那步要先装 deadcode，没装就跳过并说明怎么装。
lint:
	@sh lint.sh

fmt:
	gofmt -l -w .

# Go 源码变了就重新生成 framework。做成文件依赖而不是一个手动目标——
# 忘了跑的表现是「改了 Go 代码，装到手机上还是老样子」，一点提示都没有。
GO_SRC := $(shell find . -name '*.go' -not -path './ios/*' -not -name '*_test.go')
GOBIN  := $(shell go env GOPATH)/bin

ios/Mobile.xcframework: $(GO_SRC) go.mod
	@command -v gomobile >/dev/null 2>&1 || test -x $(GOBIN)/gomobile || { \
		echo "先装 gomobile："; \
		echo "  go install golang.org/x/mobile/cmd/gomobile@latest"; \
		echo "  gomobile init"; \
		exit 1; }
	PATH="$(GOBIN):$$PATH" gomobile bind -target=ios -o ios/Mobile.xcframework ./mobile
	@echo "framework 已更新"

ios-framework: ios/Mobile.xcframework

# 从 ios/brand 下的原图重新生成图标和头像。换图之后跑一下。
ios-assets:
	@sh ios/brand/make-assets.sh

# 重新生成 Xcode 工程。加了 .swift 就跑一下——工程里的文件清单是生成的，
# 忘了跑的表现是 cannot find 'X' in scope，看着像代码错其实是文件没进工程。
ios-project:
	@python3 ios/genproject.py

# 装到插着的 iPhone 上。条件不齐会说明白差什么。
# 装之前先验两端契约。这道检查抓的是「装上去一片空白」那一类，
# 几秒钟的事，不值得为了省它去手机上调试。
ios-install: ios/Mobile.xcframework ios-contract
	@sh ios/install-device.sh

# 拿真正的 iOS SDK 类型检查 Swift 代码。macOS 上编能过不代表 iOS 上能过——
# 不少 SwiftUI 的初始化器只在 iOS 有，或者签名不一样。需要完整 Xcode。
ios-check:
	@xcrun --sdk iphoneos --show-sdk-path >/dev/null 2>&1 || { \
		echo "需要完整 Xcode："; \
		echo "  sudo xcode-select -s /Applications/Xcode.app/Contents/Developer"; \
		exit 1; }
	xcrun -sdk iphoneos swiftc -typecheck \
		-sdk $$(xcrun --sdk iphoneos --show-sdk-path) \
		-target arm64-apple-ios26.0 ios/*.swift
	@echo "Swift 代码在 iOS SDK 下检查通过"

# 验两端的 JSON 契约：Go 真实出参能不能被 iOS 真正在用的 Models.swift 解开。
# 只要 swiftc（命令行工具就有），不需要完整 Xcode。
ios-contract:
	@command -v swiftc >/dev/null || { echo "没有 swiftc，先 xcode-select --install"; exit 1; }
	@tmp=$$(mktemp -d) && \
	go run ./ios/contract $$tmp && \
	swiftc -swift-version 5 ios/Models.swift ios/Keychain.swift \
	       ios/contract/main.swift -o $$tmp/check && \
	$$tmp/check $$tmp; rc=$$?; rm -rf $$tmp; exit $$rc

clean:
	rm -f $(BIN)
	rm -rf ios/Mobile.xcframework
