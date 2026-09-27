#!/bin/sh
# 把刚编出来的 Mac 版签上临时签名并打开。
#
# 临时签名（`-` 即 ad-hoc）是必须的：没有任何签名的 app，macOS 直接拒绝启动，
# 表现是双击没反应、日志里一句 `code signature invalid`，很难猜。
set -e
cd "$(dirname "$0")/.."

APP=$(find "$HOME/Library/Developer/Xcode/DerivedData" -maxdepth 4 \
	-name Jarvis.app -path '*Debug-maccatalyst*' 2>/dev/null | head -1)
[ -n "$APP" ] || { echo "没找到 Mac 版产物，先 make mac-build"; exit 1; }

codesign --force --deep --sign - "$APP" >/dev/null 2>&1

# **先杀掉正在跑的那个。** `open` 对已经在运行的 app 只是把它切到前台，
# 不会换成刚编出来的二进制——表现是「改了代码、重新编了、界面一点没变」，
# 而这恰恰会让人去怀疑自己的改动，最难查（踩过）。
osascript -e 'tell application "Jarvis" to quit' >/dev/null 2>&1 || true
pkill -x Jarvis >/dev/null 2>&1 || true
sleep 1

open "$APP"
echo "Mac 版已打开：$APP"
