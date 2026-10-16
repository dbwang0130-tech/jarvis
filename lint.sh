#!/bin/sh
# 代码质量检查：go vet + 可选的 deadcode。
set -e
cd "$(dirname "$0")"

echo '==> go vet'
go vet ./...

DEAD=$(go env GOPATH)/bin/deadcode
if [ ! -x "$DEAD" ]; then
  printf '\n没装 deadcode，跳过死代码检查。安装：\n  go install golang.org/x/tools/cmd/deadcode@latest\n'
  exit 0
fi

echo '==> 死代码'
OUT=$(mktemp)
trap 'rm -f "$OUT"' EXIT
"$DEAD" -test ./... >"$OUT" 2>&1 || true
if [ -s "$OUT" ]; then
  cat "$OUT"
  exit 1
fi
echo '没有死代码'
