#!/bin/sh
# 从 ios/brand 下的原图生成资源目录里的图标和头像。
#
# 原图收在仓库里而不是引用 ~/Downloads——那个目录会自动整理，文件被挪走之后
# 这里就静默失败，图标还是老样子，查半天才发现是源文件没了。
set -e
cd "$(dirname "$0")/.."

ICON=Assets.xcassets/AppIcon.appiconset/icon-1024.png
AVATAR=Assets.xcassets/Avatar.imageset/avatar.png

# logo 和头像是两张不同的图，别互相覆盖：
# logo 是圆形构图，整张缩一下就是应用图标；
# 头像是满幅的，要按头部取景裁——圆形裁剪居中切下去只剩下巴和胸口。
sips -z 1024 1024 brand/logo.png --out "$ICON" >/dev/null
swift brand/crop.swift brand/avatar.png "$AVATAR" 0.5 0.37 0.74 512

printf '图标 %s\n头像 %s\n' \
  "$(sips -g pixelWidth -g hasAlpha "$ICON" | tail -2 | tr -d '\n ')" \
  "$(sips -g pixelWidth "$AVATAR" | tail -1 | tr -d ' ')"
