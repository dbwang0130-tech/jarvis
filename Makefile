# 可执行文件叫 jarvis，装到 ~/.local/bin 之后可以直接敲 jarvis。
BIN    := jarvis
PREFIX ?= $(HOME)/.local

.PHONY: build install uninstall test fmt clean

build:
	go build -ldflags="-s -w" -o $(BIN) .

install: build
	install -d $(PREFIX)/bin
	install -m 755 $(BIN) $(PREFIX)/bin/$(BIN)
	@echo "已安装到 $(PREFIX)/bin/$(BIN)"

uninstall:
	rm -f $(PREFIX)/bin/$(BIN)

test:
	go test ./...

fmt:
	gofmt -l -w .

clean:
	rm -f $(BIN)
