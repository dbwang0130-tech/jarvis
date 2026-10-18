#!/bin/sh
# 代码质量检查：go vet + 可选的 staticcheck、deadcode。
set -e
cd "$(dirname "$0")"

echo '==> go vet'
go vet ./...

BIN=$(go env GOPATH)/bin

if [ -x "$BIN/staticcheck" ]; then
  echo '==> staticcheck'
  "$BIN/staticcheck" ./...
else
  printf '\n没装 staticcheck，跳过。安装：\n  go install honnef.co/go/tools/cmd/staticcheck@latest\n'
fi

if [ ! -x "$BIN/deadcode" ]; then
  printf '\n没装 deadcode，跳过死代码检查。安装：\n  go install golang.org/x/tools/cmd/deadcode@latest\n'
  exit 0
fi

# 不带 -test：只被测试调用的函数也算死代码。internal/eval 是只在评测里跑的
# 测试工具，入口在它自己的 _test.go 里，不算。
echo '==> 死代码'
OUT=$(mktemp)
trap 'rm -f "$OUT"' EXIT
"$BIN/deadcode" ./... 2>&1 | grep -v '^internal/eval/' >"$OUT" || true
if [ -s "$OUT" ]; then
  cat "$OUT"
  exit 1
fi
echo '没有死代码'
